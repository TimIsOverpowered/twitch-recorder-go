package twitch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"twitch-recorder-go/internal/log"
	"twitch-recorder-go/internal/metrics"
	"twitch-recorder-go/internal/ratelimit"

	"github.com/go-resty/resty/v2"
	"github.com/grafov/m3u8"
)

const (
	TwitchAPIBase   = "https://api.twitch.tv/helix"
	TwitchIDAPI     = "https://id.twitch.tv"
	TwitchGQLAPI    = "https://gql.twitch.tv/gql"
	TwitchUsherM3U8 = "https://usher.ttvnw.net"
	GQLClientId     = "kd1unb4b3q4t58fwlpcbzcbnm76a8fp"
)

var (
	ErrUserNotFound   = errors.New("user not found")
	ErrStreamNotFound = errors.New("stream not found")
)

type Client struct {
	httpClient        *resty.Client
	clientID          string
	clientSecret      string
	oauthKey          string
	accessToken       string
	expiresAt         time.Time
	mu                sync.RWMutex
	isRefreshingToken bool
	rateLimiter       *ratelimit.Limiter
	metrics           *metrics.Metrics
	tokenCache        map[string]*CachedToken
	tokenCacheMu      sync.RWMutex
}

func NewClient(clientID, clientSecret, oauthKey string, httpClient *resty.Client) *Client {
	return NewClientWithRateLimit(clientID, clientSecret, oauthKey, httpClient, 150, 400*time.Millisecond)
}

func NewClientWithRateLimit(clientID, clientSecret, oauthKey string, httpClient *resty.Client, maxTokens int, refillRate time.Duration) *Client {
	return &Client{
		httpClient:   httpClient,
		clientID:     clientID,
		clientSecret: clientSecret,
		oauthKey:     oauthKey,
		rateLimiter:  ratelimit.NewLimiter(maxTokens, refillRate),
		tokenCache:   make(map[string]*CachedToken),
	}
}

func (c *Client) SetRateLimit(maxTokens int, refillRate time.Duration) {
	if c.rateLimiter != nil {
		c.rateLimiter.SetMaxTokens(maxTokens)
		c.rateLimiter.SetRefillRate(refillRate)
	}
}

func (c *Client) SetMetrics(m *metrics.Metrics) {
	c.metrics = m
}

func (c *Client) GetUser(ctx context.Context, login string) (*User, error) {
	if err := c.ensureAccessToken(ctx); err != nil {
		if c.metrics != nil {
			c.metrics.RecordAPICall(false, 0)
		}
		return nil, err
	}

	c.rateLimiter.Wait()

	var response struct {
		Data []User `json:"data"`
	}

	resp, err := c.httpClient.R().
		SetHeader("Client-ID", c.getClientID()).
		SetHeader("Authorization", "Bearer "+c.getAccessToken()).
		SetQueryParams(map[string]string{
			"login": login,
		}).
		SetResult(&response).
		Get(TwitchAPIBase + "/users")

	if err != nil {
		if c.metrics != nil {
			c.metrics.RecordAPICall(false, 0)
		}
		return nil, err
	}

	if resp.IsError() {
		if c.metrics != nil {
			c.metrics.RecordAPICall(false, 0)
		}
		return nil, &APIError{StatusCode: resp.StatusCode(), Body: string(resp.Body())}
	}

	if c.metrics != nil {
		c.metrics.RecordAPICall(true, 1)
	}

	if len(response.Data) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrUserNotFound, login)
	}

	return &response.Data[0], nil
}

func (c *Client) GetStreams(ctx context.Context, userLogin string) (*Streams, error) {
	if err := c.ensureAccessToken(ctx); err != nil {
		if c.metrics != nil {
			c.metrics.RecordAPICall(false, 0)
		}
		return nil, err
	}

	c.rateLimiter.Wait()

	var response Streams

	resp, err := c.httpClient.R().
		SetHeader("Client-ID", c.getClientID()).
		SetHeader("Authorization", "Bearer "+c.getAccessToken()).
		SetQueryParams(map[string]string{
			"user_login": userLogin,
		}).
		SetResult(&response).
		Get(TwitchAPIBase + "/streams")

	if err != nil {
		if c.metrics != nil {
			c.metrics.RecordAPICall(false, 0)
		}
		return nil, err
	}

	if resp.IsError() {
		if c.metrics != nil {
			c.metrics.RecordAPICall(false, 0)
		}
		return nil, &APIError{StatusCode: resp.StatusCode(), Body: string(resp.Body())}
	}

	if c.metrics != nil {
		c.metrics.RecordAPICall(true, 1)
	}

	if len(response.Data) == 0 {
		return &response, fmt.Errorf("%w: %s", ErrStreamNotFound, userLogin)
	}

	return &response, nil
}

