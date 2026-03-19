package segment

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsSessionDirectory(t *testing.T) {
	tests := []struct {
		name     string
		dirName  string
		channel  string
		expected bool
	}{
		{"valid session dir", "channel_2026-03-19_14-30-00", "channel", true},
		{"invalid format", "channel", "channel", false},
		{"no underscore", "channel2026-03-19_14-30-00", "channel", false},
		{"short name", "a_b", "a", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSessionDirectory(tt.dirName, tt.channel)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsIncompleteSession(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "recovery-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	sessionDir := filepath.Join(tempDir, "testchannel_2026-03-19_14-30-00")
	err = os.MkdirAll(sessionDir, 0755)
	require.NoError(t, err)

	testFile := filepath.Join(sessionDir, "00001.ts")
	err = os.WriteFile(testFile, []byte("test"), 0644)
	require.NoError(t, err)

	incomplete := isIncompleteSession(sessionDir)
	assert.True(t, incomplete)
}

func TestIsIncompleteSessionWithFinalized(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "recovery-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	sessionDir := filepath.Join(tempDir, "testchannel_2026-03-19_14-30-00")
	err = os.MkdirAll(sessionDir, 0755)
	require.NoError(t, err)

	testFile := filepath.Join(sessionDir, "00001.ts")
	err = os.WriteFile(testFile, []byte("test"), 0644)
	require.NoError(t, err)

	finalizedFile := filepath.Join(sessionDir, "finalized.mp4")
	err = os.WriteFile(finalizedFile, []byte("finalized"), 0644)
	require.NoError(t, err)

	incomplete := isIncompleteSession(sessionDir)
	assert.False(t, incomplete)
}

func TestIsIncompleteSessionNoSegments(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "recovery-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	sessionDir := filepath.Join(tempDir, "testchannel_2026-03-19_14-30-00")
	err = os.MkdirAll(sessionDir, 0755)
	require.NoError(t, err)

	incomplete := isIncompleteSession(sessionDir)
	assert.False(t, incomplete)
}

func TestRecoverIncompleteSessions(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "recovery-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	channelDir := filepath.Join(tempDir, "testchannel")
	err = os.MkdirAll(channelDir, 0755)
	require.NoError(t, err)

	sessionDir := filepath.Join(channelDir, "testchannel_2026-03-19_14-30-00")
	err = os.MkdirAll(sessionDir, 0755)
	require.NoError(t, err)

	testFile := filepath.Join(sessionDir, "00001.ts")
	err = os.WriteFile(testFile, []byte("test"), 0644)
	require.NoError(t, err)

	RecoverIncompleteSessions(tempDir, []string{"testchannel"})
}

func TestRecoverIncompleteSessionsNoChannelDir(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "recovery-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	RecoverIncompleteSessions(tempDir, []string{"nonexistent"})
}
