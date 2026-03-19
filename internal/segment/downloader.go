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
	"twitch-recorder-go/internal/metrics"
	"twitch-recorder-go/internal/sanitize"
)

type SegmentDownloader struct {
	sessionDir  string
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
		log.Errorf("Failed to create channel directory %s: %v", channelDir, err)
	}

	return &SegmentDownloader{
		sessionDir: sessionDir,
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
	sd.totalAdded++
	return true
}

func (sd *SegmentDownloader) DownloadQueuedSegments(ctx context.Context, concurrency int) {
	sd.mu.Lock()
	segments := make([]string, len(sd.segments))
	copy(segments, sd.segments)
	sd.segments = make([]string, 0)
	sd.mu.Unlock()

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, concurrency)

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

			if err := sd.DownloadSegment(ctx, segmentURL); err != nil {
				log.Errorf("Failed to download segment: %v", err)
			}
		}(url)
	}

	wg.Wait()
}

func (sd *SegmentDownloader) DownloadSegment(ctx context.Context, url string) error {
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

		filename := sd.getSegmentFilename()
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

		duration := time.Since(startTime)
		if sd.metrics != nil {
			sd.metrics.RecordSegmentDownload(written, duration)
		}

		log.Debugf("Downloaded segment %d/%d (%.2f MB)", sd.downloaded, sd.totalAdded, float64(sd.totalSize)/1024/1024)
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
	log.Debugf("Retrying in %v...", backoff)
	time.Sleep(backoff)
}

func (sd *SegmentDownloader) getSegmentFilename() string {
	sd.counterMu.Lock()
	sd.fileCounter++
	counter := sd.fileCounter
	sd.counterMu.Unlock()

	ext := ".ts"
	if sd.format == "mp4" {
		ext = ".mp4"
	}
	return fmt.Sprintf("%05d%s", counter, ext)
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