func (c *Client) RefreshToken(ctx context.Context) error {
	c.mu.Lock()
	if c.isRefreshingToken {
		c.mu.Unlock()
		return nil
	}
	c.isRefreshingToken = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.isRefreshingToken = false
		c.mu.Unlock()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	c.rateLimiter.Wait()

	var response TwitchToken

	resp, err := c.httpClient.R().
		SetHeader("Client-ID", c.getClientID()).
		SetFormData(map[string]string{
			"client_id":     c.getClientID(),
			"client_secret": c.clientSecret,
			"grant_type":    "client_credentials",
		}).
		SetResult(&response).
		Post(TwitchIDAPI + "/oauth2/token")

	if err != nil {
		if c.metrics != nil {
			c.metrics.RecordAPICall(false, 0)
		}
		return err
	}

	if resp.IsError() {
		if c.metrics != nil {
			c.metrics.RecordAPICall(false, 0)
		}
		return &APIError{StatusCode: resp.StatusCode(), Body: string(resp.Body())}
	}

	if c.metrics != nil {
		c.metrics.RecordAPICall(true, 1)
	}

	c.accessToken = response.AccessToken
	c.expiresAt = time.Now().Add(time.Duration(response.ExpiresIn) * time.Second)

	return nil
}

func (c *Client) CheckAccessToken() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return time.Now().Before(c.expiresAt)
}

func (c *Client) ensureAccessToken(ctx context.Context) error {
	if c.CheckAccessToken() {
		return nil
	}

	return c.RefreshToken(ctx)
}

func (c *Client) getClientID() string {
	return c.clientID
}

func (c *Client) getAccessToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.accessToken
}

type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return "twitch API error: status " + string(rune(e.StatusCode)) + ", body: " + e.Body
}

func (c *Client) GetLiveTokenSig(ctx context.Context, channel string) (*TokenSig, error) {
	body := fmt.Sprintf(`{
		"operationName": "PlaybackAccessToken",
		"variables":{
			"isLive": true,
			"login": "%s",
			"isVod": false,
			"vodID": "",
			"platform": "web",
			"playerBackend": "mediaplayer",
			"playerType": "site"
		},
		"extensions":{
			"persistedQuery":{
				"version": 1,
				"sha256Hash": "0828119ded1c13477966434e15800ff57ddacf13ba1911c129dc2200705b0712"
			}
		}
	}`, channel)

	req := c.httpClient.R().
		SetHeader("Client-ID", GQLClientId).
		SetHeader("Origin", "https://twitch.tv").
		SetHeader("Referer", "https://twitch.tv").
		SetHeader("Content-Type", "text/plain;charset=UTF-8").
		SetHeader("Accept", "*/*").
		SetBody([]byte(body))

	if c.oauthKey != "" {
		req.SetHeader("Authorization", "OAuth "+c.oauthKey)
	}

	var response TokenSig
	resp, err := req.Post(TwitchGQLAPI)

	if err != nil {
		if c.metrics != nil {
			c.metrics.RecordGQLCall(false)
		}
		return nil, err
	}

	if resp.IsError() {
		if c.metrics != nil {
			c.metrics.RecordGQLCall(false)
		}
		return nil, &APIError{StatusCode: resp.StatusCode(), Body: string(resp.Body())}
	}

	if c.metrics != nil {
		c.metrics.RecordGQLCall(true)
	}

	// Manually unmarshal the response
	if err := json.Unmarshal(resp.Body(), &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal GQL response: %w", err)
	}

	if response.Data.StreamPlaybackAccessToken == nil || response.Data.StreamPlaybackAccessToken.Value == "" {
		return nil, ErrInvalidUser
	}

	return &response, nil
}

var ErrInvalidUser = errors.New("user is invalid or does not exist")

