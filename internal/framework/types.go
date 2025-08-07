package framework

import (
	"encoding/json"
	"time"
)

// EventData represents all possible data types that can be sent through the event bus
type EventData struct {
	// For chat messages
	ChatMessage    *ChatMessageData    `json:"chat_message,omitempty"`
	PrivateMessage *PrivateMessageData `json:"private_message,omitempty"`

	// For user events
	UserJoin  *UserJoinData  `json:"user_join,omitempty"`
	UserLeave *UserLeaveData `json:"user_leave,omitempty"`

	// For video events
	VideoChange *VideoChangeData `json:"video_change,omitempty"`
	QueueUpdate *QueueUpdateData `json:"queue_update,omitempty"`
	MediaUpdate *MediaUpdateData `json:"media_update,omitempty"`

	// For SQL operations
	SQLQueryRequest  *SQLQueryRequest  `json:"sql_query_request,omitempty"`
	SQLQueryResponse *SQLQueryResponse `json:"sql_query_response,omitempty"`
	SQLExecRequest   *SQLExecRequest   `json:"sql_exec_request,omitempty"`
	SQLExecResponse  *SQLExecResponse  `json:"sql_exec_response,omitempty"`
	SQLBatchRequest  *SQLBatchRequest  `json:"sql_batch_request,omitempty"`
	SQLBatchResponse *SQLBatchResponse `json:"sql_batch_response,omitempty"`

	// For plugin communication
	PluginRequest  *PluginRequest  `json:"plugin_request,omitempty"`
	PluginResponse *PluginResponse `json:"plugin_response,omitempty"`

	// For retry operations
	RetryRequest *RetryRequest `json:"retry_request,omitempty"`
	RetryStatus  *RetryStatus  `json:"retry_status,omitempty"`

	// For raw messages (e.g., sending to Cytube)
	RawMessage *RawMessageData `json:"raw_message,omitempty"`

	// For generic key-value data
	KeyValue map[string]string `json:"key_value,omitempty"`

	// For raw event passthrough - allows passing original events without transformation
	RawEvent     Event  `json:"-"`                        // Don't serialize, this is for internal event bus use only
	RawEventType string `json:"raw_event_type,omitempty"` // Identifies the type of raw event
}

// ChatMessageData represents chat message specific data
type ChatMessageData struct {
	Username    string `json:"username"`
	Message     string `json:"message"`
	UserRank    int    `json:"user_rank"`
	UserID      string `json:"user_id"`
	Channel     string `json:"channel"`
	MessageTime int64  `json:"message_time"`
}

// PrivateMessageData represents private message specific data
type PrivateMessageData struct {
	FromUser    string `json:"from_user"`
	ToUser      string `json:"to_user"`
	Message     string `json:"message"`
	MessageTime int64  `json:"message_time"`
	Channel     string `json:"channel"`
}

// UserJoinData represents user join event data
type UserJoinData struct {
	Username string `json:"username"`
	UserRank int    `json:"user_rank"`
	Channel  string `json:"channel,omitempty"`
}

// UserLeaveData represents user leave event data
type UserLeaveData struct {
	Username string `json:"username"`
	Channel  string `json:"channel,omitempty"`
}

// VideoChangeData represents video change event data
type VideoChangeData struct {
	VideoID   string `json:"video_id"`
	VideoType string `json:"video_type"`
	Duration  int    `json:"duration"`
	Title     string `json:"title"`
	Channel   string `json:"channel"`
}

// QueueItem represents a single item in the queue
type QueueItem struct {
	UID       string `json:"uid"` // Unique identifier for playlist item
	Position  int    `json:"position"`
	MediaID   string `json:"media_id"`
	MediaType string `json:"media_type"`
	Title     string `json:"title"`
	Duration  int    `json:"duration"`
	QueuedBy  string `json:"queued_by"`
	QueuedAt  int64  `json:"queued_at"` // Unix timestamp
}

// QueueUpdateData represents queue update event data
type QueueUpdateData struct {
	Channel     string      `json:"channel"`
	Action      string      `json:"action"` // "add", "remove", "move", "clear", "full"
	Items       []QueueItem `json:"items,omitempty"`
	Position    int         `json:"position,omitempty"`     // For add/remove/move
	NewPosition int         `json:"new_position,omitempty"` // For move
}

// MediaUpdateData represents media synchronization update data
type MediaUpdateData struct {
	CurrentTime float64 `json:"currentTime"`
	Paused      bool    `json:"paused"`
}

// RawMessageData represents raw message data for sending to external systems
type RawMessageData struct {
	Message string `json:"message"`
	Channel string `json:"channel"`
	Target  string `json:"target,omitempty"`
}

// RequestData represents data that can be sent in plugin requests
type RequestData struct {
	// Command execution
	Command *CommandData `json:"command,omitempty"`

	// Status query
	StatusQuery *StatusQueryData `json:"status_query,omitempty"`

	// Configuration update
	ConfigUpdate *ConfigUpdateData `json:"config_update,omitempty"`

	// Generic key-value data
	KeyValue map[string]string `json:"key_value,omitempty"`

	// Raw JSON for extensibility
	RawJSON json.RawMessage `json:"raw_json,omitempty"`
}

// ResponseData represents data that can be sent in plugin responses
type ResponseData struct {
	// Status response
	Status *PluginStatus `json:"status,omitempty"`

	// Command result
	CommandResult *CommandResultData `json:"command_result,omitempty"`

	// Generic key-value data
	KeyValue map[string]string `json:"key_value,omitempty"`

	// Raw JSON for extensibility
	RawJSON json.RawMessage `json:"raw_json,omitempty"`
}

// CommandData represents a command to be executed
type CommandData struct {
	Name   string            `json:"name"`
	Args   []string          `json:"args"`
	Params map[string]string `json:"params,omitempty"`
}

// StatusQueryData represents a status query request
type StatusQueryData struct {
	IncludeMetrics bool `json:"include_metrics"`
	IncludeErrors  bool `json:"include_errors"`
}

// ConfigUpdateData represents a configuration update request
type ConfigUpdateData struct {
	Section string          `json:"section"`
	Values  json.RawMessage `json:"values"`
}

// CommandResultData represents the result of a command execution
type CommandResultData struct {
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// RetryRequest represents a request to retry a failed operation
type RetryRequest struct {
	OperationID   string            `json:"operation_id"`
	OperationType string            `json:"operation_type"`
	TargetPlugin  string            `json:"target_plugin"`
	EventType     string            `json:"event_type"`
	Payload       json.RawMessage   `json:"payload"`
	Metadata      map[string]string `json:"metadata"`
	Priority      int               `json:"priority"`
}

// RetryStatus represents the status of a retry operation
type RetryStatus struct {
	OperationID   string    `json:"operation_id"`
	OperationType string    `json:"operation_type"`
	RetryCount    int       `json:"retry_count"`
	MaxRetries    int       `json:"max_retries"`
	Status        string    `json:"status"` // queued, processing, success, failed, dead_letter
	LastError     string    `json:"last_error,omitempty"`
	NextRetryTime time.Time `json:"next_retry_time,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}
