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

func (m *mockEventBus) QuerySync(ctx context.Context, sql string, params ...framework.SQLParam) (*sql.Rows, error) {
	return nil, fmt.Errorf("sync queries not supported in mock")
}

func (m *mockEventBus) ExecSync(ctx context.Context, sql string, params ...framework.SQLParam) (sql.Result, error) {
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

func (m *mockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
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

func TestPlugin_HandlePluginRequest_GetConfiguredChannels(t *testing.T) {
	// Create plugin with test configuration
	config := &Config{
		Rooms: []RoomConfig{
			{
				ID:      "room1",
				Channel: "channel1",
				Enabled: true,
			},
			{
				ID:      "room2",
				Channel: "channel2",
				Enabled: false,
			},
		},
	}

	plugin := NewPlugin(config)
	mockBus := &mockEventBus{}

	// Initialize the plugin
	err := plugin.Initialize(mockBus)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// Create a plugin request for configured channels
	request := &framework.DataEvent{
		EventType: "plugin.request",
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				ID:   "test-req-123",
				From: "test-plugin",
				To:   "core",
				Type: "get_configured_channels",
			},
		},
	}

	// Handle the request
	err = plugin.handlePluginRequest(request)
	if err != nil {
		t.Fatalf("handlePluginRequest() error = %v", err)
	}

	// Verify the response was sent
	mockBus.mu.Lock()
	defer mockBus.mu.Unlock()

	if len(mockBus.broadcasts) != 1 {
		t.Fatalf("Expected 1 broadcast, got %d", len(mockBus.broadcasts))
	}

	broadcast := mockBus.broadcasts[0]
	if broadcast.eventType != "plugin.response.test-plugin" {
		t.Errorf("Expected event type 'plugin.response.test-plugin', got '%s'", broadcast.eventType)
	}

	// Verify response data
	if broadcast.data == nil || broadcast.data.PluginResponse == nil {
		t.Fatal("Expected plugin response data")
	}

	resp := broadcast.data.PluginResponse
	if !resp.Success {
		t.Errorf("Expected success=true, got false with error: %s", resp.Error)
	}

	if resp.From != "core" {
		t.Errorf("Expected response from 'core', got '%s'", resp.From)
	}

	if resp.ID != "test-req-123" {
		t.Errorf("Expected response ID 'test-req-123', got '%s'", resp.ID)
	}

	// Verify the response contains channel data
	if resp.Data == nil || resp.Data.RawJSON == nil {
		t.Fatal("Expected response data with RawJSON")
	}
}

func TestPlugin_HandlePluginRequest_UnknownType(t *testing.T) {
	plugin := NewPlugin(nil)
	mockBus := &mockEventBus{}

	// Initialize the plugin
	err := plugin.Initialize(mockBus)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// Create a plugin request with unknown type
	request := &framework.DataEvent{
		EventType: "plugin.request",
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				ID:   "test-req-456",
				From: "test-plugin",
				To:   "core",
				Type: "unknown_request_type",
			},
		},
	}

	// Handle the request
	err = plugin.handlePluginRequest(request)
	if err != nil {
		t.Fatalf("handlePluginRequest() error = %v", err)
	}

	// Verify error response was sent
	mockBus.mu.Lock()
	defer mockBus.mu.Unlock()

	if len(mockBus.broadcasts) != 1 {
		t.Fatalf("Expected 1 broadcast, got %d", len(mockBus.broadcasts))
	}

	broadcast := mockBus.broadcasts[0]
	resp := broadcast.data.PluginResponse

	if resp.Success {
		t.Error("Expected success=false for unknown request type")
	}

	if resp.Error == "" {
		t.Error("Expected error message for unknown request type")
	}
}

func TestPlugin_HandlePluginRequest_WrongTarget(t *testing.T) {
	plugin := NewPlugin(nil)
	mockBus := &mockEventBus{}

	// Initialize the plugin
	err := plugin.Initialize(mockBus)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// Create a plugin request targeted at different plugin
	request := &framework.DataEvent{
		EventType: "plugin.request",
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				ID:   "test-req-789",
				From: "test-plugin",
				To:   "other-plugin", // Not targeted at core
				Type: "get_configured_channels",
			},
		},
	}

	// Handle the request
	err = plugin.handlePluginRequest(request)
	if err != nil {
		t.Fatalf("handlePluginRequest() error = %v", err)
	}

	// Verify no response was sent (request was ignored)
	mockBus.mu.Lock()
	defer mockBus.mu.Unlock()

	if len(mockBus.broadcasts) != 0 {
		t.Errorf("Expected no broadcasts for wrong target, got %d", len(mockBus.broadcasts))
	}
}
