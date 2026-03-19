# Twitch Recorder Go by OP

Record Twitch Live streams & Upload to Google Drive if needed

This program can monitor and record multiple Twitch streams live and convert it to .mp4 files.

## Quick Start

1. **Download** the latest release binary for your platform from [Releases](https://github.com/OP-Overpowered/twitch-recorder-go/releases)
2. **Run** the program - it will auto-generate a config template on first run
3. **Configure** your Twitch credentials in `config.json`
4. **Start recording** - run again and it will begin monitoring your channels

## Requirements

### Runtime

- [FFmpeg](https://ffmpeg.org/download.html) - Required for video recording

### Building from Source

- Go 1.25+
- Git

## Setup Guide

### Step 1: Get Twitch API Credentials

1. Visit https://dev.twitch.tv/console
2. Click "Register Your Application"
3. Fill in:
   - **OAuth Client ID**: Your app name
   - **OAuth Redirect URLs**: `http://localhost:8080/callback`
   - **Category**: Select any category
4. Click "Register"
5. Copy your **Client ID** and click "New Secret" to reveal your **Client Secret**

### Step 2: Get Twitch OAuth Key (Optional - Recommended)

The OAuth key bypasses ads if you have Twitch Turbo and enables higher quality streams.

1. Log in to https://twitch.tv in your browser
2. Open Developer Console (F12 or Ctrl+Shift+I / Cmd+Option+I on Mac)
3. Go to Console tab
4. Paste and run this command:
   ```javascript
   document.cookie
     .split('; ')
     .find((item) => item.startsWith('auth-token='))
     ?.split('=')[1];
   ```
5. Copy the 30-character result
6. Add to config.json as `"oauth_key"`

⚠️ **Security Warning**: This token grants full account access - keep it secret!  
To revoke: Change password or visit https://www.twitch.tv/settings/security

### Step 3: Configure Recording Directory

Update `vod_directory` in config.json to your preferred path:

- Windows: `"vod_directory": "C:\\Users\\YourName\\TwitchRecordings"`
- Mac/Linux: `"vod_directory": "/home/yourname/twitch-recordings"`

### Step 4: Add Channels to Monitor

Replace the example channel with Twitch channels you want to record:

```json
"channels": ["channel1", "channel2", "channel3"]
```

### Step 5: Google Drive Upload (Optional)

To enable automatic uploads to Google Drive:

1. Visit https://developers.google.com/drive/api/v3/enable-drive-api
2. Create a new project and enable Drive API v3
3. Go to Credentials → Create OAuth 2.0 Client ID
4. Use https://developers.google.com/oauthplayground/:
   - Click gear icon ⚙️ → Check "Use your own OAuth credentials"
   - Enter your Client ID and Secret
   - Select scopes: `Drive API v3` → `drive`, `drive.file`, `drive.metadata`
   - Authorize and exchange for tokens
5. Copy `refresh_token` and `access_token` to config.json
6. Run with `-drive` flag to enable uploads

## Configuration

The program auto-generates `config.json` on first run. Update these fields:

```json
{
  "twitch": {
    "client_id": "YOUR_TWITCH_CLIENT_ID",
    "client_secret": "YOUR_TWITCH_CLIENT_SECRET",
    "oauth_key": ""
  },
  "vod_directory": "./recordings",
  "channels": ["example_channel"],
  "twitch_app": {},
  "drive": {
    "refresh_token": "",
    "access_token": ""
  },
  "google": {
    "client_id": "",
    "client_secret": "",
    "scopes": [
      "https://www.googleapis.com/auth/drive",
      "https://www.googleapis.com/auth/drive.appdata",
      "https://www.googleapis.com/auth/drive.file",
      "https://www.googleapis.com/auth/drive.metadata"
    ],
    "endpoint": {
      "token_url": "https://oauth2.googleapis.com/token"
    }
  },
  "archive": {
    "enabled": false,
    "endpoint": "", // Supports {channel} placeholder
    "key": ""
  }
}
```

### Config Fields

| Field                  | Required | Description                                |
| ---------------------- | -------- | ------------------------------------------ |
| `twitch.client_id`     | Yes      | From Twitch Developer Console              |
| `twitch.client_secret` | Yes      | From Twitch Developer Console              |
| `twitch.oauth_key`     | No       | Browser auth token (bypass ads with Turbo) |
| `vod_directory`        | Yes      | Where to save recorded videos              |
| `channels`             | Yes      | Array of Twitch channel names to monitor   |
| `drive.refresh_token`  | No\*     | Google Drive refresh token                 |
| `drive.access_token`   | No\*     | Google Drive access token                  |
| `google.client_id`     | No\*     | Google OAuth Client ID                     |
| `google.client_secret` | No\*     | Google OAuth Client Secret                 |

\*Required only if using `-drive` flag

## Running the Program

```bash
./twitch-recorder-go -config ./config.json
```

### Options

- `-config` - Path to config file (default: ./config.json)
- `-drive` - Enable Google Drive upload (requires drive credentials in config)

## Build from Source

1. Install [Go 1.25+](https://golang.org/dl/)
2. Clone the repository:
   ```bash
   git clone https://github.com/OP-Overpowered/twitch-recorder-go.git
   cd twitch-recorder-go
   ```
3. Build:
   ```bash
   go build
   ```

## v2.0.0 Breaking Changes

- ❌ **Removed streamlink dependency** - FFmpeg is now the only download method
- ❌ **Removed `-ffmpeg` flag** - FFmpeg is always used
- ✅ **Auto-generates config.json** on first run with setup instructions
- ⚠️ **Requires Go 1.25+** for building from source
