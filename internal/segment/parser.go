package segment

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/grafov/m3u8"
)

type PlaylistParser struct {
	downloader *SegmentDownloader
	httpClient *http.Client
	mu         sync.Mutex
	lastSeq    int
	isLive     bool
}

func NewPlaylistParser(downloader *SegmentDownloader) *PlaylistParser {
	return &PlaylistParser{
		downloader: downloader,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		lastSeq: -1,
		isLive:  true,
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
			log.Printf("Stream ended (EXT-X-ENDLIST detected)")
		}

		pp.mu.Lock()
		currentSeq := int(mediaPlaylist.SeqNo)
		hasNewSegments := currentSeq > pp.lastSeq
		pp.lastSeq = currentSeq
		pp.mu.Unlock()

		if hasNewSegments {
			for _, segment := range mediaPlaylist.Segments {
				if !pp.downloader.AddSegment(segment.URI) {
					continue
				}
				log.Printf("New segment found: %s", segment.URI)
			}
		}

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

func (pp *PlaylistParser) DownloadAllSegments(ctx context.Context, concurrency int) error {
	pp.mu.Lock()
	segments := make([]string, len(pp.downloader.segments))
	copy(segments, pp.downloader.segments)
	pp.mu.Unlock()

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, concurrency)

	for _, url := range segments {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		wg.Add(1)
		semaphore <- struct{}{}

		go func(segmentURL string) {
			defer wg.Done()
			defer func() { <-semaphore }()

			if err := pp.downloader.DownloadSegment(ctx, segmentURL); err != nil {
				log.Printf("Failed to download segment: %v", err)
			}
		}(url)
	}

	wg.Wait()
	return nil
}
