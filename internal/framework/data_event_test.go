package framework

import (
	"testing"
	"time"
)

func TestDataEvent(t *testing.T) {
	// Create test event data
	eventData := &EventData{
		ChatMessage: &ChatMessageData{
			Username: "testuser",
			Message:  "!test command",
			UserRank: 1,
			UserID:   "123",
		},
	}

	// Create a DataEvent
	dataEvent := NewDataEvent("test.event", eventData)

	// Test Type() method
	if dataEvent.Type() != "test.event" {
		t.Errorf("Type() = %s, want %s", dataEvent.Type(), "test.event")
	}

	// Test Timestamp() method
	now := time.Now()
	if dataEvent.Timestamp().After(now) {
		t.Error("Timestamp() returned a time in the future")
	}
	if dataEvent.Timestamp().Before(now.Add(-1 * time.Second)) {
		t.Error("Timestamp() returned a time too far in the past")
	}

	// Test Data field
	if dataEvent.Data == nil {
		t.Fatal("Data field is nil")
	}
	if dataEvent.Data.ChatMessage == nil {
		t.Fatal("ChatMessage field is nil")
	}
	if dataEvent.Data.ChatMessage.Username != "testuser" {
		t.Errorf("ChatMessage.Username = %s, want %s", dataEvent.Data.ChatMessage.Username, "testuser")
	}
}

func TestDataEventImplementsEventInterface(t *testing.T) {
	// Verify that DataEvent implements the Event interface
	var _ Event = &DataEvent{}
	var _ Event = NewDataEvent("test", nil)
}
