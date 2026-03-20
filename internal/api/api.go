package api

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"twitch-recorder-go/internal/log"
)

const maxArchiveRetries = 3

type RecordingMetadata struct {
	Channel      string  `json:"channel"`
	StreamID     string  `json:"streamId"`
	LocalPath    string  `json:"localPath"`
	DurationSecs int64   `json:"durationSecs"`
	FileSizeMB   float64 `json:"fileSizeMb"`
	Timestamp    string  `json:"timestamp"`
}

func PostRecording(endpoint, apiKey, channel, streamID, localPath string, duration time.Duration) bool {
	return PostRecordingWithContext(context.Background(), endpoint, apiKey, channel, streamID, localPath, duration)
}

func PostRecordingWithContext(ctx context.Context, endpoint, apiKey, channel, streamID, localPath string, duration time.Duration) bool {
	if endpoint == "" || apiKey == "" {
		return false
	}

	actualEndpoint := processEndpointTemplate(endpoint, channel)

	fileSizeMB := getFileSizeMB(localPath)

	metadata := RecordingMetadata{
		Channel:      channel,
		StreamID:     streamID,
		LocalPath:    localPath,
		DurationSecs: int64(duration.Seconds()),
		FileSizeMB:   fileSizeMB,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
	}

	client := resty.New().SetTimeout(30 * time.Second)

	for attempt := 0; attempt < maxArchiveRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			log.Warnf("Context cancelled while posting to API for %s", channel)
			return false
		}

		resp, err := client.R().
			SetHeader("Accept", "application/json").
			SetHeader("Content-Type", "application/json").
			SetAuthToken(apiKey).
			SetBody(metadata).
			Post(actualEndpoint)

		if err == nil && resp.StatusCode() == http.StatusOK {
			log.Infof("Successfully posted archive metadata for %s (attempt %d)", channel, attempt+1)
			return true
		}

		if attempt < maxArchiveRetries-1 {
			backoff := time.Duration(1<<uint(attempt)) * time.Second
			log.Warnf("Failed to post to API for %s (attempt %d/%d): %v. Retrying in %v...", channel, attempt+1, maxArchiveRetries, err, backoff)
			time.Sleep(backoff)
		}
	}

	log.Errorf("Failed to post to API for %s after %d attempts", channel, maxArchiveRetries)
	return false
}

func getFileSizeMB(path string) float64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return float64(info.Size()) / (1024 * 1024)
}

func processEndpointTemplate(endpoint, channel string) string {
	if !strings.Contains(endpoint, "{channel}") {
		return endpoint
	}
	return strings.ReplaceAll(endpoint, "{channel}", channel)
}