func (c *Client) GetCachedToken(ctx context.Context, channel string) (*CachedToken, error) {
	c.tokenCacheMu.Lock()
	cached, ok := c.tokenCache[channel]
	if ok && time.Now().Before(cached.ExpiresAt) {
		c.tokenCacheMu.Unlock()
		log.Debugf("Token expires in: %v", time.Until(cached.ExpiresAt))
		return cached, nil
	}
	c.tokenCacheMu.Unlock()

	log.Infof("Fetching new token for channel %s", channel)
	tokenSig, err := c.GetLiveTokenSig(ctx, channel)
	if err != nil {
		return nil, err
	}

	expiresAt, err := extractTokenExpiration(tokenSig.Data.StreamPlaybackAccessToken.Value)
	if err != nil {
		log.Warnf("Failed to parse token expiration, using default 10min: %v", err)
		expiresAt = time.Now().Add(10 * time.Minute)
	}

	cachedToken := &CachedToken{
		Value:     tokenSig.Data.StreamPlaybackAccessToken.Value,
		Signature: tokenSig.Data.StreamPlaybackAccessToken.Signature,
		ExpiresAt: expiresAt,
	}

	c.tokenCacheMu.Lock()
	c.tokenCache[channel] = cachedToken
	c.tokenCacheMu.Unlock()

	return cachedToken, nil
}

func extractTokenExpiration(tokenValue string) (time.Time, error) {
	decoded, err := url.QueryUnescape(tokenValue)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to decode token: %w", err)
	}

	var claims map[string]interface{}
	if err := json.Unmarshal([]byte(decoded), &claims); err != nil {
		return time.Time{}, fmt.Errorf("failed to parse token JSON: %w", err)
	}

	expires, ok := claims["expires"].(float64)
	if !ok {
		return time.Time{}, errors.New("expires field not found or invalid type")
	}

	return time.Unix(int64(expires), 0).UTC(), nil
}

func (c *Client) GetLiveM3U8(ctx context.Context, channel string) (string, error) {
	cachedToken, err := c.GetCachedToken(ctx, channel)
	if err != nil {
		return "", err
	}

	randomP := strconv.Itoa(rand.Intn(9000000) + 1000000)
	m3u8URL := fmt.Sprintf(
		"%s/api/channel/hls/%s.m3u8?allow_source=true&p=%s&player=mediaplayer&include_unavailable=true&supported_codecs=av1,h265,h264&playlist_include_framerate=true&allow_spectre=true&sig=%s&token=%s",
		TwitchUsherM3U8, channel, randomP, cachedToken.Signature, cachedToken.Value,
	)

	req := c.httpClient.R()
	if c.oauthKey != "" {
		req.SetHeader("Authorization", "OAuth "+c.oauthKey)
	}
	req.SetHeader("Content-Type", "application/vnd.apple.mpegurl")

	resp, err := req.Get(m3u8URL)
	if err != nil {
		return "", fmt.Errorf("failed to fetch m3u8: %w", err)
	}

	if resp.StatusCode() == 404 {
		return "", errors.New("channel is not live")
	}

	if resp.StatusCode() != 200 {
		log.Debugf("Unexpected status code for %s: %d", channel, resp.StatusCode())
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode())
	}

	body := bytes.NewReader(resp.Body())
	playlist, listType, err := m3u8.DecodeFrom(body, true)
	if err != nil {
		return "", fmt.Errorf("failed to decode m3u8: %w", err)
	}

	if listType == m3u8.MASTER {
		masterPlaylist := playlist.(*m3u8.MasterPlaylist)
		for _, variant := range masterPlaylist.Variants {
			if strings.EqualFold(variant.Video, "chunked") {
				return variant.URI, nil
			}
		}
	}

	return "", errors.New("cannot find chunked variant in playlist")
}

