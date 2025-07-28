package usertracker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
)

// MockEventBus implements framework.EventBus for testing
type MockEventBus struct {
	mu         sync.RWMutex
	broadcasts []mockEvent
	sends      []mockEvent
	queries    []mockQuery
	execs      []mockExec
	subs       map[string][]framework.EventHandler
}

type mockEvent struct {
	eventType string
	data      *framework.EventData
	target    string
}

type mockQuery struct {
	query  string
	params []interface{}
}

type mockExec struct {
	query  string
	params []interface{}
}

// MockQueryResult implements framework.QueryResult
type MockQueryResult struct {
	rows   [][]interface{}
	cursor int
}

func (m *MockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	m.broadcasts = append(m.broadcasts, mockEvent{eventType: eventType, data: data})
	return nil
}

func (m *MockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	m.sends = append(m.sends, mockEvent{target: target, eventType: eventType, data: data})
	return nil
}

func (m *MockEventBus) Query(sql string, params ...framework.SQLParam) (framework.QueryResult, error) {
	// Convert SQLParam to interface{} for storage
	interfaceParams := make([]interface{}, len(params))
	for i, p := range params {
		interfaceParams[i] = p.Value
	}
	m.queries = append(m.queries, mockQuery{query: sql, params: interfaceParams})
	return &MockQueryResult{}, nil
}

func (m *MockEventBus) Exec(sql string, params ...framework.SQLParam) error {
	// Convert SQLParam to interface{} for storage
	interfaceParams := make([]interface{}, len(params))
	for i, p := range params {
		interfaceParams[i] = p.Value
	}
	m.execs = append(m.execs, mockExec{query: sql, params: interfaceParams})
	return nil
}

func (m *MockEventBus) QuerySync(ctx context.Context, sql string, params ...framework.SQLParam) (*sql.Rows, error) {
	return nil, fmt.Errorf("sync queries not supported in mock")
}

func (m *MockEventBus) ExecSync(ctx context.Context, sql string, params ...framework.SQLParam) (sql.Result, error) {
	return nil, fmt.Errorf("sync exec not supported in mock")
}

func (m *MockEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return m.Broadcast(eventType, data)
}

func (m *MockEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return m.Send(target, eventType, data)
}

func (m *MockEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	// Handle SQL exec requests
	if eventType == "sql.exec.request" && data != nil && data.SQLExecRequest != nil {
		// Convert SQLParam to interface{} for storage
		interfaceParams := make([]interface{}, len(data.SQLExecRequest.Params))
		for i, p := range data.SQLExecRequest.Params {
			interfaceParams[i] = p.Value
		}
		m.mu.Lock()
		m.execs = append(m.execs, mockExec{query: data.SQLExecRequest.Query, params: interfaceParams})
		m.mu.Unlock()
		return &framework.EventData{
			SQLExecResponse: &framework.SQLExecResponse{
				Success:      true,
				RowsAffected: 1,
			},
		}, nil
	}

	// Handle SQL query requests
	if eventType == "sql.query.request" && data != nil && data.SQLQueryRequest != nil {
		// Convert SQLParam to interface{} for storage
		interfaceParams := make([]interface{}, len(data.SQLQueryRequest.Params))
		for i, p := range data.SQLQueryRequest.Params {
			interfaceParams[i] = p.Value
		}
		m.mu.Lock()
		m.queries = append(m.queries, mockQuery{query: data.SQLQueryRequest.Query, params: interfaceParams})
		m.mu.Unlock()
		return &framework.EventData{
			SQLQueryResponse: &framework.SQLQueryResponse{
				Success: true,
				Columns: []string{},
				Rows:    [][]json.RawMessage{},
			},
		}, nil
	}

	return nil, fmt.Errorf("request type %s not supported in mock", eventType)
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
	if m.subs == nil {
		m.subs = make(map[string][]framework.EventHandler)
	}
	m.subs[eventType] = append(m.subs[eventType], handler)
	return nil
}

func (m *MockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	if m.subs == nil {
		m.subs = make(map[string][]framework.EventHandler)
	}
	m.subs[pattern] = append(m.subs[pattern], handler)
	return nil
}

func (m *MockEventBus) SetSQLHandlers(queryHandler, execHandler framework.EventHandler) {
	// Not needed for tests
}

func (m *MockEventBus) GetDroppedEventCounts() map[string]int64 {
	return make(map[string]int64)
}

