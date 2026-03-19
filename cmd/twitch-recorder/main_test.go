package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"twitch-recorder-go/internal/config"
)

func TestLoadConfigExists(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "main-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "config.json")
	cfg := &config.Config{
		VodDirectory: "./recordings",
		Channels:     []string{"test"},
	}
	err = config.SaveConfig(cfg, configPath)
	require.NoError(t, err)

	loadedCfg, err := loadConfig(configPath)
	assert.NoError(t, err)
	assert.NotNil(t, loadedCfg)
}

func TestOverrideWithEnv(t *testing.T) {
	os.Setenv("TWITCH_CLIENT_ID", "env_client_id")
	defer os.Unsetenv("TWITCH_CLIENT_ID")

	result := overrideWithEnv("file_value", config.GetTwitchClientID())
	assert.Equal(t, "env_client_id", result)
}

func TestOverrideWithEnvEmpty(t *testing.T) {
	os.Unsetenv("TWITCH_CLIENT_ID")

	result := overrideWithEnv("file_value", config.GetTwitchClientID())
	assert.Equal(t, "file_value", result)
}

func TestGenerateDefaultConfig(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "main-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "config.json")

	err = generateDefaultConfig(configPath)
	assert.NoError(t, err)
	assert.FileExists(t, configPath)

	data, err := os.ReadFile(configPath)
	assert.NoError(t, err)
	assert.Contains(t, string(data), "vod_directory")
}