// GetVODByChannelAndStreamID retrieves the latest archive VOD for a channel and verifies it matches the given stream ID
func (c *Client) GetVODByChannelAndStreamID(channel, streamID string) (*VOD, error) {
	if err := c.ensureAccessToken(context.Background()); err != nil {
		if c.metrics != nil {
			c.metrics.RecordAPICall(false, 0)
		}
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	user, err := c.GetUser(context.Background(), channel)
	if err != nil {
		log.Debugf("GetVODByChannelAndStreamID failed to get user for channel %s: %v", channel, err)
		return nil, fmt.Errorf("failed to get user_id for channel %s: %w", channel, err)
	}

	c.rateLimiter.Wait()

	var response struct {
		Data []VOD `json:"data"`
	}

	resp, err := c.httpClient.R().
		SetHeader("Client-ID", c.getClientID()).
		SetHeader("Authorization", "Bearer "+c.getAccessToken()).
		SetQueryParams(map[string]string{
			"user_id": user.ID,
			"type":    "archive",
			"first":   "1",
		}).
		SetResult(&response).
		Get(TwitchAPIBase + "/videos")

	if err != nil {
		log.Debugf("GetVODByChannelAndStreamID API error for channel %s: %v", channel, err)
		return nil, fmt.Errorf("failed to fetch videos: %w", err)
	}

	log.Debugf("GetVODByChannelAndStreamID response for channel %s stream_id %s: status=%d, body=%s", channel, streamID, resp.StatusCode(), string(resp.Body()))

	if resp.IsError() {
		log.Debugf("GetVODByChannelAndStreamID error response: status=%d, body=%s", resp.StatusCode(), string(resp.Body()))
		return nil, &APIError{StatusCode: resp.StatusCode(), Body: string(resp.Body())}
	}

	if len(response.Data) == 0 {
		log.Debugf("GetVODByChannelAndStreamID no videos found for channel %s", channel)
		return nil, fmt.Errorf("no archive videos found for channel %s", channel)
	}

	latestVOD := response.Data[0]
	log.Debugf("GetVODByChannelAndStreamID latest VOD for channel %s: vod_id=%s, vod_stream_id=%s, target_stream_id=%s", channel, latestVOD.ID, latestVOD.StreamID, streamID)

	if latestVOD.StreamID != streamID {
		log.Debugf("GetVODByChannelAndStreamID stream_id mismatch: expected %s, got %s", streamID, latestVOD.StreamID)
		return nil, fmt.Errorf("latest VOD for channel %s does not match stream_id %s (VOD has stream_id=%s)", channel, streamID, latestVOD.StreamID)
	}

	log.Debugf("GetVODByChannelAndStreamID found matching VOD: vod_id=%s", latestVOD.ID)
	return &latestVOD, nil
}

// FetchComments fetches the first page of chat comments for a VOD
func (c *Client) FetchComments(vodID string, offset int) (*ChatResponse, error) {
	body := fmt.Sprintf(`{
		"operationName": "VideoCommentsByOffsetOrCursor",
		"variables":{
			"videoID": "%s",
			"contentOffsetSeconds": %d
		},
		"extensions":{
			"persistedQuery":{
				"version": 1,
				"sha256Hash": "b70a3591ff0f4e0313d126c6a1502d79a1c02baebb288227c582044aa76adf6a"
			}
		}
	}`, vodID, offset)

	return c.fetchChatComments(body)
}

// FetchNextComments fetches the next page of chat comments using a cursor
func (c *Client) FetchNextComments(vodID string, cursor string) (*ChatResponse, error) {
	body := fmt.Sprintf(`{
		"operationName": "VideoCommentsByOffsetOrCursor",
		"variables":{
			"videoID": "%s",
			"cursor": "%s"
		},
		"extensions":{
			"persistedQuery":{
				"version": 1,
				"sha256Hash": "b70a3591ff0f4e0313d126c6a1502d79a1c02baebb288227c582044aa76adf6a"
			}
		}
	}`, vodID, cursor)

	return c.fetchChatComments(body)
}

// fetchChatComments is the internal method that performs the GQL request for chat comments
func (c *Client) fetchChatComments(body string) (*ChatResponse, error) {
	req := c.httpClient.R().
		SetHeader("Client-ID", GQLClientId).
		SetHeader("Origin", "https://twitch.tv").
		SetHeader("Referer", "https://twitch.tv").
		SetHeader("Content-Type", "text/plain;charset=UTF-8").
		SetHeader("Accept", "*/*").
		SetBody([]byte(body))

	var response ChatResponse
	resp, err := req.Post(TwitchGQLAPI)

	if err != nil {
		if c.metrics != nil {
			c.metrics.RecordGQLCall(false)
		}
		return nil, fmt.Errorf("failed to fetch chat comments: %w", err)
	}

	if resp.IsError() {
		if c.metrics != nil {
			c.metrics.RecordGQLCall(false)
		}
		return nil, &APIError{StatusCode: resp.StatusCode(), Body: string(resp.Body())}
	}

	if c.metrics != nil {
		c.metrics.RecordGQLCall(true)
	}

	if err := json.Unmarshal(resp.Body(), &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal chat response: %w", err)
	}

	return &response, nil
}
