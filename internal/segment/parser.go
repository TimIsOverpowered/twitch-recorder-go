package segment

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"twitch-recorder-go/internal/log"

	"github.com/grafov/m3u8"
)

type PlaylistParser struct {
	downloader  *SegmentDownloader
	httpClient  *http.Client
	mu          sync.Mutex
	lastSeq     int
	isLive      bool
	initSegment string
	format      string // "ts" or "mp4"
}

func NewPlaylistParser(downloader *SegmentDownloader) *PlaylistParser {
	return &PlaylistParser{
		downloader: downloader,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		lastSeq: -1,
		isLive:  true,
		format:  "ts", // default to TS
	}
}

func (pp *PlaylistParser) FetchNewSegments(ctx context.Context, m3u8URL string) error {
	resp, err := pp.httpClient.Get(m3u8URL)
	if err != nil {
		return fmt.Errorf("failed to fetch playlist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body := &bytes.Buffer{}
	_, err = body.ReadFrom(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read playlist body: %w", err)
	}

	pp.mu.Lock()
	pp.isLive = true
	pp.mu.Unlock()

	p, listType, err := m3u8.DecodeFrom(body, true)
	if err != nil {
		return fmt.Errorf("failed to decode m3u8: %w", err)
	}

	switch listType {
	case m3u8.MASTER:
		masterPlaylist := p.(*m3u8.MasterPlaylist)
		for _, variant := range masterPlaylist.Variants {
			if strings.EqualFold(variant.Video, "chunked") || variant.Resolution != "" {
				return pp.FetchNewSegments(ctx, variant.URI)
			}
		}
		return fmt.Errorf("no suitable variant found")

	case m3u8.MEDIA:
		mediaPlaylist := p.(*m3u8.MediaPlaylist)

		if mediaPlaylist.Closed {
			pp.mu.Lock()
			pp.isLive = false
			pp.mu.Unlock()
			log.Info("Stream ended (EXT-X-ENDLIST detected)")
		}

		if mediaPlaylist.Map != nil && pp.initSegment == "" {
			pp.initSegment = mediaPlaylist.Map.URI
			pp.downloader.SetInitSegment(pp.initSegment)
			log.DebugfC(pp.downloader.channel, "Found init segment: %s", pp.initSegment)
		}

		if len(mediaPlaylist.Segments) > 0 && mediaPlaylist.Segments[0] != nil {
			firstSeg := mediaPlaylist.Segments[0].URI
			// Strip query params for format detection
			baseURL := strings.Split(firstSeg, "?")[0]
			if strings.HasSuffix(baseURL, ".mp4") {
				pp.format = "mp4"
				log.DebugfC(pp.downloader.channel, "Detected fMP4 format")
			} else {
				pp.format = "ts"
				log.DebugfC(pp.downloader.channel, "Detected TS format")
			}
			pp.downloader.SetFormat(pp.format)
		}

		pp.mu.Lock()
		playlistStartSeq := int(mediaPlaylist.SeqNo)
		lastSeq := pp.lastSeq
		pp.mu.Unlock()

		highestAddedSeq := lastSeq
		skippedCount := 0

		for i, segment := range mediaPlaylist.Segments {
			if segment == nil || segment.URI == "" {
				continue
			}

			// Calculate this segment's sequence number
			segmentSeq := playlistStartSeq + i

			// Skip if this segment was already downloaded
			if segmentSeq <= lastSeq {
				skippedCount++
				continue
			}

			if !pp.downloader.AddSegment(segment.URI, segmentSeq) {
				continue
			}

			// Track the highest sequence number we actually added
			if segmentSeq > highestAddedSeq {
				highestAddedSeq = segmentSeq
			}

			log.DebugfC(pp.downloader.channel, "New segment found: seq=%d, url=%s", segmentSeq, segment.URI[:min(50, len(segment.URI))])
		}

		if skippedCount > 0 {
			log.DebugfC(pp.downloader.channel, "Skipped %d already-downloaded segments", skippedCount)
		}

		// Update lastSeq to the highest sequence number we actually added
		pp.mu.Lock()
		if highestAddedSeq > pp.lastSeq {
			pp.lastSeq = highestAddedSeq
		}
		pp.mu.Unlock()

	default:
		return fmt.Errorf("unknown playlist type: %v", listType)
	}

	return nil
}

func (pp *PlaylistParser) IsLive() bool {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	return pp.isLive
}

func (pp *PlaylistParser) SetLastSeq(seq int) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	pp.lastSeq = seq
}

func (pp *PlaylistParser) GetLastSeq() int {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	return pp.lastSeq
}
