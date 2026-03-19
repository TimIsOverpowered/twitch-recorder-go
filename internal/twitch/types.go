package twitch

import "time"

type User struct {
	ID    string `json:"id"`
	Login string `json:"login"`
}

type Streams struct {
	Data []struct {
		ID        string `json:"id"`
		UserID    string `json:"user_id"`
		UserName  string `json:"user_name"`
		Type      string `json:"type"`
		StartedAt string `json:"started_at"`
	} `json:"data"`
}

type TwitchToken struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

// TokenSig is the GQL response from PlaybackAccessToken query
type TokenSig struct {
	Data struct {
		Token struct {
			Value     string `json:"value"`
			Signature string `json:"signature"`
		} `json:"streamPlaybackAccessToken"`
	} `json:"data"`
	UserID    string `json:"user_id"`
	UserName  string `json:"user_name"`
	Type      string `json:"type"`
	StartedAt string `json:"started_at"`
}

// CachedToken holds token value with expiration tracking
type CachedToken struct {
	Value     string
	Signature string
	ExpiresAt time.Time
}
