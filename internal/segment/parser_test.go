package segment

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewPlaylistParser(t *testing.T) {
	sd := NewSegmentDownloader("test", time.Now())
	parser := NewPlaylistParser(sd)

	assert.NotNil(t, parser)
	assert.NotNil(t, parser.downloader)
	assert.NotNil(t, parser.httpClient)
	assert.Equal(t, -1, parser.lastSeq)
	assert.True(t, parser.isLive)
}

func TestIsLive(t *testing.T) {
	sd := NewSegmentDownloader("test", time.Now())
	parser := NewPlaylistParser(sd)

	assert.True(t, parser.IsLive())

	parser.mu.Lock()
	parser.isLive = false
	parser.mu.Unlock()

	assert.False(t, parser.IsLive())
}

func TestDownloadAllSegmentsCancellation(t *testing.T) {
	sd := NewSegmentDownloader("test", time.Now())
	sd.AddSegment("http://example.com/seg1.ts")
	sd.AddSegment("http://example.com/seg2.ts")

	parser := NewPlaylistParser(sd)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := parser.DownloadAllSegments(ctx, 4)

	assert.Error(t, err)
}

func TestDownloadAllSegmentsEmpty(t *testing.T) {
	sd := NewSegmentDownloader("test", time.Now())
	parser := NewPlaylistParser(sd)

	ctx := context.Background()
	err := parser.DownloadAllSegments(ctx, 4)

	assert.NoError(t, err)
}

func TestPlaylistParserStructure(t *testing.T) {
	sd := NewSegmentDownloader("test", time.Now())
	parser := NewPlaylistParser(sd)

	assert.NotNil(t, parser.downloader)
	assert.Equal(t, -1, parser.lastSeq)
	assert.True(t, parser.IsLive())
}
