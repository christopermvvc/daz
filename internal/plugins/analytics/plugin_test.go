package analytics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
)

// mockEventBus implements framework.EventBus for testing
type mockEventBus struct {
	subscribers map[string][]framework.EventHandler
	execCalls   []string
	queryCalls  []string
	sendCalls   []sendCall
	mu          sync.Mutex

	// Control query responses
	queryResponses map[string]*mockQueryResult
}

type sendCall struct {
	target    string
	eventType string
	data      *framework.EventData
}

func newMockEventBus() *mockEventBus {
	return &mockEventBus{
		subscribers:    make(map[string][]framework.EventHandler),
		execCalls:      []string{},
		queryCalls:     []string{},
		sendCalls:      []sendCall{},
		queryResponses: make(map[string]*mockQueryResult),
	}
}

func (m *mockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	return nil
}

func (m *mockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendCalls = append(m.sendCalls, sendCall{target, eventType, data})
	return nil
}

func (m *mockEventBus) Query(sql string, params ...framework.SQLParam) (framework.QueryResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queryCalls = append(m.queryCalls, sql)

	// Check for predefined responses
	for key, result := range m.queryResponses {
		if len(sql) >= len(key) && sql[:len(key)] == key {
			return result, nil
		}
	}

	return &mockQueryResult{}, nil
}

func (m *mockEventBus) Exec(sql string, params ...framework.SQLParam) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.execCalls = append(m.execCalls, sql)
	return nil
}

func (m *mockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribers[eventType] = append(m.subscribers[eventType], handler)
	return nil
}

func (m *mockEventBus) SetSQLHandlers(queryHandler, execHandler framework.EventHandler) {
	// No-op for tests
}

func (m *mockEventBus) GetDroppedEventCounts() map[string]int64 {
	return make(map[string]int64)
}

