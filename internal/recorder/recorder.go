package recorder

import (
	"context"
	"fmt"
	"time"

	"twitch-recorder-go/internal/log"
	"twitch-recorder-go/internal/segment"
	"twitch-recorder-go/internal/twitch"
)

type Recorder struct {
	twitchClient *twitch.Client
	channel      string
}

func NewRecorder(twitchClient *twitch.Client, channel string) *Recorder {
	return &Recorder{
		twitchClient: twitchClient,
		channel:      channel,
	}
}

func (r *Recorder) MonitorChannel(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Second)
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
	streams, err := r.twitchClient.GetStreams(ctx, r.channel)
	if err != nil {
		return fmt.Errorf("failed to get streams: %w", err)
	}

	if len(streams.Data) == 0 {
		return nil
	}

	log.Info("%s is LIVE! Starting recording...", r.channel)
	return r.recordStream(ctx, streams.Data[0].ID)
}

func (r *Recorder) recordStream(ctx context.Context, streamID string) error {
	timestamp := time.Now()
	downloader := segment.NewSegmentDownloader(r.channel, timestamp)
	parser := segment.NewPlaylistParser(downloader)

	sessionDir := downloader.GetSessionDir()
	log.Info("Recording session: %s", sessionDir)

	for {
		select {
		case <-ctx.Done():
			log.Info("Context cancelled, finalizing recording...")
			return r.finalizeRecording(downloader, sessionDir)
		default:
		}

		m3u8URL := fmt.Sprintf("%s/%s.m3u8", twitch.TwitchUsherM3U8, streamID)
		if err := parser.FetchNewSegments(ctx, m3u8URL); err != nil {
			log.Error("Error fetching playlist: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if !parser.IsLive() {
			log.Info("Stream ended, finalizing recording...")
			return r.finalizeRecording(downloader, sessionDir)
		}

		time.Sleep(3 * time.Second)
	}
}

func (r *Recorder) finalizeRecording(downloader *segment.SegmentDownloader, sessionDir string) error {
	outputFile := fmt.Sprintf("%s/%s.mp4", twitch.TwitchUsherM3U8, r.channel)
	if err := downloader.Finalize(outputFile); err != nil {
		return fmt.Errorf("failed to finalize recording: %w", err)
	}

	log.Info("Recording saved: %s", outputFile)
	return nil
}
