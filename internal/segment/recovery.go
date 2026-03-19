package segment

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func RecoverIncompleteSessions(vodDirectory string, channels []string) {
	// No-op - recovery is now automatic in recorder
}

func isIncompleteSession(sessionDir string) bool {
	tsFiles, _ := filepath.Glob(filepath.Join(sessionDir, "*.ts"))
	mp4Segments, _ := filepath.Glob(filepath.Join(sessionDir, "*.mp4"))

	allSegments := len(tsFiles) + len(mp4Segments)
	if allSegments == 0 {
		return false
	}

	files, _ := os.ReadDir(sessionDir)
	for _, f := range files {
		if !f.IsDir() && filepath.Ext(f.Name()) == ".mp4" {
			baseName := strings.TrimSuffix(f.Name(), ".mp4")
			if _, err := fmt.Sscanf(baseName, "%d", new(int)); err == nil {
				continue
			}
			return false
		}
	}

	return allSegments > 0
}

func IsSessionDirectory(name string, channel string) bool {
	pattern := fmt.Sprintf(`^%s_\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}$`, regexp.QuoteMeta(channel))
	matched, _ := regexp.MatchString(pattern, name)
	return matched
}

func FindIncompleteSession(vodDirectory, channel string) (string, error) {
	channelDir := filepath.Join(vodDirectory, channel)

	if _, err := os.Stat(channelDir); os.IsNotExist(err) {
		return "", nil
	}

	files, err := os.ReadDir(channelDir)
	if err != nil {
		return "", err
	}

	for _, f := range files {
		if !f.IsDir() {
			continue
		}

		sessionDir := filepath.Join(channelDir, f.Name())
		if isIncompleteSession(sessionDir) {
			return sessionDir, nil
		}
	}

	return "", nil
}
