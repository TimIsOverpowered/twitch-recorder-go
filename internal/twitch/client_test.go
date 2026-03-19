package twitch

import (
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
