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
		StreamPlaybackAccessToken *struct {
			Value     string `json:"value"`
			Signature string `json:"signature"`
			IsEnabled bool   `json:"is_enabled"`
		} `json:"streamPlaybackAccessToken"`
	} `json:"data"`
}

// CachedToken holds token value with expiration tracking
type CachedToken struct {
	Value     string
	Signature string
	ExpiresAt time.Time
}

// VOD represents a Twitch video-on-demand object
type VOD struct {
	ID           string `json:"id"`
	StreamID     string `json:"stream_id"`
	UserID       string `json:"user_id"`
	UserLogin    string `json:"user_login"`
	UserName     string `json:"user_name"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	CreatedAt    string `json:"created_at"`
	PublishedAt  string `json:"published_at"`
	Type         string `json:"type"`
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnail_url"`
	ViewCount    int    `json:"view_count"`
	Duration     string `json:"duration"`
}

// ChatResponse represents the GQL response for video comments
type ChatResponse struct {
	Data struct {
		Video VideoComments `json:"video"`
	} `json:"data"`
}

// VideoComments contains the video ID and comment connection
type VideoComments struct {
	ID       string            `json:"id"`
	Comments CommentConnection `json:"comments"`
}

// CommentConnection represents paginated comments
type CommentConnection struct {
	Edges    []CommentEdge `json:"edges"`
	PageInfo PageInfo      `json:"pageInfo"`
	Typename string        `json:"__typename"`
}

// PageInfo contains pagination info
type PageInfo struct {
	HasNextPage     bool   `json:"hasNextPage"`
	HasPreviousPage bool   `json:"hasPreviousPage"`
	Typename        string `json:"__typename"`
}

// CommentEdge wraps a comment node with cursor
type CommentEdge struct {
	Node     Comment `json:"node"`
	Cursor   string  `json:"cursor"`
	Typename string  `json:"__typename"`
}

// Comment represents a single chat message from the GQL API
type Comment struct {
	ID                   string   `json:"id"`
	Commenter            ChatUser `json:"commenter"`
	ContentOffsetSeconds int      `json:"contentOffsetSeconds"`
	CreatedAt            string   `json:"createdAt"`
	Message              Message  `json:"message"`
	Typename             string   `json:"__typename"`
}

// Message contains the actual message content and metadata
type Message struct {
	Fragments  []MessageFragment `json:"fragments"`
	UserBadges []Badge           `json:"userBadges"`
	UserColor  *string           `json:"userColor"`
	Typename   string            `json:"__typename"`
}

// MessageFragment represents a part of the message (text or emote)
type MessageFragment struct {
	Emote    *EmbeddedEmote `json:"emote"`
	Text     string         `json:"text"`
	Typename string         `json:"__typename"`
}

// EmbeddedEmote represents an emote in a message
type EmbeddedEmote struct {
	ID       string `json:"id"`
	EmoteID  string `json:"emoteID"`
	From     int    `json:"from"`
	Typename string `json:"__typename"`
}

// Badge represents a user badge
type Badge struct {
	ID       string `json:"id"`
	SetID    string `json:"setID"`
	Version  string `json:"version"`
	Typename string `json:"__typename"`
}

// ChatUser represents the user who sent a chat message
type ChatUser struct {
	ID          string `json:"id"`
	Login       string `json:"login"`
	DisplayName string `json:"displayName"`
	Typename    string `json:"__typename"`
}
