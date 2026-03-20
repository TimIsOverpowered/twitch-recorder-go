package recorder

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"twitch-recorder-go/internal/api"
	"twitch-recorder-go/internal/config"
	"twitch-recorder-go/internal/drive"
	"twitch-recorder-go/internal/log"
	"twitch-recorder-go/internal/metrics"
	"twitch-recorder-go/internal/segment"
	"twitch-recorder-go/internal/twitch"
)

const (
	CheckTickerInterval = 6 * time.Second
	StreamCheckTimeout  = 30 * time.Second
	FinalizeTimeout     = 5 * time.Minute
	MaxStreamFailures   = 3
	RetryDelay          = 2 * time.Second
	DownloadConcurrency = 4
)

var (
	ErrInvalidUser   = errors.New("user is invalid or does not exist")
	ErrTestFinalized = errors.New("test finalization completed")
)

type Recorder struct {
	twitchClient    *twitch.Client
	channel         string
	metrics         *metrics.Metrics
	config          *config.Config
	uploadToDrive   bool
	uploadWG        sync.WaitGroup
	failureCount    int
	maxFailures     int
	finalizeCancels []context.CancelFunc
	mu              sync.Mutex
	finalizeMu      sync.Mutex
}

func NewRecorder(twitchClient *twitch.Client, channel string, cfg *config.Config, uploadToDrive bool) *Recorder {
	return &Recorder{
		twitchClient:  twitchClient,
		channel:       channel,
		config:        cfg,
		uploadToDrive: uploadToDrive,
		maxFailures:   MaxStreamFailures,
	}
}

func (r *Recorder) WaitForUploads(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		r.uploadWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func (r *Recorder) SetMetrics(m *metrics.Metrics) {
	r.metrics = m
}

func (r *Recorder) Shutdown() {
	r.finalizeMu.Lock()
	defer r.finalizeMu.Unlock()
	for _, cancel := range r.finalizeCancels {
		cancel()
	}
	r.finalizeCancels = nil
}

func (r *Recorder) MonitorChannel(ctx context.Context) error {
	if err := r.checkAndRecord(ctx); err != nil {
		if err == ErrInvalidUser || err == ErrTestFinalized {
			return err
		}
		log.Errorf("Error checking channel %s: %v", r.channel, err)
	}

	ticker := time.NewTicker(CheckTickerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.checkAndRecord(ctx); err != nil {
				if err == ErrInvalidUser || err == ErrTestFinalized {
					return err
				}
				log.Errorf("Error checking channel %s: %v", r.channel, err)
			}
		}
	}
}

func (r *Recorder) checkAndRecord(ctx context.Context) error {
	m3u8URL, err := r.twitchClient.GetLiveM3U8(ctx, r.channel)
	if err != nil {
		if errors.Is(err, twitch.ErrInvalidUser) {
			r.mu.Lock()
			r.failureCount++
			count := r.failureCount
			r.mu.Unlock()

			if count >= r.maxFailures {
				log.ErrorfC(r.channel, "User may be invalid (failed %d times), stopping monitor", count)
				return ErrInvalidUser
			}

			log.DebugfC(r.channel, "Failed to get token (%d/%d)", count, r.maxFailures)
			return nil
		}

		log.InfofC(r.channel, "%v", err)
		return nil
	}

	r.mu.Lock()
	r.failureCount = 0
	r.mu.Unlock()

	log.InfoC(r.channel, "is LIVE! Starting recording...")
	return r.recordStream(ctx, m3u8URL)
}