func (m *mockEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

func (m *mockEventBus) QuerySync(ctx context.Context, sql string, params ...interface{}) (*sql.Rows, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queryCalls = append(m.queryCalls, sql)

	// QuerySync is not used in current tests, return nil
	// If needed in future tests, a proper mock implementation would be required
	return nil, nil
}

func (m *mockEventBus) ExecSync(ctx context.Context, sql string, params ...interface{}) (sql.Result, error) {
	m.mu.Lock()
	m.execCalls = append(m.execCalls, sql)
	m.mu.Unlock()
	return &mockSQLResult{}, nil
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

func (m *mockEventBus) HandleEvent(ctx context.Context, event framework.Event) error {
	return nil
}

// mockQueryResult implements framework.QueryResult
type mockQueryResult struct {
	rows     [][]interface{}
	rowIndex int
	closed   bool
}

func (m *mockQueryResult) Next() bool {
	if m.closed || m.rowIndex >= len(m.rows) {
		return false
	}
	m.rowIndex++
	return m.rowIndex <= len(m.rows)
}

func (m *mockQueryResult) Scan(dest ...interface{}) error {
	if m.rowIndex == 0 || m.rowIndex > len(m.rows) {
		return errors.New("no current row")
	}
	row := m.rows[m.rowIndex-1]
	for i, d := range dest {
		if i >= len(row) {
			return errors.New("column index out of range")
		}
		switch v := d.(type) {
		case *string:
			if row[i] != nil {
				*v = row[i].(string)
			}
		case *int:
			if row[i] != nil {
				*v = row[i].(int)
			}
		case *int64:
			if row[i] != nil {
				*v = row[i].(int64)
			}
		case *time.Time:
			if row[i] != nil {
				*v = row[i].(time.Time)
			}
		}
	}
	return nil
}

func (m *mockQueryResult) Close() error {
	m.closed = true
	return nil
}

// mockSQLResult implements sql.Result
type mockSQLResult struct{}

func (m *mockSQLResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (m *mockSQLResult) RowsAffected() (int64, error) {
	return 1, nil
}

func TestNewPlugin(t *testing.T) {
	// Test with nil config
	p := NewPlugin(nil)
	if p.name != "analytics" {
		t.Errorf("Expected plugin name to be 'analytics', got '%s'", p.name)
	}
	if p.config.HourlyInterval != 1*time.Hour {
		t.Errorf("Expected default hourly interval to be 1 hour, got %v", p.config.HourlyInterval)
	}
	if p.config.DailyInterval != 24*time.Hour {
		t.Errorf("Expected default daily interval to be 24 hours, got %v", p.config.DailyInterval)
	}

	// Test with custom config
	config := &Config{
		HourlyInterval: 30 * time.Minute,
		DailyInterval:  12 * time.Hour,
	}
	p = NewPlugin(config)
	if p.config.HourlyInterval != 30*time.Minute {
		t.Errorf("Expected custom hourly interval to be 30 minutes, got %v", p.config.HourlyInterval)
	}
}

func TestPluginInitialize(t *testing.T) {
	p := NewPlugin(nil)
	bus := newMockEventBus()

	err := p.Initialize(bus)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Table creation is now deferred to Start(), not Init()
	// So we should not expect it here

	// Check that context was created
	if p.ctx == nil {
		t.Error("Expected context to be created")
	}
	if p.cancel == nil {
		t.Error("Expected cancel function to be created")
	}
}

func TestPluginStart(t *testing.T) {
	p := NewPlugin(&Config{
		HourlyInterval: 1 * time.Hour,
		DailyInterval:  24 * time.Hour,
	})
	bus := newMockEventBus()

	// Initialize first
	err := p.Initialize(bus)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Start the plugin
	err = p.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Use a channel to wait for startup tasks to complete
	startupDone := make(chan struct{})
	go func() {
		// Wait for startup operations
		timer := time.NewTimer(10 * time.Millisecond)
		defer timer.Stop()
		<-timer.C
		close(startupDone)
	}()
	<-startupDone

	// Check that tables were created during Start (with proper locking)
	// Expected: 3 table creations (hourly aggregation is skipped in multi-room mode)
	expectedExecCalls := 3
	bus.mu.Lock()
	actualExecCalls := len(bus.execCalls)
	bus.mu.Unlock()
	if actualExecCalls != expectedExecCalls {
		t.Errorf("Expected %d exec calls during Start (3 tables only), got %d", expectedExecCalls, actualExecCalls)
	}

	// Check that it subscribed to the right events
	expectedSubscriptions := []string{
		eventbus.EventCytubeChatMsg,
		"plugin.analytics.stats",
	}

	for _, eventType := range expectedSubscriptions {
		if _, ok := bus.subscribers[eventType]; !ok {
			t.Errorf("Expected subscription to '%s' event", eventType)
		}
	}

	// Check that it's marked as running
	if !p.running {
		t.Error("Expected plugin to be marked as running")
	}

	// Try starting again - should fail
	err = p.Start()
	if err == nil {
		t.Error("Expected error when starting already running plugin")
	}

	// Stop the plugin
	err = p.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestHandleChatMessage(t *testing.T) {
	p := NewPlugin(nil)
	bus := newMockEventBus()

	err := p.Initialize(bus)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Clear initial exec calls from table creation
	bus.execCalls = []string{}

	// Create a chat message event
	chatData := &framework.ChatMessageData{
		Username: "testuser",
		Message:  "Hello world",
		UserRank: 1,
		Channel:  "test-channel",
	}

	event := &framework.DataEvent{
		EventType: eventbus.EventCytubeChatMsg,
		Data: &framework.EventData{
			ChatMessage: chatData,
		},
	}

	// Handle the event
	err = p.handleChatMessage(event)
	if err != nil {
		t.Fatalf("handleChatMessage failed: %v", err)
	}

	// Check that message count increased
	if p.messagesCount != 1 {
		t.Errorf("Expected message count to be 1, got %d", p.messagesCount)
	}

	// Check that user was marked active
	if !p.usersActive["testuser"] {
		t.Error("Expected testuser to be marked active")
	}

	// Check that SQL was executed
	if len(bus.execCalls) != 1 {
		t.Errorf("Expected 1 SQL exec call, got %d", len(bus.execCalls))
	}
}

func TestHandleStatsRequest(t *testing.T) {
	p := NewPlugin(nil)
	bus := newMockEventBus()

	// Set up mock query responses
	bus.queryResponses["SELECT message_count"] = &mockQueryResult{
		rows: [][]interface{}{
			{int64(100), int64(10), int64(5)},
		},
	}
	bus.queryResponses["SELECT total_messages"] = &mockQueryResult{
		rows: [][]interface{}{
			{int64(1000), int64(50), int64(20)},
		},
	}
	bus.queryResponses["SELECT username"] = &mockQueryResult{
		rows: [][]interface{}{
			{"user1", int64(500)},
			{"user2", int64(300)},
			{"user3", int64(200)},
		},
	}

	err := p.Initialize(bus)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Create stats request event
	event := &framework.DataEvent{
		EventType: "plugin.analytics.stats",
		Data: &framework.EventData{
			ChatMessage: &framework.ChatMessageData{
				Channel: "test-channel",
			},
		},
	}

	err = p.handleStatsRequest(event)
	if err != nil {
		t.Fatalf("handleStatsRequest failed: %v", err)
	}

	// Check that queries were made
	if len(bus.queryCalls) < 3 {
		t.Errorf("Expected at least 3 query calls, got %d", len(bus.queryCalls))
	}

	// Check that response was sent
	if len(bus.sendCalls) != 1 {
		t.Fatal("Expected one send call")
	}
	if bus.sendCalls[0].target != "commandrouter" {
		t.Errorf("Expected send to 'commandrouter', got '%s'", bus.sendCalls[0].target)
	}
}

func TestPluginStop(t *testing.T) {
	p := NewPlugin(&Config{
		HourlyInterval: 100 * time.Millisecond, // Short intervals for testing
		DailyInterval:  200 * time.Millisecond,
	})
	bus := newMockEventBus()

	// Initialize and start
	err := p.Initialize(bus)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	err = p.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait for goroutines to start using a channel
	routinesStarted := make(chan struct{})
	go func() {
		timer := time.NewTimer(50 * time.Millisecond)
		defer timer.Stop()
		<-timer.C
		close(routinesStarted)
	}()
	<-routinesStarted

	// Stop the plugin
	err = p.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Check that it's no longer running
	if p.running {
		t.Error("Expected plugin to be marked as not running")
	}

	// Stop again - should not error
	err = p.Stop()
	if err != nil {
		t.Error("Expected no error when stopping already stopped plugin")
	}
}

func TestDoHourlyAggregation(t *testing.T) {
	p := NewPlugin(nil)
	bus := newMockEventBus()

	// Set up mock query responses
	bus.queryResponses["SELECT COUNT(*)"] = &mockQueryResult{
		rows: [][]interface{}{
			{int64(150)}, // Message count
		},
	}
	bus.queryResponses["SELECT COUNT(DISTINCT username)"] = &mockQueryResult{
		rows: [][]interface{}{
			{15}, // Unique users
		},
	}

	err := p.Initialize(bus)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Clear initial calls
	bus.queryCalls = []string{}
	bus.execCalls = []string{}

	// Run aggregation
	p.doHourlyAggregation()

	// In multi-room mode, aggregation is skipped
	// Check that no queries were made
	if len(bus.queryCalls) != 0 {
		t.Errorf("Expected 0 query calls (skipped in multi-room mode), got %d", len(bus.queryCalls))
	}

	// Check that no insert was executed
	if len(bus.execCalls) != 0 {
		t.Errorf("Expected 0 exec calls (skipped in multi-room mode), got %d", len(bus.execCalls))
	}
}

func TestHandleEvent(t *testing.T) {
	p := NewPlugin(nil)
	event := &framework.DataEvent{}

	// HandleEvent should always return nil as this plugin uses specific subscriptions
	err := p.HandleEvent(event)
	if err != nil {
		t.Errorf("Expected HandleEvent to return nil, got %v", err)
	}
}
