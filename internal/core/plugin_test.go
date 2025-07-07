package core

import (
	"context"
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

func (m *mockEventBus) Query(sql string, params ...interface{}) (framework.QueryResult, error) {
	return nil, nil
}

func (m *mockEventBus) Exec(sql string, params ...interface{}) error {
	return nil
}

func (m *mockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	return nil
}

func TestNewPlugin(t *testing.T) {
	config := &Config{
		Cytube: CytubeConfig{
			ServerURL: "ws://localhost",
			Channel:   "test",
			Username:  "test",
			Password:  "test",
		},
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Database: "test",
			User:     "test",
			Password: "test",
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
	ctx := context.Background()

	// Core plugin doesn't handle events, just publishes them
	err := plugin.HandleEvent(ctx, &framework.CytubeEvent{})
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
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Database: "test",
			User:     "test",
			Password: "test",
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

	// Verify SQL module was created
	if plugin.sqlModule == nil {
		t.Error("sqlModule was not created during initialization")
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
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Database: "test",
			User:     "test",
			Password: "test",
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
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Database: "test",
			User:     "test",
			Password: "test",
		},
	}

	plugin := NewPlugin(config)

	// Stop should not panic even without initialization
	err := plugin.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}
