package segment

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewPlaylistParser(t *testing.T) {
	sd := NewSegmentDownloader(".", "test", time.Now())
	parser := NewPlaylistParser(sd)

	assert.NotNil(t, parser)
	assert.NotNil(t, parser.downloader)
	assert.NotNil(t, parser.httpClient)
	assert.Equal(t, -1, parser.lastSeq)
	assert.True(t, parser.isLive)
}

func TestIsLive(t *testing.T) {
	sd := NewSegmentDownloader(".", "test", time.Now())
	parser := NewPlaylistParser(sd)

	assert.True(t, parser.IsLive())

	parser.mu.Lock()
	parser.isLive = false
	parser.mu.Unlock()

	assert.False(t, parser.IsLive())
}

func TestDownloadQueuedSegmentsCancellation(t *testing.T) {
	sd := NewSegmentDownloader(".", "test", time.Now())
	sd.AddSegment("http://example.com/seg1.ts", 1)
	sd.AddSegment("http://example.com/seg2.ts", 2)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sd.DownloadQueuedSegments(ctx, 4)
}

func TestDownloadQueuedSegmentsEmpty(t *testing.T) {
	sd := NewSegmentDownloader(".", "test", time.Now())

	ctx := context.Background()
	sd.DownloadQueuedSegments(ctx, 4)
}

func TestPlaylistParserStructure(t *testing.T) {
	sd := NewSegmentDownloader(".", "test", time.Now())
	parser := NewPlaylistParser(sd)

	assert.NotNil(t, parser.downloader)
	assert.Equal(t, -1, parser.lastSeq)
	assert.True(t, parser.IsLive())
}
