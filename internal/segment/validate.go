package segment

import (
	"errors"
	"fmt"
	"os"

	"twitch-recorder-go/internal/log"
	"twitch-recorder-go/internal/sanitize"
)

func ValidateConfig(vodDirectory string, channels []string) error {
	if vodDirectory == "" {
		return errors.New("vod_directory is required")
	}

	if _, err := os.Stat(vodDirectory); os.IsNotExist(err) {
		if err := os.MkdirAll(vodDirectory, 0755); err != nil {
			return fmt.Errorf("cannot create vod_directory: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("error accessing vod_directory: %w", err)
	}

	info, err := os.Stat(vodDirectory)
	if err != nil {
		return fmt.Errorf("cannot stat vod_directory: %w", err)
	}

	if !info.IsDir() {
		return errors.New("vod_directory must be a directory")
	}

	testFile := vodDirectory + "/.write_test"
	if err := os.WriteFile(testFile, []byte(""), 0644); err != nil {
		return fmt.Errorf("vod_directory is not writable: %w", err)
	}
	os.Remove(testFile)

	if len(channels) == 0 {
		return errors.New("at least one channel must be specified")
	}

	for _, channel := range channels {
		if channel == "" {
			return errors.New("channel name cannot be empty")
		}

		sanitized := sanitize.SanitizeChannelName(channel)
		if sanitized != channel {
			log.Warnf("Channel name '%s' sanitized to '%s'", channel, sanitized)
		}
	}

	return nil
}
