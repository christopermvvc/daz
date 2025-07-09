package eventfilter

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
)

// MockEventBus implements framework.EventBus for testing
type MockEventBus struct {
	broadcasts    []broadcast
	sends         []send
	subscriptions map[string][]framework.EventHandler
	execCalls     []string
}

type broadcast struct {
	eventType string
	data      *framework.EventData
}

type send struct {
	target    string
	eventType string
	data      *framework.EventData
}

func NewMockEventBus() *MockEventBus {
	return &MockEventBus{
		broadcasts:    []broadcast{},
		sends:         []send{},
		subscriptions: make(map[string][]framework.EventHandler),
		execCalls:     []string{},
	}
}

func (m *MockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	m.broadcasts = append(m.broadcasts, broadcast{eventType: eventType, data: data})

	// Trigger subscribed handlers
	if handlers, ok := m.subscriptions[eventType]; ok {
		for _, handler := range handlers {
			event := &framework.DataEvent{
				EventType: eventType,
				EventTime: time.Now(),
				Data:      data,
			}
			_ = handler(event)
		}
	}
	return nil
}

func (m *MockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	m.sends = append(m.sends, send{target: target, eventType: eventType, data: data})
	return nil
}

func (m *MockEventBus) Query(sql string, params ...framework.SQLParam) (framework.QueryResult, error) {
	return &MockQueryResult{}, nil
}

func (m *MockEventBus) Exec(sql string, params ...framework.SQLParam) error {
	m.execCalls = append(m.execCalls, sql)
	return nil
}

func (m *MockEventBus) QuerySync(ctx context.Context, sql string, params ...interface{}) (*sql.Rows, error) {
	return nil, fmt.Errorf("sync queries not supported in mock")
}

func (m *MockEventBus) ExecSync(ctx context.Context, sql string, params ...interface{}) (sql.Result, error) {
	return nil, fmt.Errorf("sync exec not supported in mock")
}

func (m *MockEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return m.Broadcast(eventType, data)
}

func (m *MockEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return m.Send(target, eventType, data)
}

func (m *MockEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	return nil, fmt.Errorf("request not supported in mock")
}

func (m *MockEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
	// Mock implementation - can be empty
}

func (m *MockEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	return nil
}

func (m *MockEventBus) UnregisterPlugin(name string) error {
	return nil
}

func (m *MockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	m.subscriptions[eventType] = append(m.subscriptions[eventType], handler)
	return nil
}

func (m *MockEventBus) SetSQLHandlers(queryHandler, execHandler framework.EventHandler) {
}

func (m *MockEventBus) GetDroppedEventCounts() map[string]int64 {
	return make(map[string]int64)
}

