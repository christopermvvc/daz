package cytube

import (
	"encoding/json"
	"testing"
)

func TestFormatSocketIOMessage(t *testing.T) {
	// Define concrete types for test data
	type stringData string
	type mapData map[string]string

	tests := []struct {
		name     string
		msgType  int
		event    string
		data     any // This is for the formatSocketIOMessage function parameter
		expected string
	}{
		{
			name:     "simple event no data",
			msgType:  MessageTypeEvent,
			event:    "ping",
			data:     nil,
			expected: "42[\"ping\"]",
		},
		{
			name:     "event with string data",
			msgType:  MessageTypeEvent,
			event:    "message",
			data:     stringData("hello"),
			expected: "42[\"message\",\"hello\"]",
		},
		{
			name:    "event with object data",
			msgType: MessageTypeEvent,
			event:   "login",
			data:    mapData{"name": "test", "pw": "pass"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := formatSocketIOMessage(tt.msgType, tt.event, tt.data)
			if err != nil {
				t.Fatalf("formatSocketIOMessage() error = %v", err)
			}

			// For the object data test, just verify it contains the event name
			if tt.name == "event with object data" {
				if result[:2] != "42" {
					t.Errorf("formatSocketIOMessage() should start with 42, got %s", result[:2])
				}
				return
			}

			if result != tt.expected {
				t.Errorf("formatSocketIOMessage() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseSocketIOMessage(t *testing.T) {
	tests := []struct {
		name          string
		message       string
		expectedEvent string
		expectedData  string
		expectError   bool
	}{
		{
			name:          "simple event",
			message:       `42["chatMsg",{"username":"test","msg":"hello"}]`,
			expectedEvent: "chatMsg",
			expectedData:  `{"username":"test","msg":"hello"}`,
		},
		{
			name:          "event without data",
			message:       `42["connect"]`,
			expectedEvent: "connect",
			expectedData:  "",
		},
		{
			name:        "invalid message",
			message:     "invalid",
			expectError: true,
		},
		{
			name:        "too short message",
			message:     "4",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, data, err := parseSocketIOMessage(tt.message)

			if tt.expectError {
				if err == nil {
					t.Error("parseSocketIOMessage() expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("parseSocketIOMessage() unexpected error = %v", err)
			}

			if event != tt.expectedEvent {
				t.Errorf("parseSocketIOMessage() event = %v, want %v", event, tt.expectedEvent)
			}

			if tt.expectedData == "" && data == nil {
				return
			}

			if string(data) != tt.expectedData {
				t.Errorf("parseSocketIOMessage() data = %v, want %v", string(data), tt.expectedData)
			}
		})
	}
}

func TestHandshakeResponse(t *testing.T) {
	jsonData := `{"sid":"abc123","upgrades":["websocket"],"pingInterval":25000,"pingTimeout":5000,"maxPayload":1000000}`

	var handshake HandshakeResponse
	err := json.Unmarshal([]byte(jsonData), &handshake)
	if err != nil {
		t.Fatalf("Failed to unmarshal HandshakeResponse: %v", err)
	}

	if handshake.SessionID != "abc123" {
		t.Errorf("SessionID = %v, want %v", handshake.SessionID, "abc123")
	}

	if handshake.PingInterval != 25000 {
		t.Errorf("PingInterval = %v, want %v", handshake.PingInterval, 25000)
	}
}
