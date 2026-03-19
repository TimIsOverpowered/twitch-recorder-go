package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name        string
		configFile  string
		expectError bool
	}{
		{
			name:        "missing file",
			configFile:  "nonexistent.json",
			expectError: true,
		},
		{
			name:        "invalid json",
			configFile:  "invalid.json",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadConfig(tt.configFile)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, cfg)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, cfg)
			}
		})
	}
}

func TestSaveConfig(t *testing.T) {
	dir, err := os.MkdirTemp("", "config-test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	configPath := filepath.Join(dir, "config.json")

	cfg := &Config{
		VodDirectory: "./recordings",
		Channels:     []string{"test_channel"},
	}

	err = SaveConfig(cfg, configPath)
	assert.NoError(t, err)
	assert.FileExists(t, configPath)

	data, err := os.ReadFile(configPath)
	assert.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestGetEnvCredentials(t *testing.T) {
	os.Setenv("TWITCH_CLIENT_ID", "test_client_id")
	os.Setenv("TWITCH_CLIENT_SECRET", "test_client_secret")
	os.Setenv("GOOGLE_CLIENT_ID", "test_google_id")
	os.Setenv("GOOGLE_CLIENT_SECRET", "test_google_secret")

	defer func() {
		os.Unsetenv("TWITCH_CLIENT_ID")
		os.Unsetenv("TWITCH_CLIENT_SECRET")
		os.Unsetenv("GOOGLE_CLIENT_ID")
		os.Unsetenv("GOOGLE_CLIENT_SECRET")
	}()

	assert.Equal(t, "test_client_id", GetTwitchClientID())
	assert.Equal(t, "test_client_secret", GetTwitchClientSecret())
	assert.Equal(t, "test_google_id", GetGoogleClientID())
	assert.Equal(t, "test_google_secret", GetGoogleClientSecret())
}

func TestGetEnvCredentialsEmpty(t *testing.T) {
	os.Unsetenv("TWITCH_CLIENT_ID")
	os.Unsetenv("TWITCH_CLIENT_SECRET")
	os.Unsetenv("GOOGLE_CLIENT_ID")
	os.Unsetenv("GOOGLE_CLIENT_SECRET")

	assert.Equal(t, "", GetTwitchClientID())
	assert.Equal(t, "", GetTwitchClientSecret())
	assert.Equal(t, "", GetGoogleClientID())
	assert.Equal(t, "", GetGoogleClientSecret())
}

func TestConfigStructure(t *testing.T) {
	cfg := &Config{}
	cfg.VodDirectory = "./test"
	cfg.Channels = []string{"channel1", "channel2"}
	cfg.Twitch.ClientID = "client_id"
	cfg.TwitchToken.AccessToken = "access_token"
	cfg.TwitchToken.ExpiresIn = 3600

	assert.Equal(t, "./test", cfg.VodDirectory)
	assert.Len(t, cfg.Channels, 2)
	assert.Equal(t, "client_id", cfg.Twitch.ClientID)
	assert.Equal(t, "access_token", cfg.TwitchToken.AccessToken)
	assert.Equal(t, 3600, cfg.TwitchToken.ExpiresIn)
}