func (m *MockEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

// MockQueryResult implements framework.QueryResult
type MockQueryResult struct {
}

func (m *MockQueryResult) Scan(dest ...interface{}) error {
	return nil
}

func (m *MockQueryResult) Next() bool {
	return false
}

func (m *MockQueryResult) Close() error {
	return nil
}

func TestNewPlugin(t *testing.T) {
	// Test with nil config
	p := NewPlugin(nil)
	if p.name != "eventfilter" {
		t.Errorf("Expected name 'eventfilter', got '%s'", p.name)
	}
	if p.config.CommandPrefix != "!" {
		t.Errorf("Expected default command prefix '!', got '%s'", p.config.CommandPrefix)
	}
	if p.config.DefaultCooldown != 5 {
		t.Errorf("Expected default cooldown 5, got %d", p.config.DefaultCooldown)
	}

	// Test with custom config
	config := &Config{
		CommandPrefix:   "#",
		DefaultCooldown: 10,
		AdminUsers:      []string{"admin1", "admin2"},
	}
	p2 := NewPlugin(config)
	if p2.config.CommandPrefix != "#" {
		t.Errorf("Expected command prefix '#', got '%s'", p2.config.CommandPrefix)
	}
	if p2.config.DefaultCooldown != 10 {
		t.Errorf("Expected cooldown 10, got %d", p2.config.DefaultCooldown)
	}
}

func TestInitialize(t *testing.T) {
	p := NewPlugin(nil)
	mockBus := NewMockEventBus()

	err := p.Initialize(mockBus)
	if err != nil {
		t.Fatalf("Failed to initialize plugin: %v", err)
	}

	// Schema creation is now deferred to Start(), not Init()
	// So we should not expect it here
}

func TestStartStop(t *testing.T) {
	p := NewPlugin(nil)
	mockBus := NewMockEventBus()

	if err := p.Initialize(mockBus); err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	// Test starting
	if err := p.Start(); err != nil {
		t.Fatalf("Failed to start plugin: %v", err)
	}

	if !p.running {
		t.Error("Plugin should be running after Start()")
	}

	// Check that schema was created during Start
	schemaCreated := false
	for _, call := range mockBus.execCalls {
		if strings.Contains(call, "CREATE TABLE IF NOT EXISTS daz_eventfilter_rules") {
			schemaCreated = true
			break
		}
	}
	if !schemaCreated {
		t.Error("Expected schema creation during Start(), but it wasn't called")
	}

	// Test double start
	if err := p.Start(); err == nil {
		t.Error("Expected error when starting already running plugin")
	}

	// Test stopping
	if err := p.Stop(); err != nil {
		t.Fatalf("Failed to stop plugin: %v", err)
	}

	if p.running {
		t.Error("Plugin should not be running after Stop()")
	}

	// Test double stop
	if err := p.Stop(); err != nil {
		t.Error("Expected no error when stopping already stopped plugin")
	}
}

func TestCommandDetection(t *testing.T) {
	p := NewPlugin(nil)
	mockBus := NewMockEventBus()

	if err := p.Initialize(mockBus); err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	// Register a test command
	p.mu.Lock()
	p.commandRegistry["test"] = &CommandInfo{
		PluginName: "testplugin",
		MinRank:    0,
		Enabled:    true,
	}
	p.registryLoaded = true
	p.mu.Unlock()

	// Create a chat message event with a command
	chatData := &framework.EventData{
		ChatMessage: &framework.ChatMessageData{
			Username: "testuser",
			Message:  "!test arg1 arg2",
			UserRank: 1,
			Channel:  "testchannel",
		},
	}

	// Trigger the chat message handler
	chatEvent := &framework.DataEvent{
		EventType: eventbus.EventCytubeChatMsg,
		EventTime: time.Now(),
		Data:      chatData,
	}

	// Call the handler directly
	err := p.handleChatMessage(chatEvent)
	if err != nil {
		t.Fatalf("Failed to handle chat message: %v", err)
	}

	// Check that command was routed
	if len(mockBus.broadcasts) == 0 {
		t.Fatal("Expected command to be broadcast")
	}

	broadcast := mockBus.broadcasts[0]
	if broadcast.eventType != "command.testplugin.execute" {
		t.Errorf("Expected event type 'command.testplugin.execute', got '%s'", broadcast.eventType)
	}

	if broadcast.data.PluginRequest == nil {
		t.Fatal("Expected PluginRequest in broadcast data")
	}
	if broadcast.data.PluginRequest.To != "testplugin" {
		t.Errorf("Expected target 'testplugin', got '%s'", broadcast.data.PluginRequest.To)
	}
}

func TestEventRouting(t *testing.T) {
	p := NewPlugin(&Config{
		RoutingRules: []RoutingRule{
			{
				EventType:    "cytube.event.userJoin",
				TargetPlugin: "usertracker",
				Priority:     10,
				Enabled:      true,
			},
		},
	})
	mockBus := NewMockEventBus()

	if err := p.Initialize(mockBus); err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	// Create a user join event
	eventData := &framework.EventData{
		UserJoin: &framework.UserJoinData{
			Username: "newuser",
			UserRank: 0,
		},
	}

	event := &framework.DataEvent{
		EventType: "cytube.event.userJoin",
		EventTime: time.Now(),
		Data:      eventData,
	}

	// Handle the event
	err := p.handleCytubeEvent(event)
	if err != nil {
		t.Fatalf("Failed to handle event: %v", err)
	}

	// Check that event was routed
	if len(mockBus.sends) == 0 {
		t.Fatal("Expected event to be sent to usertracker")
	}

	send := mockBus.sends[0]
	if send.target != "usertracker" {
		t.Errorf("Expected target 'usertracker', got '%s'", send.target)
	}
	if send.eventType != "plugin.usertracker.event" {
		t.Errorf("Expected event type 'plugin.usertracker.event', got '%s'", send.eventType)
	}
}

func TestCommandRegistration(t *testing.T) {
	p := NewPlugin(nil)
	mockBus := NewMockEventBus()

	if err := p.Initialize(mockBus); err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Failed to start: %v", err)
	}

	// Create a registration event
	regData := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			From: "testplugin",
			Type: "register",
			Data: &framework.RequestData{
				KeyValue: map[string]string{
					"commands": "cmd1,cmd2,cmd3",
					"min_rank": "2",
				},
			},
		},
	}

	regEvent := &framework.DataEvent{
		EventType: "command.register",
		EventTime: time.Now(),
		Data:      regData,
	}

	// Handle the registration
	err := p.handleRegisterEvent(regEvent)
	if err != nil {
		t.Fatalf("Failed to handle registration: %v", err)
	}

	// Verify commands were registered
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Check primary command
	cmd1, exists := p.commandRegistry["cmd1"]
	if !exists {
		t.Fatal("Expected cmd1 to be registered")
	}
	if cmd1.PluginName != "testplugin" {
		t.Errorf("Expected plugin name 'testplugin', got '%s'", cmd1.PluginName)
	}
	if cmd1.MinRank != 2 {
		t.Errorf("Expected min rank 2, got %d", cmd1.MinRank)
	}
	if cmd1.IsAlias {
		t.Error("cmd1 should not be an alias")
	}

	// Check alias
	cmd2, exists := p.commandRegistry["cmd2"]
	if !exists {
		t.Fatal("Expected cmd2 to be registered")
	}
	if !cmd2.IsAlias {
		t.Error("cmd2 should be an alias")
	}
	if cmd2.PrimaryCommand != "cmd1" {
		t.Errorf("Expected primary command 'cmd1', got '%s'", cmd2.PrimaryCommand)
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
		{"cytube.event.chatMsg", "plugin.*", false},
	}

	for _, test := range tests {
		result := matchesEventType(test.eventType, test.pattern)
		if result != test.expected {
			t.Errorf("matchesEventType(%s, %s) = %v, expected %v",
				test.eventType, test.pattern, result, test.expected)
		}
	}
}

