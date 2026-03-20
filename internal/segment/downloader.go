package segment

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"twitch-recorder-go/internal/log"
	"twitch-recorder-go/internal/metrics"
	"twitch-recorder-go/internal/sanitize"
)

type SessionMetadata struct {
	StreamID    string `json:"stream_id"`
	PlaylistURL string `json:"playlist_url"`
	SessionDir  string `json:"session_dir"`
	Channel     string `json:"channel"`
	StartTime   string `json:"start_time"`
	LastUpdated string `json:"last_updated"`
	Format      string `json:"format"`
	FileCounter int    `json:"file_counter"`
	LastSeq     int    `json:"last_seq"`
}

const (
	MetadataFileName = "current_session.json"
)

type SegmentDownloader struct {
	sessionDir  string
	channel     string
	seen        map[string]bool
	segments    []string
	mu          sync.Mutex
	downloaded  int
	totalSize   int64
	metrics     *metrics.Metrics
	format      string // "ts" or "mp4"
	initSegment string
	fileCounter int
	counterMu   sync.Mutex
	totalAdded  int
}

func NewSegmentDownloader(vodDirectory, channel string, timestamp time.Time) *SegmentDownloader {
	safeChannel := sanitize.SanitizeChannelName(channel)
	channelDir := filepath.Join(vodDirectory, safeChannel)
	sessionDir := filepath.Join(channelDir, timestamp.Format("2006-01-02_15-04-05"))

	if err := os.MkdirAll(channelDir, 0755); err != nil {
		log.ErrorfC(channel, "Failed to create channel directory %s: %v", channelDir, err)
	}

	return &SegmentDownloader{
		sessionDir: sessionDir,
		channel:    channel,
		seen:       make(map[string]bool),
		segments:   make([]string, 0),
	}
}

func NewSegmentDownloaderFromSession(sessionDir string) *SegmentDownloader {
	sd := &SegmentDownloader{
		sessionDir: sessionDir,
		seen:       make(map[string]bool),
		segments:   make([]string, 0),
	}

	metadata, err := sd.LoadSessionMetadata()
	if err != nil {
		log.Warnf("Failed to load session metadata: %v", err)
	}

	if metadata != nil {
		sd.channel = metadata.Channel
		sd.fileCounter = metadata.FileCounter
		if metadata.Format != "" {
			sd.format = metadata.Format
		}
		log.InfofC(sd.channel, "Restored session state: fileCounter=%d, lastSeq=%d", sd.fileCounter, metadata.LastSeq)
	} else {
		sd.fileCounter = sd.scanForHighestFileNumber()
		sd.format = sd.DetectFormatFromFiles()
		log.Infof("Scanned session directory: fileCounter=%d, format=%s", sd.fileCounter, sd.format)
	}

	sd.cleanupTempFiles()

	return sd
}

func (sd *SegmentDownloader) AddSegment(url string) bool {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	if sd.seen[url] {
		return false
	}

	sd.seen[url] = true
	sd.segments = append(sd.segments, url)
	sd.totalAdded++
	return true
}

func (sd *SegmentDownloader) DownloadQueuedSegments(ctx context.Context, concurrency int) {
	sd.mu.Lock()
	segments := make([]string, len(sd.segments))
	copy(segments, sd.segments)
	sd.segments = make([]string, 0)
	batchSize := len(segments)
	sd.mu.Unlock()

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, concurrency)
	batchDownloaded := 0
	batchMu := sync.Mutex{}

	for _, url := range segments {
		select {
		case <-ctx.Done():
			return
		default:
		}

		wg.Add(1)
		semaphore <- struct{}{}

		go func(segmentURL string) {
			defer wg.Done()
			defer func() { <-semaphore }()

			if err := sd.DownloadSegment(ctx, segmentURL, batchSize); err != nil {
				log.Errorf("Failed to download segment: %v", err)
			} else {
				batchMu.Lock()
				batchDownloaded++
				batchMu.Unlock()
			}
		}(url)
	}

	wg.Wait()
	if batchSize > 0 {
		log.DebugfC(sd.channel, "Batch complete: downloaded %d/%d segments", batchDownloaded, batchSize)
	}
}