func (r *Recorder) recordStream(ctx context.Context, m3u8URL string) error {
	startTime := time.Now()

	downloader, sessionDir, streamID, parser, err := r.findOrCreateSession()
	if err != nil {
		log.ErrorfC(r.channel, "Failed to find or create session: %v", err)
		return err
	}

	if r.metrics != nil {
		r.metrics.RecordRecordingStart()
	}

	streamIDCtx, streamIDCancel := context.WithTimeout(ctx, StreamCheckTimeout)
	defer streamIDCancel()

	streamIDChan := make(chan string, 1)
	go r.getCurrentStreamIDWithRetry(streamIDCtx, streamIDChan)

	var finalizeTimer <-chan time.Time
	if r.config.TestFinalizeAfter > 0 {
		finalizeTimer = time.After(time.Duration(r.config.TestFinalizeAfter) * time.Second)
	}

	initSegmentDownloaded := false

	for {
		select {
		case <-ctx.Done():
			log.InfoC(r.channel, "Context cancelled, finalizing recording...")
			return r.finalizeRecording(downloader, sessionDir, streamID, startTime, false)
		case <-finalizeTimer:
			log.InfofC(r.channel, "[TEST] Forced finalization triggered after %d seconds", r.config.TestFinalizeAfter)
			return r.finalizeRecording(downloader, sessionDir, streamID, startTime, true)
		case newStreamID := <-streamIDChan:
			if newStreamID != "" && streamID == "" {
				streamID = newStreamID
				log.InfofC(r.channel, "Stream ID: %s", streamID)
			}
		default:
		}

		if err := parser.FetchNewSegments(ctx, m3u8URL); err != nil {
			log.ErrorfC(r.channel, "Error fetching playlist: %v", err)
			time.Sleep(RetryDelay)
			continue
		}

		if !parser.IsLive() {
			log.InfoC(r.channel, "Stream ended, finalizing recording...")
			return r.finalizeRecording(downloader, sessionDir, streamID, startTime, false)
		}

		initURI := downloader.GetInitSegment()
		if initURI != "" && !initSegmentDownloaded {
			metadata, _ := downloader.LoadSessionMetadata()
			if metadata != nil && metadata.FileCounter > 0 {
				log.DebugfC(r.channel, "Skipping init segment (already downloaded in previous session)")
				initSegmentDownloaded = true
			} else {
				log.InfoC(r.channel, "Downloading init segment...")

				sessionDir := downloader.GetSessionDir()
				if err := os.MkdirAll(sessionDir, 0755); err != nil {
					log.ErrorfC(r.channel, "Failed to create session directory: %v", err)
				}

				initPath := filepath.Join(sessionDir, "init.mp4")

				resp, err := http.Get(initURI)
				if err != nil {
					log.ErrorfC(r.channel, "Failed to download init segment: %v", err)
				} else {
					out, err := os.Create(initPath)
					if err != nil {
						resp.Body.Close()
						log.ErrorfC(r.channel, "Failed to create init file: %v", err)
					} else {
						defer out.Close()
						defer resp.Body.Close()
						_, err = io.Copy(out, resp.Body)
						if err != nil {
							log.ErrorfC(r.channel, "Failed to write init segment: %v", err)
						} else {
							downloader.SetInitSegment("init.mp4")
							initSegmentDownloaded = true
							log.DebugfC(r.channel, "Downloaded init segment to init.mp4")
						}
					}
				}
			}
		}

		downloader.DownloadQueuedSegments(ctx, DownloadConcurrency)

		if streamID != "" {
			lastSeq := downloader.GetLastDownloadedSeq()
			if lastSeq > 0 {
				if err := downloader.SaveSessionMetadataAfterDownload(streamID, m3u8URL, lastSeq); err != nil {
					log.WarnfC(r.channel, "Failed to save session metadata: %v", err)
				} else {
					log.DebugfC(r.channel, "Saved metadata with stream_id=%s, lastSeq=%d", streamID, lastSeq)
				}
			}
		}

		time.Sleep(RetryDelay)
	}
}

func (r *Recorder) getCurrentStreamIDWithRetry(ctx context.Context, streamIDChan chan<- string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		streams, err := r.twitchClient.GetStreams(ctx, r.channel)
		if err == nil && len(streams.Data) > 0 {
			streamID := streams.Data[0].ID
			select {
			case streamIDChan <- streamID:
				return
			default:
				return
			}
		}

		time.Sleep(RetryDelay)
	}
}

