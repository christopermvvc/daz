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
	Time int64 `json:"time"`
}

// ChatMessageSendPayload represents outgoing chat message data
type ChatMessageSendPayload struct {
	Message string `json:"msg"`
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

// MediaUpdatePayload represents media synchronization update event data
type MediaUpdatePayload struct {
	CurrentTime float64 `json:"currentTime"`
	Paused      bool    `json:"paused"`
}

// LoginPayload represents user authentication event data
type LoginPayload struct {
	Name    string   `json:"name"`
	Rank    int      `json:"rank"`
	Success bool     `json:"success"`
	Reason  string   `json:"reason,omitempty"`  // Present on failure
	IP      string   `json:"ip,omitempty"`      // May be present for admins
	Aliases []string `json:"aliases,omitempty"` // Previous usernames
}

// AddUserPayload represents user registration/addition event data
type AddUserPayload struct {
	Name       string    `json:"name"`
	Rank       int       `json:"rank"`
	AddedBy    string    `json:"addedBy,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	Registered bool      `json:"registered"`
	Email      string    `json:"email,omitempty"` // May be hidden
}

// MetaPayload represents channel metadata change event data
type MetaPayload struct {
	Field      string `json:"field"`     // What changed
	OldValue   string `json:"oldValue"`  // Previous value as string
	NewValue   string `json:"newValue"`  // New value as string
	ChangedBy  string `json:"changedBy"` // User who made change
	ChangeType string `json:"type"`      // "title", "description", "settings", etc.
}

// PlaylistItem represents a single item in a playlist event
type PlaylistItem struct {
	ID       string `json:"uid"`
	MediaID  string `json:"media_id"`
	Type     string `json:"type"` // youtube, vimeo, etc.
	Title    string `json:"title"`
	Duration int    `json:"duration"` // seconds
	AddedBy  string `json:"queueby"`
	Temp     bool   `json:"temp"` // Temporary addition
}

// PlaylistPayload represents playlist modification event data
type PlaylistPayload struct {
	Action   string         `json:"action"` // "add", "remove", "move", "clear"
	Items    []PlaylistItem `json:"items,omitempty"`
	Position int            `json:"pos,omitempty"`  // For add/move
	From     int            `json:"from,omitempty"` // For move
	To       int            `json:"to,omitempty"`   // For move
	User     string         `json:"user,omitempty"` // Who made change
}

// SetPlaylistMetaPayload represents playlist metadata event data
type SetPlaylistMetaPayload struct {
	Count   int    `json:"count"`
	RawTime int    `json:"rawTime"`
	Time    string `json:"time"`
}

// SetUserRankPayload represents user rank change event data
type SetUserRankPayload struct {
	Name string `json:"name"`
	Rank int    `json:"rank"`
}

// SetPermissionsPayload represents channel permissions event data
type SetPermissionsPayload struct {
	Seeplaylist              float64 `json:"seeplaylist"`
	Playlistadd              float64 `json:"playlistadd"`
	Playlistnext             float64 `json:"playlistnext"`
	Playlistmove             float64 `json:"playlistmove"`
	Playlistdelete           float64 `json:"playlistdelete"`
	Playlistjump             float64 `json:"playlistjump"`
	Playlistaddlist          float64 `json:"playlistaddlist"`
	Oplaylistadd             float64 `json:"oplaylistadd"`
	Oplaylistnext            float64 `json:"oplaylistnext"`
	Oplaylistmove            float64 `json:"oplaylistmove"`
	Oplaylistdelete          float64 `json:"oplaylistdelete"`
	Oplaylistjump            float64 `json:"oplaylistjump"`
	Oplaylistaddlist         float64 `json:"oplaylistaddlist"`
	Playlistaddcustom        float64 `json:"playlistaddcustom"`
	Playlistaddrawfile       float64 `json:"playlistaddrawfile"`
	Playlistaddlive          float64 `json:"playlistaddlive"`
	Exceedmaxlength          float64 `json:"exceedmaxlength"`
	Addnontemp               float64 `json:"addnontemp"`
	Settemp                  float64 `json:"settemp"`
	Playlistshuffle          float64 `json:"playlistshuffle"`
	Playlistclear            float64 `json:"playlistclear"`
	Pollctl                  float64 `json:"pollctl"`
	Pollvote                 float64 `json:"pollvote"`
	Viewhiddenpoll           float64 `json:"viewhiddenpoll"`
	Voteskip                 float64 `json:"voteskip"`
	Viewvoteskip             float64 `json:"viewvoteskip"`
	Mute                     float64 `json:"mute"`
	Kick                     float64 `json:"kick"`
	Ban                      float64 `json:"ban"`
	Motdedit                 float64 `json:"motdedit"`
	Filteredit               float64 `json:"filteredit"`
	Filterimport             float64 `json:"filterimport"`
	Emoteedit                float64 `json:"emoteedit"`
	Emoteimport              float64 `json:"emoteimport"`
	Playlistlock             float64 `json:"playlistlock"`
	Leaderctl                float64 `json:"leaderctl"`
	Drink                    float64 `json:"drink"`
	Chat                     float64 `json:"chat"`
	Chatclear                float64 `json:"chatclear"`
	Exceedmaxitems           float64 `json:"exceedmaxitems"`
	Deletefromchannellib     float64 `json:"deletefromchannellib"`
	Exceedmaxdurationperuser float64 `json:"exceedmaxdurationperuser"`
}

// AntifloodParams represents chat antiflood configuration
type AntifloodParams struct {
	Burst     int `json:"burst"`
	Sustained int `json:"sustained"`
	Cooldown  int `json:"cooldown"`
}

// ChannelOptsPayload represents channel options event data
type ChannelOptsPayload struct {
	AllowVoteskip              bool            `json:"allow_voteskip"`
	VoteskipRatio              float64         `json:"voteskip_ratio"`
	AfkTimeout                 int             `json:"afk_timeout"`
	Pagetitle                  string          `json:"pagetitle"`
	Maxlength                  int             `json:"maxlength"`
	ExternalCSS                string          `json:"externalcss"`
	ExternalJS                 string          `json:"externaljs"`
	ChatAntiflood              bool            `json:"chat_antiflood"`
	ChatAntifloodParams        AntifloodParams `json:"chat_antiflood_params"`
	ShowPublic                 bool            `json:"show_public"`
	EnableLinkRegex            bool            `json:"enable_link_regex"`
	Password                   bool            `json:"password"`
	AllowDupes                 bool            `json:"allow_dupes"`
	Torbanned                  bool            `json:"torbanned"`
	BlockAnonymousUsers        bool            `json:"block_anonymous_users"`
	AllowAsciiControl          bool            `json:"allow_ascii_control"`
	PlaylistMaxPerUser         int             `json:"playlist_max_per_user"`
	NewUserChatDelay           int             `json:"new_user_chat_delay"`
	NewUserChatLinkDelay       int             `json:"new_user_chat_link_delay"`
	PlaylistMaxDurationPerUser int             `json:"playlist_max_duration_per_user"`
}

// ChannelCSSJSPayload represents channel CSS/JS event data
type ChannelCSSJSPayload struct {
	CSS     string `json:"css"`
	CSSHash string `json:"cssHash"`
	JS      string `json:"js"`
	JSHash  string `json:"jsHash"`
}

// SetUserMetaPayload represents user metadata update event data
type SetUserMetaPayload struct {
	Name string `json:"name"`
	Meta struct {
		AFK   bool `json:"afk"`
		Muted bool `json:"muted"`
	} `json:"meta"`
}

// SetAFKPayload represents AFK status change event data
type SetAFKPayload struct {
	Name string `json:"name"`
	AFK  bool   `json:"afk"`
}

// PrivateMessagePayload represents private message event data
type PrivateMessagePayload struct {
	Username string                 `json:"username"`
	Msg      string                 `json:"msg"`
	Meta     map[string]interface{} `json:"meta"`
	Time     int64                  `json:"time"`
	To       string                 `json:"to"`
}

// EventPayload is an interface for all event payloads that can be sent
type EventPayload interface {
	// IsEventPayload is a marker method to ensure only valid event types implement this
	IsEventPayload()
}

// Implement EventPayload for existing payload types
func (ChannelJoinData) IsEventPayload()        {}
func (LoginData) IsEventPayload()              {}
func (ChatMessagePayload) IsEventPayload()     {}
func (ChatMessageSendPayload) IsEventPayload() {}
func (UserPayload) IsEventPayload()            {}
func (MediaPayload) IsEventPayload()           {}
func (MediaUpdatePayload) IsEventPayload()     {}
func (LoginPayload) IsEventPayload()           {}
func (AddUserPayload) IsEventPayload()         {}
func (MetaPayload) IsEventPayload()            {}
func (PlaylistPayload) IsEventPayload()        {}
func (SetPlaylistMetaPayload) IsEventPayload() {}
func (SetUserRankPayload) IsEventPayload()     {}
func (SetPermissionsPayload) IsEventPayload()  {}
func (ChannelOptsPayload) IsEventPayload()     {}
func (ChannelCSSJSPayload) IsEventPayload()    {}

// GenericEventData represents parsed generic event data with common fields
type GenericEventData struct {
	User     string `json:"user,omitempty"`
	Username string `json:"username,omitempty"`
	Name     string `json:"name,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Action   string `json:"action,omitempty"`
	// Additional fields can be added as needed
}
