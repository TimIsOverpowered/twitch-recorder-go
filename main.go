package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/grafov/m3u8"
	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

const (
	TWITCH_API_BASE   = "https://api.twitch.tv/helix"
	TWITCH_ID_API     = "https://id.twitch.tv"
	TWITCH_GQL_API    = "https://gql.twitch.tv/gql"
	TWITCH_CLIENT_ID  = "kimne78kx3ncx6brgo4mv6wki5h1ko"
	TWITCH_USHER_M3U8 = "https://usher.ttvnw.net"
)

type Config struct {
	Twitch struct {
		ClientId     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		OAuthKey     string `json:"oauth_key"`
	} `json:"twitch"`
	Vod_directory string   `json:"vod_directory"`
	Channels      []string `json:"channels"`
	TwitchToken   `json:"twitch_app"`
	Drive         struct {
		Refresh_Token string    `json:"refresh_token"`
		Access_Token  string    `json:"access_token"`
		TokenType     string    `json:"token_type"`
		Expiry        time.Time `json:"expiry"`
	} `json:"drive"`
	Google struct {
		ClientId     string   `json:"client_id"`
		ClientSecret string   `json:"client_secret"`
		Scopes       []string `json:"scopes"`
		Endpoint     struct {
			TokenURL string `json:"token_url"`
		} `json:"endpoint"`
	} `json:"google"`
}

var config *Config
var use_ffmpeg bool
var upload_to_drive bool
var cfgPath string
var isRefreshingTwitchToken bool

type TwitchToken struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

func main() {

	configPath, err := ParseFlags()
	if err != nil {
		log.Fatal(err)
	}
	config, err = NewConfig(configPath)
	if err != nil {
		log.Fatal(err)
	}

	cfgPath = configPath
	var wg sync.WaitGroup
	wg.Add(1)
	for _, channel := range config.Channels {
		if !checkIfUserExists(channel) {
			log.Printf("%s does not exist", channel)
			time.Sleep(500 * time.Millisecond)
			continue
		}
		go Interval(channel, nil)
	}
	wg.Wait()
}

func NewConfig(configPath string) (*Config, error) {
	config := &Config{}

	d, err := ioutil.ReadFile(configPath)
	if err != nil {
		log.Fatalf("unable to read config %v", err)
	}
	if err := json.Unmarshal(d, &config); err != nil {
		log.Fatalf("unable to read config %v", err)
	}

	return config, nil
}

func ParseFlags() (string, error) {
	var configPath string

	flag.BoolVar(&use_ffmpeg, "ffmpeg", false, "use ffmpeg custom logic to download instead of streamlink")
	flag.BoolVar(&upload_to_drive, "drive", false, "upload to drive. make sure you supply refresh_token & access_token in config")
	flag.StringVar(&configPath, "config", "./config.json", "path to config file")
	flag.Parse()

	if err := ValidateConfigPath(configPath); err != nil {
		return "", err
	}

	return configPath, nil
}

func ValidateConfigPath(path string) error {
	s, err := os.Stat(path)
	if err != nil {
		return err
	}
	if s.IsDir() {
		return fmt.Errorf("'%s' is a directory, not a normal file", path)
	}
	return nil
}

func fileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	} else if os.IsNotExist(err) {
		return false
	} else {
		log.Printf("%v", err)
		return false
	}
}

type Value struct {
	Expires int64 `json:"expires"`
}

func Interval(channel string, token *TokenSig) {
	for token == nil {
		var err error
		token, err = getLiveTokenSig(channel)
		if err != nil {
			log.Printf("[%s] %v", channel, err)
			time.Sleep(5 * time.Second)
		}
	}

	var value *Value

	if err := json.Unmarshal([]byte(token.Data.Token.Value), &value); err != nil {
		log.Printf("[%s] Something went wrong trying to get token sig expiration.. %v", channel, err)
	}

	if time.Now().Unix() >= value.Expires {
		log.Printf("[%s] Live Token Sig Expired...", channel)
		time.AfterFunc(6*time.Second, func() {
			Interval(channel, nil)
		})
		return
	}

	Check(channel, token)

	time.AfterFunc(6*time.Second, func() {
		Interval(channel, token)
	})
}

