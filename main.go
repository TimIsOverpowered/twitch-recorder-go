package main

import (
	"bytes"
	"context"
	"encoding/base64"
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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/grafov/m3u8"
	"github.com/klauspost/compress/zstd"
	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

const (
	TWITCH_API_BASE     = "https://api.twitch.tv/helix"
	TWITCH_ID_API       = "https://id.twitch.tv"
	TWITCH_GQL_API      = "https://gql.twitch.tv/gql"
	TWITCH_CLIENT_ID    = "kimne78kx3ncx6brgo4mv6wki5h1ko"
	TWITCH_USHER_M3U8   = "https://usher.ttvnw.net"
	TWITCH_COMMENTS_API = "https://api.twitch.tv/v5"
	BTTV_API            = "https://api.betterttv.net/3"
	BTTV_CDN            = "https://cdn.betterttv.net"
	FFZ_API             = "https://api.frankerfacez.com/v1"
	FFZ_CDN             = "https://cdn.frankerfacez.com"
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
var record_chat bool

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
			continue
		}
		tokenSig, err := getLiveTokenSig(channel)
		if err != nil {
			log.Printf("[%s] %v", channel, err)
		}
		go Interval(channel, tokenSig)
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
	flag.BoolVar(&record_chat, "chat", false, "saves chat")
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
	d, err := ioutil.ReadFile(cfgPath)
	if err != nil {
		log.Fatalf("unable to read config %v", err)
	}
	if err := json.Unmarshal(d, &config); err != nil {
		log.Fatalf("unable to read config %v", err)
	}

	var value *Value

	if err := json.Unmarshal([]byte(token.Data.Token.Value), &value); err != nil {
		log.Printf("[%s] Something went wrong trying to get token sig expiration.. %v", channel, err)
	}

	if time.Now().Unix() >= value.Expires {
		log.Printf("[%s] Live Token Sig Expired...", channel)
		token, err = getLiveTokenSig(channel)
		if err != nil {
			log.Printf("[%s] %v", channel, err)
		}
	}

	if err == nil {
		Check(channel, token)
	}

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
		if record_chat {
			go func() {
				stream, err := getStreamObject(channel)
				if err != nil {
					log.Printf("[%s] %v", channel, err)
					return
				}
				for len(stream.StreamsData) == 0 {
					stream, err = getStreamObject(channel)
					if err != nil {
						log.Printf("[%s] %v", channel, err)
					}
					time.Sleep(5 * time.Second)
				}

				vod_data, err := getVodObject(channel)
				if err != nil {
					log.Printf("[%s] %v", channel, err)
					return
				}

				for vod_data.VodData[0].Stream_id != stream.StreamsData[0].Id {
					time.Sleep(5 * time.Second)
					vod_data, err = getVodObject(channel)
					if err != nil {
						log.Printf("[%s] %v", channel, err)
						return
					}
				}
				log.Printf("[%s] Recording %s chat..", channel, vod_data.VodData[0].Id)
				recordComments(channel, vod_data.VodData[0].Id, stream.StreamsData[0].Id, "")
			}()
		}
		err := record(m3u8, channel)
		if err != nil {
			log.Printf("[%s] %v", channel, err)
		}
	}
}

