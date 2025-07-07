package framework

import (
	"encoding/json"
	"time"
)

type CytubeEvent struct {
	EventType   string            `json:"type"`
	EventTime   time.Time         `json:"timestamp"`
	ChannelName string            `json:"channel"`
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
	Username string `json:"username"`
	Message  string `json:"message"`
	UserRank int    `json:"user_rank"`
	UserID   string `json:"user_id"`
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

type UserLeaveEvent struct {
	CytubeEvent
	Username string `json:"username"`
}

type SQLRequest struct {
	ID        string        `json:"id"`
	Query     string        `json:"query"`
	Params    []interface{} `json:"params"` // SQL params remain interface{} as per standard practice
	Timeout   time.Duration `json:"timeout"`
	RequestBy string        `json:"request_by"`
}

type SQLResponse struct {
	ID       string `json:"id"`
	Success  bool   `json:"success"`
	Error    error  `json:"error,omitempty"`
	Rows     []byte `json:"rows,omitempty"`
	RowCount int64  `json:"row_count,omitempty"`
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
