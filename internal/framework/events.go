package framework

import (
	"encoding/json"
	"time"
)

type CytubeEvent struct {
	EventType   string            `json:"type"`
	EventTime   time.Time         `json:"timestamp"`
	ChannelName string            `json:"channel"`
	RoomID      string            `json:"room_id"` // ID of the room this event came from
	RawData     json.RawMessage   `json:"raw_data"`
	Metadata    map[string]string `json:"metadata"`
}

func (e *CytubeEvent) Type() string {
	return e.EventType
}

func (e *CytubeEvent) Timestamp() time.Time {
	return e.EventTime
}

type ChatMessageEvent struct {
	CytubeEvent
	Username    string `json:"username"`
	Message     string `json:"message"`
	UserRank    int    `json:"user_rank"`
	UserID      string `json:"user_id"`
	MessageTime int64  `json:"message_time"`
}

type UserJoinEvent struct {
	CytubeEvent
	Username string `json:"username"`
	UserRank int    `json:"user_rank"`
}

type VideoChangeEvent struct {
	CytubeEvent
	VideoID   string `json:"video_id"`
	VideoType string `json:"video_type"`
	Duration  int    `json:"duration"`
	Title     string `json:"title"`
}

type MediaUpdateEvent struct {
	CytubeEvent
	CurrentTime float64 `json:"current_time"`
	Paused      bool    `json:"paused"`
}

type UserLeaveEvent struct {
	CytubeEvent
	Username string `json:"username"`
}

type QueueEvent struct {
	CytubeEvent
	Action      string      `json:"action"` // "add", "remove", "move", "clear", "full"
	Items       []QueueItem `json:"items,omitempty"`
	Position    int         `json:"position,omitempty"`
	NewPosition int         `json:"new_position,omitempty"`
}

// SQLParam represents a SQL parameter value that can be safely passed to queries
// NOTE: The Value field uses `any` type because it must accept all SQL-compatible types
// including string, int, int64, float64, bool, time.Time, []byte, and nil.
// This is required for compatibility with database/sql driver interfaces.
type SQLParam struct {
	Value any `json:"value"`
}

// NewSQLParam creates a new SQLParam with the given value
func NewSQLParam(value any) SQLParam {
	return SQLParam{Value: value}
}

// SQLQueryRequest represents a SQL query request event
type SQLQueryRequest struct {
	ID            string        `json:"id"`
	CorrelationID string        `json:"correlation_id"`
	Query         string        `json:"query"`
	Params        []SQLParam    `json:"params"`
	Timeout       time.Duration `json:"timeout"`
	RequestBy     string        `json:"request_by"`
}

// SQLQueryResponse represents a SQL query response event
type SQLQueryResponse struct {
	ID            string              `json:"id"`
	CorrelationID string              `json:"correlation_id"`
	Success       bool                `json:"success"`
	Error         string              `json:"error,omitempty"`
	Columns       []string            `json:"columns,omitempty"`
	Rows          [][]json.RawMessage `json:"rows,omitempty"`
}

// SQLExecRequest represents a SQL exec request event
type SQLExecRequest struct {
	ID            string        `json:"id"`
	CorrelationID string        `json:"correlation_id"`
	Query         string        `json:"query"`
	Params        []SQLParam    `json:"params"`
	Timeout       time.Duration `json:"timeout"`
	RequestBy     string        `json:"request_by"`
}

// SQLExecResponse represents a SQL exec response event
type SQLExecResponse struct {
	ID            string `json:"id"`
	CorrelationID string `json:"correlation_id"`
	Success       bool   `json:"success"`
	Error         string `json:"error,omitempty"`
	RowsAffected  int64  `json:"rows_affected,omitempty"`
	LastInsertID  int64  `json:"last_insert_id,omitempty"`
}

type PluginRequest struct {
	ID      string       `json:"id"`
	From    string       `json:"from"`
	To      string       `json:"to"`
	Type    string       `json:"type"`
	Data    *RequestData `json:"data"`
	ReplyTo string       `json:"reply_to,omitempty"`
}

type PluginResponse struct {
	ID      string        `json:"id"`
	From    string        `json:"from"`
	Success bool          `json:"success"`
	Data    *ResponseData `json:"data,omitempty"`
	Error   string        `json:"error,omitempty"`
}