func (r *Recorder) findOrCreateSession() (*segment.SegmentDownloader, string, string, *segment.PlaylistParser, error) {
	incompleteSession, err := segment.FindIncompleteSession(r.config.VodDirectory, r.channel)
	if err != nil {
		return nil, "", "", nil, err
	}

	var streamID string

	if incompleteSession != "" {
		log.InfofC(r.channel, "Found incomplete session: %s", incompleteSession)

		downloader := segment.NewSegmentDownloaderFromSession(incompleteSession)
		sessionDir := downloader.GetSessionDir()
		parser := segment.NewPlaylistParser(downloader)

		metadata, err := downloader.LoadSessionMetadata()
		if err != nil {
			log.WarnfC(r.channel, "Failed to load session metadata: %v", err)
			return downloader, sessionDir, "", parser, nil
		}

		if metadata != nil {
			lastUpdated, parseErr := time.Parse(time.RFC3339, metadata.LastUpdated)
			if parseErr == nil && time.Since(lastUpdated) > time.Minute {
				log.WarnfC(r.channel, "Session is too old (%v ago), starting fresh", time.Since(lastUpdated))
				if cleanErr := downloader.CleanupIncompleteSession(); cleanErr != nil {
					log.WarnfC(r.channel, "Failed to cleanup old session: %v", cleanErr)
				}
				timestamp := time.Now()
				downloader = segment.NewSegmentDownloader(r.config.VodDirectory, r.channel, timestamp)
				sessionDir = downloader.GetSessionDir()
				parser = segment.NewPlaylistParser(downloader)
				log.InfofC(r.channel, "Started new recording session: %s", sessionDir)
				return downloader, sessionDir, "", parser, nil
			}

			streamID = metadata.StreamID

			diskLastSeq := downloader.GetLastDownloadedSeq()
			if metadata.LastSeq > diskLastSeq {
				parser.SetLastSeq(metadata.LastSeq)
				log.InfofC(r.channel, "Using metadata lastSeq=%d", metadata.LastSeq)
			} else {
				parser.SetLastSeq(diskLastSeq)
				log.InfofC(r.channel, "Using disk scan lastSeq=%d (metadata had %d)", diskLastSeq, metadata.LastSeq)
			}

			if metadata.Format != "" {
				downloader.SetFormat(metadata.Format)
			}
		}

		return downloader, sessionDir, streamID, parser, nil
	}

	timestamp := time.Now()
	downloader := segment.NewSegmentDownloader(r.config.VodDirectory, r.channel, timestamp)
	sessionDir := downloader.GetSessionDir()
	parser := segment.NewPlaylistParser(downloader)
	log.InfofC(r.channel, "Started new recording session: %s", sessionDir)

	return downloader, sessionDir, "", parser, nil
}

func (r *Recorder) finalizeRecording(downloader *segment.SegmentDownloader, sessionDir string, streamID string, startTime time.Time, isTest bool) error {
	folderName := streamID
	if folderName == "" {
		folderName = r.channel
	}
	if streamID == "" && folderName == r.channel {
		folderName = filepath.Base(sessionDir)
	}

	outputFile := fmt.Sprintf("%s/%s.mp4", sessionDir, folderName)

	_, cancel := context.WithCancel(context.Background())
	r.finalizeMu.Lock()
	r.finalizeCancels = append(r.finalizeCancels, cancel)
	r.finalizeMu.Unlock()

	resultChan, _ := downloader.FinalizeAsync(outputFile)

	r.uploadWG.Add(1)

	go func() {
		defer r.uploadWG.Done()

		result := <-resultChan

		if result.Err != nil {
			log.ErrorfC(r.channel, "Failed to finalize recording: %v", result.Err)
			if r.metrics != nil {
				r.metrics.RecordRecordingFailure()
			}
			return
		}

		log.InfofC(r.channel, "Recording saved: %s", result.OutputFile)

		duration := time.Since(startTime)
		if r.metrics != nil {
			r.metrics.RecordRecordingComplete(duration)
		}

		fileInfo, err := os.Stat(result.OutputFile)
		var fileSize int64 = 0
		if err == nil {
			fileSize = fileInfo.Size()
		}

		if !isTest && r.uploadToDrive {
			err := drive.UploadToDrive(r.config, r.channel, folderName, result.OutputFile)
			success := err == nil

			if r.metrics != nil {
				r.metrics.RecordDriveUpload(fileSize, success)
			}

			if err != nil {
				log.WarnfC(r.channel, "Failed to upload to Drive: %v", err)
			}
		} else if isTest {
			log.DebugfC(r.channel, "[TEST] Skipped Drive upload (test mode)")
		}

		if !isTest && r.config.Archive.Enabled && r.config.Archive.Endpoint != "" && r.config.Archive.Key != "" {
			success := api.PostRecordingWithContext(context.Background(), r.config.Archive.Endpoint, r.config.Archive.Key, r.channel, streamID, result.OutputFile, duration)
			if r.metrics != nil {
				r.metrics.RecordArchiveAPICall(success)
			}
		} else if isTest {
			log.DebugfC(r.channel, "[TEST] Skipped Archive API post (test mode)")
		}
	}()

	if isTest {
		return ErrTestFinalized
	}

	return nil
}