func TestParseRank(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		hasError bool
	}{
		{"0", 0, false},
		{"5", 5, false},
		{"100", 100, false},
		{"invalid", 0, true},
		{"", 0, true},
	}

	for _, test := range tests {
		result, err := parseRank(test.input)
		if test.hasError && err == nil {
			t.Errorf("Expected error for input '%s', got none", test.input)
		}
		if !test.hasError && err != nil {
			t.Errorf("Unexpected error for input '%s': %v", test.input, err)
		}
		if result != test.expected {
			t.Errorf("parseRank(%s) = %d, expected %d", test.input, result, test.expected)
		}
	}
}

func TestGetDefaultRoutingRules(t *testing.T) {
	rules := getDefaultRoutingRules()

	// Check that we have some default rules
	if len(rules) == 0 {
		t.Fatal("Expected default routing rules")
	}

	// Check for specific expected rules
	hasUserJoin := false
	hasMediaChange := false
	hasAnalytics := false

	for _, rule := range rules {
		switch rule.EventType {
		case "cytube.event.userJoin":
			hasUserJoin = true
			if rule.TargetPlugin != "usertracker" {
				t.Errorf("Expected userJoin to route to usertracker, got %s", rule.TargetPlugin)
			}
		case "cytube.event.changeMedia":
			hasMediaChange = true
			if rule.TargetPlugin != "mediatracker" {
				t.Errorf("Expected changeMedia to route to mediatracker, got %s", rule.TargetPlugin)
			}
		case "cytube.event.*":
			hasAnalytics = true
			if rule.TargetPlugin != "analytics" {
				t.Errorf("Expected wildcard to route to analytics, got %s", rule.TargetPlugin)
			}
		}
	}

	if !hasUserJoin {
		t.Error("Missing userJoin routing rule")
	}
	if !hasMediaChange {
		t.Error("Missing changeMedia routing rule")
	}
	if !hasAnalytics {
		t.Error("Missing analytics wildcard routing rule")
	}
}
