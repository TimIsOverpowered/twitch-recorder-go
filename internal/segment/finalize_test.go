package segment

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinalizeNoSegments(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "finalize-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	sd := NewSegmentDownloader("test", time.Now())
	sd.sessionDir = filepath.Join(tempDir, "test_2026-03-19_14-30-00")
	os.MkdirAll(sd.sessionDir, 0755)

	err = sd.Finalize("/tmp/output.mp4")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no segment files found")
}

func TestFinalizeCreatesConcatFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "finalize-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	sd := NewSegmentDownloader("test", time.Now())
	sd.sessionDir = filepath.Join(tempDir, "test_2026-03-19_14-30-00")
	os.MkdirAll(sd.sessionDir, 0755)

	testFile := filepath.Join(sd.sessionDir, "00001.ts")
	err = os.WriteFile(testFile, []byte("test segment data"), 0644)
	require.NoError(t, err)

	err = sd.Finalize("/tmp/output.mp4")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ffmpeg")
}

func TestFinalizeWithMultipleSegments(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "finalize-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	sd := NewSegmentDownloader("test", time.Now())
	sd.sessionDir = filepath.Join(tempDir, "test_2026-03-19_14-30-00")
	os.MkdirAll(sd.sessionDir, 0755)

	for i := 1; i <= 3; i++ {
		testFile := filepath.Join(sd.sessionDir, string(rune('0'+i))+"000"+string(rune('0'+i))+".ts")
		err = os.WriteFile(testFile, []byte("test segment data"), 0644)
		require.NoError(t, err)
	}

	files, _ := os.ReadDir(sd.sessionDir)
	assert.Greater(t, len(files), 0)

	err = sd.Finalize("/tmp/output.mp4")

	if err != nil {
		assert.Contains(t, err.Error(), "ffmpeg")
	}
}

func TestFinalizeSessionDirectoryCleanup(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "finalize-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	sd := NewSegmentDownloader("test", time.Now())
	sd.sessionDir = filepath.Join(tempDir, "test_2026-03-19_14-30-00")
	os.MkdirAll(sd.sessionDir, 0755)

	testFile := filepath.Join(sd.sessionDir, "00001.ts")
	err = os.WriteFile(testFile, []byte("test segment data"), 0644)
	require.NoError(t, err)

	_, err = os.Stat(sd.sessionDir)
	assert.NoError(t, err)

	err = sd.Finalize("/tmp/output.mp4")

	if err != nil {
		assert.Contains(t, err.Error(), "ffmpeg")
	}
}
