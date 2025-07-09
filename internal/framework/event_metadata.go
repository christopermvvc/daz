package framework

import (
	"time"
)

// EventMetadata provides enhanced metadata for event routing and logging decisions
type EventMetadata struct {
	// Core metadata
	CorrelationID string    `json:"correlation_id,omitempty"`
	Source        string    `json:"source"`           // Plugin that generated the event
	Target        string    `json:"target,omitempty"` // Target plugin for direct sends
	EventType     string    `json:"event_type"`       // Hierarchical type (e.g., "cytube.event.chatMsg")
	Timestamp     time.Time `json:"timestamp"`
	Priority      int       `json:"priority"` // 0 = normal, higher = higher priority

	// Logging metadata
	Loggable bool     `json:"loggable"`            // Should this event be logged?
	LogLevel string   `json:"log_level,omitempty"` // If loggable, at what level?
	Tags     []string `json:"tags,omitempty"`      // Additional tags for filtering

	// Request/Response metadata
	ReplyTo string        `json:"reply_to,omitempty"` // Where to send response
	Timeout time.Duration `json:"timeout,omitempty"`  // Request timeout
}

// EnhancedEventData extends EventData with metadata
type EnhancedEventData struct {
	*EventData
	Metadata *EventMetadata `json:"metadata"`
}

// NewEventMetadata creates metadata with defaults
func NewEventMetadata(source, eventType string) *EventMetadata {
	return &EventMetadata{
		Source:    source,
		EventType: eventType,
		Timestamp: time.Now(),
		Priority:  0,
		Loggable:  false, // Default to not loggable unless explicitly set
	}
}

// WithCorrelationID sets the correlation ID for request tracking
func (m *EventMetadata) WithCorrelationID(id string) *EventMetadata {
	m.CorrelationID = id
	return m
}

// WithTarget sets the target plugin for direct sends
func (m *EventMetadata) WithTarget(target string) *EventMetadata {
	m.Target = target
	return m
}

// WithLogging enables logging with the specified level
func (m *EventMetadata) WithLogging(level string) *EventMetadata {
	m.Loggable = true
	m.LogLevel = level
	return m
}

// WithTags adds tags for filtering
func (m *EventMetadata) WithTags(tags ...string) *EventMetadata {
	m.Tags = append(m.Tags, tags...)
	return m
}

// WithReplyTo sets where responses should be sent
func (m *EventMetadata) WithReplyTo(replyTo string) *EventMetadata {
	m.ReplyTo = replyTo
	return m
}

// WithTimeout sets the request timeout
func (m *EventMetadata) WithTimeout(timeout time.Duration) *EventMetadata {
	m.Timeout = timeout
	return m
}

// WithPriority sets the message priority
func (m *EventMetadata) WithPriority(priority int) *EventMetadata {
	m.Priority = priority
	return m
}
