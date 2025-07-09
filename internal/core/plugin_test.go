package core

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
)

type mockEventBus struct {
	mu         sync.Mutex
	broadcasts []broadcastCall
}

type broadcastCall struct {
	eventType string
	data      *framework.EventData
}

func (m *mockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.broadcasts = append(m.broadcasts, broadcastCall{eventType: eventType, data: data})
	return nil
}

func (m *mockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	return nil
}

func (m *mockEventBus) Query(sql string, params ...framework.SQLParam) (framework.QueryResult, error) {
	return nil, nil
}

func (m *mockEventBus) Exec(sql string, params ...framework.SQLParam) error {
	return nil
}

func (m *mockEventBus) QuerySync(ctx context.Context, sql string, params ...interface{}) (*sql.Rows, error) {
	return nil, fmt.Errorf("sync queries not supported in mock")
}

func (m *mockEventBus) ExecSync(ctx context.Context, sql string, params ...interface{}) (sql.Result, error) {
	return nil, fmt.Errorf("sync exec not supported in mock")
}

func (m *mockEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return m.Broadcast(eventType, data)
}

func (m *mockEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return m.Send(target, eventType, data)
}

func (m *mockEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	return nil, fmt.Errorf("request not supported in mock")
}

func (m *mockEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
	// Mock implementation - can be empty
}

func (m *mockEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	return nil
}

func (m *mockEventBus) UnregisterPlugin(name string) error {
	return nil
}

func (m *mockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	return nil
}

func (m *mockEventBus) SetSQLHandlers(queryHandler, execHandler framework.EventHandler) {
	// Mock implementation - can be empty
}

func (m *mockEventBus) GetDroppedEventCounts() map[string]int64 {
	return make(map[string]int64)
}

func (m *mockEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

func TestNewPlugin(t *testing.T) {
	config := &Config{
		Rooms: []RoomConfig{
			{
				ID:       "test-room",
				Channel:  "test",
				Username: "test",
				Password: "test",
				Enabled:  true,
			},
		},
	}

	plugin := NewPlugin(config)
	if plugin == nil {
		t.Fatal("NewPlugin() returned nil")
	}

	if plugin.config == nil {
		t.Fatal("plugin.config is nil")
	}

	if plugin.ctx == nil {
		t.Fatal("plugin.ctx is nil")
	}

	if plugin.cancel == nil {
		t.Fatal("plugin.cancel is nil")
	}
}

func TestPlugin_Name(t *testing.T) {
	plugin := &Plugin{}
	name := plugin.Name()
	if name != "core" {
		t.Errorf("Name() = %v, want %v", name, "core")
	}
}

func TestPlugin_SupportsStream(t *testing.T) {
	plugin := &Plugin{}
	if plugin.SupportsStream() {
		t.Error("SupportsStream() = true, want false")
	}
}

func TestPlugin_HandleEvent(t *testing.T) {
	plugin := &Plugin{}

	// Core plugin doesn't handle events, just publishes them
	err := plugin.HandleEvent(&framework.CytubeEvent{})
	if err != nil {
		t.Errorf("HandleEvent() error = %v", err)
	}
}

func TestPlugin_Initialize(t *testing.T) {
	config := &Config{
		Rooms: []RoomConfig{
			{
				ID:       "test-room",
				Channel:  "test",
				Username: "testuser",
				Password: "testpass",
				Enabled:  true,
			},
		},
	}

	plugin := NewPlugin(config)
	mockBus := &mockEventBus{}

	// Initialize should set up the event bus and modules
	err := plugin.Initialize(mockBus)
	if err != nil {
		t.Errorf("Initialize() error = %v", err)
	}

	// Verify event bus was set
	if plugin.eventBus != mockBus {
		t.Error("eventBus was not set correctly during initialization")
	}

	// Verify RoomManager was created
	if plugin.roomManager == nil {
		t.Error("roomManager was not created during initialization")
	}
}

func TestPlugin_EventBroadcasting(t *testing.T) {
	config := &Config{
		Rooms: []RoomConfig{
			{
				ID:       "test-room",
				Channel:  "test",
				Username: "testuser",
				Password: "testpass",
				Enabled:  true,
			},
		},
	}

	plugin := NewPlugin(config)
	mockBus := &mockEventBus{}

	// Initialize with mock event bus
	err := plugin.Initialize(mockBus)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// Note: Event broadcasting is now handled by RoomManager
	// We can test that the event handlers are properly initialized
	if plugin.eventHandlers == nil {
		t.Error("Event handlers were not initialized")
	}

	// Verify chat message handler exists
	if _, ok := plugin.eventHandlers["*framework.ChatMessageEvent"]; !ok {
		t.Error("Chat message event handler not found")
	}
}

func TestPlugin_Stop(t *testing.T) {
	config := &Config{
		Rooms: []RoomConfig{
			{
				ID:      "test-room",
				Channel: "test",
				Enabled: true,
			},
		},
	}

	plugin := NewPlugin(config)

	// Stop should not panic even without initialization
	err := plugin.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}
