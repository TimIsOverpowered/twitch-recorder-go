package utils

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"time"
)

type ConfigStruct struct {
	Twitch struct {
		ClientId     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		OAuthKey     string `json:"oauth_key"`
	} `json:"twitch"`
	Vod_directory string      `json:"vod_directory"`
	Channels      []Channel   `json:"channels"`
	TwitchToken   TwitchToken `json:"twitch_app"`
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

type TwitchToken struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

type Channel struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

var Config *ConfigStruct
var CfgPath string
var USE_FFMPEG bool
var UPLOAD_TO_DRIVE bool

func init() {
	configPath, err := ParseFlags()
	if err != nil {
		log.Fatal(err)
	}
	Config, err = NewConfig(configPath)
	if err != nil {
		log.Fatal(err)
	}

	CfgPath = configPath
}

func NewConfig(configPath string) (*ConfigStruct, error) {
	config := &ConfigStruct{}

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

	flag.BoolVar(&USE_FFMPEG, "ffmpeg", false, "use ffmpeg custom logic to download instead of streamlink")
	flag.BoolVar(&UPLOAD_TO_DRIVE, "drive", false, "upload to drive. make sure you supply refresh_token & access_token in config")
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
