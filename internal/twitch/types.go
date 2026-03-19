package twitch

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
