package twitch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"twitch-recorder-go/internal/log"
	"twitch-recorder-go/internal/metrics"
	"twitch-recorder-go/internal/ratelimit"

	"github.com/go-resty/resty/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/grafov/m3u8"
)

const (
	TwitchAPIBase   = "https://api.twitch.tv/helix"
	TwitchIDAPI     = "https://id.twitch.tv"
	TwitchGQLAPI    = "https://gql.twitch.tv/gql"
	TwitchUsherM3U8 = "https://usher.ttvnw.net"
	GQLClientId     = "kd1unb4b3q4t58fwlpcbzcbnm76a8fp"
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
	return &Client{
		httpClient:   httpClient,
		clientID:     clientID,
		clientSecret: clientSecret,
		oauthKey:     oauthKey,
		rateLimiter:  ratelimit.NewLimiter(150, 400*time.Millisecond),
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
		return nil, nil
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

	return &response, nil
}

func (c *Client) RefreshToken(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isRefreshingToken {
		return nil
	}

	c.isRefreshingToken = true
	defer func() { c.isRefreshingToken = false }()

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
		return nil, errors.New("failed to get live token sig")
	}

	return &response, nil
}

func (c *Client) GetCachedToken(ctx context.Context, channel string) (*CachedToken, error) {
	c.tokenCacheMu.RLock()
	cached, ok := c.tokenCache[channel]
	c.tokenCacheMu.RUnlock()

	if ok && time.Now().Before(cached.ExpiresAt) {
		return cached, nil
	}

	log.Infof("Fetching new token for channel %s", channel)
	tokenSig, err := c.GetLiveTokenSig(ctx, channel)
	if err != nil {
		return nil, err
	}

	expiresAt, err := extractTokenExpiration(tokenSig.Data.StreamPlaybackAccessToken.Value)
	if err != nil {
		log.Warnf("Failed to parse token expiration, using default 4min: %v", err)
		expiresAt = time.Now().Add(4 * time.Minute)
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
	token, _, err := jwt.NewParser().ParseUnverified(tokenValue, jwt.MapClaims{})
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse JWT: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return time.Time{}, errors.New("invalid token claims")
	}

	exp, ok := claims["exp"].(float64)
	if !ok {
		return time.Time{}, errors.New("exp claim not found or invalid type")
	}

	return time.Unix(int64(exp), 0), nil
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
