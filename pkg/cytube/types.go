package cytube

import (
	"encoding/json"
	"time"
)

// Event represents a Cytube event
type Event struct {
	Type string          `json:"name"`
	Data json.RawMessage `json:"args"`
}

// ReconnectConfig holds configuration for connection retry logic
type ReconnectConfig struct {
	MaxAttempts    int
	RetryDelay     time.Duration
	CooldownPeriod time.Duration
	OnReconnecting func(attempt int)
	OnCooldown     func(until time.Time)
}

// EventType constants
type EventType string

const (
	EventTypeChatMessage EventType = "chatMsg"
	EventTypeUserJoin    EventType = "userJoin"
	EventTypeUserLeave   EventType = "userLeave"
	EventTypeVideoChange EventType = "changeMedia"
)

// ChannelJoinData represents the data sent when joining a channel
type ChannelJoinData struct {
	Name string `json:"name"`
}

// LoginData represents the data sent when logging in
type LoginData struct {
	Name     string `json:"name"`
	Password string `json:"pw"`
}

// ChatMessagePayload represents the incoming chat message data
type ChatMessagePayload struct {
	Username string `json:"username"`
	Message  string `json:"msg"`
	Rank     int    `json:"rank"`
	Meta     struct {
		UID string `json:"uid"`
	} `json:"meta"`
}

// UserPayload represents user join/leave event data
type UserPayload struct {
	Name string `json:"name"`
	Rank int    `json:"rank"`
}

// MediaPayload represents media change event data
type MediaPayload struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Duration int    `json:"duration"`
	Title    string `json:"title"`
}
