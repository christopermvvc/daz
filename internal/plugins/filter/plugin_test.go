package filter

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
)

// mockEventBus implements framework.EventBus for testing
type mockEventBus struct {
	broadcasts    []broadcastCall
	sends         []sendCall
	subscriptions map[string][]framework.EventHandler
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
	}
}

func (m *mockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	m.broadcasts = append(m.broadcasts, broadcastCall{eventType, data})
	return nil
}

func (m *mockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	m.sends = append(m.sends, sendCall{target, eventType, data})
	return nil
}

func (m *mockEventBus) Query(sql string, params ...interface{}) (framework.QueryResult, error) {
	return nil, nil
}

func (m *mockEventBus) Exec(sql string, params ...interface{}) error {
	return nil
}

func (m *mockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	m.subscriptions[eventType] = append(m.subscriptions[eventType], handler)
	return nil
}

func TestNewPlugin(t *testing.T) {
	// Test with nil config
	p := NewPlugin(nil)
	if p.config.CommandPrefix != "!" {
		t.Errorf("Expected default command prefix '!', got %s", p.config.CommandPrefix)
	}
	if len(p.config.RoutingRules) == 0 {
		t.Error("Expected default routing rules to be set")
	}

	// Test with custom config
	config := &Config{
		CommandPrefix: "$",
		RoutingRules:  []RoutingRule{},
	}
	p = NewPlugin(config)
	if p.config.CommandPrefix != "$" {
		t.Errorf("Expected command prefix '$', got %s", p.config.CommandPrefix)
	}
}

func TestPluginInitialize(t *testing.T) {
	p := NewPlugin(nil)
	eb := newMockEventBus()

	err := p.Initialize(eb)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	if p.eventBus == nil {
		t.Error("EventBus not set after Initialize")
	}
	if p.ctx == nil {
		t.Error("Context not created after Initialize")
	}
}

func TestPluginStart(t *testing.T) {
	p := NewPlugin(nil)
	eb := newMockEventBus()
	if err := p.Initialize(eb); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	err := p.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Check subscriptions
	if len(eb.subscriptions["cytube.event"]) != 1 {
		t.Error("Expected subscription to cytube.event")
	}
	if len(eb.subscriptions[eventbus.EventCytubeChatMsg]) != 1 {
		t.Error("Expected subscription to chat messages")
	}

	// Test starting when already running
	err = p.Start()
	if err == nil {
		t.Error("Expected error when starting already running plugin")
	}
}

func TestPluginStop(t *testing.T) {
	p := NewPlugin(nil)
	eb := newMockEventBus()
	if err := p.Initialize(eb); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	err := p.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Test stopping when already stopped
	err = p.Stop()
	if err != nil {
		t.Error("Expected no error when stopping already stopped plugin")
	}
}

func TestPluginStatus(t *testing.T) {
	p := NewPlugin(nil)
	eb := newMockEventBus()
	if err := p.Initialize(eb); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	status := p.Status()
	if status.State != "stopped" {
		t.Errorf("Expected state 'stopped', got %s", status.State)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	status = p.Status()
	if status.State != "running" {
		t.Errorf("Expected state 'running', got %s", status.State)
	}
}

func TestHandleChatMessage(t *testing.T) {
	p := NewPlugin(&Config{CommandPrefix: "!"})
	eb := newMockEventBus()
	if err := p.Initialize(eb); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Test command detection
	chatMsg := framework.CytubeEvent{
		EventType:   "chatMsg",
		EventTime:   time.Now(),
		ChannelName: "test-channel",
		RawData:     json.RawMessage(`{"username": "testuser", "msg": "!help arg1 arg2"}`),
	}

	// Get the handler and call it
	handlers := eb.subscriptions[eventbus.EventCytubeChatMsg]
	if len(handlers) == 0 {
		t.Fatal("No chat message handler found")
	}

	err := handlers[0](&chatMsg)
	if err != nil {
		t.Fatalf("Handler error: %v", err)
	}

	// Check if command was sent
	if len(eb.sends) != 1 {
		t.Fatalf("Expected 1 send call, got %d", len(eb.sends))
	}

	send := eb.sends[0]
	if send.target != "commands" {
		t.Errorf("Expected target 'commands', got %s", send.target)
	}
	if send.eventType != eventbus.EventPluginCommand {
		t.Errorf("Expected event type %s, got %s", eventbus.EventPluginCommand, send.eventType)
	}

	// Check command data
	if send.data.PluginRequest == nil {
		t.Fatal("Expected PluginRequest in data")
	}
	if send.data.PluginRequest.Data.Command == nil {
		t.Fatal("Expected Command in request data")
	}
	cmd := send.data.PluginRequest.Data.Command
	if cmd.Name != "help" {
		t.Errorf("Expected command name 'help', got %s", cmd.Name)
	}
	if len(cmd.Args) != 2 || cmd.Args[0] != "arg1" || cmd.Args[1] != "arg2" {
		t.Errorf("Expected args [arg1 arg2], got %v", cmd.Args)
	}
}

func TestHandleChatMessageNonCommand(t *testing.T) {
	p := NewPlugin(&Config{CommandPrefix: "!"})
	eb := newMockEventBus()
	if err := p.Initialize(eb); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Test non-command message
	chatMsg := framework.CytubeEvent{
		EventType:   "chatMsg",
		EventTime:   time.Now(),
		ChannelName: "test-channel",
		RawData:     json.RawMessage(`{"username": "testuser", "msg": "regular message"}`),
	}

	handlers := eb.subscriptions[eventbus.EventCytubeChatMsg]
	err := handlers[0](&chatMsg)
	if err != nil {
		t.Fatalf("Handler error: %v", err)
	}

	// Should not send any commands
	if len(eb.sends) != 0 {
		t.Errorf("Expected no sends for non-command message, got %d", len(eb.sends))
	}
}

func TestMatchesEventType(t *testing.T) {
	tests := []struct {
		eventType string
		pattern   string
		expected  bool
	}{
		{"cytube.event.chatMsg", "cytube.event.chatMsg", true},
		{"cytube.event.chatMsg", "cytube.event.*", true},
		{"cytube.event.chatMsg", "*", true},
		{"cytube.event.chatMsg", "cytube.event.userJoin", false},
		{"plugin.test", "cytube.*", false},
	}

	for _, tt := range tests {
		result := matchesEventType(tt.eventType, tt.pattern)
		if result != tt.expected {
			t.Errorf("matchesEventType(%s, %s) = %v, expected %v",
				tt.eventType, tt.pattern, result, tt.expected)
		}
	}
}

func TestRouteToPlugin(t *testing.T) {
	p := NewPlugin(nil)
	eb := newMockEventBus()
	if err := p.Initialize(eb); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	event := &framework.CytubeEvent{
		EventType:   "cytube.event.changeMedia",
		EventTime:   time.Now(),
		ChannelName: "test-channel",
		RawData:     json.RawMessage(`{"id": "abc123", "type": "youtube"}`),
	}

	p.routeToPlugin("mediatracker", event)

	if len(eb.sends) != 1 {
		t.Fatalf("Expected 1 send, got %d", len(eb.sends))
	}

	send := eb.sends[0]
	if send.target != "mediatracker" {
		t.Errorf("Expected target 'mediatracker', got %s", send.target)
	}
	if !strings.Contains(send.eventType, "mediatracker") {
		t.Errorf("Expected event type to contain 'mediatracker', got %s", send.eventType)
	}
}
