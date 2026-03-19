package metrics_test

import (
	"sync"
	"testing"
	"time"

	"twitch-recorder-go/internal/metrics"
)

func TestNewMetrics(t *testing.T) {
	m := metrics.NewMetrics()
	if m == nil {
		t.Fatal("expected non-nil metrics")
	}
}

func TestRecordSegmentDownload(t *testing.T) {
	m := metrics.NewMetrics()

	m.RecordSegmentDownload(1024*1024, 5*time.Second) // 1MB

	stats := m.GetStats()
	if stats.SegmentsDownloaded != 1 {
		t.Errorf("expected 1 segment downloaded, got %d", stats.SegmentsDownloaded)
	}
	if stats.BytesDownloaded != 1024*1024 {
		t.Errorf("expected 1MB downloaded, got %d", stats.BytesDownloaded)
	}
}

func TestRecordSegmentFailure(t *testing.T) {
	m := metrics.NewMetrics()

	m.RecordSegmentFailure("timeout")
	m.RecordSegmentFailure("network_error")
	m.RecordSegmentFailure("timeout")

	stats := m.GetStats()
	if stats.SegmentsFailed != 3 {
		t.Errorf("expected 3 failed segments, got %d", stats.SegmentsFailed)
	}
}

func TestRecordAPICall(t *testing.T) {
	m := metrics.NewMetrics()

	m.RecordAPICall(true, 10)
	m.RecordAPICall(false, 5)
	m.RecordAPICall(true, 10)

	stats := m.GetStats()
	if stats.APICallsTotal != 3 {
		t.Errorf("expected 3 API calls, got %d", stats.APICallsTotal)
	}
	if stats.APICallsFailed != 1 {
		t.Errorf("expected 1 failed API call, got %d", stats.APICallsFailed)
	}
	if stats.APIQuotaUsed != 25 {
		t.Errorf("expected 25 quota used, got %d", stats.APIQuotaUsed)
	}
}

func TestRecordRecordingLifecycle(t *testing.T) {
	m := metrics.NewMetrics()

	m.RecordRecordingStart()
	m.RecordRecordingComplete(30 * time.Minute)

	m.RecordRecordingStart()
	m.RecordRecordingFailure()

	m.RecordRecordingStart()
	m.RecordRecordingComplete(45 * time.Minute)

	stats := m.GetStats()
	if stats.RecordingsStarted != 3 {
		t.Errorf("expected 3 recordings started, got %d", stats.RecordingsStarted)
	}
	if stats.RecordingsCompleted != 2 {
		t.Errorf("expected 2 recordings completed, got %d", stats.RecordingsCompleted)
	}
	if stats.RecordingsFailed != 1 {
		t.Errorf("expected 1 recording failed, got %d", stats.RecordingsFailed)
	}
	if stats.TotalRecordingDuration != 75*time.Minute {
		t.Errorf("expected 75min total duration, got %v", stats.TotalRecordingDuration)
	}
}

func TestRecordStreamCheck(t *testing.T) {
	m := metrics.NewMetrics()

	m.RecordStreamCheck(true)
	m.RecordStreamCheck(false)
	m.RecordStreamCheck(true)
	m.RecordStreamCheck(true)
	m.RecordStreamCheck(false)

	stats := m.GetStats()
	if stats.StreamsChecked != 5 {
		t.Errorf("expected 5 streams checked, got %d", stats.StreamsChecked)
	}
	if stats.StreamsOnline != 3 {
		t.Errorf("expected 3 streams online, got %d", stats.StreamsOnline)
	}
	if stats.StreamsOffline != 2 {
		t.Errorf("expected 2 streams offline, got %d", stats.StreamsOffline)
	}
}

func TestGetStatsSuccessRate(t *testing.T) {
	m := metrics.NewMetrics()

	for i := 0; i < 90; i++ {
		m.RecordSegmentDownload(1024, time.Second)
	}

	for i := 0; i < 10; i++ {
		m.RecordSegmentFailure("error")
	}

	stats := m.GetStats()
	expectedRate := 90.0
	if stats.DownloadSuccessRate != expectedRate {
		t.Fatalf("expected %.1f%% success rate, got %.2f", expectedRate, stats.DownloadSuccessRate)
	}
}

func TestGetStatsAverageDuration(t *testing.T) {
	m := metrics.NewMetrics()

	m.RecordSegmentDownload(1024, 2*time.Second)
	m.RecordSegmentDownload(1024, 4*time.Second)
	m.RecordSegmentDownload(1024, 6*time.Second)

	stats := m.GetStats()
	if stats.AvgDownloadDuration != 4*time.Second {
		t.Errorf("expected 4s avg duration, got %v", stats.AvgDownloadDuration)
	}
}

func TestReset(t *testing.T) {
	m := metrics.NewMetrics()

	m.RecordSegmentDownload(1024, time.Second)
	m.RecordAPICall(true, 10)

	stats := m.GetStats()
	if stats.SegmentsDownloaded != 1 {
		t.Fatal("should have 1 segment before reset")
	}

	m.Reset()

	stats = m.GetStats()
	if stats.SegmentsDownloaded != 0 {
		t.Errorf("expected 0 segments after reset, got %d", stats.SegmentsDownloaded)
	}
	if stats.APICallsTotal != 0 {
		t.Errorf("expected 0 API calls after reset, got %d", stats.APICallsTotal)
	}
}

func TestGetUptime(t *testing.T) {
	m := metrics.NewMetrics()
	time.Sleep(10 * time.Millisecond)

	uptime := m.GetUptime()
	if uptime < 10*time.Millisecond {
		t.Errorf("expected uptime >= 10ms, got %v", uptime)
	}
}

func TestConcurrentAccess(t *testing.T) {
	m := metrics.NewMetrics()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(4)

		go func() {
			defer wg.Done()
			m.RecordSegmentDownload(1024, time.Second)
		}()

		go func() {
			defer wg.Done()
			m.RecordSegmentFailure("error")
		}()

		go func() {
			defer wg.Done()
			m.RecordAPICall(true, 5)
		}()

		go func() {
			defer wg.Done()
			_ = m.GetStats()
		}()
	}

	wg.Wait()

	stats := m.GetStats()
	if stats.SegmentsDownloaded != 100 {
		t.Errorf("expected 100 segments downloaded, got %d", stats.SegmentsDownloaded)
	}
	if stats.SegmentsFailed != 100 {
		t.Errorf("expected 100 segments failed, got %d", stats.SegmentsFailed)
	}
	if stats.APICallsTotal != 100 {
		t.Errorf("expected 100 API calls, got %d", stats.APICallsTotal)
	}
}

func TestStatsZeroValues(t *testing.T) {
	m := metrics.NewMetrics()
	stats := m.GetStats()

	if stats.SegmentsDownloaded != 0 {
		t.Errorf("expected 0 segments downloaded, got %d", stats.SegmentsDownloaded)
	}
	if stats.DownloadSuccessRate != 0.0 {
		t.Fatalf("expected 0.0 success rate, got %.2f", stats.DownloadSuccessRate)
	}
	if stats.AvgDownloadDuration != 0 {
		t.Errorf("expected 0 avg duration, got %v", stats.AvgDownloadDuration)
	}
}