type User struct {
	UserData []struct {
		User_id string `json:"id"`
		Login   string `json:"login"`
	} `json:"data"`
}

func checkIfUserExists(channel string) bool {
	user := getUserObject(channel)
	if len(user.UserData) == 0 {
		return false
	} else {
		return true
	}
}

func Check(channel string, token *TokenSig) {
	m3u8, live := CheckIfLive(token, channel)
	if live {
		err := record(m3u8, channel)
		if err != nil {
			log.Printf("[%s] %v", channel, err)
		}
	}
}

func getUserObject(channel string) *User {
	tokenExpired := checkAccessToken()
	if tokenExpired {
		err := refreshTwitchToken(channel)
		for err != nil {
			time.Sleep(5 * time.Second)
			err = refreshTwitchToken(channel)
			log.Println("Client-Id or Client-Secret may be incorrect!")
		}
	}

	log.Printf("[%s] Getting user object", channel)
	client := resty.New()

	resp, _ := client.R().
		SetHeader("Accept", "application/json").
		SetAuthToken(config.TwitchToken.AccessToken).
		SetHeader("Client-ID", config.Twitch.ClientId).
		Get(TWITCH_API_BASE + "/users?login=" + channel)

	if resp.StatusCode() != 200 {
		log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
		log.Printf(string(resp.Body()))
	}

	var user User
	if err := json.Unmarshal(resp.Body(), &user); err != nil {
		log.Printf("[%s] %v", channel, err)
	}

	return &user
}

type Vod struct {
	VodData []struct {
		Id            string `json:"id"`
		Stream_id     string `json:"stream_id"`
		User_id       string `json:"user_id"`
		User_name     string `json:"user_login"`
		Created_at    string `json:"created_at"`
		Thumbnail_url string `json:"thumbnail_url"`
	} `json:"data"`
}

func checkAccessToken() bool {
	log.Printf("Checking Twitch App Access Token\n")

	client := resty.New()

	resp, _ := client.R().
		SetHeader("Accept", "application/json").
		SetAuthToken(config.TwitchToken.AccessToken).
		SetHeader("Client-ID", config.Twitch.ClientId).
		Get(TWITCH_ID_API + "/oauth2/validate")

	if resp.StatusCode() != 200 {
		log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
		log.Printf("Twitch App Access Token has expired..")
		return true
	} else {
		return false
	}
}

func refreshAccessToken() error {
	log.Printf("Refreshing Twitch App Access Token\n")

	client := resty.New()

	resp, _ := client.R().
		SetHeader("Accept", "application/json").
		Post(TWITCH_ID_API + "/oauth2/token" + "?client_id=" + config.Twitch.ClientId + "&client_secret=" + config.Twitch.ClientSecret + "&grant_type=client_credentials")

	if resp.StatusCode() != 200 {
		log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
		return errors.New(string(resp.Body()))
	} else {
		var token TwitchToken
		err := json.Unmarshal(resp.Body(), &token)
		if err != nil {
			return err
		}

		config.TwitchToken = token

		d, err := json.MarshalIndent(config, "", " ")
		if err != nil {
			return err
		}

		err = ioutil.WriteFile(cfgPath, d, 0777)
		return err
	}
}

type Streams struct {
	StreamsData []struct {
		Id         string `json:"id"`
		User_id    string `json:"user_id"`
		User_name  string `json:"user_name"`
		Type       string `json:"type"`
		Started_at string `json:"started_at"`
	} `json:"data"`
	Pagination struct {
		Cursor string `json:"cursor"`
	} `json:"pagination"`
}

func refreshTwitchToken(channel string) error {
	if isRefreshingTwitchToken {
		for isRefreshingTwitchToken {
			log.Printf("[%s] Waiting for Twitch App Access Token", channel)
			time.Sleep(1 * time.Second)
		}
		return nil
	} else {
		isRefreshingTwitchToken = true
		err := refreshAccessToken()
		isRefreshingTwitchToken = false
		return err
	}
}

