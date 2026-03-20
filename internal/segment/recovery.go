package segment

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func isIncompleteSession(sessionDir string) bool {
	metadataPath := filepath.Join(filepath.Dir(sessionDir), "current_session.json")

	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		return false
	}

	tsFiles, _ := filepath.Glob(filepath.Join(sessionDir, "*.ts"))

	if len(tsFiles) == 0 {
		return false
	}

	mp4Files, _ := filepath.Glob(filepath.Join(sessionDir, "*.mp4"))
	for _, f := range mp4Files {
		baseName := strings.TrimSuffix(filepath.Base(f), ".mp4")
		if _, err := fmt.Sscanf(baseName, "%d", new(int)); err != nil {
			return false
		}
	}

	return true
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
