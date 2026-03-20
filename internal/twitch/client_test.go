package twitch

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewClient(t *testing.T) {
	client := NewClient("test_id", "test_secret", "test_oauth", nil)

	assert.Equal(t, "test_id", client.clientID)
	assert.Equal(t, "test_secret", client.clientSecret)
	assert.Equal(t, "test_oauth", client.oauthKey)
}

func TestCheckAccessToken(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt time.Time
		expect    bool
	}{
		{
			name:      "token valid",
			expiresAt: time.Now().Add(time.Hour),
			expect:    true,
		},
		{
			name:      "token expired",
			expiresAt: time.Now().Add(-time.Hour),
			expect:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient("test_id", "test_secret", "test_oauth", nil)
			client.expiresAt = tt.expiresAt

			result := client.CheckAccessToken()
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestGetClientID(t *testing.T) {
	tests := []struct {
		name     string
		clientID string
		expectID string
	}{
		{
			name:     "custom client id",
			clientID: "custom_id",
			expectID: "custom_id",
		},
		{
			name:     "empty client id",
			clientID: "",
			expectID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.clientID, "test_secret", "test_oauth", nil)
			assert.Equal(t, tt.expectID, client.getClientID())
		})
	}
}

func TestAPIError(t *testing.T) {
	err := &APIError{StatusCode: 401, Body: "Unauthorized"}
	assert.Contains(t, err.Error(), "twitch API error")
}

func TestUserStructure(t *testing.T) {
	user := &User{
		ID:    "123",
		Login: "testuser",
	}

	assert.Equal(t, "123", user.ID)
	assert.Equal(t, "testuser", user.Login)
}

func TestStreamsStructure(t *testing.T) {
	streams := &Streams{
		Data: []struct {
			ID        string `json:"id"`
			UserID    string `json:"user_id"`
			UserName  string `json:"user_name"`
			Type      string `json:"type"`
			StartedAt string `json:"started_at"`
		}{
			{
				ID:        "1",
				UserID:    "123",
				UserName:  "testuser",
				Type:      "live",
				StartedAt: "2026-03-19T12:00:00Z",
			},
		},
	}

	assert.Len(t, streams.Data, 1)
	assert.Equal(t, "testuser", streams.Data[0].UserName)
	assert.Equal(t, "live", streams.Data[0].Type)
}

func TestTwitchTokenStructure(t *testing.T) {
	token := &TwitchToken{
		AccessToken: "test_token",
		ExpiresIn:   3600,
		TokenType:   "bearer",
	}

	assert.Equal(t, "test_token", token.AccessToken)
	assert.Equal(t, 3600, token.ExpiresIn)
	assert.Equal(t, "bearer", token.TokenType)
}

func TestConcurrentTokenCacheAccess(t *testing.T) {
	client := NewClient("test_id", "test_secret", "test_oauth", nil)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(3)

		go func(ch string) {
			defer wg.Done()
			client.tokenCacheMu.Lock()
			client.tokenCache[ch] = &CachedToken{
				Value:     "test_token",
				ExpiresAt: time.Now().Add(time.Hour),
			}
			client.tokenCacheMu.Unlock()
		}("channel_" + string(rune('a'+i)))

		go func(ch string) {
			defer wg.Done()
			client.tokenCacheMu.RLock()
			_, ok := client.tokenCache[ch]
			client.tokenCacheMu.RUnlock()
			_ = ok
		}("channel_" + string(rune('a'+i)))

		go func(ch string) {
			defer wg.Done()
			client.tokenCacheMu.Lock()
			delete(client.tokenCache, ch)
			client.tokenCacheMu.Unlock()
		}("channel_" + string(rune('a'+i)))
	}

	wg.Wait()
	assert.NotNil(t, client.tokenCache)
}

func TestConcurrentTokenRefresh(t *testing.T) {
	client := NewClient("test_id", "test_secret", "test_oauth", nil)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.mu.Lock()
			wasRefreshing := client.isRefreshingToken
			if !wasRefreshing {
				client.isRefreshingToken = true
			}
			client.mu.Unlock()

			time.Sleep(1 * time.Millisecond)

			client.mu.Lock()
			if wasRefreshing {
				client.isRefreshingToken = false
			}
			client.mu.Unlock()
		}()
	}

	wg.Wait()
	assert.False(t, client.isRefreshingToken)
}

func TestErrUserNotFound(t *testing.T) {
	err := fmt.Errorf("%w: testuser", ErrUserNotFound)
	assert.ErrorIs(t, err, ErrUserNotFound)
	assert.Contains(t, err.Error(), "testuser")
}

func TestErrStreamNotFound(t *testing.T) {
	err := fmt.Errorf("%w: testuser", ErrStreamNotFound)
	assert.ErrorIs(t, err, ErrStreamNotFound)
	assert.Contains(t, err.Error(), "testuser")
}