func getStreamObject(channel string) (*Streams, error) {
	//Check if APP Access token has expired.. If so, refresh it.
	tokenExpired := checkAccessToken()
	if tokenExpired {
		err := refreshTwitchToken(channel)
		for err != nil {
			time.Sleep(5 * time.Second)
			err = refreshTwitchToken(channel)
			log.Println("Client-Id or Client-Secret may be incorrect!")
		}
	}

	log.Printf("[%s] Getting stream object", channel)
	client := resty.New()

	resp, _ := client.R().
		SetHeader("Accept", "application/json").
		SetAuthToken(config.TwitchToken.AccessToken).
		SetHeader("Client-ID", config.Twitch.ClientId).
		Get(TWITCH_API_BASE + "/streams/?user_login=" + channel)

	if resp.StatusCode() != 200 {
		log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
		return nil, errors.New("Something went wrong retrieving streams object from twitch")
	} else {
		var streams Streams
		err := json.Unmarshal(resp.Body(), &streams)
		if err != nil {
			return nil, err
		}
		return &streams, nil
	}
}

func CheckIfLive(token *TokenSig, channel string) (string, bool) {
	log.Printf("[%s] Checking if live", channel)

	m3u8, err := getLiveM3u8(channel, token)
	if err != nil {
		log.Printf("[%s] %v", channel, err)
		return "", false
	}

	return m3u8, true
}

func record(m3u8 string, channel string) error {
	date := time.Now().Format("01-02-2006")
	log.Printf("[%s] is live. %s", channel, date)
	var path string
	if runtime.GOOS == "windows" {
		path = config.Vod_directory + "\\" + channel + "\\"
	} else {
		path = config.Vod_directory + "/" + channel + "/"
	}
	if !fileExists(path) {
		os.MkdirAll(path, 0777)
	}
	fileName := date + ".mp4"

	if fileExists(path + fileName) {
		os.Remove(path + fileName)
	}

	var stream *Streams
	var err error

	go func() {
		stream, err = getStreamObject(channel)
		if err != nil {
			log.Printf("[%s] %v", channel, err)
		}
		for len(stream.StreamsData) == 0 {
			stream, err = getStreamObject(channel)
			if err != nil {
				log.Printf("[%s] %v", channel, err)
			}
			time.Sleep(5 * time.Second)
		}
	}()

	if !use_ffmpeg {
		//use streamlink
		cmd := exec.Command("streamlink", "-o", path+fileName, "twitch.tv/"+channel, "best", "--twitch-disable-hosting", "--twitch-disable-ads", "--twitch-disable-reruns")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
	} else {
		//use ffmpeg
		log.Printf("[%s] Executing ffmpeg: %s", channel, "ffmpeg -y -i "+m3u8+" -c copy -copyts -start_at_zero -bsf:a aac_adtstoasc -f mp4 "+path+fileName)
		cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "warning", "-y", "-rw_timeout", "3000000", "-i", m3u8, "-c", "copy", "-copyts", "-start_at_zero", "-bsf:a", "aac_adtstoasc", "-f", "mp4", path+fileName)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
		log.Printf("[%s] Finished downloading.. Saved at: %s", channel, path+fileName)
	}

	if len(stream.StreamsData) == 0 {
		return errors.New(channel + "'s stream object not found..")
	}

	new_fileName := stream.StreamsData[0].Id + ".mp4"
	e := os.Rename(path+fileName, path+new_fileName)
	if e != nil {
		return err
	}

	if upload_to_drive {
		go func() {
			err := uploadToDrive(path, new_fileName, channel, stream.StreamsData[0].Id)
			if err != nil {
				log.Printf("[%s] %v", channel, err)
			}
		}()
	}

	return nil
}

