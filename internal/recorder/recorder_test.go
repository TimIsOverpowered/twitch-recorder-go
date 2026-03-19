package recorder

import (
	"context"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/assert"
	"twitch-recorder-go/internal/config"
	"twitch-recorder-go/internal/twitch"
)

func TestNewRecorder(t *testing.T) {
	client := twitch.NewClient("test_id", "test_secret", "test_oauth", nil)
	cfg := &config.Config{}
	recorder := NewRecorder(client, "test_channel", cfg, false)

	assert.NotNil(t, recorder)
	assert.Equal(t, "test_channel", recorder.channel)
}

func TestMonitorChannelCancellation(t *testing.T) {
	httpClient := resty.New().SetTimeout(time.Second)
	client := twitch.NewClient("test_id", "test_secret", "test_oauth", httpClient)
	ctx, cancel := context.WithCancel(context.Background())

	cfg := &config.Config{}
	recorder := NewRecorder(client, "test_channel", cfg, false)

	done := make(chan error)
	go func() {
		done <- recorder.MonitorChannel(ctx)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

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
	recorder := NewRecorder(client, "test_channel", cfg, false)

	assert.NotNil(t, recorder.twitchClient)
	assert.Equal(t, "test_channel", recorder.channel)
}
