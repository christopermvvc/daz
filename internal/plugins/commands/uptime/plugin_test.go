package uptime

import (
	"context"
	"database/sql"
	"fmt"
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
	queryResult   *mockQueryResult
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
		queryResult:   &mockQueryResult{},
		sendNotify:    make(chan struct{}, 10),
	}
}

func (m *mockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.broadcasts = append(m.broadcasts, broadcastCall{eventType: eventType, data: data})
	// Notify that Broadcast was called (for tests expecting response)
	select {
	case m.sendNotify <- struct{}{}:
	default:
	}
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

func (m *mockEventBus) Query(sql string, params ...framework.SQLParam) (framework.QueryResult, error) {
	return m.queryResult, nil
}

func (m *mockEventBus) Exec(sql string, params ...framework.SQLParam) error {
	return nil
}

func (m *mockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscriptions[eventType] = append(m.subscriptions[eventType], handler)
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

type mockQueryResult struct {
	hasNext bool
	value   *time.Time
}

func (m *mockQueryResult) Next() bool {
	result := m.hasNext
	m.hasNext = false // Only return true once
	return result
}

func (m *mockQueryResult) Scan(dest ...interface{}) error {
	if len(dest) > 0 && m.value != nil {
		if ptr, ok := dest[0].(**time.Time); ok {
			*ptr = m.value
		}
	}
	return nil
}

func (m *mockQueryResult) Close() error {
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

	if p.name != "uptime" {
		t.Errorf("Expected name 'uptime', got '%s'", p.name)
	}
}

func TestInit(t *testing.T) {
	plugin := New().(*Plugin)
	bus := newMockEventBus()

	err := plugin.Init(nil, bus)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if plugin.eventBus == nil {
		t.Error("EventBus not set")
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

	if plugin.startTime.IsZero() {
		t.Error("Start time should be set")
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
	if status.Name != "uptime" {
		t.Errorf("Status.Name = %s, want uptime", status.Name)
	}
	if status.State != "stopped" {
		t.Errorf("Status.State = %s, want stopped", status.State)
	}

	// Test running status
	err = plugin.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
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

	// Use a timer to ensure some uptime for testing
	uptime := time.NewTimer(100 * time.Millisecond)
	<-uptime.C

	// Create a command event
	eventData := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			From: "commandrouter",
			To:   "uptime",
			Data: &framework.RequestData{
				Command: &framework.CommandData{
					Name: "uptime",
					Params: map[string]string{
						"channel": "test-channel",
					},
				},
			},
		},
	}

	event := framework.NewDataEvent("command.uptime.execute", eventData)

	// Handle the command
	bus.mu.Lock()
	handler := bus.subscriptions["command.uptime.execute"][0]
	bus.mu.Unlock()
	err = handler(event)
	if err != nil {
		t.Errorf("handleCommand() error = %v", err)
	}

	// Wait for async handler to complete - it will broadcast the response
	// Wait for async handler to complete using channel notification
	select {
	case <-bus.sendNotify:
		// Handler completed
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Timeout waiting for async handler")
	}

	// Allow a brief moment for the broadcast to be recorded
	done := make(chan struct{})
	go func() {
		timer := time.NewTimer(10 * time.Millisecond)
		defer timer.Stop()
		<-timer.C
		close(done)
	}()
	<-done

	// Check that a response was broadcast (not sent)
	bus.mu.Lock()
	// We should have 2 broadcasts: 1 for command registration + 1 for the response
	broadcastCount := len(bus.broadcasts)
	if broadcastCount != 2 {
		bus.mu.Unlock()
		t.Fatalf("Expected 2 broadcasts, got %d", broadcastCount)
	}
	// Get the second broadcast (the response)
	broadcast := bus.broadcasts[1]
	bus.mu.Unlock()
	if broadcast.eventType != "cytube.send" {
		t.Errorf("Expected broadcast eventType 'cytube.send', got '%s'", broadcast.eventType)
	}

	// Check message content
	if broadcast.data.RawMessage == nil {
		t.Fatal("Expected RawMessage data")
	}

	message := broadcast.data.RawMessage.Message
	if !strings.Contains(message, "Bot uptime:") {
		t.Error("Uptime message should contain 'Bot uptime:'")
	}

	// Should have at least seconds
	if !strings.Contains(message, "second") {
		t.Error("Uptime message should contain time units")
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "seconds only",
			duration: 45 * time.Second,
			expected: "Bot uptime: 45 seconds",
		},
		{
			name:     "minutes and seconds",
			duration: 2*time.Minute + 30*time.Second,
			expected: "Bot uptime: 2 minutes and 30 seconds",
		},
		{
			name:     "hours minutes seconds",
			duration: 3*time.Hour + 15*time.Minute + 45*time.Second,
			expected: "Bot uptime: 3 hours, 15 minutes and 45 seconds",
		},
		{
			name:     "days hours minutes",
			duration: 2*24*time.Hour + 5*time.Hour + 30*time.Minute + 10*time.Second,
			expected: "Bot uptime: 2 days, 5 hours, 30 minutes and 10 seconds",
		},
		{
			name:     "singular units",
			duration: 1*24*time.Hour + 1*time.Hour + 1*time.Minute + 1*time.Second,
			expected: "Bot uptime: 1 day, 1 hour, 1 minute and 1 second",
		},
		{
			name:     "zero seconds",
			duration: 0,
			expected: "Bot uptime: 0 seconds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatUptime(tt.duration)
			if result != tt.expected {
				t.Errorf("formatUptime(%v) = %s, want %s", tt.duration, result, tt.expected)
			}
		})
	}
}
