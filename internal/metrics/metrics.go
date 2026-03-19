package metrics

import (
	"sync"
	"time"
)

type Metrics struct {
	mu sync.Mutex

	// Download metrics
	segmentsDownloaded int64
	segmentsFailed     int64
	bytesDownloaded    int64
	downloadErrors     map[string]int64

	// API metrics
	apiCallsTotal   int64
	apiCallsFailed  int64
	apiQuotaUsed    int64
	lastAPICallTime time.Time

	// GQL metrics
	gqlCallsTotal  int64
	gqlCallsFailed int64

	// Archive API metrics
	archiveAPICallsTotal   int64
	archiveAPICallsFailed  int64
	archiveAPILastCallTime time.Time

	// Drive upload metrics
	driveUploadsTotal   int64
	driveUploadsFailed  int64
	driveBytesUploaded  int64
	driveLastUploadTime time.Time

	// Recording metrics
	recordingsStarted      int64
	recordingsCompleted    int64
	recordingsFailed       int64
	totalRecordingDuration time.Duration

	// Stream monitoring metrics
	streamsChecked int64
	streamsOnline  int64
	streamsOffline int64

	// Timing metrics
	avgDownloadDuration time.Duration
	downloadDurations   []time.Duration

	startTime time.Time
}

func NewMetrics() *Metrics {
	return &Metrics{
		downloadErrors:    make(map[string]int64),
		startTime:         time.Now(),
		downloadDurations: make([]time.Duration, 0),
	}
}

func (m *Metrics) RecordSegmentDownload(size int64, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.segmentsDownloaded++
	m.bytesDownloaded += size
	m.downloadDurations = append(m.downloadDurations, duration)
}

func (m *Metrics) RecordSegmentFailure(errorType string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.segmentsFailed++
	m.downloadErrors[errorType]++
}

func (m *Metrics) RecordAPICall(success bool, quotaUsed int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.apiCallsTotal++
	m.lastAPICallTime = time.Now()

	if !success {
		m.apiCallsFailed++
	}

	if quotaUsed > 0 {
		m.apiQuotaUsed += int64(quotaUsed)
	}
}

func (m *Metrics) RecordRecordingStart() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recordingsStarted++
}

func (m *Metrics) RecordRecordingComplete(duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.recordingsCompleted++
	m.totalRecordingDuration += duration
}

func (m *Metrics) RecordRecordingFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recordingsFailed++
}

func (m *Metrics) RecordStreamCheck(online bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.streamsChecked++
	if online {
		m.streamsOnline++
	} else {
		m.streamsOffline++
	}
}

func (m *Metrics) RecordGQLCall(success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.gqlCallsTotal++
	if !success {
		m.gqlCallsFailed++
	}
}

func (m *Metrics) RecordArchiveAPICall(success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.archiveAPICallsTotal++
	m.archiveAPILastCallTime = time.Now()
	if !success {
		m.archiveAPICallsFailed++
	}
}

func (m *Metrics) RecordDriveUpload(size int64, success bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.driveUploadsTotal++
	m.driveLastUploadTime = time.Now()

	if success {
		m.driveBytesUploaded += size
	} else {
		m.driveUploadsFailed++
	}
}

type Stats struct {
	// Download stats
	SegmentsDownloaded  int64         `json:"segments_downloaded"`
	SegmentsFailed      int64         `json:"segments_failed"`
	BytesDownloaded     int64         `json:"bytes_downloaded"`
	DownloadSuccessRate float64       `json:"download_success_rate"`
	AvgDownloadDuration time.Duration `json:"avg_download_duration"`

	// API stats
	APICallsTotal   int64     `json:"api_calls_total"`
	APICallsFailed  int64     `json:"api_calls_failed"`
	APIQuotaUsed    int64     `json:"api_quota_used"`
	LastAPICallTime time.Time `json:"last_api_call_time"`

	// GQL stats
	GQLCallsTotal  int64 `json:"gql_calls_total"`
	GQLCallsFailed int64 `json:"gql_calls_failed"`

	// Archive API stats
	ArchiveAPICallsTotal   int64     `json:"archive_api_calls_total"`
	ArchiveAPICallsFailed  int64     `json:"archive_api_calls_failed"`
	ArchiveAPILastCallTime time.Time `json:"archive_api_last_call_time"`

	// Drive upload stats
	DriveUploadsTotal   int64     `json:"drive_uploads_total"`
	DriveUploadsFailed  int64     `json:"drive_uploads_failed"`
	DriveBytesUploaded  int64     `json:"drive_bytes_uploaded"`
	DriveLastUploadTime time.Time `json:"drive_last_upload_time"`

	// Recording stats
	RecordingsStarted      int64         `json:"recordings_started"`
	RecordingsCompleted    int64         `json:"recordings_completed"`
	RecordingsFailed       int64         `json:"recordings_failed"`
	TotalRecordingDuration time.Duration `json:"total_recording_duration"`

	// Stream stats
	StreamsChecked int64 `json:"streams_checked"`
	StreamsOnline  int64 `json:"streams_online"`
	StreamsOffline int64 `json:"streams_offline"`

	// Runtime
	Uptime time.Duration `json:"uptime"`
}

