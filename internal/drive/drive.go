package drive

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
	"twitch-recorder-go/internal/config"
	"twitch-recorder-go/internal/log"
)

// ProgressReader wraps io.Reader to track upload progress
type ProgressReader struct {
	Reader     io.Reader
	Total      int64
	Current    int64
	Mutex      sync.Mutex
	OnProgress func(current, total int64)
}

func (pr *ProgressReader) Read(p []byte) (n int, err error) {
	n, err = pr.Reader.Read(p)
	pr.Mutex.Lock()
	pr.Current += int64(n)
	if pr.OnProgress != nil {
		pr.OnProgress(pr.Current, pr.Total)
	}
	pr.Mutex.Unlock()
	return n, err
}

// UploadToDrive uploads a recording file to Google Drive
// Folder structure: channel/streamID/filename.mp4
func UploadToDrive(cfg *config.Config, channel, streamOrTimestamp, localPath string) error {
	if cfg.Drive.RefreshToken == "" || cfg.Google.ClientID == "" {
		return fmt.Errorf("drive credentials not configured")
	}

	ctx := context.Background()

	googleConfig := &oauth2.Config{
		ClientID:     cfg.Google.ClientID,
		ClientSecret: cfg.Google.ClientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL: cfg.Google.Endpoint.TokenURL,
		},
		Scopes: cfg.Google.Scopes,
	}

	tok := &oauth2.Token{
		AccessToken:  cfg.Drive.AccessToken,
		RefreshToken: cfg.Drive.RefreshToken,
		TokenType:    cfg.Drive.TokenType,
	}

	tokenSource := googleConfig.TokenSource(ctx, tok)

	client := &http.Client{
		Transport: &oauth2.Transport{
			Source: tokenSource,
			Base:   http.DefaultTransport,
		},
	}

	srv, err := drive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return fmt.Errorf("failed to create Drive service: %w", err)
	}

	rootFolderID, err := findOrCreateFolder(srv, ctx, channel, "")
	if err != nil {
		return fmt.Errorf("failed to find/create root folder: %w", err)
	}

	streamFolderID, err := findOrCreateFolder(srv, ctx, streamOrTimestamp, rootFolderID)
	if err != nil {
		return fmt.Errorf("failed to find/create stream folder: %w", err)
	}

	fileName := filepath.Base(localPath)
	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	fileInfo, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file info: %w", err)
	}

	mimeType := mime.TypeByExtension(filepath.Ext(fileName))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	log.Infof("[%s] Uploading to Drive... (%.2f MB)", channel, float64(fileInfo.Size())/(1024*1024))

	var lastProgress int64
	progressReader := &ProgressReader{
		Reader: f,
		Total:  fileInfo.Size(),
		OnProgress: func(current, total int64) {
			if current-lastProgress >= 1024*1024 || current == total {
				lastProgress = current
				percent := float64(current) / float64(total) * 100
				log.Infof("[%s] Uploading: %.1f%% (%.2f/%.2f MB)\r", channel, percent, float64(current)/(1024*1024), float64(total)/(1024*1024))
			}
		},
	}

	file := &drive.File{
		Name:     fileName,
		Parents:  []string{streamFolderID},
		MimeType: mimeType,
	}

	uploader := srv.Files.Create(file).Media(progressReader).Context(ctx).Fields("id, name")

	res, err := uploader.Do()
	if err != nil {
		return fmt.Errorf("failed to upload file: %w", err)
	}

	log.Infof("[%s] Uploaded %s to Drive (ID: %s)", channel, res.Name, res.Id)
	return nil
}

func findOrCreateFolder(srv *drive.Service, ctx context.Context, name string, parentID string) (string, error) {
	query := fmt.Sprintf("name='%s' and mimeType='application/vnd.google-apps.folder' and trashed=false", sanitizeQuery(name))
	if parentID != "" {
		query += fmt.Sprintf(" and '%s' in parents", parentID)
	}

	fileList, err := srv.Files.List().Q(query).Fields("files(id, name)").Context(ctx).Do()
	if err != nil {
		return "", fmt.Errorf("failed to list files: %w", err)
	}

	for _, file := range fileList.Files {
		if strings.EqualFold(file.Name, name) {
			log.Debugf("[%s] Found existing folder %s (ID: %s)", name, name, file.Id)
			return file.Id, nil
		}
	}

	log.Debugf("[%s] Creating folder %s", name, name)
	res, err := srv.Files.Create(&drive.File{
		Name:     name,
		MimeType: "application/vnd.google-apps.folder",
		Parents:  []string{parentID},
	}).Context(ctx).Fields("id, name").Do()

	if err != nil {
		return "", fmt.Errorf("failed to create folder: %w", err)
	}

	log.Debugf("[%s] Created folder %s (ID: %s)", name, name, res.Id)
	return res.Id, nil
}

func sanitizeQuery(s string) string {
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}
