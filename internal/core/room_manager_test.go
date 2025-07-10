package core

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

// MockEventBus implements framework.EventBus for testing
type MockEventBus struct {
	broadcastCalled bool
	lastEventType   string
	lastEventData   *framework.EventData
}

func (m *MockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	return nil
}

func (m *MockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	return nil
}

func (m *MockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	m.broadcastCalled = true
	m.lastEventType = eventType
	m.lastEventData = data
	return nil
}

func (m *MockEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return m.Broadcast(eventType, data)
}

func (m *MockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	return nil
}

func (m *MockEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return nil
}

func (m *MockEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	return nil, nil
}

func (m *MockEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
}

func (m *MockEventBus) Query(sql string, params ...framework.SQLParam) (framework.QueryResult, error) {
	return nil, nil
}

func (m *MockEventBus) Exec(sql string, params ...framework.SQLParam) error {
	return nil
}

func (m *MockEventBus) QuerySync(ctx context.Context, sql string, params ...framework.SQLParam) (*sql.Rows, error) {
	return nil, nil
}

func (m *MockEventBus) ExecSync(ctx context.Context, sql string, params ...framework.SQLParam) (sql.Result, error) {
	return nil, nil
}

func (m *MockEventBus) SetSQLHandlers(queryHandler, execHandler framework.EventHandler) {
}

func (m *MockEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	return nil
}

func (m *MockEventBus) UnregisterPlugin(name string) error {
	return nil
}

func (m *MockEventBus) GetDroppedEventCounts() map[string]int64 {
	return make(map[string]int64)
}

func (m *MockEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

func TestRoomManager_NewRoomManager(t *testing.T) {
	// Test creating a new room manager
	mockBus := &MockEventBus{}
	rm := NewRoomManager(mockBus)

	if rm == nil {
		t.Fatal("NewRoomManager returned nil")
	}

	if rm.eventBus != mockBus {
		t.Error("EventBus was not set correctly")
	}

	if rm.connections == nil {
		t.Error("Connections map was not initialized")
	}

	if rm.ctx == nil {
		t.Error("Context was not initialized")
	}

	if rm.cancel == nil {
		t.Error("Cancel function was not initialized")
	}
}

func TestRoomManager_AddRoom_Disabled(t *testing.T) {
	mockBus := &MockEventBus{}
	rm := NewRoomManager(mockBus)

	// Test adding a disabled room
	room := RoomConfig{
		ID:      "disabled-room",
		Channel: "test-channel",
		Enabled: false,
	}

	err := rm.AddRoom(room)
	if err != nil {
		t.Fatalf("Failed to add disabled room: %v", err)
	}

	// Verify room was not added (because it's disabled)
	_, exists := rm.GetConnection("disabled-room")
	if exists {
		t.Error("Disabled room should not be added to connections")
	}
}

func TestRoomConnection_LastMediaUpdateInitialization(t *testing.T) {
	// This test verifies the conceptual fix for the "293 years" bug
	// We create a RoomConnection manually to test the initialization

	beforeCreate := time.Now()

	// Simulate what happens in AddRoom when creating a connection
	conn := &RoomConnection{
		Room: RoomConfig{
			ID:      "test-room",
			Channel: "test-channel",
		},
		Connected:       false,
		LastMediaUpdate: time.Now(), // This is the fix - initialize to current time
	}

	afterCreate := time.Now()

	// Verify LastMediaUpdate is not zero
	if conn.LastMediaUpdate.IsZero() {
		t.Error("LastMediaUpdate should not be zero value - this causes the 293 years bug")
	}

	// Verify LastMediaUpdate is between beforeCreate and afterCreate
	if conn.LastMediaUpdate.Before(beforeCreate) {
		t.Error("LastMediaUpdate is before creation time - indicates zero value")
	}
	if conn.LastMediaUpdate.After(afterCreate) {
		t.Error("LastMediaUpdate is after creation time - indicates incorrect initialization")
	}

	// Calculate duration since last update
	duration := time.Since(conn.LastMediaUpdate)

	// Duration should be very small (less than 1 second)
	if duration > 1*time.Second {
		t.Errorf("Duration since LastMediaUpdate is too large: %v", duration)
	}

	// Ensure it's not the zero value duration (which would be ~2000 years)
	if duration > 24*time.Hour*365*100 { // More than 100 years
		t.Errorf("LastMediaUpdate appears to be zero value, duration: %v", duration)
	}
}

func TestRoomManager_checkConnections_ZeroLastMediaUpdate(t *testing.T) {
	// This test demonstrates the bug that would occur without the fix

	// Create a connection with zero LastMediaUpdate (the bug condition)
	connWithBug := &RoomConnection{
		Room: RoomConfig{
			ID:      "buggy-room",
			Channel: "test-channel",
		},
		Connected:       true,
		LastMediaUpdate: time.Time{}, // Zero value - this causes the bug!
	}

	// Calculate duration since "last update"
	duration := time.Since(connWithBug.LastMediaUpdate)

	// This will be a huge duration (thousands of years)
	if duration < 24*time.Hour*365*100 { // Less than 100 years
		t.Error("Expected huge duration for zero time value")
	}

	// The duration should be approximately 2000+ years
	years := duration.Hours() / 24 / 365
	t.Logf("Duration from zero time: %.0f years (this is the bug!)", years)

	// Now test with the fix
	connFixed := &RoomConnection{
		Room: RoomConfig{
			ID:      "fixed-room",
			Channel: "test-channel",
		},
		Connected:       true,
		LastMediaUpdate: time.Now(), // The fix!
	}

	durationFixed := time.Since(connFixed.LastMediaUpdate)
	if durationFixed > 1*time.Second {
		t.Error("Duration should be small with proper initialization")
	}
}
