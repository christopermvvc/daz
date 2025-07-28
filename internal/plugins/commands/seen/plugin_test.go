package seen

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

type mockEventBus struct {
	mu            sync.Mutex
	subscriptions map[string][]framework.EventHandler
	broadcasts    []broadcastCall
	sends         []sendCall
	sendNotify    chan struct{}
}

type broadcastCall struct {
	eventType string
	data      *framework.EventData
}

type sendCall struct {
	target    string
	eventType string
	data      *framework.EventData
}

func newMockEventBus() *mockEventBus {
	return &mockEventBus{
		subscriptions: make(map[string][]framework.EventHandler),
		sendNotify:    make(chan struct{}, 10),
	}
}

func (m *mockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	m.mu.Lock()
	m.broadcasts = append(m.broadcasts, broadcastCall{eventType: eventType, data: data})
	handlers := m.subscriptions[eventType]
	m.mu.Unlock()

	// Call all handlers for this event type
	for _, handler := range handlers {
		event := &framework.DataEvent{
			EventType: eventType,
			Data: data,
		}
		if err := handler(event); err != nil {
			return err
		}
	}

	select {
	case m.sendNotify <- struct{}{}:
	default:
	}
	return nil
}

func (m *mockEventBus) Send(target, eventType string, data *framework.EventData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends = append(m.sends, sendCall{target: target, eventType: eventType, data: data})
	select {
	case m.sendNotify <- struct{}{}:
	default:
	}
	return nil
}

func (m *mockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscriptions[eventType] = append(m.subscriptions[eventType], handler)
	return nil
}

func (m *mockEventBus) Unsubscribe(eventType string, handler framework.EventHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	handlers := m.subscriptions[eventType]
	for i, h := range handlers {
		if &h == &handler {
			m.subscriptions[eventType] = append(handlers[:i], handlers[i+1:]...)
			break
		}
	}
	return nil
}

func (m *mockEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return m.Broadcast(eventType, data)
}

func (m *mockEventBus) SendWithMetadata(target, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return m.Send(target, eventType, data)
}

func (m *mockEventBus) Request(ctx context.Context, target, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	return nil, nil
}

func (m *mockEventBus) Request2(ctx context.Context, target, eventType string, data *framework.EventData) (*framework.EventData, error) {
	return nil, nil
}

func (m *mockEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
	// No-op for tests
}

func (m *mockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	return m.Subscribe(pattern, handler)
}

func (m *mockEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	return nil
}

func (m *mockEventBus) UnregisterPlugin(name string) error {
	return nil
}

func (m *mockEventBus) GetDroppedEventCounts() map[string]int64 {
	return make(map[string]int64)
}

func (m *mockEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

func (m *mockEventBus) getBroadcasts() []broadcastCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]broadcastCall, len(m.broadcasts))
	copy(result, m.broadcasts)
	return result
}

func TestPlugin_Basic(t *testing.T) {
	// Create mock event bus
	bus := newMockEventBus()

	// Create plugin
	plugin := New()
	seenPlugin := plugin.(*Plugin)

	// Init plugin
	if err := plugin.Init(nil, bus); err != nil {
		t.Fatalf("Failed to init plugin: %v", err)
	}

	// Start plugin
	if err := plugin.Start(); err != nil {
		t.Fatalf("Failed to start plugin: %v", err)
	}

	// Check if plugin is running
	if !seenPlugin.running {
		t.Error("Plugin should be running after start")
	}

	// Check status
	status := plugin.Status()
	if status.State != "running" {
		t.Errorf("Expected state 'running', got '%s'", status.State)
	}

	// Stop plugin
	if err := plugin.Stop(); err != nil {
		t.Fatalf("Failed to stop plugin: %v", err)
	}

	// Check if plugin is not running after stop
	if seenPlugin.running {
		t.Error("Plugin should not be running after stop")
	}
}

