package sql

import (
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

func TestLogFieldValue_ToInterface(t *testing.T) {
	tests := []struct {
		name     string
		field    LogFieldValue
		expected interface{}
	}{
		{
			name:     "string value",
			field:    NewLogFieldString("test"),
			expected: "test",
		},
		{
			name:     "int value",
			field:    NewLogFieldInt(42),
			expected: 42,
		},
		{
			name:     "int64 value",
			field:    NewLogFieldInt64(9999999999),
			expected: int64(9999999999),
		},
		{
			name:     "float64 value",
			field:    NewLogFieldFloat64(3.14),
			expected: 3.14,
		},
		{
			name:     "bool value",
			field:    NewLogFieldBool(true),
			expected: true,
		},
		{
			name:     "time value",
			field:    NewLogFieldTime(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)),
			expected: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "null value",
			field:    NewLogFieldNull(),
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.field.ToInterface()
			if result != tt.expected {
				t.Errorf("ToInterface() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestParseIntFromString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *int
	}{
		{
			name:     "valid int",
			input:    "42",
			expected: intPtr(42),
		},
		{
			name:     "negative int",
			input:    "-10",
			expected: intPtr(-10),
		},
		{
			name:     "invalid string",
			input:    "abc",
			expected: nil,
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "float string",
			input:    "3.14",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseIntFromString(tt.input)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("parseIntFromString(%s) = %v, want nil", tt.input, *result)
				}
			} else if result == nil || *result != *tt.expected {
				t.Errorf("parseIntFromString(%s) = %v, want %v", tt.input, result, *tt.expected)
			}
		})
	}
}

func TestGenericTransform(t *testing.T) {
	plugin := &Plugin{
		eventBus: &mockEventBus{},
	}
	lm := NewLoggerMiddleware(plugin, 0)

	event := &framework.CytubeEvent{
		EventType:   "test.event",
		EventTime:   time.Now(),
		ChannelName: "test-channel",
		Metadata: map[string]string{
			"channel":  "test-channel",
			"username": "testuser",
			"extra":    "data",
		},
	}

	rule := LoggerRule{
		Table: "test_logs",
	}

	transform := lm.transforms["generic_transform"]
	fields, err := transform(event, rule)
	if err != nil {
		t.Fatalf("generic_transform failed: %v", err)
	}

	// Check required fields
	if _, ok := fields["event_type"]; !ok {
		t.Error("event_type field missing")
	}
	if _, ok := fields["timestamp"]; !ok {
		t.Error("timestamp field missing")
	}
	if _, ok := fields["channel"]; !ok {
		t.Error("channel field missing")
	}
	if _, ok := fields["username"]; !ok {
		t.Error("username field missing")
	}
	if _, ok := fields["extra"]; !ok {
		t.Error("extra field missing")
	}

	// Verify field values
	if fields["event_type"].StringValue != "test.event" {
		t.Errorf("event_type = %s, want %s", fields["event_type"].StringValue, "test.event")
	}
}

func TestChatTransform(t *testing.T) {
	plugin := &Plugin{
		eventBus: &mockEventBus{},
	}
	lm := NewLoggerMiddleware(plugin, 0)

	event := &framework.CytubeEvent{
		EventType:   "chat.message",
		EventTime:   time.Now(),
		ChannelName: "test-channel",
		Metadata: map[string]string{
			"channel":  "test-channel",
			"username": "testuser",
			"msg":      "Hello, world!",
			"rank":     "3",
		},
	}

	rule := LoggerRule{
		Table: "chat_logs",
	}

	transform := lm.transforms["chat_transform"]
	fields, err := transform(event, rule)
	if err != nil {
		t.Fatalf("chat_transform failed: %v", err)
	}

	// Check chat-specific fields
	if fields["username"].StringValue != "testuser" {
		t.Errorf("username = %s, want %s", fields["username"].StringValue, "testuser")
	}
	if fields["message"].StringValue != "Hello, world!" {
		t.Errorf("message = %s, want %s", fields["message"].StringValue, "Hello, world!")
	}
	if fields["user_rank"].IntValue != 3 {
		t.Errorf("user_rank = %d, want %d", fields["user_rank"].IntValue, 3)
	}
}

func TestBuildInsertQuery(t *testing.T) {
	plugin := &Plugin{
		eventBus: &mockEventBus{},
	}
	lm := NewLoggerMiddleware(plugin, 0)

	fields := LogFieldMap{
		"event_type": NewLogFieldString("test.event"),
		"username":   NewLogFieldString("testuser"),
		"timestamp":  NewLogFieldTime(time.Now()),
		"count":      NewLogFieldInt(42),
	}

	query, params := lm.buildInsertQuery("test_table", fields)

	// Verify query structure (order of columns may vary)
	if !contains(query, "INSERT INTO test_table") {
		t.Errorf("Query doesn't contain INSERT INTO clause: %s", query)
	}
	if !contains(query, "VALUES") {
		t.Errorf("Query doesn't contain VALUES clause: %s", query)
	}

	// Verify parameter count
	if len(params) != 4 {
		t.Errorf("Expected 4 parameters, got %d", len(params))
	}

	// Verify all fields are represented
	for key := range fields {
		if !contains(query, key) {
			t.Errorf("Query missing column %s: %s", key, query)
		}
	}
}

// Helper functions
func intPtr(i int) *int {
	return &i
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[0:len(substr)] == substr || contains(s[1:], substr)))
}