func uploadToDrive(path string, fileName string, channel string, streamId string) error {
	if !fileExists(path + fileName) {
		return errors.New("File does not exist.. do not upload")
	}
	log.Printf("[%s] Uploading to drive..", channel)
	//upload to gdrive
	ctx := context.Background()
	var googleConfig oauth2.Config
	googleConfig.ClientID = config.Google.ClientId
	googleConfig.ClientSecret = config.Google.ClientSecret
	googleConfig.Endpoint.TokenURL = config.Google.Endpoint.TokenURL
	googleConfig.Scopes = config.Google.Scopes
	client, err := getClient(&googleConfig)
	if err != nil {
		return err
	}

	srv, err := drive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return err
	}

	//Retrieve Folder Id using channel name.
	nextPageToken := ""
	fileList, err := getDriveFileList(srv, nextPageToken)
	if err != nil {
		return err
	}
	rootFolderId := ""
	streamIdFolder := ""
	for {
		for _, file := range fileList.Files {
			if strings.EqualFold(file.Name, channel) {
				log.Printf("[%s] Found root folder %s", channel, file.Id)
				rootFolderId = file.Id
				continue
			}
			if strings.EqualFold(file.Name, streamId) {
				log.Printf("[%s] Found stream id folder %s", channel, file.Id)
				streamIdFolder = file.Id
				continue
			}
			if len(rootFolderId) > 0 && len(streamIdFolder) > 0 {
				break
			}
		}
		nextPageToken = fileList.NextPageToken
		fileList, err = getDriveFileList(srv, nextPageToken)
		if fileList.NextPageToken == "" || (len(rootFolderId) > 0 && len(streamIdFolder) > 0) {
			break
		}
	}

	//Create root folder if it doesn't exist.
	if len(rootFolderId) == 0 {
		log.Printf("[%s] Creating root folder", channel)
		res, err := srv.Files.Create(&drive.File{Name: channel, MimeType: "application/vnd.google-apps.folder"}).Do()
		if err != nil {
			return err
		}
		rootFolderId = res.Id
	}

	if len(streamIdFolder) == 0 {
		//Create Stream Id Folder if it doesn't exist
		log.Printf("[%s] Creating %s folder", channel, streamId)
		res, err := srv.Files.Create(&drive.File{Name: streamId, MimeType: "application/vnd.google-apps.folder", Parents: []string{rootFolderId}}).Do()
		if err != nil {
			return err
		}
		streamIdFolder = res.Id
	}

	//Upload FIle to Drive
	log.Printf("[%s] Uploading file", channel)
	f, err := os.Open(path + fileName)
	if err != nil {
		return err
	}
	defer f.Close()
	// Grab file info
	inputInfo, err := f.Stat()
	if err != nil {
		return err
	}
	getRate := MeasureTransferRate()

	// progress call back
	showProgress := func(current, total int64) {
		fmt.Printf("Uploaded at %s, %s/%s\r", getRate(current), Comma(current), Comma(total))
	}

	res, err := srv.Files.Create(&drive.File{Name: fileName, Parents: []string{streamIdFolder}}).ResumableMedia(context.Background(), f, inputInfo.Size(), mime.TypeByExtension(filepath.Ext(fileName))).ProgressUpdater(showProgress).Do()
	if err != nil {
		return err
	}
	log.Printf("[%s] Uploaded %s Drive Id: %s", channel, res.Name, res.Id)

	return nil
}

func Comma(v int64) string {
	sign := ""
	if v < 0 {
		sign = "-"
		v = 0 - v
	}

	parts := []string{"", "", "", "", "", "", ""}
	j := len(parts) - 1

	for v > 999 {
		parts[j] = strconv.FormatInt(v%1000, 10)
		switch len(parts[j]) {
		case 2:
			parts[j] = "0" + parts[j]
		case 1:
			parts[j] = "00" + parts[j]
		}
		v = v / 1000
		j--
	}
	parts[j] = strconv.Itoa(int(v))
	return sign + strings.Join(parts[j:], ",")
}

func FileSizeFormat(bytes int64, forceBytes bool) string {
	if forceBytes {
		return fmt.Sprintf("%v B", bytes)
	}

	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}

	var i int
	value := float64(bytes)

	for value > 1000 {
		value /= 1000
		i++
	}
	return fmt.Sprintf("%.1f %s", value, units[i])
}

func MeasureTransferRate() func(int64) string {
	start := time.Now()

	return func(bytes int64) string {
		seconds := int64(time.Now().Sub(start).Seconds())
		if seconds < 1 {
			return fmt.Sprintf("%s/s", FileSizeFormat(bytes, false))
		}
		bps := bytes / seconds
		return fmt.Sprintf("%s/s", FileSizeFormat(bps, false))
	}
}