func (sd *SegmentDownloader) DownloadSegment(ctx context.Context, url string, batchSize int) error {
	maxRetries := 5
	var lastErr error
	startTime := time.Now()

	for attempt := 0; attempt < maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := http.Get(url)
		if err != nil {
			lastErr = fmt.Errorf("failed to request segment (attempt %d/%d): %w", attempt+1, maxRetries, err)
			sd.sleepWithBackoff(attempt)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("unexpected status code %d (attempt %d/%d)", resp.StatusCode, attempt+1, maxRetries)
			resp.Body.Close()
			sd.sleepWithBackoff(attempt)
			continue
		}

		filename, fileNum := sd.getSegmentFilenameWithNumber()
		tmpPath := filepath.Join(sd.sessionDir, filename+".tmp")
		finalPath := filepath.Join(sd.sessionDir, filename)

		dir := filepath.Dir(tmpPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			resp.Body.Close()
			return fmt.Errorf("failed to create directory: %w", err)
		}

		out, err := os.Create(tmpPath)
		if err != nil {
			resp.Body.Close()
			return fmt.Errorf("failed to create file: %w", err)
		}

		written, err := io.Copy(out, resp.Body)
		out.Close()
		resp.Body.Close()

		if err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to write segment: %w", err)
		}

		if err := os.Rename(tmpPath, finalPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("failed to rename temp file: %w", err)
		}

		sd.mu.Lock()
		sd.downloaded++
		sd.totalSize += written
		sd.seen[url] = true
		sd.mu.Unlock()

		duration := time.Since(startTime)
		if sd.metrics != nil {
			sd.metrics.RecordSegmentDownload(written, duration)
		}

		log.DebugfC(sd.channel, "Downloaded segment #%d (%.2f MB)", fileNum, float64(written)/1024/1024)
		return nil
	}

	if sd.metrics != nil {
		sd.metrics.RecordSegmentFailure(lastErr.Error())
	}
	return lastErr
}

func (sd *SegmentDownloader) sleepWithBackoff(attempt int) {
	backoff := time.Duration(1<<uint(attempt)) * time.Second
	if backoff > 8*time.Second {
		backoff = 8 * time.Second
	}
	log.DebugfC(sd.channel, "Retrying in %v...", backoff)
	time.Sleep(backoff)
}

func (sd *SegmentDownloader) getSegmentFilenameWithNumber() (string, int) {
	sd.counterMu.Lock()
	sd.fileCounter++
	counter := sd.fileCounter
	sd.counterMu.Unlock()

	ext := ".ts"
	if sd.format == "mp4" {
		ext = ".mp4"
	}
	return fmt.Sprintf("%d%s", counter, ext), counter
}

func (sd *SegmentDownloader) getSegmentFilename() string {
	filename, _ := sd.getSegmentFilenameWithNumber()
	return filename
}

func (sd *SegmentDownloader) GetSessionDir() string {
	return sd.sessionDir
}

func (sd *SegmentDownloader) GetDownloadedCount() int {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	return sd.downloaded
}

func (sd *SegmentDownloader) SetMetrics(m *metrics.Metrics) {
	sd.metrics = m
}

func (sd *SegmentDownloader) SetFormat(format string) {
	sd.format = format
}

func (sd *SegmentDownloader) SetInitSegment(uri string) {
	sd.initSegment = uri
}

func (sd *SegmentDownloader) GetFormat() string {
	return sd.format
}

func (sd *SegmentDownloader) GetInitSegment() string {
	return sd.initSegment
}

func (sd *SegmentDownloader) CleanupOnError() {
	if err := os.RemoveAll(sd.sessionDir); err != nil {
		log.Errorf("Failed to cleanup session dir: %v", err)
	}
}

func (sd *SegmentDownloader) GetChannelDir() string {
	return filepath.Dir(sd.sessionDir)
}

func (sd *SegmentDownloader) SaveSessionMetadata(streamID, playlistURL string) error {
	channelDir := sd.GetChannelDir()
	if err := os.MkdirAll(channelDir, 0755); err != nil {
		return fmt.Errorf("failed to create channel directory: %w", err)
	}

	metadata := SessionMetadata{
		StreamID:    streamID,
		PlaylistURL: playlistURL,
		SessionDir:  sd.sessionDir,
		Channel:     filepath.Base(channelDir),
		StartTime:   time.Now().Format(time.RFC3339),
		LastUpdated: time.Now().Format(time.RFC3339),
		Format:      sd.format,
	}

	metadataPath := filepath.Join(channelDir, MetadataFileName)
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	log.DebugfC(sd.channel, "Saved session metadata to %s", metadataPath)
	return nil
}

func (sd *SegmentDownloader) LoadSessionMetadata() (*SessionMetadata, error) {
	channelDir := sd.GetChannelDir()
	metadataPath := filepath.Join(channelDir, MetadataFileName)

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var metadata SessionMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	log.DebugfC(sd.channel, "Loaded session metadata from %s", metadataPath)
	return &metadata, nil
}

func (sd *SegmentDownloader) DeleteSessionMetadata() error {
	channelDir := sd.GetChannelDir()
	metadataPath := filepath.Join(channelDir, MetadataFileName)

	if err := os.Remove(metadataPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete metadata: %w", err)
	}

	log.DebugfC(sd.channel, "Deleted session metadata from %s", metadataPath)
	return nil
}

