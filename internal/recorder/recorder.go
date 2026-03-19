package recorder

import (
	"context"
	"fmt"
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

type Recorder struct {
	twitchClient  *twitch.Client
	channel       string
	metrics       *metrics.Metrics
	config        *config.Config
	uploadToDrive bool
	uploadWG      sync.WaitGroup
}

func NewRecorder(twitchClient *twitch.Client, channel string, cfg *config.Config, uploadToDrive bool) *Recorder {
	return &Recorder{
		twitchClient:  twitchClient,
		channel:       channel,
		config:        cfg,
		uploadToDrive: uploadToDrive,
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

func (r *Recorder) MonitorChannel(ctx context.Context) error {
	if err := r.checkAndRecord(ctx); err != nil {
		log.Errorf("Error checking channel %s: %v", r.channel, err)
	}

	ticker := time.NewTicker(6 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.checkAndRecord(ctx); err != nil {
				log.Errorf("Error checking channel %s: %v", r.channel, err)
			}
		}
	}
}

func (r *Recorder) checkAndRecord(ctx context.Context) error {
	m3u8URL, err := r.twitchClient.GetLiveM3U8(ctx, r.channel)
	if err != nil {
		log.Errorf("[%s] %v", r.channel, err)
		return nil
	}

	log.Infof("[%s] is LIVE! Starting recording...", r.channel)
	return r.recordStream(ctx, m3u8URL)
}

func (r *Recorder) recordStream(ctx context.Context, m3u8URL string) error {
	startTime := time.Now()

	downloader, sessionDir, streamID, parser, err := r.findOrCreateSession(ctx, m3u8URL, startTime)
	if err != nil {
		log.Errorf("Failed to find or create session: %v", err)
		return err
	}

	if r.metrics != nil {
		r.metrics.RecordRecordingStart()
	}

	streamIDChan := make(chan string, 1)
	go r.getCurrentStreamIDWithRetry(ctx, streamIDChan)

	initSegmentDownloaded := false
	metadataSaved := false

	for {
		select {
		case <-ctx.Done():
			log.Info("Context cancelled, finalizing recording...")
			return r.finalizeRecording(downloader, sessionDir, streamIDChan, startTime)
		case newStreamID := <-streamIDChan:
			if newStreamID != "" && streamID == "" {
				streamID = newStreamID
				log.Infof("Stream ID: %s", streamID)
			}
		default:
		}

		if err := parser.FetchNewSegments(ctx, m3u8URL); err != nil {
			log.Errorf("Error fetching playlist: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if !parser.IsLive() {
			log.Info("Stream ended, finalizing recording...")
			return r.finalizeRecording(downloader, sessionDir, streamIDChan, startTime)
		}

		initURI := downloader.GetInitSegment()
		if initURI != "" && !initSegmentDownloaded {
			log.Info("Downloading init segment...")
			if err := downloader.DownloadSegment(ctx, initURI, 1); err != nil {
				log.Errorf("Failed to download init segment: %v", err)
			} else {
				initSegmentDownloaded = true
			}
		}

		downloader.DownloadQueuedSegments(ctx, 4)

		// Save metadata after every download batch if we have stream_id
		if streamID != "" && !metadataSaved {
			lastSeq := parser.GetLastSeq()
			if err := downloader.SaveSessionMetadataAfterDownload(streamID, m3u8URL, lastSeq); err != nil {
				log.Warnf("Failed to save session metadata: %v", err)
			} else {
				metadataSaved = true
				log.Debugf("Initial metadata saved with stream_id=%s, lastSeq=%d", streamID, lastSeq)
			}
		}

		time.Sleep(2 * time.Second)
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

		time.Sleep(5 * time.Second)
	}
}

func (r *Recorder) getCurrentStreamIDOnce(ctx context.Context) (string, error) {
	streams, err := r.twitchClient.GetStreams(ctx, r.channel)
	if err != nil {
		return "", err
	}

	if len(streams.Data) == 0 {
		return "", fmt.Errorf("channel is not live")
	}

	return streams.Data[0].ID, nil
}

func (r *Recorder) findOrCreateSession(ctx context.Context, m3u8URL string, startTime time.Time) (*segment.SegmentDownloader, string, string, *segment.PlaylistParser, error) {
	incompleteSession, err := segment.FindIncompleteSession(r.config.VodDirectory, r.channel)
	if err != nil {
		return nil, "", "", nil, err
	}

	var streamID string

	if incompleteSession != "" {
		log.Infof("Found incomplete session: %s", incompleteSession)

		downloader := segment.NewSegmentDownloaderFromSession(incompleteSession)
		sessionDir := downloader.GetSessionDir()
		parser := segment.NewPlaylistParser(downloader)

		metadata, err := downloader.LoadSessionMetadata()
		if err != nil {
			log.Warnf("Failed to load session metadata: %v", err)
			return downloader, sessionDir, "", parser, nil
		}

		if metadata != nil {
			// Check if session is too old (more than 1 minute - Twitch only keeps ~30s of segments)
			lastUpdated, parseErr := time.Parse(time.RFC3339, metadata.LastUpdated)
			if parseErr == nil && time.Since(lastUpdated) > time.Minute {
				log.Warnf("Session is too old (%v ago), starting fresh", time.Since(lastUpdated))
				if cleanErr := downloader.CleanupIncompleteSession(); cleanErr != nil {
					log.Warnf("Failed to cleanup old session: %v", cleanErr)
				}
				timestamp := time.Now()
				downloader = segment.NewSegmentDownloader(r.config.VodDirectory, r.channel, timestamp)
				sessionDir = downloader.GetSessionDir()
				parser = segment.NewPlaylistParser(downloader)
				log.Infof("Started new recording session: %s", sessionDir)
				return downloader, sessionDir, "", parser, nil
			}

			streamID = metadata.StreamID
			if metadata.LastSeq > 0 {
				parser.SetLastSeq(metadata.LastSeq)
			}
			if metadata.Format != "" {
				downloader.SetFormat(metadata.Format)
			}
			log.Infof("Resuming session (stream_id: %s, lastSeq: %d)", streamID, metadata.LastSeq)
		}

		return downloader, sessionDir, streamID, parser, nil
	}

	timestamp := time.Now()
	downloader := segment.NewSegmentDownloader(r.config.VodDirectory, r.channel, timestamp)
	sessionDir := downloader.GetSessionDir()
	parser := segment.NewPlaylistParser(downloader)
	log.Infof("Started new recording session: %s", sessionDir)

	return downloader, sessionDir, "", parser, nil
}

func (r *Recorder) finalizeRecording(downloader *segment.SegmentDownloader, sessionDir string, streamIDChan chan string, startTime time.Time) error {
	var streamID string
	select {
	case id := <-streamIDChan:
		if id != "" {
			streamID = id
		}
	default:
	}

	folderName := r.channel
	if streamID != "" {
		folderName = streamID
	} else {
		folderName = filepath.Base(sessionDir)
	}

	outputName := folderName
	outputFile := fmt.Sprintf("%s/%s.mp4", sessionDir, outputName)

	resultChan, cancel := downloader.FinalizeAsync(outputFile)
	defer cancel()

	r.uploadWG.Add(1)

	go func() {
		defer r.uploadWG.Done()

		result := <-resultChan

		if result.Err != nil {
			log.Errorf("Failed to finalize recording for %s: %v", r.channel, result.Err)
			if r.metrics != nil {
				r.metrics.RecordRecordingFailure()
			}
			return
		}

		log.Infof("Recording saved: %s", result.OutputFile)

		duration := time.Since(startTime)
		if r.metrics != nil {
			r.metrics.RecordRecordingComplete(duration)
		}

		fileInfo, err := os.Stat(result.OutputFile)
		var fileSize int64 = 0
		if err == nil {
			fileSize = fileInfo.Size()
		}

		if r.uploadToDrive {
			err := drive.UploadToDrive(r.config, r.channel, folderName, result.OutputFile)
			success := err == nil

			if r.metrics != nil {
				r.metrics.RecordDriveUpload(fileSize, success)
			}

			if err != nil {
				log.Warnf("[%s] Failed to upload to Drive: %v", r.channel, err)
			}
		}

		if r.config.Archive.Enabled && r.config.Archive.Endpoint != "" && r.config.Archive.Key != "" {
			success := api.PostRecording(r.config.Archive.Endpoint, r.config.Archive.Key, r.channel, streamID, result.OutputFile, duration)
			if r.metrics != nil {
				r.metrics.RecordArchiveAPICall(success)
			}
		}
	}()

	return nil
}
