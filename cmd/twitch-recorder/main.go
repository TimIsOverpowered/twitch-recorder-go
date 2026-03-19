package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/go-resty/resty/v2"
	"twitch-recorder-go/internal/config"
	"twitch-recorder-go/internal/log"
	"twitch-recorder-go/internal/metrics"
	"twitch-recorder-go/internal/recorder"
	"twitch-recorder-go/internal/segment"
	"twitch-recorder-go/internal/twitch"
)

var (
	cfgPath           string
	uploadToDrive     bool
	httpClient        *resty.Client
	isRefreshingToken bool
	tokenMu           sync.Mutex
)

func init() {
	httpClient = resty.New().
		SetTimeout(30 * time.Second).
		SetRetryCount(3).
		SetRetryWaitTime(time.Second).
		SetRetryAfter(func(c *resty.Client, r *resty.Response) (time.Duration, error) {
			if r.StatusCode() >= 429 && r.StatusCode() < 500 {
				retryAfter := r.Header().Get("Retry-After")
				if retryAfter != "" {
					if seconds, err := strconv.Atoi(retryAfter); err == nil {
						return time.Duration(seconds) * time.Second, nil
					}
				}
			}
			return 0, nil
		})
}

func main() {
	log.Init()

	flag.BoolVar(&uploadToDrive, "drive", false, "Upload recordings to Google Drive")
	flag.StringVar(&cfgPath, "config", "config.json", "Path to config file")
	flag.Parse()

	c, err := loadConfig(cfgPath)
	if err != nil {
		log.Error("Failed to load config: %v", err)
		os.Exit(1)
	}

	if err := segment.ValidateConfig(c.VodDirectory, c.Channels); err != nil {
		log.Error("Invalid configuration: %v", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	segment.RecoverIncompleteSessions(c.VodDirectory, c.Channels)

	m := metrics.NewMetrics()

	twitchClient := createTwitchClient(c)
	twitchClient.SetMetrics(m)

	var wg sync.WaitGroup
	for _, channel := range c.Channels {
		user, err := twitchClient.GetUser(ctx, channel)
		if err != nil {
			log.Warn("Error checking user %s: %v", channel, err)
			continue
		}
		if user == nil {
			log.Info("%s does not exist", channel)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		wg.Add(1)
		go func(ch string) {
			defer wg.Done()
			rec := recorder.NewRecorder(twitchClient, ch, c)
			rec.SetMetrics(m)
			rec.MonitorChannel(ctx)
		}(channel)
	}

	go func() {
		wg.Wait()
		cancel()
	}()

	<-ctx.Done()
	printMetrics(m)
	log.Info("Shutting down gracefully...")
}

func loadConfig(configPath string) (*config.Config, error) {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := generateDefaultConfig(configPath); err != nil {
			return nil, err
		}
		log.Info("Configuration file created. Please edit it and run again.")
		os.Exit(0)
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	cfg.Twitch.ClientID = overrideWithEnv(cfg.Twitch.ClientID, config.GetTwitchClientID())
	cfg.Twitch.ClientSecret = overrideWithEnv(cfg.Twitch.ClientSecret, config.GetTwitchClientSecret())

	return cfg, nil
}

func createTwitchClient(c *config.Config) *twitch.Client {
	clientID := c.Twitch.ClientID
	if clientID == "" {
		clientID = config.GetTwitchClientID()
	}

	clientSecret := c.Twitch.ClientSecret
	if clientSecret == "" {
		clientSecret = config.GetTwitchClientSecret()
	}

	return twitch.NewClient(clientID, clientSecret, c.Twitch.OAuthKey, httpClient)
}

func overrideWithEnv(cfgVal, envVal string) string {
	if envVal != "" {
		return envVal
	}
	return cfgVal
}

func printMetrics(m *metrics.Metrics) {
	stats := m.GetStats()
	log.Info("========================================")
	log.Info("METRICS SUMMARY")
	log.Info("========================================")
	log.Info("Uptime: %v", stats.Uptime)
	log.Info("")
	log.Info("DOWNLOAD STATS:")
	log.Info("  Segments Downloaded: %d", stats.SegmentsDownloaded)
	log.Info("  Segments Failed: %d", stats.SegmentsFailed)
	log.Info("  Bytes Downloaded: %.2f MB", float64(stats.BytesDownloaded)/1024/1024)
	log.Info("  Success Rate: %.1f%%", stats.DownloadSuccessRate)
	log.Info("  Avg Download Duration: %v", stats.AvgDownloadDuration)
	log.Info("")
	log.Info("API STATS:")
	log.Info("  Total API Calls: %d", stats.APICallsTotal)
	log.Info("  Failed API Calls: %d", stats.APICallsFailed)
	log.Info("  API Quota Used: %d", stats.APIQuotaUsed)
	if !stats.LastAPICallTime.IsZero() {
		log.Info("  Last API Call: %v ago", time.Since(stats.LastAPICallTime))
	}
	log.Info("")
	log.Info("GQL STATS:")
	log.Info("  Total GQL Calls: %d", stats.GQLCallsTotal)
	log.Info("  Failed GQL Calls: %d", stats.GQLCallsFailed)
	log.Info("")
	log.Info("RECORDING STATS:")
	log.Info("  Recordings Started: %d", stats.RecordingsStarted)
	log.Info("  Recordings Completed: %d", stats.RecordingsCompleted)
	log.Info("  Recordings Failed: %d", stats.RecordingsFailed)
	log.Info("  Total Recording Duration: %v", stats.TotalRecordingDuration)
	log.Info("")
	log.Info("STREAM MONITORING:")
	log.Info("  Streams Checked: %d", stats.StreamsChecked)
	log.Info("  Streams Online: %d", stats.StreamsOnline)
	log.Info("  Streams Offline: %d", stats.StreamsOffline)
	log.Info("")
	log.Info("ARCHIVE API STATS:")
	log.Info("  Total API Posts: %d", stats.ArchiveAPICallsTotal)
	log.Info("  Failed API Posts: %d", stats.ArchiveAPICallsFailed)
	if !stats.ArchiveAPILastCallTime.IsZero() {
		log.Info("  Last API Post: %v ago", time.Since(stats.ArchiveAPILastCallTime))
	}
	log.Info("========================================")
}

func generateDefaultConfig(configPath string) error {
	defaultConfig := `{
  "twitch": {
    "client_id": "YOUR_TWITCH_CLIENT_ID",
    "client_secret": "YOUR_TWITCH_CLIENT_SECRET",
    "oauth_key": ""
  },
  "vod_directory": "./recordings",
  "channels": ["example_channel"],
  "twitch_app": {
    "access_token": "",
    "expires_in": 0,
    "token_type": ""
  },
  "drive": {
    "refresh_token": "",
    "access_token": "",
    "token_type": "",
    "expiry": "0001-01-01T00:00:00Z"
  },
  "google": {
    "client_id": "",
    "client_secret": "",
    "scopes": ["https://www.googleapis.com/auth/drive.file"],
    "endpoint": {
      "token_url": "https://oauth2.googleapis.com/token"
    }
  },
  "archive_api_enabled": false,
  // Supports {channel} placeholder, e.g.: "https://archive.overpowered.tv/{channel}/v2/live"
  "archive_api_endpoint": "",
  "archive_api_key": ""
}`

	setupInstructions := `

================================================================================
CONFIGURATION GUIDE - Twitch Recorder Go v2.0.0
================================================================================

STEP 1: Get Twitch API Credentials
-----------------------------------
1. Visit https://dev.twitch.tv/console
2. Create a new application (name it anything, e.g., "Twitch Recorder")
3. Set OAuth Redirect URL to: http://localhost:8080/callback
4. Copy your Client ID and Client Secret
5. Replace "YOUR_TWITCH_CLIENT_ID" and "YOUR_TWITCH_CLIENT_SECRET" in config.json

STEP 2: Get Twitch OAuth Key (Optional - Recommended for Turbo users)
----------------------------------------------------------------------
The OAuth key bypasses ads if you have Twitch Turbo and enables higher qualities.

To get your Twitch auth token:
1. Log in to https://twitch.tv in your browser
2. Open Developer Console (F12 or Ctrl+Shift+I / Cmd+Option+I on Mac)
3. Go to Console tab
4. Paste and run this command:
   document.cookie.split("; ").find(item=>item.startsWith("auth-token="))?.split("=")[1]
5. Copy the 30-character result
6. Add to config.json as "oauth_key" (leave empty if not using)

⚠️ This token grants full account access - keep it secret!
To revoke: Change password or visit https://www.twitch.tv/settings/security

STEP 3: Configure Recording Directory
--------------------------------------
Change "vod_directory": "./recordings" to your preferred path.
Example (Windows): "vod_directory": "C:\\Users\\YourName\\TwitchRecordings"
Example (Mac/Linux): "vod_directory": "/home/yourname/twitch-recordings"

STEP 4: Add Channels to Monitor
--------------------------------
Replace "example_channel" with the Twitch channel names you want to record.
You can add multiple channels:
"channels": ["channel1", "channel2", "channel3"]

STEP 5: Google Drive Upload (Optional)
---------------------------------------
To enable automatic uploads to Google Drive:
1. Visit https://developers.google.com/drive/api/v3/enable-drive-api
2. Create a new project and enable Drive API
3. Create OAuth 2.0 credentials
4. Use https://developers.google.com/oauthplayground/ to get tokens:
   - Select Drive API v3 scopes (drive, drive.file, drive.metadata)
   - Authorize and exchange for tokens
5. Fill in "client_id", "client_secret", "refresh_token", and "access_token"
6. Run with -drive flag to enable uploads

STEP 6: Archive API Integration (Optional)
-------------------------------------------
To enable automatic posting of recording metadata to your archive API:
1. Set "archive_api_enabled": true
2. Set "archive_api_endpoint" to your API URL
   - Supports {channel} placeholder in URL
   - Example with channel in path: "https://archive.overpowered.tv/{channel}/v2/live"
   - Example without channel in path: "https://api.xqc.wtf/v2/live"
3. Set "archive_api_key" to your authentication key

The recorder will automatically post the following after each successful recording:
- Channel name (in both URL if {channel} used, and in JSON body)
- Stream ID
- Local file path
- Recording duration
- File size
- Timestamp

API posts are asynchronous and won't block recording operations. Errors are logged but don't affect recording finalization.

================================================================================
After configuring, save this file and run twitch-recorder-go again!
================================================================================
`

	fullConfig := defaultConfig + setupInstructions
	return os.WriteFile(configPath, []byte(fullConfig), 0644)
}
