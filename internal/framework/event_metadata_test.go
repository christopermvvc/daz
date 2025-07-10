package framework

import (
	"testing"
	"time"
)

func TestNewEventMetadata(t *testing.T) {
	source := "test-plugin"
	eventType := "test.event"

	metadata := NewEventMetadata(source, eventType)

	if metadata.Source != source {
		t.Errorf("Expected source %s, got %s", source, metadata.Source)
	}

	if metadata.EventType != eventType {
		t.Errorf("Expected event type %s, got %s", eventType, metadata.EventType)
	}

	if metadata.Priority != 0 {
		t.Errorf("Expected priority 0, got %d", metadata.Priority)
	}

	if metadata.Loggable {
		t.Error("Expected loggable to be false by default")
	}

	// Check timestamp is recent
	if time.Since(metadata.Timestamp) > time.Second {
		t.Error("Timestamp should be recent")
	}
}

func TestEventMetadataChaining(t *testing.T) {
	metadata := NewEventMetadata("source", "event.type").
		WithCorrelationID("test-123").
		WithTarget("target-plugin").
		WithLogging("info").
		WithTags("tag1", "tag2").
		WithReplyTo("reply-plugin").
		WithTimeout(30 * time.Second).
		WithPriority(PriorityHigh)

	if metadata.CorrelationID != "test-123" {
		t.Errorf("Expected correlation ID test-123, got %s", metadata.CorrelationID)
	}

	if metadata.Target != "target-plugin" {
		t.Errorf("Expected target target-plugin, got %s", metadata.Target)
	}

	if !metadata.Loggable {
		t.Error("Expected loggable to be true")
	}

	if metadata.LogLevel != "info" {
		t.Errorf("Expected log level info, got %s", metadata.LogLevel)
	}

	if len(metadata.Tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(metadata.Tags))
	}

	if metadata.ReplyTo != "reply-plugin" {
		t.Errorf("Expected reply to reply-plugin, got %s", metadata.ReplyTo)
	}

	if metadata.Timeout != 30*time.Second {
		t.Errorf("Expected timeout 30s, got %s", metadata.Timeout)
	}

	if metadata.Priority != PriorityHigh {
		t.Errorf("Expected priority %d, got %d", PriorityHigh, metadata.Priority)
	}
}

func TestWithLogging(t *testing.T) {
	metadata := NewEventMetadata("source", "event")

	// Initially not loggable
	if metadata.Loggable {
		t.Error("Should not be loggable by default")
	}

	// Enable logging
	metadata.WithLogging("debug")

	if !metadata.Loggable {
		t.Error("Should be loggable after WithLogging")
	}

	if metadata.LogLevel != "debug" {
		t.Errorf("Expected log level debug, got %s", metadata.LogLevel)
	}
}

func TestWithTags(t *testing.T) {
	metadata := NewEventMetadata("source", "event")

	// Add tags in multiple calls
	metadata.WithTags("tag1")
	metadata.WithTags("tag2", "tag3")

	if len(metadata.Tags) != 3 {
		t.Errorf("Expected 3 tags, got %d", len(metadata.Tags))
	}

	expectedTags := []string{"tag1", "tag2", "tag3"}
	for i, tag := range metadata.Tags {
		if tag != expectedTags[i] {
			t.Errorf("Expected tag %s at position %d, got %s", expectedTags[i], i, tag)
		}
	}
}

func TestEnhancedEventData(t *testing.T) {
	// Test that EnhancedEventData properly embeds both EventData and metadata
	eventData := &EventData{
		ChatMessage: &ChatMessageData{
			Username: "testuser",
			Message:  "test message",
		},
	}

	metadata := NewEventMetadata("chat", "cytube.event.chatMsg")

	enhanced := &EnhancedEventData{
		EventData: eventData,
		Metadata:  metadata,
	}

	if enhanced.ChatMessage.Username != "testuser" {
		t.Error("Should be able to access embedded EventData fields")
	}

	if enhanced.Metadata.EventType != "cytube.event.chatMsg" {
		t.Error("Should be able to access metadata fields")
	}
}

func TestWithPriorityValidation(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"normal priority", PriorityNormal, PriorityNormal},
		{"high priority", PriorityHigh, PriorityHigh},
		{"urgent priority", PriorityUrgent, PriorityUrgent},
		{"critical priority", PriorityCritical, PriorityCritical},
		{"below normal clamped to normal", -1, PriorityNormal},
		{"above critical clamped to critical", 5, PriorityCritical},
		{"very high value clamped to critical", 100, PriorityCritical},
		{"very low value clamped to normal", -100, PriorityNormal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata := NewEventMetadata("test", "test.event")
			metadata.WithPriority(tt.input)

			if metadata.Priority != tt.expected {
				t.Errorf("WithPriority(%d) = %d, expected %d", tt.input, metadata.Priority, tt.expected)
			}
		})
	}
}
