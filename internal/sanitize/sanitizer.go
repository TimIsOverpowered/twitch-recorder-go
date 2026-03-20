package sanitize

import (
	"regexp"
	"strings"
)

var invalidPathChars = regexp.MustCompile(`[<>:"/\\|?*]`)

func SanitizeChannelName(name string) string {
	name = strings.TrimSpace(name)

	name = invalidPathChars.ReplaceAllString(name, "_")

	name = strings.ReplaceAll(name, "..", "_")

	runes := []rune(name)
	validRunes := make([]rune, 0, len(runes))
	for _, r := range runes {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			validRunes = append(validRunes, r)
		}
	}
	name = string(validRunes)

	if len(name) > 25 {
		name = name[:25]
	}

	if len(name) < 3 {
		padding := 3 - len(name)
		name = name + strings.Repeat("_", padding)
	}

	if len(name) == 0 {
		return "unknown"
	}

	return name
}

func SanitizeFilename(filename string) string {
	filename = invalidPathChars.ReplaceAllString(filename, "_")

	filename = strings.ReplaceAll(filename, "..", "_")

	maxLength := 200
	if len(filename) > maxLength {
		extIdx := strings.LastIndex(filename, ".")
		if extIdx > 0 {
			ext := filename[extIdx:]
			filename = filename[:maxLength-len(ext)] + ext
		} else {
			filename = filename[:maxLength]
		}
	}

	return strings.TrimSpace(filename)
}

func IsSafePath(basePath, fullPath string) bool {
	fullPath = strings.ToLower(fullPath)
	basePath = strings.ToLower(basePath)

	if !strings.HasPrefix(fullPath, basePath) {
		return false
	}

	relative := fullPath[len(basePath):]
	return !strings.Contains(relative, "..")
}
