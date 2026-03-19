package segment

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewSegmentDownloader(t *testing.T) {
	timestamp := time.Now()
	sd := NewSegmentDownloader("testchannel", timestamp)

	assert.NotNil(t, sd)
	assert.NotEmpty(t, sd.sessionDir)
	assert.Equal(t, 0, len(sd.segments))
	assert.Equal(t, 0, len(sd.seen))
}

func TestSegmentDownloader_AddSegment(t *testing.T) {
	sd := NewSegmentDownloader("test", time.Now())

	url1 := "https://example.com/segment1.ts"
	url2 := "https://example.com/segment2.ts"

	result1 := sd.AddSegment(url1)
	assert.True(t, result1, "First add should return true")

	result2 := sd.AddSegment(url1)
	assert.False(t, result2, "Duplicate add should return false")

	result3 := sd.AddSegment(url2)
	assert.True(t, result3, "Second unique URL should return true")

	assert.Equal(t, 2, len(sd.segments))
	assert.Equal(t, 2, len(sd.seen))
}

func TestSegmentDownloader_AddSegmentConcurrent(t *testing.T) {
	sd := NewSegmentDownloader("test", time.Now())

	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func(n int) {
			url := "https://example.com/segment" + string(rune('0'+n)) + ".ts"
			sd.AddSegment(url)
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	assert.Equal(t, 100, len(sd.segments), "All segments should be added")
	assert.Equal(t, 100, len(sd.seen))
}

func TestSegmentDownloader_getSegmentFilename(t *testing.T) {
	sd := NewSegmentDownloader("test", time.Now())

	filename1 := sd.getSegmentFilename("https://example.com/segment1.ts")
	filename2 := sd.getSegmentFilename("https://example.com/segment1.ts")
	filename3 := sd.getSegmentFilename("https://example.com/segment2.ts")

	assert.Equal(t, filename1, filename2, "Same URL should produce same filename")
	assert.NotEqual(t, filename1, filename3, "Different URLs should produce different filenames")
	assert.Regexp(t, `^\d{5}\.ts$`, filename1, "Filename should match pattern")
}

func TestSegmentDownloader_GetDownloadedCount(t *testing.T) {
	sd := NewSegmentDownloader("test", time.Now())

	count := sd.GetDownloadedCount()
	assert.Equal(t, 0, count, "Initial count should be zero")

	sd.downloaded = 5
	count = sd.GetDownloadedCount()
	assert.Equal(t, 5, count)
}

func TestSegmentDownloader_GetSessionDir(t *testing.T) {
	expectedDir := "testchannel_2024-01-01_12-00-00"
	sd := NewSegmentDownloader("testchannel", time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))

	assert.Equal(t, expectedDir, sd.GetSessionDir())
}

func TestSegmentDownloader_CleanupOnError(t *testing.T) {
	testDir := "test_cleanup_dir"
	os.MkdirAll(testDir, 0755)
	defer os.RemoveAll(testDir)

	sd := &SegmentDownloader{
		sessionDir: testDir,
	}

	info, err := os.Stat(testDir)
	assert.NoError(t, err)
	assert.True(t, info.IsDir(), "Directory should exist before cleanup")
	sd.CleanupOnError()
	_, err = os.Stat(testDir)
	assert.Error(t, err, "Directory should be removed after cleanup")
	assert.True(t, os.IsNotExist(err))
}

func TestDownloadSegment_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	sd := NewSegmentDownloader("test", time.Now())
	err := sd.DownloadSegment(ctx, "https://example.com/segment.ts")

	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestDownloadSegment_InvalidURL(t *testing.T) {
	ctx := context.Background()
	sd := NewSegmentDownloader("test", time.Now())

	err := sd.DownloadSegment(ctx, "http://invalid.invalid/segment.ts")
	assert.Error(t, err)
}

func TestSessionDirFormat(t *testing.T) {
	testCases := []struct {
		channel  string
		time     time.Time
		expected string
	}{
		{
			channel:  "streamer1",
			time:     time.Date(2024, 3, 15, 10, 30, 45, 0, time.UTC),
			expected: "streamer1_2024-03-15_10-30-45",
		},
		{
			channel:  "my_channel",
			time:     time.Date(2025, 12, 25, 0, 0, 0, 0, time.UTC),
			expected: "my_channel_2025-12-25_00-00-00",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.channel, func(t *testing.T) {
			sd := NewSegmentDownloader(tc.channel, tc.time)
			assert.Equal(t, tc.expected, sd.GetSessionDir())
		})
	}
}

func TestGetSegmentFilename_PathJoin(t *testing.T) {
	sd := NewSegmentDownloader("test", time.Now())
	filename := sd.getSegmentFilename("https://example.com/test.ts")

	path := filepath.Join(sd.sessionDir, filename)
	assert.Contains(t, path, sd.sessionDir)
	assert.Contains(t, path, filename)
	assert.True(t, len(path) >= 3 && path[len(path)-3:] == ".ts", "Path should end with .ts")
}