func getClient(c *oauth2.Config) (*http.Client, error) {
	var tok oauth2.Token
	tok.AccessToken = config.Drive.Access_Token
	tok.RefreshToken = config.Drive.Refresh_Token
	tok.TokenType = config.Drive.TokenType
	tok.Expiry = config.Drive.Expiry
	tokenSource := c.TokenSource(oauth2.NoContext, &tok)
	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, err
	}
	if newToken.AccessToken != tok.AccessToken {
		log.Println("Saving new drive tokens..")
		config.Drive.Access_Token = newToken.AccessToken
		config.Drive.Refresh_Token = newToken.RefreshToken
		config.Drive.TokenType = newToken.TokenType
		config.Drive.Expiry = newToken.Expiry
		d, err := json.MarshalIndent(config, "", " ")
		if err != nil {
			return nil, err
		}
		err = ioutil.WriteFile(cfgPath, d, 0777)
		if err != nil {
			return nil, err
		}
	}
	return oauth2.NewClient(context.Background(), tokenSource), nil
}

func getDriveFileList(driveSvc *drive.Service, nextPageToken string) (*drive.FileList, error) {
	return driveSvc.Files.List().Fields("nextPageToken, files/*").PageToken(nextPageToken).Do()
}

type TokenSig struct {
	Data struct {
		Token struct {
			Value     string `json:"value"`
			Signature string `json:"signature"`
		} `json:"streamPlaybackAccessToken"`
	} `json:"data"`
	User_id    string `json:"user_id"`
	User_name  string `json:"user_name"`
	Type       string `json:"type"`
	Started_at string `json:"started_at"`
}

func getLiveTokenSig(channel string) (*TokenSig, error) {
	log.Printf("[%s] Getting stream token & signature", channel)
	client := resty.New().R()

	if len(config.Twitch.OAuthKey) > 0 {
		client.SetHeader("Authorization", "OAuth "+config.Twitch.OAuthKey)
	}

	body := []byte(fmt.Sprintf(`{
        "operationName": "PlaybackAccessToken",
        "variables":{
            "isLive": true,
            "login": "%v",
            "isVod": false,
            "vodID": "",
			"platform": "web",
			"playerBackend": "mediaplayer",
			"playerType": "site"
		},
		"extensions":{
			"persistedQuery":{
				"version": 1,
				"sha256Hash": "0828119ded1c13477966434e15800ff57ddacf13ba1911c129dc2200705b0712"
			}
		}
	}`, channel))

	resp, _ := client.
		SetHeader("Client-ID", TWITCH_CLIENT_ID).
		SetHeader("Origin", "https://twitch.tv").
		SetHeader("Referer", "https://twitch.tv").
		SetHeader("Content-Type", "text/plain;charset=UTF-8").
		SetHeader("Accept", "*/*").
		SetBody(body).
		Post(TWITCH_GQL_API)

	if resp.StatusCode() != 200 {
		log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
		return nil, errors.New(string(resp.Body()))
	}

	var tokenSig TokenSig
	if err := json.Unmarshal(resp.Body(), &tokenSig); err != nil {
		return nil, err
	}

	return &tokenSig, nil
}

func getLiveM3u8(channel string, tokenSig *TokenSig) (string, error) {
	log.Printf("[%s] Getting m3u8", channel)
	client := resty.New().R()

	if len(config.Twitch.OAuthKey) > 0 {
		client.SetHeader("Authorization", "OAuth "+config.Twitch.OAuthKey)
	}

	resp, _ := client.
		SetHeader("Content-Type", "application/vnd.apple.mpegurl").
		Get(TWITCH_USHER_M3U8 + "/api/channel/hls/" + channel + ".m3u8?allow_source=true&p=" + strconv.Itoa(rand.Intn(10000000-1000000)+1000000) + "&player=twitchweb&playlist_include_framerate=true&allow_spectre=true&sig=" + tokenSig.Data.Token.Signature + "&token=" + tokenSig.Data.Token.Value)

	if resp.StatusCode() != 200 {
		if resp.StatusCode() != 404 {
			log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
		}
		return "", errors.New("is not live..")
	}

	p, listType, err := m3u8.DecodeFrom(bytes.NewReader(resp.Body()), true)
	if err != nil {
		return "", err
	}
	switch listType {
	case m3u8.MASTER:
		for _, variant := range p.(*m3u8.MasterPlaylist).Variants {
			if strings.EqualFold(variant.Video, "chunked") {
				return variant.URI, nil
			}
		}
	}
	return "", errors.New("Cannot find the chunked variant..")
}
