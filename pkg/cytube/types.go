package cytube

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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

// FlexibleDuration handles both string and int duration values from the server
type FlexibleDuration int

// UnmarshalJSON implements json.Unmarshaler to handle both string and int values
func (fd *FlexibleDuration) UnmarshalJSON(data []byte) error {
	// Check for null
	if string(data) == "null" {
		return fmt.Errorf("duration cannot be null")
	}

	// Try to unmarshal as int first
	var intVal int
	if err := json.Unmarshal(data, &intVal); err == nil {
		*fd = FlexibleDuration(intVal)
		return nil
	}

	// Try to unmarshal as string
	var strVal string
	if err := json.Unmarshal(data, &strVal); err != nil {
		return fmt.Errorf("duration must be a string or number: %w", err)
	}

	// Check if it's a time format (HH:MM:SS or MM:SS)
	if strings.Contains(strVal, ":") {
		parts := strings.Split(strVal, ":")
		var seconds int

		switch len(parts) {
		case 2: // MM:SS
			minutes, err := strconv.Atoi(parts[0])
			if err != nil {
				return fmt.Errorf("invalid minutes in duration: %w", err)
			}
			secs, err := strconv.Atoi(parts[1])
			if err != nil {
				return fmt.Errorf("invalid seconds in duration: %w", err)
			}
			seconds = minutes*60 + secs

		case 3: // HH:MM:SS
			hours, err := strconv.Atoi(parts[0])
			if err != nil {
				return fmt.Errorf("invalid hours in duration: %w", err)
			}
			minutes, err := strconv.Atoi(parts[1])
			if err != nil {
				return fmt.Errorf("invalid minutes in duration: %w", err)
			}
			secs, err := strconv.Atoi(parts[2])
			if err != nil {
				return fmt.Errorf("invalid seconds in duration: %w", err)
			}
			seconds = hours*3600 + minutes*60 + secs

		default:
			return fmt.Errorf("invalid time format: %s", strVal)
		}

		*fd = FlexibleDuration(seconds)
		return nil
	}

	// Convert plain number string to int
	intVal, err := strconv.Atoi(strVal)
	if err != nil {
		return fmt.Errorf("duration string must be a valid number or time format: %w", err)
	}

	*fd = FlexibleDuration(intVal)
	return nil
}

// Int returns the duration as an int
func (fd FlexibleDuration) Int() int {
	return int(fd)
}

// MediaPayload represents media change event data
type MediaPayload struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Duration FlexibleDuration `json:"duration"`
	Title    string           `json:"title"`
}