func TestPlugin_CommandRegistration(t *testing.T) {
	bus := newMockEventBus()
	plugin := New()

	// Track command registration
	registered := false
	bus.Subscribe("command.register", func(event framework.Event) error {
		dataEvent, ok := event.(*framework.DataEvent)
		if ok && dataEvent.Data != nil && dataEvent.Data.PluginRequest != nil {
			req := dataEvent.Data.PluginRequest
			if req.From == "seen" && req.Data.KeyValue["commands"] == "seen" {
				registered = true
			}
		}
		return nil
	})

	if err := plugin.Init(nil, bus); err != nil {
		t.Fatalf("Failed to init plugin: %v", err)
	}

	if err := plugin.Start(); err != nil {
		t.Fatalf("Failed to start plugin: %v", err)
	}

	// Process any broadcasts that happened during Start
	broadcasts := bus.getBroadcasts()
	for _, bc := range broadcasts {
		if bc.eventType == "command.register" {
			// Already handled via subscription
		}
	}

	if !registered {
		t.Error("Command was not registered")
	}

	plugin.Stop()
}

func TestPlugin_HandleCommand(t *testing.T) {
	bus := newMockEventBus()
	plugin := New()

	if err := plugin.Init(nil, bus); err != nil {
		t.Fatalf("Failed to init plugin: %v", err)
	}

	if err := plugin.Start(); err != nil {
		t.Fatalf("Failed to start plugin: %v", err)
	}

	// Track response with mutex for thread safety
	var responseMu sync.Mutex
	responseReceived := false
	var responseMessage string

	bus.Subscribe("plugin.response", func(event framework.Event) error {
		dataEvent, ok := event.(*framework.DataEvent)
		if ok && dataEvent.Data != nil && dataEvent.Data.PluginResponse != nil {
			resp := dataEvent.Data.PluginResponse
			if resp.From == "seen" {
				responseMu.Lock()
				responseReceived = true
				if resp.Data != nil && resp.Data.CommandResult != nil {
					responseMessage = resp.Data.CommandResult.Output
				}
				responseMu.Unlock()
			}
		}
		return nil
	})

	// Create command event without args (should return usage)
	cmdEvent := &framework.DataEvent{
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				ID:   "test-1",
				From: "test",
				To:   "seen",
				Type: "command",
				Data: &framework.RequestData{
					Command: &framework.CommandData{
						Name: "seen",
						Args: []string{},
						Params: map[string]string{
							"channel":  "test-channel",
							"username": "testuser",
						},
					},
				},
			},
		},
	}

	// Need to manually trigger the handler since we subscribed to plugin.response
	// but the plugin subscribes to command.seen.execute
	handlers := bus.subscriptions["command.seen.execute"]
	if len(handlers) > 0 {
		handlers[0](cmdEvent)
	}

	// Wait a moment for async processing
	time.Sleep(100 * time.Millisecond)

	responseMu.Lock()
	gotResponse := responseReceived
	gotMessage := responseMessage
	responseMu.Unlock()

	if !gotResponse {
		t.Error("No response received")
	}

	if gotMessage != "Usage: !seen <username>" {
		t.Errorf("Expected usage message, got: %s", gotMessage)
	}

	plugin.Stop()
}

func TestPlugin_Dependencies(t *testing.T) {
	plugin := New()
	seenPlugin := plugin.(*Plugin)
	deps := seenPlugin.Dependencies()

	if len(deps) != 1 || deps[0] != "sql" {
		t.Errorf("Expected dependency on 'sql', got: %v", deps)
	}
}

func TestPlugin_Name(t *testing.T) {
	plugin := New()
	if plugin.Name() != "seen" {
		t.Errorf("Expected name 'seen', got '%s'", plugin.Name())
	}
}

func TestPlugin_InitWithConfig(t *testing.T) {
	bus := newMockEventBus()
	plugin := New()

	// Test with empty config
	if err := plugin.Init(json.RawMessage("{}"), bus); err != nil {
		t.Fatalf("Failed to init with empty config: %v", err)
	}

	// Test with nil config
	plugin2 := New()
	if err := plugin2.Init(nil, bus); err != nil {
		t.Fatalf("Failed to init with nil config: %v", err)
	}
}