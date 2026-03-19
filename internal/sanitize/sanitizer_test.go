package sanitize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeChannelName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"valid name", "mychannel", "mychannel"},
		{"name with spaces", "  my channel  ", "mychannel"},
		{"name with invalid chars", "my<channel>", "my_channel_"},
		{"name with slashes", "my/channel", "my_channel"},
		{"name with dots", "my..channel", "my_channel"},
		{"too long name", "abcdefghijklmnopqrstuvwxyz", "abcdefghijklmnopqrstuvwxy"},
		{"empty name", "", "___"},
		{"name with pipe", "my|channel", "my_channel"},
		{"name with question mark", "my?channel", "my_channel"},
		{"name with asterisk", "my*channel", "my_channel"},
		{"starts with number", "123channel", "123channel"},
		{"special start char", "!channel", "channel"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeChannelName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"valid filename", "recording.ts", "recording.ts"},
		{"filename with invalid chars", "rec<or>ding.ts", "rec_or_ding.ts"},
		{"filename with dots", "re..cording.ts", "re_cording.ts"},
		{"filename with colons", "2026:03:19.ts", "2026_03_19.ts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeFilename(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsSafePath(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		fullPath string
		expected bool
	}{
		{"valid path", "/recordings/channel", "/recordings/channel/file.ts", true},
		{"path traversal", "/recordings", "/recordings/../etc/passwd", false},
		{"different base", "/recordings", "/other/path", false},
		{"contains dots", "/recordings", "/recordings/channel/../../tmp", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSafePath(tt.basePath, tt.fullPath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeChannelNameNoPathTraversal(t *testing.T) {
	malicious := []string{
		"../etc/passwd",
		"..\\..\\windows\\system32",
		".../malicious",
		"channel/../secret",
	}

	for _, input := range malicious {
		result := SanitizeChannelName(input)
		assert.NotContains(t, result, "..", "Sanitized name should not contain '..' for input: %s", input)
	}
}

func TestSanitizeFilenameMaxLength(t *testing.T) {
	longName := strings.Repeat("a", 300) + ".ts"
	result := SanitizeFilename(longName)

	assert.LessOrEqual(t, len(result), 200, "Filename should not exceed max length")
	assert.True(t, strings.HasSuffix(result, ".ts"), "Extension should be preserved")
}
