package about

import (
	"encoding/json"
	"strings"
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
	sendNotify    chan struct{} // Notify when Send is called
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
	defer m.mu.Unlock()
	m.broadcasts = append(m.broadcasts, broadcastCall{eventType: eventType, data: data})
	return nil
}

func (m *mockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends = append(m.sends, sendCall{target: target, eventType: eventType, data: data})
	// Notify that Send was called
	select {
	case m.sendNotify <- struct{}{}:
	default:
	}
	return nil
}

func (m *mockEventBus) Query(sql string, params ...interface{}) (framework.QueryResult, error) {
	return nil, nil
}

func (m *mockEventBus) Exec(sql string, params ...interface{}) error {
	return nil
}

func (m *mockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscriptions[eventType] = append(m.subscriptions[eventType], handler)
	return nil
}

func TestNew(t *testing.T) {
	plugin := New()
	if plugin == nil {
		t.Fatal("New() returned nil")
	}

	p, ok := plugin.(*Plugin)
	if !ok {
		t.Fatal("New() did not return *Plugin")
	}

	if p.name != "about" {
		t.Errorf("Expected name 'about', got '%s'", p.name)
	}
}

func TestInit(t *testing.T) {
	tests := []struct {
		name   string
		config json.RawMessage
		want   *Config
	}{
		{
			name:   "default config",
			config: nil,
			want: &Config{
				Version:     "0.1.0",
				Description: "Daz - A modular chat bot for Cytube",
				Author:      "hildolfr",
				Repository:  "https://github.com/hildolfr/daz",
			},
		},
		{
			name:   "custom config",
			config: json.RawMessage(`{"version": "1.0.0", "author": "test"}`),
			want: &Config{
				Version: "1.0.0",
				Author:  "test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := New().(*Plugin)
			bus := newMockEventBus()

			err := plugin.Init(tt.config, bus)
			if err != nil {
				t.Fatalf("Init() error = %v", err)
			}

			if plugin.eventBus == nil {
				t.Error("EventBus not set")
			}

			if tt.want != nil {
				if plugin.config.Version != tt.want.Version {
					t.Errorf("Version = %s, want %s", plugin.config.Version, tt.want.Version)
				}
				if tt.name == "default config" {
					if plugin.config.Author != tt.want.Author {
						t.Errorf("Author = %s, want %s", plugin.config.Author, tt.want.Author)
					}
				}
			}
		})
	}
}

func TestStartStop(t *testing.T) {
	plugin := New().(*Plugin)
	bus := newMockEventBus()

	err := plugin.Init(nil, bus)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Test Start
	err = plugin.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !plugin.running {
		t.Error("Plugin should be running after Start()")
	}

	// Check subscription
	bus.mu.Lock()
	subCount := len(bus.subscriptions)
	bus.mu.Unlock()
	if subCount != 1 {
		t.Errorf("Expected 1 subscription, got %d", subCount)
	}

	// Check command registration
	bus.mu.Lock()
	bcastCount := len(bus.broadcasts)
	bus.mu.Unlock()
	if bcastCount != 1 {
		t.Errorf("Expected 1 broadcast (command registration), got %d", bcastCount)
	}

	// Test double start
	err = plugin.Start()
	if err == nil {
		t.Error("Expected error on double Start()")
	}

	// Test Stop
	err = plugin.Stop()
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if plugin.running {
		t.Error("Plugin should not be running after Stop()")
	}
}

func TestHandleEvent(t *testing.T) {
	plugin := New().(*Plugin)
	bus := newMockEventBus()

	err := plugin.Init(nil, bus)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// HandleEvent should always return nil
	event := &framework.CytubeEvent{
		EventType: "test",
		EventTime: time.Now(),
	}

	err = plugin.HandleEvent(event)
	if err != nil {
		t.Errorf("HandleEvent() error = %v", err)
	}
}

func TestStatus(t *testing.T) {
	plugin := New().(*Plugin)
	bus := newMockEventBus()

	err := plugin.Init(nil, bus)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Test stopped status
	status := plugin.Status()
	if status.Name != "about" {
		t.Errorf("Status.Name = %s, want about", status.Name)
	}
	if status.State != "stopped" {
		t.Errorf("Status.State = %s, want stopped", status.State)
	}

	// Test running status
	plugin.Start()
	status = plugin.Status()
	if status.State != "running" {
		t.Errorf("Status.State = %s, want running", status.State)
	}
}

func TestHandleCommand(t *testing.T) {
	plugin := New().(*Plugin)
	bus := newMockEventBus()

	err := plugin.Init(nil, bus)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	err = plugin.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Create a command event
	eventData := framework.EventData{
		PluginRequest: &framework.PluginRequest{
			From: "commandrouter",
			To:   "about",
			Data: &framework.RequestData{
				Command: &framework.CommandData{
					Name: "about",
					Params: map[string]string{
						"channel": "test-channel",
					},
				},
			},
		},
	}

	eventJSON, _ := json.Marshal(eventData)
	event := &framework.CytubeEvent{
		EventType: "command.about.execute",
		RawData:   eventJSON,
	}

	// Handle the command
	bus.mu.Lock()
	handler := bus.subscriptions["command.about.execute"][0]
	bus.mu.Unlock()
	err = handler(event)
	if err != nil {
		t.Errorf("handleCommand() error = %v", err)
	}

	// Wait for async handler to complete
	select {
	case <-bus.sendNotify:
		// Send was called
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Timeout waiting for handler to send response")
	}

	// Check that a response was sent
	bus.mu.Lock()
	sendCount := len(bus.sends)
	if sendCount != 1 {
		bus.mu.Unlock()
		t.Fatalf("Expected 1 send, got %d", sendCount)
	}
	send := bus.sends[0]
	bus.mu.Unlock()
	if send.target != "core" {
		t.Errorf("Expected send target 'core', got '%s'", send.target)
	}
	if send.eventType != "cytube.send" {
		t.Errorf("Expected send eventType 'cytube.send', got '%s'", send.eventType)
	}

	// Check message content
	if send.data.RawMessage == nil {
		t.Fatal("Expected RawMessage data")
	}

	message := send.data.RawMessage.Message
	if !strings.Contains(message, "Daz") {
		t.Error("About message should contain 'Daz'")
	}
	if !strings.Contains(message, "v0.1.0") {
		t.Error("About message should contain version")
	}
	if !strings.Contains(message, "hildolfr") {
		t.Error("About message should contain author")
	}
}
