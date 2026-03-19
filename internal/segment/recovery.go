package segment

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

func RecoverIncompleteSessions(vodDirectory string, channels []string) {
	// No-op - recovery is now automatic in recorder
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
