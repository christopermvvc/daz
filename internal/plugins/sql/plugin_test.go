package sql

import (
	"encoding/json"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
)

func TestNewPlugin(t *testing.T) {
	p := NewPlugin()
	if p == nil {
		t.Fatal("NewPlugin returned nil")
	}
	if p.Name() != "sql" {
		t.Errorf("Expected plugin name 'sql', got '%s'", p.Name())
	}
}

func TestPluginInit(t *testing.T) {
	p := NewPlugin()

	config := Config{
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Database: "test",
			User:     "test",
			Password: "test",
		},
		LoggerRules: []LoggerRule{
			{
				EventPattern: "cytube.event.chatMsg",
				Enabled:      true,
				Table:        "daz_chat_log",
				Fields:       []string{"username", "message"},
			},
		},
	}

	configData, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	mockBus := &mockEventBus{}
	err = p.Init(configData, mockBus)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if len(p.loggerRules) != 1 {
		t.Errorf("Expected 1 logger rule, got %d", len(p.loggerRules))
	}
}

func TestFindMatchingEventTypes(t *testing.T) {
	p := NewPlugin()

	tests := []struct {
		pattern  string
		expected int
		contains []string
	}{
		{
			pattern:  "cytube.event.*",
			expected: 7,
			contains: []string{"cytube.event.chatMsg", "cytube.event.userJoin", "cytube.event.pm"},
		},
		{
			pattern:  "cytube.event.chatMsg",
			expected: 1,
			contains: []string{"cytube.event.chatMsg"},
		},
		{
			pattern:  "plugin.analytics.*",
			expected: 1,
			contains: []string{"plugin.analytics."},
		},
	}

	for _, test := range tests {
		matches := p.findMatchingEventTypes(test.pattern)
		if len(matches) != test.expected {
			t.Errorf("Pattern %s: expected %d matches, got %d", test.pattern, test.expected, len(matches))
		}
		for _, expected := range test.contains {
			found := false
			for _, match := range matches {
				if match == expected {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Pattern %s: expected to find %s in matches", test.pattern, expected)
			}
		}
	}
}

func TestExtractFieldsForRule(t *testing.T) {
	p := NewPlugin()

	// Test chat message extraction
	data := &framework.EventData{
		ChatMessage: &framework.ChatMessageData{
			Username: "testuser",
			Message:  "hello world",
			UserRank: 1,
			UserID:   "123",
			Channel:  "test",
		},
	}

	rule := LoggerRule{
		Fields: []string{"username", "message"},
	}

	fields := p.extractFieldsForRule(rule, data)

	if fields.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%v'", fields.Username)
	}
	if fields.Message != "hello world" {
		t.Errorf("Expected message 'hello world', got '%v'", fields.Message)
	}
	// When specific fields are requested, other fields should be empty/zero
	if fields.UserRank != 0 {
		t.Error("Expected user_rank to be filtered out (zero value)")
	}
}

// mockEventBus implements a minimal EventBus interface for testing
type mockEventBus struct {
	framework.EventBus
}

func (m *mockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	return nil
}

func (m *mockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	return nil
}

func (m *mockEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	return nil
}
