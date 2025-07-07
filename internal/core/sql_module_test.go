package core

import (
	"context"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
)

func TestNewSQLModule(t *testing.T) {
	config := DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		Database: "test_db",
		User:     "test_user",
		Password: "test_pass",
	}

	module, err := NewSQLModule(config, nil)
	if err != nil {
		t.Fatalf("NewSQLModule() error = %v", err)
	}

	if module == nil {
		t.Fatal("NewSQLModule() returned nil")
	}

	if module.config.Host != config.Host {
		t.Errorf("config.Host = %v, want %v", module.config.Host, config.Host)
	}
}

func TestSQLModule_LogEvent(t *testing.T) {
	module := &SQLModule{}
	ctx := context.Background()

	// Test with nil pool
	event := &framework.ChatMessageEvent{
		CytubeEvent: framework.CytubeEvent{
			EventType:   "chatMsg",
			ChannelName: "test-channel",
		},
		Username: "testuser",
		Message:  "test message",
	}

	err := module.LogEvent(ctx, event)
	if err == nil {
		t.Error("LogEvent() with nil pool should return error")
	}
}

func TestSQLModule_HandleSQLRequest(t *testing.T) {
	module := &SQLModule{}
	ctx := context.Background()

	// Test with nil pool
	req := framework.SQLRequest{
		Query:  "SELECT * FROM test",
		Params: []interface{}{},
	}

	_, err := module.HandleSQLRequest(ctx, req)
	if err == nil {
		t.Error("HandleSQLRequest() with nil pool should return error")
	}
}

func TestSQLModule_Stop(t *testing.T) {
	module := &SQLModule{}

	// Should not panic with nil pool
	err := module.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}