type LoginEvent struct {
	CytubeEvent
	Username   string   `json:"username"`
	UserRank   int      `json:"user_rank"`
	Success    bool     `json:"success"`
	FailReason string   `json:"fail_reason,omitempty"`
	IPAddress  string   `json:"ip_address,omitempty"`
	Aliases    []string `json:"aliases,omitempty"`
}

type AddUserEvent struct {
	CytubeEvent
	Username   string `json:"username"`
	UserRank   int    `json:"user_rank"`
	AddedBy    string `json:"added_by,omitempty"`
	Registered bool   `json:"registered"`
	Email      string `json:"email,omitempty"`
}

type ChannelMetaEvent struct {
	CytubeEvent
	Field      string `json:"field"`
	OldValue   string `json:"old_value"`
	NewValue   string `json:"new_value"`
	ChangedBy  string `json:"changed_by"`
	ChangeType string `json:"change_type"`
}

type PlaylistEvent struct {
	CytubeEvent
	Action    string      `json:"action"`
	Items     []QueueItem `json:"items,omitempty"`
	Position  int         `json:"position,omitempty"`
	FromPos   int         `json:"from_pos,omitempty"`
	ToPos     int         `json:"to_pos,omitempty"`
	User      string      `json:"user,omitempty"`
	ItemCount int         `json:"item_count"`
}

// PlaylistArrayEvent represents a full playlist sent as an array
type PlaylistArrayEvent struct {
	CytubeEvent
	Items []PlaylistItem `json:"items"`
}

// PlaylistItem represents a single item in the playlist
type PlaylistItem struct {
	MediaID   string                 `json:"media_id"`
	MediaType string                 `json:"media_type"`
	Title     string                 `json:"title"`
	Duration  int                    `json:"duration"`
	QueuedBy  string                 `json:"queued_by"`
	Position  int                    `json:"position"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// FieldValue represents a parsed field value from a generic event
type FieldValue struct {
	String  string  `json:"string,omitempty"`
	Number  float64 `json:"number,omitempty"`
	Boolean bool    `json:"boolean,omitempty"`
	IsNull  bool    `json:"is_null,omitempty"`
}

type GenericEvent struct {
	CytubeEvent
	UnknownType string                `json:"unknown_type"`
	ParsedData  map[string]FieldValue `json:"parsed_data,omitempty"`
	RawJSON     json.RawMessage       `json:"raw_json"`
}

type SetPlaylistMetaEvent struct {
	CytubeEvent
	Count         int    `json:"count"`
	RawTime       int    `json:"raw_time"`
	FormattedTime string `json:"formatted_time"`
}

type UserCountEvent struct {
	CytubeEvent
	Count int `json:"count"`
}

type SetUserRankEvent struct {
	CytubeEvent
	Username string `json:"username"`
	Rank     int    `json:"rank"`
}

type SetPlaylistLockedEvent struct {
	CytubeEvent
	Locked bool `json:"locked"`
}

type SetPermissionsEvent struct {
	CytubeEvent
	Permissions map[string]float64 `json:"permissions"`
}

type SetMotdEvent struct {
	CytubeEvent
	Motd string `json:"motd"`
}

// ChannelOption represents a single channel configuration option
type ChannelOption struct {
	StringValue string  `json:"string_value,omitempty"`
	IntValue    int     `json:"int_value,omitempty"`
	FloatValue  float64 `json:"float_value,omitempty"`
	BoolValue   bool    `json:"bool_value,omitempty"`
	ValueType   string  `json:"value_type"` // "string", "int", "float", "bool"
}

type ChannelOptsEvent struct {
	CytubeEvent
	Options map[string]ChannelOption `json:"options"`
}

type ChannelCSSJSEvent struct {
	CytubeEvent
	CSS     string `json:"css"`
	CSSHash string `json:"css_hash"`
	JS      string `json:"js"`
	JSHash  string `json:"js_hash"`
}

type SetUserMetaEvent struct {
	CytubeEvent
	Username string `json:"username"`
	AFK      bool   `json:"afk"`
	Muted    bool   `json:"muted"`
}

type SetAFKEvent struct {
	CytubeEvent
	Username string `json:"username"`
	AFK      bool   `json:"afk"`
}

type ClearVoteskipVoteEvent struct {
	CytubeEvent
}

type PrivateMessageEvent struct {
	CytubeEvent
	FromUser    string `json:"from_user"`
	ToUser      string `json:"to_user"`
	Message     string `json:"message"`
	MessageTime int64  `json:"message_time"`
}