func getUserObject(channel string) *User {
	tokenExpired := checkAccessToken()
	if tokenExpired {
		err := refreshAccessToken()
		for err != nil {
			time.Sleep(5 * time.Second)
			err = refreshAccessToken()
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

func getVodObject(channel string) (*Vod, error) {
	//Check if APP Access token has expired.. If so, refresh it.
	tokenExpired := checkAccessToken()
	if tokenExpired {
		err := refreshAccessToken()
		for err != nil {
			time.Sleep(5 * time.Second)
			err = refreshAccessToken()
			log.Println("Client-Id or Client-Secret may be incorrect!")
		}
	}

	user := getUserObject(channel)

	log.Printf("[%s] Getting vod object", channel)
	client := resty.New()

	resp, _ := client.R().
		SetHeader("Accept", "application/json").
		SetAuthToken(config.TwitchToken.AccessToken).
		SetHeader("Client-ID", config.Twitch.ClientId).
		Get(TWITCH_API_BASE + "/videos/?user_id=" + user.UserData[0].User_id)

	if resp.StatusCode() != 200 {
		log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
	}

	var vod Vod
	err := json.Unmarshal(resp.Body(), &vod)
	if err != nil {
		return nil, err
	}

	return &vod, err
}

func zstdCompress(file []byte, channel string, vodId string) []byte {
	log.Printf("[%s] starting zstd compress %s chat.json", channel, vodId)

	var encoder, _ = zstd.NewWriter(nil)
	return encoder.EncodeAll(file, make([]byte, 0, len(file)))
}

type FFZRoom struct {
	Room struct {
		Set int `json:"set"`
	} `json:"room"`
}

//https://api.frankerfacez.com/v1/room/
func getFFZEmotes(channel string) (*FFZSet, error) {
	log.Printf("[%s] Getting FFZ Emotes", channel)
	client := resty.New()

	resp, _ := client.R().
		SetHeader("Accept", "application/json").
		Get(FFZ_API + "/room/" + strings.ToLower(channel))

	if resp.StatusCode() != 200 {
		log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
		return nil, errors.New(string(resp.Body()))
	}

	var room FFZRoom
	err := json.Unmarshal(resp.Body(), &room)
	if err != nil {
		return nil, err
	}

	resp, _ = client.R().
		SetHeader("Accept", "application/json").
		Get(FFZ_API + "/set/" + fmt.Sprintf("%v", room.Room.Set))

	if resp.StatusCode() != 200 {
		log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
		return nil, errors.New(string(resp.Body()))
	}

	var set FFZSet
	err = json.Unmarshal(resp.Body(), &set)
	if err != nil {
		return nil, err
	}

	for _, emote := range set.Set.Emotes {
		resp, _ = client.R().
			Get(FFZ_CDN + "/emote/" + fmt.Sprintf("%v", emote.Id) + "/1x")

		var base64Encoding string
		mimeType := http.DetectContentType(resp.Body())
		switch mimeType {
		case "image/jpeg":
			base64Encoding += "data:image/jpeg;base64,"
		case "image/png":
			base64Encoding += "data:image/png;base64,"
		case "image/gif":
			base64Encoding += "data:image/gif;base64,"
		}

		base64Encoding += base64.StdEncoding.EncodeToString(resp.Body())
		emote.Base64 = base64Encoding
	}

	return &set, nil
}

//https://api.betterttv.net/3/cached/users/twitch/
func getBTTVEmotes(channel string) (*BTTV, error) {
	log.Printf("[%s] Getting BTTV Emotes", channel)
	twitchId := getUserObject(channel).UserData[0].User_id

	client := resty.New()
	resp, _ := client.R().
		SetHeader("Accept", "application/json").
		Get(BTTV_API + "/cached/users/twitch/" + twitchId)

	if resp.StatusCode() != 200 {
		log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
		return nil, errors.New(string(resp.Body()))
	}

	var bttv BTTV
	err := json.Unmarshal(resp.Body(), &bttv)
	if err != nil {
		return nil, err
	}

	for i, emote := range bttv.Emotes {
		resp, _ = client.R().
			Get(BTTV_CDN + "/emote/" + emote.Id + "/1x")

		var base64Encoding string
		mimeType := http.DetectContentType(resp.Body())
		switch mimeType {
		case "image/jpeg":
			base64Encoding += "data:image/jpeg;base64,"
		case "image/png":
			base64Encoding += "data:image/png;base64,"
		case "image/gif":
			base64Encoding += "data:image/gif;base64,"
		}

		base64Encoding += base64.StdEncoding.EncodeToString(resp.Body())
		bttv.Emotes[i].Base64 = base64Encoding
	}

	return &bttv, nil
}

func recordComments(channel string, vodId string, streamId string, cursor string, retry_optional ...int) {
	retry := 0
	if len(retry_optional) > 0 {
		retry = retry_optional[0]
	}
	log.Printf("[%s] Checking Comments.. retry: %v cursor: %s", channel, retry, cursor)

	path := filepath.FromSlash(config.Vod_directory + "/" + channel + "/")
	if !fileExists(path) {
		os.MkdirAll(path, 0777)
	}

	fileName := vodId + ".json"
	if fileExists(path + fileName) {
		chatFile, _ := ioutil.ReadFile(path + fileName)
		var oldComments VodComments
		err := json.Unmarshal(chatFile, &oldComments)
		if err != nil {
			log.Printf("[%s] %v", channel, err)
			return
		}

		if retry == 10 {
			ffz, err := getFFZEmotes(channel)
			if err != nil {
				log.Printf("[%s] %v", channel, err)
			}
			oldComments.FFZSet = ffz
			bttv, err := getBTTVEmotes(channel)
			if err != nil {
				log.Printf("[%s] %v", channel, err)
			}
			oldComments.BTTV = bttv

			d, err := json.Marshal(oldComments)
			if err != nil {
				log.Printf("[%s] %v", channel, err)
				return
			}

			compressedFile := zstdCompress(d, channel, vodId)
			os.Remove(path + fileName)
			fileName = fileName + ".zst"
			err = ioutil.WriteFile(path+fileName, compressedFile, 0777)
			if err != nil {
				log.Printf("[%s] %v", channel, err)
				return
			}

			log.Printf("[%s] Saved chat at %s", channel, path+fileName)

			if upload_to_drive {
				go func() {
					err := uploadToDrive(path, fileName, channel, streamId)
					if err != nil {
						log.Printf("[%s] %v", channel, err)
						return
					}
					os.Remove(path + fileName)
				}()
			}
			return
		}

		if len(cursor) == 0 {
			lastOffset := oldComments.Comments[len(oldComments.Comments)-1].Content_offset_seconds
			comments, err := fetchComments(vodId, fmt.Sprintf("%f", lastOffset))
			if err != nil {
				log.Printf("[%s] %v", channel, err)
				retry = retry + 1
				time.AfterFunc(60*time.Second, func() {
					recordComments(channel, vodId, streamId, cursor, retry)
				})
				return
			}

			//only add to array if it doesn't exist in original array...
			for _, comment := range comments.Comments {
				commentExists := false
				for i := 0; i < len(oldComments.Comments); i++ {
					v := oldComments.Comments[i]
					if v.Id == comment.Id {
						commentExists = true
						break
					}
				}
				if !commentExists {
					oldComments.Comments = append(oldComments.Comments, comment)
					log.Printf("[%s] Current Offset: %v", channel, comment.Content_offset_seconds)
				}
			}

			cursor = comments.Cursor
		}

		for len(cursor) != 0 {
			time.Sleep(500 * time.Millisecond)
			nextComments := fetchNextComments(vodId, cursor)
			for nextComments == nil {
				time.Sleep(500 * time.Millisecond)
				nextComments = fetchNextComments(vodId, cursor)
			}
			cursor = nextComments.Cursor
			for _, comment := range nextComments.Comments {
				commentExists := false
				for i := 0; i < len(oldComments.Comments); i++ {
					v := oldComments.Comments[i]
					if v.Id == comment.Id {
						commentExists = true
						break
					}
				}
				if !commentExists {
					oldComments.Comments = append(oldComments.Comments, comment)
				}
			}
			log.Printf("[%s] Current Offset: %v", channel, nextComments.Comments[len(nextComments.Comments)-1].Content_offset_seconds)
		}

		d, err := json.Marshal(oldComments)
		if err != nil {
			log.Printf("[%s] %v", channel, err)
			return
		}
		err = ioutil.WriteFile(path+fileName, d, 0777)

		stream, err := getStreamObject(channel)
		if err != nil {
			log.Printf("[%s] %v", channel, err)
		}
		if len(stream.StreamsData) > 0 && streamId == stream.StreamsData[0].Id {
			time.AfterFunc(60*time.Second, func() {
				recordComments(channel, vodId, streamId, cursor, retry)
			})
		} else if retry < 10 {
			retry = retry + 1
			time.AfterFunc(60*time.Second, func() {
				recordComments(channel, vodId, streamId, cursor, retry)
			})
		}
		return
	}

	comments, err := fetchComments(vodId, "0")
	if err != nil {
		log.Printf("[%s] %v", channel, err)
		return
	}
	cursor = comments.Cursor
	if len(comments.Comments) == 0 {
		time.AfterFunc(60*time.Second, func() {
			recordComments(channel, vodId, streamId, cursor, retry)
		})
		return
	}
	log.Printf("[%s] Current Offset: %v", channel, comments.Comments[len(comments.Comments)-1].Content_offset_seconds)
	for len(cursor) != 0 {
		time.Sleep(500 * time.Millisecond)
		nextComments := fetchNextComments(vodId, cursor)
		for nextComments == nil {
			time.Sleep(500 * time.Millisecond)
			nextComments = fetchNextComments(vodId, cursor)
		}
		cursor = nextComments.Cursor
		comments.Comments = append(comments.Comments, nextComments.Comments...)
		log.Printf("[%s] Current Offset: %v", channel, nextComments.Comments[len(nextComments.Comments)-1].Content_offset_seconds)
	}
	d, err := json.Marshal(comments)
	if err != nil {
		log.Printf("[%s] %v", channel, err)
		return
	}
	err = ioutil.WriteFile(path+fileName, d, 0777)
	if err != nil {
		log.Printf("[%s] %v", channel, err)
		return
	}

	time.AfterFunc(60*time.Second, func() {
		recordComments(channel, vodId, streamId, cursor, retry)
	})

	log.Printf("[%s] Saved inital %s chat.json", channel, vodId)
}

type FFZSet struct {
	Set struct {
		Emotes []struct {
			Id     int    `json:"id"`
			Code   string `json:"name"`
			Base64 string `json:",omitempty"`
		} `json:"emoticons"`
	} `json:"set"`
}

type BTTV struct {
	Emotes []struct {
		Id     string `json:"id"`
		Code   string `json:"code"`
		Base64 string `json:",omitempty"`
	} `json:"channelEmotes"`
}

type VodComments struct {
	FFZSet   *FFZSet `json:",omitempty"`
	BTTV     *BTTV   `json:",omitempty"`
	Comments []struct {
		Id                     string  `json:"_id"`
		Channel_id             string  `json:"channel_id"`
		Content_id             string  `json:"content_id"`
		Content_offset_seconds float32 `json:"content_offset_seconds"`
		Commenter              struct {
			Display_name string `json:"display_name"`
		} `json:"commenter"`
		Message struct {
			Fragments []struct {
				Text     string `json:"text"`
				Emoticon *struct {
					Emoticon_id     string `json:"emoticon_id"`
					Emoticon_set_id string `json:"emoticon_set_id"`
				} `json:"emoticon,omitempty"`
			} `json:"fragments"`
			Is_action   bool `json:"is_action"`
			User_badges *[]struct {
				Id      string `json:"_id"`
				Version string `json:"version"`
			} `json:"user_badges,omitempty"`
			User_color string `json:"user_color"`
		} `json:"message"`
	} `json:"comments"`
	Cursor string `json:"_next,omitempty"`
}

//https://api.twitch.tv/v5/videos/${vodId}/comments?content_offset_seconds=${offset}
func fetchComments(vodId string, offset string) (*VodComments, error) {
	client := resty.New()

	resp, _ := client.R().
		SetHeader("Accept", "application/json").
		SetHeader("Client-ID", TWITCH_CLIENT_ID).
		Get(TWITCH_COMMENTS_API + "/videos/" + vodId + "/comments?content_offset_seconds=" + offset)
	if resp.StatusCode() != 200 {
		log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
		return nil, errors.New(string(resp.Body()))
	}

	var vodComments VodComments
	err := json.Unmarshal(resp.Body(), &vodComments)
	if err != nil {
		log.Printf("%v", err)
	}

	return &vodComments, nil
}

//https://api.twitch.tv/v5/videos/${vodId}/comments?cursor=${cursor}
func fetchNextComments(vodId string, cursor string) *VodComments {
	client := resty.New()

	resp, _ := client.R().
		SetHeader("Accept", "application/json").
		SetHeader("Client-ID", TWITCH_CLIENT_ID).
		Get(TWITCH_COMMENTS_API + "/videos/" + vodId + "/comments?cursor=" + cursor)

	if resp.StatusCode() != 200 {
		log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
		log.Printf(string(resp.Body()))
		return nil
	}

	var vodComments VodComments
	err := json.Unmarshal(resp.Body(), &vodComments)
	if err != nil {
		log.Printf("%v", err)
	}

	return &vodComments
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

func getStreamObject(channel string) (*Streams, error) {
	//Check if APP Access token has expired.. If so, refresh it.
	tokenExpired := checkAccessToken()
	if tokenExpired {
		err := refreshAccessToken()
		for err != nil {
			time.Sleep(5 * time.Second)
			err = refreshAccessToken()
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

func parseM3u8(client *resty.Client, m3u8Uri string) (m3u8.Playlist, int, error) {
	resp, _ := client.R().
		Get(m3u8Uri)

	statusCode := resp.StatusCode()
	if statusCode != 200 {
		return nil, statusCode, errors.New(string(resp.Body()))
	}

	buffer := bytes.NewBuffer(resp.Body())

	p, _, err := m3u8.Decode(*buffer, true)
	if err != nil {
		return nil, statusCode, errors.New("Failed to decode m3u8..")
	}

	return p, statusCode, nil
}

func downloadSegment(client *resty.Client, segmentUri string, filePath string) ([]byte, error) {
	resp, _ := client.R().
		Get(segmentUri)

	statusCode := resp.StatusCode()
	if statusCode != 200 {
		return nil, errors.New(string(resp.Body()))
	}

	return resp.Body(), nil
}

func download(client *resty.Client, m3u8Uri string, channel string, path string, lastDownloadedTSUrl string) (int, int, string, []string) {
	if !fileExists(path) {
		os.MkdirAll(path, 0777)
	}

	totalSegments := 0

	m3u8Object, statusCode, err := parseM3u8(client, m3u8Uri)
	if err != nil {
		log.Printf("[%s] %v", channel, err)
	}

	segments := m3u8Object.(*m3u8.MediaPlaylist).Segments
	index := 0
	for i, segment := range segments {
		if segment == nil {
			continue
		}

		if segment.URI == lastDownloadedTSUrl {
			index = i + 1
			break
		}
	}

	var filePaths []string
	for i := index; i < len(segments); i++ {
		segment := segments[i]
		if segment == nil {
			continue
		}
		lastDownloadedTSUrl = segment.URI

		fileName := strconv.Itoa(i) + ".ts"
		filePath := path + fileName

		filePaths = append(filePaths, filePath)

		downloadedSegment, err := downloadSegment(client, segment.URI, filePath)
		if err != nil {
			log.Printf("[%s] %v", channel, err)
		}

		if err := ioutil.WriteFile(filePath, downloadedSegment, 0777); err != nil {
			log.Printf("[%s] %v", channel, err)
		}
		totalSegments = totalSegments + 1
	}

	return statusCode, totalSegments, lastDownloadedTSUrl, filePaths
}

func record(m3u8Uri string, channel string) error {
	date := time.Now().Format("01-02-2006")
	path := filepath.FromSlash(config.Vod_directory + "/" + channel + "/")
	tsFilePath := filepath.FromSlash(path + "ts/")
	mp4FileName := channel + ".mp4"

	if fileExists(mp4FileName) {
		os.Remove(mp4FileName)
	}

	var stream *Streams

	go func(channel string) {
		stream, err := getStreamObject(channel)
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
	}(channel)

	if !fileExists(path) {
		os.MkdirAll(path, 0777)
	}

	client := resty.New().SetRetryCount(10)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		var statusCode int
		lastDownloadedTSUrl := ""
		totalSegments := 0
		var tsFilePaths []string
		for statusCode != 404 {
			statusCode, totalSegments, lastDownloadedTSUrl, tsFilePaths = download(client, m3u8Uri, channel, tsFilePath, lastDownloadedTSUrl)
			log.Printf("[%s] Downloaded %v segments", channel, totalSegments)

			concatPath := tsFilePath + "concat.txt"
			var err error

			concatTSFile, err := os.Create(concatPath)
			if err != nil {
				log.Fatal(err)
			}

			for _, tsFile := range tsFilePaths {
				_, err = concatTSFile.WriteString("file '" + tsFile + "'\n")
				if err != nil {
					log.Fatal(err)
				}
			}

			concatTSFile.Close()

			concatTSMp4FileName := "tempts_" + channel + ".mp4"
			log.Printf("[%s] Executing ffmpeg: %s", channel, "ffmpeg -y -f concat -safe 0 -i "+concatPath+" -c copy -bsf:a aac_adtstoasc -f mp4 "+path+concatTSMp4FileName)
			cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "warning", "-y", "-f", "concat", "-safe", "0", "-i", concatPath, "-c", "copy", "-bsf:a", "aac_adtstoasc", "-f", "mp4", path+concatTSMp4FileName)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Run()

			os.RemoveAll(tsFilePath)

			tempMp4FileName := "temp_" + channel + ".mp4"

			if fileExists(path + mp4FileName) {
				concatMp4FilePath := path + "inputs.txt"
				concatMp4File, err := os.Create(concatMp4FilePath)
				if err != nil {
					log.Fatal(err)
				}
				_, err = concatMp4File.WriteString("file '" + path + mp4FileName + "'\n")
				if err != nil {
					log.Fatal(err)
				}

				_, err = concatMp4File.WriteString("file '" + path + concatTSMp4FileName + "'\n")
				if err != nil {
					log.Fatal(err)
				}

				concatMp4File.Close()

				log.Printf("[%s] Executing ffmpeg: %s", channel, "ffmpeg -y -f concat -safe 0 -i "+concatMp4FilePath+" -c copy -f mp4 "+path+tempMp4FileName)
				cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "warning", "-y", "-f", "concat", "-safe", "0", "-i", concatMp4FilePath, "-c", "copy", "-f", "mp4", path+tempMp4FileName)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				cmd.Run()
				os.Remove(path + concatTSMp4FileName)
				os.Remove(concatMp4FilePath)
				os.Remove(path + mp4FileName)
				os.Rename(path+tempMp4FileName, path+mp4FileName)
			} else {
				os.Rename(path+concatTSMp4FileName, path+mp4FileName)
			}

			log.Printf("[%s] Saved mp4 at: %s", channel, path+mp4FileName)

			time.Sleep(6 * time.Second)
		}
		defer wg.Done()
	}()
	wg.Wait()

	var finalFileName string
	if stream != nil {
		finalFileName = stream.StreamsData[0].Id + ".mp4"
		os.Rename(path+mp4FileName, path+finalFileName)
	} else {
		finalFileName = date + ".mp4"
		os.Rename(path+mp4FileName, path+finalFileName)
	}

	if upload_to_drive {
		go func() {
			err := uploadToDrive(path, finalFileName, channel, stream.StreamsData[0].Id)
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
