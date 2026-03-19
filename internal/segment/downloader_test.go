package segment

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSegmentDownloader(t *testing.T) {
	now := time.Date(2026, 3, 19, 14, 30, 0, 0, time.UTC)
	sd := NewSegmentDownloader(".", "testchannel", now)

	assert.Contains(t, sd.sessionDir, "testchannel")
	assert.Contains(t, sd.sessionDir, "2026-03-19_14-30-00")
	assert.NotNil(t, sd.seen)
	assert.NotNil(t, sd.segments)
}

func TestAddSegment(t *testing.T) {
	sd := NewSegmentDownloader(".", "test", time.Now())

	added1 := sd.AddSegment("http://example.com/segment1.ts")
	added2 := sd.AddSegment("http://example.com/segment2.ts")
	added3 := sd.AddSegment("http://example.com/segment1.ts")

	assert.True(t, added1)
	assert.True(t, added2)
	assert.False(t, added3)
	assert.Len(t, sd.segments, 2)
}

func TestAddSegmentConcurrency(t *testing.T) {
	sd := NewSegmentDownloader(".", "test", time.Now())

	var wg sync.WaitGroup
	segments := make([]string, 100)
	for i := range segments {
		segments[i] = "http://example.com/segment" + string(rune('0'+i)) + ".ts"
	}

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(start int) {
			defer wg.Done()
			for j := start; j < start+10; j++ {
				sd.AddSegment(segments[j])
			}
		}(i * 10)
	}

	wg.Wait()
	assert.Len(t, sd.segments, 100)
}

func TestGetSegmentFilename(t *testing.T) {
	sd := NewSegmentDownloader(".", "test", time.Now())

	filename1 := sd.getSegmentFilename()
	filename2 := sd.getSegmentFilename()
	filename3 := sd.getSegmentFilename()

	assert.Equal(t, "00001.ts", filename1)
	assert.Equal(t, "00002.ts", filename2)
	assert.Equal(t, "00003.ts", filename3)
}

func TestDownloadSegmentSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test segment data"))
	}))
	defer server.Close()

	sd := NewSegmentDownloader(".", "test", time.Now())
	defer sd.CleanupOnError()

	err := sd.DownloadSegment(context.Background(), server.URL)

	assert.NoError(t, err)
	assert.Equal(t, 1, sd.GetDownloadedCount())
}

func TestDownloadSegmentRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test segment data"))
	}))
	defer server.Close()

	sd := NewSegmentDownloader(".", "test", time.Now())
	defer sd.CleanupOnError()

	err := sd.DownloadSegment(context.Background(), server.URL)

	assert.NoError(t, err)
	assert.Equal(t, 3, attempts)
}

func TestDownloadSegmentMaxRetries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	sd := NewSegmentDownloader(".", "test", time.Now())
	defer sd.CleanupOnError()

	err := sd.DownloadSegment(context.Background(), server.URL)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "attempt 5/5")
}

func TestDownloadSegmentCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sd := NewSegmentDownloader(".", "test", time.Now())

	err := sd.DownloadSegment(ctx, "http://example.com/segment.ts")

	assert.Error(t, err)
}

func TestDownloadSegmentCreatesDirectory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "segment-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test segment data"))
	}))
	defer server.Close()

	sd := NewSegmentDownloader(".", "test", time.Now())
	sd.sessionDir = filepath.Join(tempDir, "test_2026-03-19_14-30-00")

	err = sd.DownloadSegment(context.Background(), server.URL)

	assert.NoError(t, err)
	_, err = os.Stat(sd.sessionDir)
	assert.NoError(t, err)
}

func TestGetSessionDir(t *testing.T) {
	now := time.Date(2026, 3, 19, 14, 30, 0, 0, time.UTC)
	sd := NewSegmentDownloader(".", "testchannel", now)

	assert.Contains(t, sd.GetSessionDir(), "testchannel")
	assert.Contains(t, sd.GetSessionDir(), "2026-03-19_14-30-00")
}

func TestGetDownloadedCount(t *testing.T) {
	sd := NewSegmentDownloader(".", "test", time.Now())

	count := sd.GetDownloadedCount()
	assert.Equal(t, 0, count)

	sd.downloaded = 5
	count = sd.GetDownloadedCount()
	assert.Equal(t, 5, count)
}

func TestCleanupOnError(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "segment-test")
	require.NoError(t, err)

	testFile := filepath.Join(tempDir, "test.ts")
	err = os.WriteFile(testFile, []byte("test"), 0644)
	require.NoError(t, err)

	sd := NewSegmentDownloader(".", "test", time.Now())
	sd.sessionDir = tempDir

	sd.CleanupOnError()

	_, err = os.Stat(tempDir)
	assert.True(t, os.IsNotExist(err))
}
