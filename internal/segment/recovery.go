package segment

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"twitch-recorder-go/internal/log"
)

func RecoverIncompleteSessions(vodDirectory string, channels []string) {
	for _, channel := range channels {
		channelDir := filepath.Join(vodDirectory, channel)

		if _, err := os.Stat(channelDir); os.IsNotExist(err) {
			continue
		}

		sessions, err := filepath.Glob(filepath.Join(channelDir, fmt.Sprintf("%s_*", channel)))
		if err != nil {
			log.Errorf("Failed to scan sessions for %s: %v", channel, err)
			continue
		}

		for _, sessionDir := range sessions {
			if !isIncompleteSession(sessionDir) {
				continue
			}

			log.Infof("Found incomplete session: %s", sessionDir)

			downloader := &SegmentDownloader{
				sessionDir: filepath.Base(sessionDir),
				seen:       make(map[string]bool),
				segments:   make([]string, 0),
			}

			outputFile := filepath.Join(channelDir, fmt.Sprintf("%s.mp4", filepath.Base(sessionDir)))

			if err := downloader.Finalize(outputFile); err != nil {
				log.Errorf("Failed to finalize incomplete session %s: %v", sessionDir, err)
			} else {
				log.Infof("Recovered session: %s -> %s", sessionDir, outputFile)
			}
		}
	}
}

func isIncompleteSession(sessionDir string) bool {
	tsFiles, _ := filepath.Glob(filepath.Join(sessionDir, "*.ts"))

	if len(tsFiles) == 0 {
		return false
	}

	files, _ := os.ReadDir(sessionDir)
	for _, f := range files {
		if !f.IsDir() && filepath.Ext(f.Name()) == ".mp4" {
			return false
		}
	}

	return len(tsFiles) > 0
}

func IsSessionDirectory(name string, channel string) bool {
	pattern := fmt.Sprintf(`^%s_\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}$`, regexp.QuoteMeta(channel))
	matched, _ := regexp.MatchString(pattern, name)
	return matched
}
