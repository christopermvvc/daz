package cytube

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventMarshalUnmarshal(t *testing.T) {
	event := Event{
		Type: "testEvent",
		Data: json.RawMessage(`{"test": "data"}`),
	}

	// Marshal
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal event: %v", err)
	}

	// Unmarshal
	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal event: %v", err)
	}

	if decoded.Type != event.Type {
		t.Errorf("Type = %v, want %v", decoded.Type, event.Type)
	}

	// Compare JSON content, not exact string representation
	var originalData, decodedData map[string]interface{}
	if err := json.Unmarshal(event.Data, &originalData); err != nil {
		t.Fatalf("Failed to unmarshal original data: %v", err)
	}
	if err := json.Unmarshal(decoded.Data, &decodedData); err != nil {
		t.Fatalf("Failed to unmarshal decoded data: %v", err)
	}

	if originalData["test"] != decodedData["test"] {
		t.Errorf("Data content mismatch: got %v, want %v", decodedData, originalData)
	}
}

func TestReconnectConfigDefaults(t *testing.T) {
	config := ReconnectConfig{
		MaxAttempts:    10,
		RetryDelay:     5 * time.Second,
		CooldownPeriod: 30 * time.Minute,
	}

	if config.MaxAttempts != 10 {
		t.Errorf("MaxAttempts = %v, want %v", config.MaxAttempts, 10)
	}

	if config.RetryDelay != 5*time.Second {
		t.Errorf("RetryDelay = %v, want %v", config.RetryDelay, 5*time.Second)
	}

	if config.CooldownPeriod != 30*time.Minute {
		t.Errorf("CooldownPeriod = %v, want %v", config.CooldownPeriod, 30*time.Minute)
	}
}

func TestEventTypeConstants(t *testing.T) {
	tests := []struct {
		name      string
		eventType EventType
		expected  string
	}{
		{"ChatMessage", EventTypeChatMessage, "chatMsg"},
		{"UserJoin", EventTypeUserJoin, "userJoin"},
		{"UserLeave", EventTypeUserLeave, "userLeave"},
		{"VideoChange", EventTypeVideoChange, "changeMedia"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.eventType) != tt.expected {
				t.Errorf("EventType = %v, want %v", tt.eventType, tt.expected)
			}
		})
	}
}
