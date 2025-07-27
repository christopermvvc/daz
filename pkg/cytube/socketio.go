package cytube

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Engine.IO packet types (EIO protocol layer)
const (
	EngineIOOpen    = "0"
	EngineIOClose   = "1"
	EngineIOPing    = "2"
	EngineIOPong    = "3"
	EngineIOMessage = "4"
	EngineIOUpgrade = "5"
	EngineIONoop    = "6"
)

// Socket.IO message types
const (
	MessageTypeConnect    = 0
	MessageTypeDisconnect = 1
	MessageTypeEvent      = 2
	MessageTypeAck        = 3
	MessageTypeError      = 4
)

type HandshakeResponse struct {
	SessionID    string   `json:"sid"`
	Upgrades     []string `json:"upgrades"`
	PingInterval int      `json:"pingInterval"`
	PingTimeout  int      `json:"pingTimeout"`
	MaxPayload   int      `json:"maxPayload"`
}

func formatSocketIOMessage(msgType int, event string, data json.RawMessage) (string, error) {
	// Format: 42["event_name",data]
	// First element is the event name as a JSON string
	eventNameJSON, err := json.Marshal(event)
	if err != nil {
		return "", fmt.Errorf("failed to marshal event name: %w", err)
	}

	// Build the array manually
	var jsonData []byte
	if len(data) > 0 {
		jsonData = []byte(fmt.Sprintf("[%s,%s]", eventNameJSON, data))
	} else {
		jsonData = []byte(fmt.Sprintf("[%s]", eventNameJSON))
	}

	return fmt.Sprintf("%s%d%s", EngineIOMessage, msgType, jsonData), nil
}

func parseSocketIOMessage(msg string) (string, json.RawMessage, error) {
	// Skip packet type and message type
	if len(msg) < 2 {
		return "", nil, fmt.Errorf("message too short")
	}

	// Find the JSON array start
	jsonStart := strings.Index(msg, "[")
	if jsonStart == -1 {
		return "", nil, fmt.Errorf("no JSON array found")
	}

	var eventData []json.RawMessage
	if err := json.Unmarshal([]byte(msg[jsonStart:]), &eventData); err != nil {
		return "", nil, fmt.Errorf("failed to parse event data: %w", err)
	}

	if len(eventData) == 0 {
		return "", nil, fmt.Errorf("empty event data")
	}

	var eventName string
	if err := json.Unmarshal(eventData[0], &eventName); err != nil {
		return "", nil, fmt.Errorf("failed to parse event name: %w", err)
	}

	var data json.RawMessage
	if len(eventData) > 1 {
		data = eventData[1]
	}

	return eventName, data, nil
}
