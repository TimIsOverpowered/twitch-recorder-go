package twitch

import (
	"context"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"twitch-recorder-go/internal/ratelimit"
)

const (
	TwitchAPIBase   = "https://api.twitch.tv/helix"
	TwitchIDAPI     = "https://id.twitch.tv"
	TwitchGQLAPI    = "https://gql.twitch.tv/gql"
	TwitchUsherM3U8 = "https://usher.ttvnw.net"
	DefaultClientID = "kd1unb4b3q4t58fwlpcbzcbnm76a8fp"
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
}

func NewClient(clientID, clientSecret, oauthKey string, httpClient *resty.Client) *Client {
	return &Client{
		httpClient:   httpClient,
		clientID:     clientID,
		clientSecret: clientSecret,
		oauthKey:     oauthKey,
		rateLimiter:  ratelimit.NewLimiter(150, 400*time.Millisecond),
	}
}

func (c *Client) SetRateLimit(maxTokens int, refillRate time.Duration) {
	if c.rateLimiter != nil {
		c.rateLimiter.SetMaxTokens(maxTokens)
		c.rateLimiter.SetRefillRate(refillRate)
	}
}

func (c *Client) GetUser(ctx context.Context, login string) (*User, error) {
	if err := c.ensureAccessToken(ctx); err != nil {
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
		return nil, err
	}

	if resp.IsError() {
		return nil, &APIError{StatusCode: resp.StatusCode(), Body: string(resp.Body())}
	}

	if len(response.Data) == 0 {
		return nil, nil
	}

	return &response.Data[0], nil
}

func (c *Client) GetStreams(ctx context.Context, userLogin string) (*Streams, error) {
	if err := c.ensureAccessToken(ctx); err != nil {
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
		return nil, err
	}

	if resp.IsError() {
		return nil, &APIError{StatusCode: resp.StatusCode(), Body: string(resp.Body())}
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
		return err
	}

	if resp.IsError() {
		return &APIError{StatusCode: resp.StatusCode(), Body: string(resp.Body())}
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
	if c.clientID != "" {
		return c.clientID
	}
	return DefaultClientID
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
