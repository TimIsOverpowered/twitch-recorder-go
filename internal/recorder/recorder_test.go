package recorder

import (
	"context"
	"sync"
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

func TestShutdownCancelsFinalizeContexts(t *testing.T) {
	client := twitch.NewClient("test_id", "test_secret", "test_oauth", nil)
	cfg := &config.Config{}
	recorder := NewRecorder(client, "test_channel", cfg, false)

	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())

	recorder.finalizeMu.Lock()
	recorder.finalizeCancels = append(recorder.finalizeCancels, cancel1, cancel2)
	recorder.finalizeMu.Unlock()

	recorder.Shutdown()

	done1 := make(chan struct{})
	done2 := make(chan struct{})

	go func() {
		<-ctx1.Done()
		close(done1)
	}()

	go func() {
		<-ctx2.Done()
		close(done2)
	}()

	select {
	case <-done1:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ctx1 was not cancelled")
	}

	select {
	case <-done2:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ctx2 was not cancelled")
	}
}

func TestConcurrentFinalizeCancelAdd(t *testing.T) {
	client := twitch.NewClient("test_id", "test_secret", "test_oauth", nil)
	cfg := &config.Config{}
	recorder := NewRecorder(client, "test_channel", cfg, false)

	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(2)

		go func() {
			defer wg.Done()
			_, cancel := context.WithCancel(context.Background())
			recorder.finalizeMu.Lock()
			recorder.finalizeCancels = append(recorder.finalizeCancels, cancel)
			recorder.finalizeMu.Unlock()
			time.Sleep(1 * time.Millisecond)
			cancel()
		}()

		go func() {
			defer wg.Done()
			recorder.Shutdown()
		}()
	}

	wg.Wait()
}
