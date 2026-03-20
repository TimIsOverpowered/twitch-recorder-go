package chatlogs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"twitch-recorder-go/internal/config"
	"twitch-recorder-go/internal/log"
	"twitch-recorder-go/internal/twitch"
)

const (
	RetryDelay   = 2 * time.Second
	MaxRetries   = 5
	RequestDelay = 100 * time.Millisecond
)

// FetchAndSaveChatLogs fetches chat logs for a stream and saves them to disk
func FetchAndSaveChatLogs(cfg *config.Config, client *twitch.Client, channel, streamID, outputDir string) error {
	vod, err := client.GetVODByChannelAndStreamID(channel, streamID)
	if err != nil {
		log.Errorf("VOD not found for stream_id %s: %v", streamID, err)
		return err
	}

	log.Infof("Fetching chat logs for VOD %s (stream_id: %s)", vod.ID, streamID)

	var allComments []twitch.Comment

	response, err := client.FetchComments(vod.ID, 0)
	if err != nil {
		log.Errorf("Failed to fetch initial chat logs: %v", err)
		return err
	}

	log.Infof("%v", response.Data.Video)

	comments, hasNextPage := extractComments(response)
	allComments = append(allComments, comments...)
	log.Debugf("Fetched initial %d chat messages", len(comments))

	for hasNextPage {
		time.Sleep(RequestDelay)

		cursor := ""
		if len(response.Data.Video.Comments.Edges) > 0 {
			cursor = response.Data.Video.Comments.Edges[len(response.Data.Video.Comments.Edges)-1].Cursor
		}

		var paginatedResponse *twitch.ChatResponse
		var fetchErr error

		for attempt := 0; attempt < MaxRetries; attempt++ {
			paginatedResponse, fetchErr = client.FetchNextComments(vod.ID, cursor)
			if fetchErr == nil {
				break
			}

			log.Warnf("Failed to fetch chat page (attempt %d/%d): %v", attempt+1, MaxRetries, fetchErr)
			time.Sleep(RetryDelay * time.Duration(attempt+1))
		}

		if fetchErr != nil {
			log.Errorf("Failed to fetch chat logs after %d retries: %v", MaxRetries, fetchErr)
			break
		}

		response = paginatedResponse
		comments, hasNextPage = extractComments(response)
		allComments = append(allComments, comments...)
		log.Debugf("Fetched page with %d messages (total: %d)", len(comments), len(allComments))
	}

	if len(allComments) == 0 {
		log.Infof("No chat messages found for stream_id %s", streamID)
		return nil
	}

	outputFile := filepath.Join(outputDir, streamID+"_chat.json")
	if err := saveChatLogs(outputFile, allComments); err != nil {
		log.Errorf("Failed to save chat logs: %v", err)
		return err
	}

	log.Infof("Saved %d chat messages to %s", len(allComments), outputFile)
	return nil
}

// extractComments extracts raw comment nodes from a ChatResponse and returns whether there's more pages
func extractComments(response *twitch.ChatResponse) ([]twitch.Comment, bool) {
	var comments []twitch.Comment

	edges := response.Data.Video.Comments.Edges
	for _, edge := range edges {
		comments = append(comments, edge.Node)
	}

	return comments, response.Data.Video.Comments.PageInfo.HasNextPage
}

// saveChatLogs saves chat messages to a JSON file
func saveChatLogs(filePath string, comments []twitch.Comment) error {
	data, err := json.MarshalIndent(comments, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}
