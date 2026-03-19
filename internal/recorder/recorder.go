package recorder

import (
	"context"
	"fmt"
	"time"

	"twitch-recorder-go/internal/api"
	"twitch-recorder-go/internal/config"
	"twitch-recorder-go/internal/log"
	"twitch-recorder-go/internal/metrics"
	"twitch-recorder-go/internal/segment"
	"twitch-recorder-go/internal/twitch"
)

type Recorder struct {
	twitchClient *twitch.Client
	channel      string
	metrics      *metrics.Metrics
	config       *config.Config
}

func NewRecorder(twitchClient *twitch.Client, channel string, cfg *config.Config) *Recorder {
	return &Recorder{
		twitchClient: twitchClient,
		channel:      channel,
		config:       cfg,
	}
}

func (r *Recorder) SetMetrics(m *metrics.Metrics) {
	r.metrics = m
}

func (r *Recorder) MonitorChannel(ctx context.Context) error {
	ticker := time.NewTicker(6 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.checkAndRecord(ctx); err != nil {
				log.Error("Error checking channel %s: %v", r.channel, err)
			}
		}
	}
}

func (r *Recorder) checkAndRecord(ctx context.Context) error {
	m3u8URL, err := r.twitchClient.GetLiveM3U8(ctx, r.channel)
	if err != nil {
		log.Debug("Channel %s is not live", r.channel)
		return nil
	}

	log.Info("%s is LIVE! Starting recording...", r.channel)
	return r.recordStream(ctx, m3u8URL)
}

func (r *Recorder) recordStream(ctx context.Context, m3u8URL string) error {
	startTime := time.Now()
	timestamp := time.Now()
	downloader := segment.NewSegmentDownloader(r.channel, timestamp)
	parser := segment.NewPlaylistParser(downloader)

	if r.metrics != nil {
		r.metrics.RecordRecordingStart()
	}

	sessionDir := downloader.GetSessionDir()
	log.Info("Recording session: %s", sessionDir)

	streamIDChan := make(chan string, 1)
	go r.pollStreamID(ctx, streamIDChan)

	initSegmentDownloaded := false

	for {
		select {
		case <-ctx.Done():
			log.Info("Context cancelled, finalizing recording...")
			duration := time.Since(startTime)
			if r.metrics != nil {
				r.metrics.RecordRecordingComplete(duration)
			}
			return r.finalizeRecording(downloader, sessionDir, streamIDChan, startTime)
		case streamID := <-streamIDChan:
			log.Info("Stream ID: %s", streamID)
		default:
		}

		if err := parser.FetchNewSegments(ctx, m3u8URL); err != nil {
			log.Error("Error fetching playlist: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if !parser.IsLive() {
			log.Info("Stream ended, finalizing recording...")
			duration := time.Since(startTime)
			if r.metrics != nil {
				r.metrics.RecordRecordingComplete(duration)
			}
			return r.finalizeRecording(downloader, sessionDir, streamIDChan, startTime)
		}

		initURI := downloader.GetInitSegment()
		if initURI != "" && !initSegmentDownloaded {
			log.Info("Downloading init segment...")
			if err := downloader.DownloadSegment(ctx, initURI); err != nil {
				log.Error("Failed to download init segment: %v", err)
			} else {
				initSegmentDownloaded = true
			}
		}

		time.Sleep(3 * time.Second)
	}
}

func (r *Recorder) pollStreamID(ctx context.Context, streamIDChan chan<- string) {
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

func (r *Recorder) finalizeRecording(downloader *segment.SegmentDownloader, sessionDir string, streamIDChan chan string, startTime time.Time) error {
	var streamID string
	select {
	case id := <-streamIDChan:
		if id != "" {
			streamID = id
		}
	default:
	}

	outputName := r.channel
	if streamID != "" {
		outputName = streamID
	}
	outputFile := fmt.Sprintf("%s/%s.mp4", sessionDir, outputName)
	if err := downloader.Finalize(outputFile); err != nil {
		if r.metrics != nil {
			r.metrics.RecordRecordingFailure()
		}
		return fmt.Errorf("failed to finalize recording: %w", err)
	}

	log.Info("Recording saved: %s", outputFile)

	duration := time.Since(startTime)
	if r.config.ArchiveAPIEnabled && r.config.ArchiveAPIEndpoint != "" && r.config.ArchiveApiKey != "" {
		go func() {
			success := api.PostRecording(r.config.ArchiveAPIEndpoint, r.config.ArchiveApiKey, r.channel, streamID, outputFile, duration)
			if r.metrics != nil {
				r.metrics.RecordArchiveAPICall(success)
			}
		}()
	}

	return nil
}
