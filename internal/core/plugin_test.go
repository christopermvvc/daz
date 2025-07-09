package core

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
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
		Cytube: CytubeConfig{
			ServerURL: "ws://localhost",
			Channel:   "test",
			Username:  "test",
			Password:  "test",
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
		Cytube: CytubeConfig{
			ServerURL: "ws://localhost:8080",
			Channel:   "test",
			Username:  "testuser",
			Password:  "testpass",
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

	// Verify Cytube client was created
	if plugin.cytubeConn == nil {
		t.Error("cytubeConn was not created during initialization")
	}

	// Verify event channel was created
	if plugin.eventChan == nil {
		t.Error("eventChan was not created during initialization")
	}
}

func TestPlugin_EventBroadcasting(t *testing.T) {
	config := &Config{
		Cytube: CytubeConfig{
			ServerURL: "ws://localhost:8080",
			Channel:   "test",
		},
	}

	plugin := NewPlugin(config)
	mockBus := &mockEventBus{}

	// Initialize with mock event bus
	err := plugin.Initialize(mockBus)
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	// Simulate a chat message event
	chatEvent := &framework.ChatMessageEvent{
		CytubeEvent: framework.CytubeEvent{
			EventType: "chatMessage",
		},
		Username: "testuser",
		Message:  "Hello, world!",
		UserRank: 3,
		UserID:   "user123",
	}

	// Send event through the channel
	plugin.eventChan <- chatEvent

	// Give the goroutine time to process
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	<-ctx.Done()

	// Verify the event was broadcast (with proper locking)
	mockBus.mu.Lock()
	defer mockBus.mu.Unlock()

	if len(mockBus.broadcasts) != 1 {
		t.Errorf("Expected 1 broadcast, got %d", len(mockBus.broadcasts))
	} else {
		broadcast := mockBus.broadcasts[0]
		if broadcast.eventType != eventbus.EventCytubeChatMsg {
			t.Errorf("Expected event type '%s', got '%s'", eventbus.EventCytubeChatMsg, broadcast.eventType)
		}
		if broadcast.data.ChatMessage == nil {
			t.Error("Expected ChatMessage data to be set")
		} else {
			if broadcast.data.ChatMessage.Username != "testuser" {
				t.Errorf("Expected username 'testuser', got '%s'", broadcast.data.ChatMessage.Username)
			}
			if broadcast.data.ChatMessage.Message != "Hello, world!" {
				t.Errorf("Expected message 'Hello, world!', got '%s'", broadcast.data.ChatMessage.Message)
			}
		}
	}
}

func TestPlugin_Stop(t *testing.T) {
	config := &Config{
		Cytube: CytubeConfig{
			Channel: "test",
		},
	}

	plugin := NewPlugin(config)

	// Stop should not panic even without initialization
	err := plugin.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}