func (m *MockEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

func (r *MockQueryResult) Scan(dest ...interface{}) error {
	if r.cursor < 0 || r.cursor >= len(r.rows) {
		return sql.ErrNoRows
	}
	row := r.rows[r.cursor]
	for i, v := range row {
		if i < len(dest) {
			switch d := dest[i].(type) {
			case *string:
				*d = v.(string)
			case *int:
				*d = v.(int)
			case *bool:
				*d = v.(bool)
			case *time.Time:
				*d = v.(time.Time)
			case *sql.NullTime:
				if v == nil {
					*d = sql.NullTime{Valid: false}
				} else {
					*d = sql.NullTime{Time: v.(time.Time), Valid: true}
				}
			}
		}
	}
	return nil
}

func (r *MockQueryResult) Next() bool {
	r.cursor++
	return r.cursor < len(r.rows)
}

func (r *MockQueryResult) Close() error {
	return nil
}

func TestPluginInitialization(t *testing.T) {
	plugin := NewPlugin(nil)

	if plugin.Name() != "usertracker" {
		t.Errorf("Expected plugin name 'usertracker', got '%s'", plugin.Name())
	}
}

func TestPluginInitialize(t *testing.T) {
	plugin := NewPlugin(nil)
	mockBus := &MockEventBus{}

	err := plugin.Init(nil, mockBus)
	if err != nil {
		t.Fatalf("Failed to initialize plugin: %v", err)
	}

	// Table creation is now deferred to Start(), not Init()
	// So we should not expect it here
}

func TestPluginStart(t *testing.T) {
	plugin := NewPlugin(nil)
	mockBus := &MockEventBus{}

	err := plugin.Init(nil, mockBus)
	if err != nil {
		t.Fatalf("Failed to initialize plugin: %v", err)
	}

	err = plugin.Start()
	if err != nil {
		t.Fatalf("Failed to start plugin: %v", err)
	}

	// Wait for async table creation
	time.Sleep(5 * time.Second)

	// Check that tables were created after Start
	// We expect 2 table creations + 1 stored function creation (or attempt)
	mockBus.mu.RLock()
	execCount := len(mockBus.execs)
	mockBus.mu.RUnlock()

	// Should have at least 2 executions (tables), possibly 3 if function creation succeeded
	if execCount < 2 || execCount > 3 {
		t.Errorf("Expected 2-3 SQL exec queries after Start (tables + optional function), got %d", execCount)
	}

	// Check subscriptions
	expectedSubs := []string{
		"cytube.event.addUser",
		eventbus.EventCytubeUserLeave,
		eventbus.EventCytubeChatMsg,
		"plugin.usertracker.seen",
	}

	for _, sub := range expectedSubs {
		if _, ok := mockBus.subs[sub]; !ok {
			t.Errorf("Expected subscription to %s", sub)
		}
	}

	// Test stop
	err = plugin.Stop()
	if err != nil {
		t.Fatalf("Failed to stop plugin: %v", err)
	}
}

func TestHandleUserJoin(t *testing.T) {
	plugin := NewPlugin(nil)
	mockBus := &MockEventBus{}

	err := plugin.Init(nil, mockBus)
	if err != nil {
		t.Fatalf("Failed to initialize plugin: %v", err)
	}

	err = plugin.Start()
	if err != nil {
		t.Fatalf("Failed to start plugin: %v", err)
	}

	// Create user join event
	joinData := &framework.EventData{
		UserJoin: &framework.UserJoinData{
			Username: "testuser",
			UserRank: 2,
			Channel:  "testchannel",
		},
	}

	event := framework.NewDataEvent("cytube.event.addUser", joinData)

	// Get the handler and call it
	handlers := mockBus.subs["cytube.event.addUser"]
	if len(handlers) == 0 {
		t.Fatal("No handler registered for user join")
	}

	// Clear execs from initialization
	mockBus.execs = nil

	err = handlers[0](event)
	if err != nil {
		t.Fatalf("Handler failed: %v", err)
	}

	// Should have 3 SQL execs (deactivate old sessions + new session + history)
	if len(mockBus.execs) != 3 {
		t.Errorf("Expected 3 SQL execs, got %d", len(mockBus.execs))
	}

	// Check in-memory state
	plugin.mu.RLock()
	user, exists := plugin.users["testuser"]
	plugin.mu.RUnlock()

	if !exists {
		t.Error("User not found in memory")
	} else {
		if user.Username != "testuser" {
			t.Errorf("Expected username 'testuser', got '%s'", user.Username)
		}
		if user.Rank != 2 {
			t.Errorf("Expected rank 2, got %d", user.Rank)
		}
		if !user.IsActive {
			t.Error("User should be active")
		}
	}
}

func TestHandleUserLeave(t *testing.T) {
	plugin := NewPlugin(nil)
	mockBus := &MockEventBus{}

	err := plugin.Init(nil, mockBus)
	if err != nil {
		t.Fatalf("Failed to initialize plugin: %v", err)
	}

	err = plugin.Start()
	if err != nil {
		t.Fatalf("Failed to start plugin: %v", err)
	}

	// First add a user
	plugin.mu.Lock()
	plugin.users["testuser"] = &UserState{
		Username:     "testuser",
		Rank:         2,
		JoinedAt:     time.Now(),
		LastActivity: time.Now(),
		IsActive:     true,
	}
	plugin.mu.Unlock()

	// Create user leave event
	leaveData := &framework.EventData{
		UserLeave: &framework.UserLeaveData{
			Username: "testuser",
			Channel:  "testchannel",
		},
	}

	event := framework.NewDataEvent(eventbus.EventCytubeUserLeave, leaveData)

	// Get the handler and call it
	handlers := mockBus.subs[eventbus.EventCytubeUserLeave]
	if len(handlers) == 0 {
		t.Fatal("No handler registered for user leave")
	}

	// Clear execs from initialization
	mockBus.execs = nil

	err = handlers[0](event)
	if err != nil {
		t.Fatalf("Handler failed: %v", err)
	}

	// Should have 2 SQL execs (session update + history)
	if len(mockBus.execs) != 2 {
		t.Errorf("Expected 2 SQL execs, got %d", len(mockBus.execs))
	}

	// Check in-memory state
	plugin.mu.RLock()
	user, exists := plugin.users["testuser"]
	plugin.mu.RUnlock()

	if !exists {
		t.Error("User should still exist in memory")
	} else if user.IsActive {
		t.Error("User should not be active")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{30 * time.Second, "30 seconds"},
		{5 * time.Minute, "5 minutes"},
		{2 * time.Hour, "2 hours"},
		{36 * time.Hour, "1 days"},
		{7 * 24 * time.Hour, "7 days"},
	}

	for _, test := range tests {
		result := formatDuration(test.duration)
		if result != test.expected {
			t.Errorf("formatDuration(%v) = %s, expected %s", test.duration, result, test.expected)
		}
	}
}

// TestUserListHandling tests the complete userlist event flow
func TestUserListHandling(t *testing.T) {
	// Create mock event bus
	bus := &MockEventBus{
		subs: make(map[string][]framework.EventHandler),
	}

	// Create plugin
	plugin := &Plugin{
		name:      "usertracker",
		eventBus:  bus,
		sqlClient: framework.NewSQLClient(bus, "usertracker"),
		config: &Config{
			InactivityTimeout: 30 * time.Minute,
		},
		users:              make(map[string]*UserState),
		readyChan:          make(chan struct{}),
		processingUserlist: make(map[string]bool),
		status: framework.PluginStatus{
			Name:  "usertracker",
			State: "initialized",
		},
	}

	// Set up context
	plugin.ctx, plugin.cancel = context.WithCancel(context.Background())
	defer plugin.cancel()

	// Test userlist.start handling
	t.Run("UserListStart", func(t *testing.T) {
		// Add some existing users
		plugin.mu.Lock()
		plugin.users["olduser1"] = &UserState{
			Username: "olduser1",
			IsActive: true,
		}
		plugin.users["olduser2"] = &UserState{
			Username: "olduser2",
			IsActive: true,
		}
		plugin.mu.Unlock()

		// Create userlist.start event
		startEvent := &framework.DataEvent{
			Data: &framework.EventData{
				RawMessage: &framework.RawMessageData{
					Message: "3",
					Channel: "test-channel",
				},
			},
		}

		// Handle start event
		err := plugin.handleUserListStart(startEvent)
		if err != nil {
			t.Errorf("handleUserListStart failed: %v", err)
		}

		// Verify state
		plugin.userlistMutex.RLock()
		if !plugin.processingUserlist["test-channel"] {
			t.Error("Expected processingUserlist[test-channel] to be true")
		}
		plugin.userlistMutex.RUnlock()

		plugin.mu.RLock()
		if len(plugin.users) != 0 {
			t.Errorf("Expected users to be cleared, but found %d users", len(plugin.users))
		}
		plugin.mu.RUnlock()

		// Verify SQL exec was called to deactivate users
		if len(bus.execs) == 0 {
			t.Error("Expected SQL exec to be called")
		}
	})

	// Test adding users during userlist processing
	t.Run("UserListAddUsers", func(t *testing.T) {
		// Add users from userlist
		users := []struct {
			name string
			rank int
		}{
			{"alice", 2},
			{"bob", 1},
			{"charlie", 0},
		}

		for _, user := range users {
			addUserEvent := &framework.AddUserEvent{
				CytubeEvent: framework.CytubeEvent{
					EventType:   "addUser",
					EventTime:   time.Now(),
					ChannelName: "test-channel",
					Metadata: map[string]string{
						"from_userlist": "true",
					},
				},
				Username: user.name,
				UserRank: user.rank,
			}

			err := plugin.handleUserJoin(addUserEvent)
			if err != nil {
				t.Errorf("handleUserJoin failed for %s: %v", user.name, err)
			}
		}

		// Verify users were added
		plugin.mu.RLock()
		if len(plugin.users) != 3 {
			t.Errorf("Expected 3 users, got %d", len(plugin.users))
		}
		for _, user := range users {
			if _, exists := plugin.users[user.name]; !exists {
				t.Errorf("Expected user %s to exist", user.name)
			}
		}
		plugin.mu.RUnlock()
	})

	// Test userlist.end handling
	t.Run("UserListEnd", func(t *testing.T) {
		// Create userlist.end event
		endEvent := &framework.DataEvent{
			Data: &framework.EventData{
				RawMessage: &framework.RawMessageData{
					Message: "3",
					Channel: "test-channel",
				},
			},
		}

		// Handle end event
		err := plugin.handleUserListEnd(endEvent)
		if err != nil {
			t.Errorf("handleUserListEnd failed: %v", err)
		}

		// Verify state is cleared
		plugin.userlistMutex.RLock()
		if plugin.processingUserlist["test-channel"] {
			t.Error("Expected processingUserlist[test-channel] to be false/deleted")
		}
		plugin.userlistMutex.RUnlock()
	})
}

// TestUserListMetadata tests that addUser events from userlist have proper metadata
func TestUserListMetadata(t *testing.T) {
	bus := &MockEventBus{
		subs: make(map[string][]framework.EventHandler),
	}

	plugin := &Plugin{
		name:      "usertracker",
		eventBus:  bus,
		sqlClient: framework.NewSQLClient(bus, "usertracker"),
		config: &Config{
			InactivityTimeout: 30 * time.Minute,
		},
		users:     make(map[string]*UserState),
		readyChan: make(chan struct{}),
		status: framework.PluginStatus{
			Name:  "usertracker",
			State: "initialized",
		},
	}

	plugin.ctx, plugin.cancel = context.WithCancel(context.Background())
	defer plugin.cancel()

	// Test regular user join (not from userlist)
	t.Run("RegularUserJoin", func(t *testing.T) {
		joinEvent := &framework.AddUserEvent{
			CytubeEvent: framework.CytubeEvent{
				EventType:   "addUser",
				EventTime:   time.Now(),
				ChannelName: "test-channel",
				Metadata:    map[string]string{}, // No from_userlist metadata
			},
			Username: "regularuser",
			UserRank: 1,
		}

		err := plugin.handleUserJoin(joinEvent)
		if err != nil {
			t.Errorf("handleUserJoin failed: %v", err)
		}

		// Verify user was added
		plugin.mu.RLock()
		if _, exists := plugin.users["regularuser"]; !exists {
			t.Error("Expected regularuser to exist")
		}
		plugin.mu.RUnlock()
	})

	// Test user join from userlist
	t.Run("UserListUserJoin", func(t *testing.T) {
		joinEvent := &framework.AddUserEvent{
			CytubeEvent: framework.CytubeEvent{
				EventType:   "addUser",
				EventTime:   time.Now(),
				ChannelName: "test-channel",
				Metadata: map[string]string{
					"from_userlist": "true",
				},
			},
			Username: "userlistuser",
			UserRank: 2,
		}

		err := plugin.handleUserJoin(joinEvent)
		if err != nil {
			t.Errorf("handleUserJoin failed: %v", err)
		}

		// Verify user was added
		plugin.mu.RLock()
		if _, exists := plugin.users["userlistuser"]; !exists {
			t.Error("Expected userlistuser to exist")
		}
		plugin.mu.RUnlock()
	})
}
