package cytube

import (
	"encoding/json"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
)

func TestNewParser(t *testing.T) {
	parser := NewParser("test-channel")
	if parser.channel != "test-channel" {
		t.Errorf("channel = %v, want %v", parser.channel, "test-channel")
	}
}

func TestParseChatMessage(t *testing.T) {
	parser := NewParser("test-channel")

	rawData := json.RawMessage(`{"username":"testuser","msg":"Hello world","rank":1,"meta":{"uid":"user123"}}`)
	event := Event{
		Type: "chatMsg",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	chatMsg, ok := parsed.(*framework.ChatMessageEvent)
	if !ok {
		t.Fatalf("Expected ChatMessageEvent, got %T", parsed)
	}

	if chatMsg.Username != "testuser" {
		t.Errorf("Username = %v, want %v", chatMsg.Username, "testuser")
	}

	if chatMsg.Message != "Hello world" {
		t.Errorf("Message = %v, want %v", chatMsg.Message, "Hello world")
	}

	if chatMsg.UserRank != 1 {
		t.Errorf("UserRank = %v, want %v", chatMsg.UserRank, 1)
	}

	if chatMsg.UserID != "user123" {
		t.Errorf("UserID = %v, want %v", chatMsg.UserID, "user123")
	}
}

func TestParseUserJoin(t *testing.T) {
	parser := NewParser("test-channel")

	rawData := json.RawMessage(`{"name":"newuser","rank":2}`)
	event := Event{
		Type: "userJoin",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	userJoin, ok := parsed.(*framework.UserJoinEvent)
	if !ok {
		t.Fatalf("Expected UserJoinEvent, got %T", parsed)
	}

	if userJoin.Username != "newuser" {
		t.Errorf("Username = %v, want %v", userJoin.Username, "newuser")
	}

	if userJoin.UserRank != 2 {
		t.Errorf("UserRank = %v, want %v", userJoin.UserRank, 2)
	}
}

func TestParseUserLeave(t *testing.T) {
	parser := NewParser("test-channel")

	rawData := json.RawMessage(`{"name":"leavinguser"}`)
	event := Event{
		Type: "userLeave",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	userLeave, ok := parsed.(*framework.UserLeaveEvent)
	if !ok {
		t.Fatalf("Expected UserLeaveEvent, got %T", parsed)
	}

	if userLeave.Username != "leavinguser" {
		t.Errorf("Username = %v, want %v", userLeave.Username, "leavinguser")
	}
}

func TestParseVideoChange(t *testing.T) {
	parser := NewParser("test-channel")

	rawData := json.RawMessage(`{"id":"dQw4w9WgXcQ","type":"youtube","duration":212,"title":"Never Gonna Give You Up"}`)
	event := Event{
		Type: "changeMedia",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	videoChange, ok := parsed.(*framework.VideoChangeEvent)
	if !ok {
		t.Fatalf("Expected VideoChangeEvent, got %T", parsed)
	}

	if videoChange.VideoID != "dQw4w9WgXcQ" {
		t.Errorf("VideoID = %v, want %v", videoChange.VideoID, "dQw4w9WgXcQ")
	}

	if videoChange.VideoType != "youtube" {
		t.Errorf("VideoType = %v, want %v", videoChange.VideoType, "youtube")
	}

	if videoChange.Duration != 212 {
		t.Errorf("Duration = %v, want %v", videoChange.Duration, 212)
	}

	if videoChange.Title != "Never Gonna Give You Up" {
		t.Errorf("Title = %v, want %v", videoChange.Title, "Never Gonna Give You Up")
	}
}

func TestParseVideoChangeWithStringDuration(t *testing.T) {
	parser := NewParser("test-channel")

	// Test with duration as a string
	rawData := json.RawMessage(`{"id":"abc123","type":"youtube","duration":"212","title":"Test Video"}`)
	event := Event{
		Type: "changeMedia",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	videoChange, ok := parsed.(*framework.VideoChangeEvent)
	if !ok {
		t.Fatalf("Expected VideoChangeEvent, got %T", parsed)
	}

	if videoChange.VideoID != "abc123" {
		t.Errorf("VideoID = %v, want %v", videoChange.VideoID, "abc123")
	}

	if videoChange.VideoType != "youtube" {
		t.Errorf("VideoType = %v, want %v", videoChange.VideoType, "youtube")
	}

	if videoChange.Duration != 212 {
		t.Errorf("Duration = %v, want %v", videoChange.Duration, 212)
	}

	if videoChange.Title != "Test Video" {
		t.Errorf("Title = %v, want %v", videoChange.Title, "Test Video")
	}
}

func TestParseUnknownEvent(t *testing.T) {
	parser := NewParser("test-channel")

	rawData := json.RawMessage(`{"some":"data"}`)
	event := Event{
		Type: "unknownEvent",
		Data: rawData,
	}

	parsed, err := parser.ParseEvent(event)
	if err != nil {
		t.Fatalf("ParseEvent failed: %v", err)
	}

	baseEvent, ok := parsed.(*framework.CytubeEvent)
	if !ok {
		t.Fatalf("Expected CytubeEvent, got %T", parsed)
	}

	if baseEvent.Type() != "unknownEvent" {
		t.Errorf("Type() = %v, want %v", baseEvent.Type(), "unknownEvent")
	}
}

func TestParseInvalidJSON(t *testing.T) {
	parser := NewParser("test-channel")

	tests := []struct {
		name      string
		eventType string
		data      json.RawMessage
	}{
		{"invalid chat JSON", "chatMsg", json.RawMessage(`invalid json`)},
		{"invalid user join", "userJoin", json.RawMessage(`123`)},
		{"invalid user leave", "userLeave", json.RawMessage(`true`)},
		{"invalid video change", "changeMedia", json.RawMessage(`"not an object"`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := Event{
				Type: tt.eventType,
				Data: tt.data,
			}

			_, err := parser.ParseEvent(event)
			if err == nil {
				t.Error("Expected error for invalid data, got nil")
			}
		})
	}
}
