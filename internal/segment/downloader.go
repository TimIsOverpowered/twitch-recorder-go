package segment

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
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
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to request segment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	filename := sd.getSegmentFilename(url)
	path := filepath.Join(sd.sessionDir, filename)

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		os.Remove(path)
		return fmt.Errorf("failed to write segment: %w", err)
	}

	sd.mu.Lock()
	sd.downloaded++
	sd.totalSize += written
	sd.mu.Unlock()

	log.Printf("Downloaded segment %d/%d (%.2f MB)", sd.downloaded, len(sd.segments), float64(sd.totalSize)/1024/1024)
	return nil
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
		log.Printf("Failed to cleanup session dir: %v", err)
	}
}
