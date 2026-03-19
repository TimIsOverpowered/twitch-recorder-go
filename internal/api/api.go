package api

import (
	"os"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"twitch-recorder-go/internal/log"
)

type RecordingMetadata struct {
	Channel      string  `json:"channel"`
	StreamID     string  `json:"streamId"`
	LocalPath    string  `json:"localPath"`
	DurationSecs int64   `json:"durationSecs"`
	FileSizeMB   float64 `json:"fileSizeMb"`
	Timestamp    string  `json:"timestamp"`
}

func PostRecording(endpoint, apiKey, channel, streamID, localPath string, duration time.Duration) bool {
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
	resp, err := client.R().
		SetHeader("Accept", "application/json").
		SetHeader("Content-Type", "application/json").
		SetAuthToken(apiKey).
		SetBody(metadata).
		Post(actualEndpoint)

	if err != nil {
		log.Warnf("Failed to post to API for %s: %v", channel, err)
		return false
	}

	if resp.StatusCode() != 200 {
		log.Warnf("API post failed for %s: expected 200, got %d", channel, resp.StatusCode())
		return false
	}

	log.Infof("Successfully posted recording to API: %s", channel)
	return true
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
