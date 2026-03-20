package config

import (
	"encoding/json"
	"os"
	"time"
)

type Config struct {
	Twitch struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		OAuthKey     string `json:"oauth_key"`
	} `json:"twitch"`
	VodDirectory string   `json:"vod_directory"`
	Channels     []string `json:"channels"`
	TwitchToken  `json:"twitch_app"`
	Drive        struct {
		RefreshToken string    `json:"refresh_token"`
		AccessToken  string    `json:"access_token"`
		TokenType    string    `json:"token_type"`
		Expiry       time.Time `json:"expiry"`
	} `json:"drive"`
	Google struct {
		ClientID     string   `json:"client_id"`
		ClientSecret string   `json:"client_secret"`
		Scopes       []string `json:"scopes"`
		Endpoint     struct {
			TokenURL string `json:"token_url"`
		} `json:"endpoint"`
	} `json:"google"`
	Archive struct {
		Enabled  bool   `json:"enabled"`
		Endpoint string `json:"endpoint"`
		Key      string `json:"key"`
	} `json:"archive"`
	TestFinalizeAfter int `json:"-"`
}

type TwitchToken struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

func LoadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	config := &Config{}
	if err := json.Unmarshal(data, config); err != nil {
		return nil, err
	}

	return config, nil
}

func SaveConfig(config *Config, configPath string) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0600)
}

func GetTwitchClientID() string {
	if id := os.Getenv("TWITCH_CLIENT_ID"); id != "" {
		return id
	}
	return ""
}

func GetTwitchClientSecret() string {
	if secret := os.Getenv("TWITCH_CLIENT_SECRET"); secret != "" {
		return secret
	}
	return ""
}

func GetGoogleClientID() string {
	if id := os.Getenv("GOOGLE_CLIENT_ID"); id != "" {
		return id
	}
	return ""
}

func GetGoogleClientSecret() string {
	if secret := os.Getenv("GOOGLE_CLIENT_SECRET"); secret != "" {
		return secret
	}
	return ""
}
