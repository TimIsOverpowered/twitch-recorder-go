# Twitch Recorder Go by OP

Record Twitch Live streams & Upload to Google Drive if needed

This program can monitor and record multiple Twitch streams live and convert it to .mp4 files.

## Requirements

[FFmpeg](https://ffmpeg.org/download.html) Required for video recording

[Twitch](https://dev.twitch.tv/console) Register a Twitch app

## Google Drive Upload

Optional: Upload recorded videos to Google Drive.

Use `-drive` flag to enable Drive upload (requires drive credentials in config).

[Drive](https://developers.google.com/drive/api/v3/enable-drive-api) Register a Google API APP with OAuth2 & Use Google Playground OAuth2 to get Access & Refresh tokens

[Playground](https://developers.google.com/oauthplayground/)

## Config

Create `config.json` file (use -config to specify the path to the config file)

```properties
{
 "twitch": {
  "client_id": "XXXXXXXXXX",
  "client_secret": "XXXXXXXXXX",
  "oauth_key": "XXXXXXXXXX"
 },
 "vod_directory": "C:\\Users\\Overpowered",
 "channels": [
  "pokelawls",
  "sodapoppin"
 ],
 "twitch_app": {},
 "drive": {
    "refresh_token": "XXXXXXXXXX",
    "access_token": "XXXXXXXXXX"
 },
 "google": {
  "client_id": "XXXXXXXXXX",
  "client_secret": "XXXXXXXXXX",
  "scopes": [
   "https://www.googleapis.com/auth/drive",
   "https://www.googleapis.com/auth/drive.appdata",
   "https://www.googleapis.com/auth/drive.file",
   "https://www.googleapis.com/auth/drive.metadata"
  ],
  "endpoint": {
   "token_url": "https://oauth2.googleapis.com/token"
  }
 }
}
```

Optional: Twitch OAuth Key (Used if you are using FFMPEG + twitch turbo to bypass ads), Drive, Google

## Running the program

Run in a shell like cmd or bash terminal.

```shell script
./path-to-twitch-recorder-go/twitch-recorder-go -config ./config.json
```

### Options

- `-config` - Path to config file (default: ./config.json)
- `-drive` - Enable Google Drive upload (requires drive credentials in config)

## Build

1. Install [go](https://golang.org/dl/)
2. Git clone the project

Run in a shell like cmd or bash terminal.

```shell script
go build
```
