package recorder

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"twitch-recorder-go/internal/config"
	"twitch-recorder-go/internal/twitch"
)

func TestNewRecorder(t *testing.T) {
	client := twitch.NewClient("test_id", "test_secret", "test_oauth", nil)
	cfg := &config.Config{}
	recorder := NewRecorder(client, "test_channel", cfg)

	assert.NotNil(t, recorder)
	assert.Equal(t, "test_channel", recorder.channel)
}

func TestMonitorChannelCancellation(t *testing.T) {
	client := twitch.NewClient("test_id", "test_secret", "test_oauth", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := &config.Config{}
	recorder := NewRecorder(client, "test_channel", cfg)

	done := make(chan error)
	go func() {
		done <- recorder.MonitorChannel(ctx)
	}()

	select {
	case err := <-done:
		assert.Error(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("MonitorChannel did not return on context cancellation")
	}
}

func TestRecorderStructure(t *testing.T) {
	client := twitch.NewClient("test_id", "test_secret", "test_oauth", nil)
	cfg := &config.Config{}
	recorder := NewRecorder(client, "test_channel", cfg)

	assert.NotNil(t, recorder.twitchClient)
	assert.Equal(t, "test_channel", recorder.channel)
}