func (sd *SegmentDownloader) DetectFormatFromFiles() string {
	tsFiles, _ := filepath.Glob(filepath.Join(sd.sessionDir, "*.ts"))
	mp4Files, _ := filepath.Glob(filepath.Join(sd.sessionDir, "*.mp4"))

	if len(mp4Files) > len(tsFiles) {
		return "mp4"
	}
	return "ts"
}

func (sd *SegmentDownloader) ClearSeenMap() {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	sd.seen = make(map[string]bool)
}

func (sd *SegmentDownloader) DeleteSessionDir() error {
	if err := os.RemoveAll(sd.sessionDir); err != nil {
		return fmt.Errorf("failed to delete session directory: %w", err)
	}

	log.Warnf("Deleted session directory: %s", sd.sessionDir)
	return nil
}

func (sd *SegmentDownloader) CleanupIncompleteSession() error {
	if err := sd.DeleteSessionMetadata(); err != nil {
		log.Warnf("Failed to delete metadata: %v", err)
	}

	if err := sd.DeleteSessionDir(); err != nil {
		log.Warnf("Failed to delete session dir: %v", err)
	}

	return nil
}

func (sd *SegmentDownloader) scanForHighestFileNumber() int {
	tsFiles, _ := filepath.Glob(filepath.Join(sd.sessionDir, "*.ts"))
	mp4Files, _ := filepath.Glob(filepath.Join(sd.sessionDir, "*.mp4"))

	maxNum := 0
	for _, file := range tsFiles {
		name := filepath.Base(file)
		var num int
		_, err := fmt.Sscanf(name, "%d.ts", &num)
		if err == nil && num > maxNum {
			maxNum = num
		}
	}

	for _, file := range mp4Files {
		name := filepath.Base(file)
		var num int
		_, err := fmt.Sscanf(name, "%d.mp4", &num)
		if err == nil && num > maxNum {
			maxNum = num
		}
	}

	return maxNum
}

func (sd *SegmentDownloader) cleanupTempFiles() error {
	tsTmpFiles, _ := filepath.Glob(filepath.Join(sd.sessionDir, "*.ts.tmp"))
	mp4TmpFiles, _ := filepath.Glob(filepath.Join(sd.sessionDir, "*.mp4.tmp"))

	for _, file := range tsTmpFiles {
		if err := os.Remove(file); err != nil {
			log.Warnf("Failed to remove temp file %s: %v", file, err)
		}
	}

	for _, file := range mp4TmpFiles {
		if err := os.Remove(file); err != nil {
			log.Warnf("Failed to remove temp file %s: %v", file, err)
		}
	}

	return nil
}

func (sd *SegmentDownloader) SaveSessionMetadataAfterDownload(streamID, playlistURL string, lastSeq int) error {
	sd.mu.Lock()
	fileCounter := sd.fileCounter
	format := sd.format
	sd.mu.Unlock()

	log.DebugfC(sd.channel, "Saving metadata: fileCounter=%d, lastSeq=%d", fileCounter, lastSeq)

	channelDir := sd.GetChannelDir()
	if err := os.MkdirAll(channelDir, 0755); err != nil {
		return fmt.Errorf("failed to create channel directory: %w", err)
	}

	metadata := SessionMetadata{
		StreamID:    streamID,
		PlaylistURL: playlistURL,
		SessionDir:  sd.sessionDir,
		Channel:     filepath.Base(channelDir),
		StartTime:   time.Now().Format(time.RFC3339),
		LastUpdated: time.Now().Format(time.RFC3339),
		Format:      format,
		FileCounter: fileCounter,
		LastSeq:     lastSeq,
	}

	metadataPath := filepath.Join(channelDir, MetadataFileName)
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	log.DebugfC(sd.channel, "Saved session metadata after download")
	return nil
}

type FinalizeResult struct {
	OutputFile string
	Err        error
}

// FinalizeAsync starts finalization in a goroutine and returns a channel for the result
func (sd *SegmentDownloader) FinalizeAsync(outputFile string) (<-chan FinalizeResult, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	resultChan := make(chan FinalizeResult, 1)

	go func() {
		defer close(resultChan)

		select {
		case <-ctx.Done():
			resultChan <- FinalizeResult{OutputFile: outputFile, Err: ctx.Err()}
			return
		default:
		}

		result := FinalizeResult{OutputFile: outputFile}
		if err := sd.finalizeInternal(outputFile); err != nil {
			result.Err = err
		}
		resultChan <- result
	}()

	return resultChan, cancel
}

// Keep synchronous Finalize for backward compatibility
func (sd *SegmentDownloader) Finalize(outputFile string) error {
	return sd.finalizeInternal(outputFile)
}
