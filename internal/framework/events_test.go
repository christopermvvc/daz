package framework

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCytubeEvent(t *testing.T) {
	now := time.Now()
	event := &CytubeEvent{
		EventType:   "chatMsg",
		EventTime:   now,
		ChannelName: "test-channel",
		RawData:     json.RawMessage(`{"test": "data"}`),
		Metadata:    map[string]string{"key": "value"},
	}

	if event.Type() != "chatMsg" {
		t.Errorf("Type() = %v, want %v", event.Type(), "chatMsg")
	}

	if event.Timestamp() != now {
		t.Errorf("Timestamp() = %v, want %v", event.Timestamp(), now)
	}
}

func TestChatMessageEvent(t *testing.T) {
	event := &ChatMessageEvent{
		CytubeEvent: CytubeEvent{
			EventType:   "chatMsg",
			EventTime:   time.Now(),
			ChannelName: "test-channel",
		},
		Username: "testuser",
		Message:  "Hello, world!",
		UserRank: 1,
		UserID:   "user123",
	}

	if event.Username != "testuser" {
		t.Errorf("Username = %v, want %v", event.Username, "testuser")
	}

	if event.Message != "Hello, world!" {
		t.Errorf("Message = %v, want %v", event.Message, "Hello, world!")
	}

	if event.Type() != "chatMsg" {
		t.Errorf("Type() = %v, want %v", event.Type(), "chatMsg")
	}
}

func TestSQLRequest(t *testing.T) {
	req := &SQLRequest{
		ID:        "req123",
		Query:     "SELECT * FROM users WHERE id = $1",
		Params:    []SQLParam{{Value: 123}},
		Timeout:   5 * time.Second,
		RequestBy: "test-plugin",
	}

	if req.ID != "req123" {
		t.Errorf("ID = %v, want %v", req.ID, "req123")
	}

	if len(req.Params) != 1 {
		t.Errorf("len(Params) = %v, want %v", len(req.Params), 1)
	}
}

func TestPluginRequest(t *testing.T) {
	req := &PluginRequest{
		ID:   "req456",
		From: "plugin-a",
		To:   "plugin-b",
		Type: "command.execute",
		Data: &RequestData{
			KeyValue: map[string]string{"command": "test"},
		},
	}

	if req.From != "plugin-a" {
		t.Errorf("From = %v, want %v", req.From, "plugin-a")
	}

	if req.To != "plugin-b" {
		t.Errorf("To = %v, want %v", req.To, "plugin-b")
	}
}