func (m *Metrics) GetStats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()

	totalDownloads := m.segmentsDownloaded + m.segmentsFailed
	successRate := 0.0
	if totalDownloads > 0 {
		successRate = float64(m.segmentsDownloaded) / float64(totalDownloads) * 100
	}

	avgDuration := time.Duration(0)
	if len(m.downloadDurations) > 0 {
		totalDur := time.Duration(0)
		for _, d := range m.downloadDurations {
			totalDur += d
		}
		avgDuration = totalDur / time.Duration(len(m.downloadDurations))
	}

	return Stats{
		SegmentsDownloaded:     m.segmentsDownloaded,
		SegmentsFailed:         m.segmentsFailed,
		BytesDownloaded:        m.bytesDownloaded,
		DownloadSuccessRate:    successRate,
		AvgDownloadDuration:    avgDuration,
		APICallsTotal:          m.apiCallsTotal,
		APICallsFailed:         m.apiCallsFailed,
		APIQuotaUsed:           m.apiQuotaUsed,
		LastAPICallTime:        m.lastAPICallTime,
		GQLCallsTotal:          m.gqlCallsTotal,
		GQLCallsFailed:         m.gqlCallsFailed,
		ArchiveAPICallsTotal:   m.archiveAPICallsTotal,
		ArchiveAPICallsFailed:  m.archiveAPICallsFailed,
		ArchiveAPILastCallTime: m.archiveAPILastCallTime,
		DriveUploadsTotal:      m.driveUploadsTotal,
		DriveUploadsFailed:     m.driveUploadsFailed,
		DriveBytesUploaded:     m.driveBytesUploaded,
		DriveLastUploadTime:    m.driveLastUploadTime,
		RecordingsStarted:      m.recordingsStarted,
		RecordingsCompleted:    m.recordingsCompleted,
		RecordingsFailed:       m.recordingsFailed,
		TotalRecordingDuration: m.totalRecordingDuration,
		StreamsChecked:         m.streamsChecked,
		StreamsOnline:          m.streamsOnline,
		StreamsOffline:         m.streamsOffline,
		Uptime:                 time.Since(m.startTime),
	}
}

func (m *Metrics) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.segmentsDownloaded = 0
	m.segmentsFailed = 0
	m.bytesDownloaded = 0
	m.downloadErrors = make(map[string]int64)
	m.apiCallsTotal = 0
	m.apiCallsFailed = 0
	m.apiQuotaUsed = 0
	m.lastAPICallTime = time.Time{}
	m.gqlCallsTotal = 0
	m.gqlCallsFailed = 0
	m.archiveAPICallsTotal = 0
	m.archiveAPICallsFailed = 0
	m.archiveAPILastCallTime = time.Time{}
	m.driveUploadsTotal = 0
	m.driveUploadsFailed = 0
	m.driveBytesUploaded = 0
	m.driveLastUploadTime = time.Time{}
	m.recordingsStarted = 0
	m.recordingsCompleted = 0
	m.recordingsFailed = 0
	m.totalRecordingDuration = 0
	m.streamsChecked = 0
	m.streamsOnline = 0
	m.streamsOffline = 0
	m.downloadDurations = make([]time.Duration, 0)
	m.startTime = time.Now()
}

func (m *Metrics) GetUptime() time.Duration {
	return time.Since(m.startTime)
}
