package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"twitch-recorder-go/internal/config"
	"twitch-recorder-go/internal/segment"
)

func TestConfigLoadAndSave(t *testing.T) {
	dir, err := os.MkdirTemp("", "integration-test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	configPath := filepath.Join(dir, "config.json")

	cfg := &config.Config{
		VodDirectory: "./recordings",
		Channels:     []string{"channel1", "channel2"},
	}
	cfg.Twitch.ClientID = "test_client_id"
	cfg.Twitch.ClientSecret = "test_client_secret"

	err = config.SaveConfig(cfg, configPath)
	require.NoError(t, err)

	loadedCfg, err := config.LoadConfig(configPath)
	require.NoError(t, err)

	assert.Equal(t, cfg.VodDirectory, loadedCfg.VodDirectory)
	assert.Equal(t, cfg.Channels, loadedCfg.Channels)
	assert.Equal(t, cfg.Twitch.ClientID, loadedCfg.Twitch.ClientID)
}

func TestSegmentDownloaderIntegration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "segment-integration-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	now := time.Now()
	sd := segment.NewSegmentDownloader("testchannel", now)

	sessionDir := sd.GetSessionDir()
	expectedDir := "testchannel_" + now.Format("2006-01-02_15-04-05")
	assert.Equal(t, expectedDir, sessionDir)

	seg1 := "http://example.com/segment1.ts"
	seg2 := "http://example.com/segment2.ts"

	added1 := sd.AddSegment(seg1)
	added2 := sd.AddSegment(seg2)
	added3 := sd.AddSegment(seg1)

	assert.True(t, added1)
	assert.True(t, added2)
	assert.False(t, added3)
}

func TestValidateConfigIntegration(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "validate-integration-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	err = segment.ValidateConfig(tempDir, []string{"valid_channel"})
	assert.NoError(t, err)

	nonExistentDir := filepath.Join(tempDir, "new_dir")
	err = segment.ValidateConfig(nonExistentDir, []string{"valid_channel"})
	assert.NoError(t, err)

	_, err = os.Stat(nonExistentDir)
	assert.NoError(t, err)

	err = segment.ValidateConfig(tempDir, []string{})
	assert.Error(t, err)

	err = segment.ValidateConfig(tempDir, []string{"invalid@channel"})
	assert.Error(t, err)
}

func TestConfigWithEnvOverrides(t *testing.T) {
	os.Setenv("TWITCH_CLIENT_ID", "env_client_id")
	os.Setenv("TWITCH_CLIENT_SECRET", "env_client_secret")
	defer func() {
		os.Unsetenv("TWITCH_CLIENT_ID")
		os.Unsetenv("TWITCH_CLIENT_SECRET")
	}()

	assert.Equal(t, "env_client_id", config.GetTwitchClientID())
	assert.Equal(t, "env_client_secret", config.GetTwitchClientSecret())
}

func TestSegmentRecoveryScenario(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "recovery-test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	channelDir := filepath.Join(tempDir, "testchannel")
	err = os.MkdirAll(channelDir, 0755)
	require.NoError(t, err)

	sessionDir := filepath.Join(channelDir, "testchannel_2026-03-19_14-30-00")
	err = os.MkdirAll(sessionDir, 0755)
	require.NoError(t, err)

	testFile := filepath.Join(sessionDir, "00001.ts")
	err = os.WriteFile(testFile, []byte("test segment data"), 0644)
	require.NoError(t, err)

	files, err := os.ReadDir(channelDir)
	require.NoError(t, err)

	hasSessionDir := false
	for _, f := range files {
		if f.IsDir() && len(f.Name()) > 10 {
			hasSessionDir = true
			break
		}
	}

	assert.True(t, hasSessionDir)
	assert.FileExists(t, testFile)
}

func TestFullConfigWorkflow(t *testing.T) {
	dir, err := os.MkdirTemp("", "workflow-test")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	configPath := filepath.Join(dir, "config.json")
	vodDir := filepath.Join(dir, "recordings")

	cfg := &config.Config{
		VodDirectory: vodDir,
		Channels:     []string{"test_channel"},
	}
	cfg.Twitch.ClientID = "client_id"

	err = config.SaveConfig(cfg, configPath)
	require.NoError(t, err)

	err = segment.ValidateConfig(vodDir, cfg.Channels)
	assert.NoError(t, err)

	loadedCfg, err := config.LoadConfig(configPath)
	require.NoError(t, err)

	assert.Equal(t, vodDir, loadedCfg.VodDirectory)
	assert.Len(t, loadedCfg.Channels, 1)
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sd := segment.NewSegmentDownloader("test", time.Now())

	err := sd.DownloadSegment(ctx, "http://example.com/segment.ts")

	assert.Error(t, err)
}
