package sql

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

func TestHandleLogRequest(t *testing.T) {
	p := &Plugin{
		name:     "sql",
		eventBus: &mockEventBus{},
	}

	// Create a log request event
	logReq := LogRequest{
		EventType: "test.event",
		Table:     "test_table",
		Data:      json.RawMessage(`{"test": "data"}`),
	}

	reqData, _ := json.Marshal(logReq)

	eventData := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			ID:   "test-123",
			From: "test-plugin",
			To:   "sql",
			Type: "log.request",
			Data: &framework.RequestData{
				RawJSON: reqData,
			},
		},
	}

	event := framework.NewDataEvent("log.request", eventData)

	// Mock pool would be nil, so this should handle gracefully
	err := p.handleLogRequest(event)
	if err == nil {
		t.Error("Expected error when pool is nil")
	}
}

func TestHandleSQLQuery(t *testing.T) {
	p := &Plugin{
		name:     "sql",
		eventBus: &mockEventBus{},
	}

	// Create a SQL query request
	eventData := &framework.EventData{
		SQLRequest: &framework.SQLRequest{
			ID:         "query-123",
			Query:      "SELECT * FROM test",
			Params:     []framework.SQLParam{},
			Timeout:    5 * time.Second,
			RequestBy:  "test-plugin",
			IsSync:     false,
			ResponseCh: "",
		},
	}

	event := framework.NewDataEvent("sql.query", eventData)

	// Should return error when pool is nil
	err := p.handleSQLQuery(event)
	if err == nil {
		t.Error("Expected error when database not connected")
	}
}

func TestHandleSQLExec(t *testing.T) {
	p := &Plugin{
		name:     "sql",
		eventBus: &mockEventBus{},
	}

	// Create a SQL exec request
	eventData := &framework.EventData{
		SQLRequest: &framework.SQLRequest{
			ID:         "exec-123",
			Query:      "INSERT INTO test (name) VALUES ($1)",
			Params:     []framework.SQLParam{{Value: "test"}},
			Timeout:    5 * time.Second,
			RequestBy:  "test-plugin",
			IsSync:     false,
			ResponseCh: "",
		},
	}

	event := framework.NewDataEvent("sql.exec", eventData)

	// Should return error when pool is nil
	err := p.handleSQLExec(event)
	if err == nil {
		t.Error("Expected error when database not connected")
	}
}

func TestHandleConfigureLogging(t *testing.T) {
	p := &Plugin{
		name:        "sql",
		eventBus:    &mockEventBus{},
		loggerRules: []LoggerRule{},
	}

	// Create new logger rules
	newRules := []LoggerRule{
		{
			EventPattern: "test.event.*",
			Enabled:      true,
			Table:        "test_logs",
		},
	}

	rulesData, _ := json.Marshal(newRules)

	eventData := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			ID:      "config-123",
			From:    "admin",
			To:      "sql",
			Type:    "log.configure",
			ReplyTo: "admin",
			Data: &framework.RequestData{
				ConfigUpdate: &framework.ConfigUpdateData{
					Section: "logger_rules",
					Values:  rulesData,
				},
			},
		},
	}

	event := framework.NewDataEvent("log.configure", eventData)

	err := p.handleConfigureLogging(event)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if len(p.loggerRules) != 1 {
		t.Errorf("Expected 1 logger rule, got %d", len(p.loggerRules))
	}
}

func TestLogEventData(t *testing.T) {
	p := &Plugin{
		name:          "sql",
		eventBus:      &mockEventBus{},
		eventsHandled: 0,
	}

	rule := LoggerRule{
		Table:  "test_table",
		Fields: []string{"username", "message"},
	}

	eventData := &framework.EventData{
		ChatMessage: &framework.ChatMessageData{
			Username: "testuser",
			Message:  "test message",
			Channel:  "test",
		},
	}

	event := &framework.DataEvent{
		EventType: "test.event",
		EventTime: time.Now(),
		Data:      eventData,
	}

	// Should fail when pool is nil
	err := p.logEventData(rule, event)
	if err == nil {
		t.Error("Expected error when pool is nil")
	}

	// Should increment events handled counter
	if p.eventsHandled != 1 {
		t.Errorf("Expected eventsHandled to be 1, got %d", p.eventsHandled)
	}
}
