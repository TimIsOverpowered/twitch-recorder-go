package twitch

type Client interface {
	GetUser(login string) (*User, error)
	GetStreams(userLogin string) (*Streams, error)
	RefreshToken() error
	CheckAccessToken() bool
}

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
