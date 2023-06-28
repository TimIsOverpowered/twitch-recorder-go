package platform

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"strconv"
	"strings"
	"time"

	utils "twitch-recorder-go/utils"

	"github.com/go-resty/resty/v2"
	"github.com/grafov/m3u8"
)

const (
	TWITCH_API_BASE   = "https://api.twitch.tv/helix"
	TWITCH_ID_API     = "https://id.twitch.tv"
	TWITCH_GQL_API    = "https://gql.twitch.tv/gql"
	TWITCH_CLIENT_ID  = "kd1unb4b3q4t58fwlpcbzcbnm76a8fp"
	TWITCH_USHER_M3U8 = "https://usher.ttvnw.net"
)

var isRefreshingTwitchToken bool

type TwitchUser struct {
	UserData []struct {
		User_id string `json:"id"`
		Login   string `json:"login"`
	} `json:"data"`
}

func TwitchCheckIfUserExists(channel string) bool {
	user := getUserObject(channel)
	if len(user.UserData) == 0 {
		return false
	} else {
		return true
	}
}

func getUserObject(channel string) *TwitchUser {
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
		SetAuthToken(utils.Config.TwitchToken.AccessToken).
		SetHeader("Client-ID", utils.Config.Twitch.ClientId).
		Get(TWITCH_API_BASE + "/users?login=" + channel)

	if resp.StatusCode() != 200 {
		log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
		log.Printf(string(resp.Body()))
	}

	var user TwitchUser
	if err := json.Unmarshal(resp.Body(), &user); err != nil {
		log.Printf("[Twitch] [%s] %v", channel, err)
	}

	return &user
}

type TwitchVod struct {
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
		SetAuthToken(utils.Config.TwitchToken.AccessToken).
		SetHeader("Client-ID", utils.Config.Twitch.ClientId).
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
		Post(TWITCH_ID_API + "/oauth2/token" + "?client_id=" + utils.Config.Twitch.ClientId + "&client_secret=" + utils.Config.Twitch.ClientSecret + "&grant_type=client_credentials")

	if resp.StatusCode() != 200 {
		log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
		return errors.New(string(resp.Body()))
	} else {
		var token utils.TwitchToken
		err := json.Unmarshal(resp.Body(), &token)
		if err != nil {
			return err
		}

		utils.Config.TwitchToken = token

		d, err := json.MarshalIndent(utils.Config, "", " ")
		if err != nil {
			return err
		}

		err = ioutil.WriteFile(utils.CfgPath, d, 0777)
		return err
	}
}

type TwitchStreams struct {
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
			log.Printf("[Twitch] [%s] Waiting for Twitch App Access Token", channel)
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

func TwitchGetStreamObject(channel string) (*TwitchStreams, error) {
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
		SetAuthToken(utils.Config.TwitchToken.AccessToken).
		SetHeader("Client-ID", utils.Config.Twitch.ClientId).
		Get(TWITCH_API_BASE + "/streams/?user_login=" + channel)

	if resp.StatusCode() != 200 {
		log.Printf("Unexpected status code, expected %d, got %d instead", 200, resp.StatusCode())
		return nil, errors.New("Something went wrong retrieving streams object from twitch")
	} else {
		var streams TwitchStreams
		err := json.Unmarshal(resp.Body(), &streams)
		if err != nil {
			return nil, err
		}
		return &streams, nil
	}
}

func TwitchCheckIfLive(token *TwitchTokenSig, channel string) (string, bool) {
	log.Printf("[Twitch] [%s] Checking if live", channel)

	m3u8, err := getTwitchLiveM3u8(channel, token)
	if err != nil {
		log.Printf("[%s] %v", channel, err)
		return "", false
	}

	return m3u8, true
}

type TwitchTokenSig struct {
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

type TwitchValue struct {
	Expires int64 `json:"expires"`
}

func TwitchGetLiveTokenSig(channel string) (*TwitchTokenSig, error) {
	log.Printf("[Twitch] [%s] Getting stream token & signature", channel)
	client := resty.New().R()

	if len(utils.Config.Twitch.OAuthKey) > 0 {
		client.SetHeader("Authorization", "OAuth "+utils.Config.Twitch.OAuthKey)
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

	var tokenSig TwitchTokenSig
	if err := json.Unmarshal(resp.Body(), &tokenSig); err != nil {
		return nil, err
	}

	return &tokenSig, nil
}

func getTwitchLiveM3u8(channel string, tokenSig *TwitchTokenSig) (string, error) {
	log.Printf("[Twitch] [%s] Getting m3u8", channel)
	client := resty.New().R()

	if len(utils.Config.Twitch.OAuthKey) > 0 {
		client.SetHeader("Authorization", "OAuth "+utils.Config.Twitch.OAuthKey)
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
