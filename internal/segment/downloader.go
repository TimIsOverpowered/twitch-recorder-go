package segment

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"twitch-recorder-go/internal/log"
)

type SegmentDownloader struct {
	sessionDir string
	seen       map[string]bool
	segments   []string
	mu         sync.Mutex
	downloaded int
	totalSize  int64
}

func NewSegmentDownloader(channel string, timestamp time.Time) *SegmentDownloader {
	dir := fmt.Sprintf("%s_%s", channel, timestamp.Format("2006-01-02_15-04-05"))
	return &SegmentDownloader{
		sessionDir: dir,
		seen:       make(map[string]bool),
		segments:   make([]string, 0),
	}
}

func (sd *SegmentDownloader) AddSegment(url string) bool {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	if sd.seen[url] {
		return false
	}

	sd.seen[url] = true
	sd.segments = append(sd.segments, url)
	return true
}

func (sd *SegmentDownloader) DownloadSegment(ctx context.Context, url string) error {
	maxRetries := 5
	var lastErr error

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

		filename := sd.getSegmentFilename(url)
		path := filepath.Join(sd.sessionDir, filename)

		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0755); err != nil {
			resp.Body.Close()
			return fmt.Errorf("failed to create directory: %w", err)
		}

		out, err := os.Create(path)
		if err != nil {
			resp.Body.Close()
			return fmt.Errorf("failed to create file: %w", err)
		}

		written, err := io.Copy(out, resp.Body)
		out.Close()
		resp.Body.Close()

		if err != nil {
			os.Remove(path)
			return fmt.Errorf("failed to write segment: %w", err)
		}

		sd.mu.Lock()
		sd.downloaded++
		sd.totalSize += written
		sd.mu.Unlock()

		log.Debug("Downloaded segment %d/%d (%.2f MB)", sd.downloaded, len(sd.segments), float64(sd.totalSize)/1024/1024)
		return nil
	}

	return lastErr
}

func (sd *SegmentDownloader) sleepWithBackoff(attempt int) {
	backoff := time.Duration(1<<uint(attempt)) * time.Second
	if backoff > 8*time.Second {
		backoff = 8 * time.Second
	}
	log.Debug("Retrying in %v...", backoff)
	time.Sleep(backoff)
}

func (sd *SegmentDownloader) getSegmentFilename(url string) string {
	hash := 0
	for _, c := range url {
		hash = hash*31 + int(c)
	}
	return fmt.Sprintf("%05d.ts", abs(hash)%100000)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (sd *SegmentDownloader) GetSessionDir() string {
	return sd.sessionDir
}

func (sd *SegmentDownloader) GetDownloadedCount() int {
	sd.mu.Lock()
	defer sd.mu.Unlock()
	return sd.downloaded
}

func (sd *SegmentDownloader) CleanupOnError() {
	if err := os.RemoveAll(sd.sessionDir); err != nil {
		log.Error("Failed to cleanup session dir: %v", err)
	}
}
