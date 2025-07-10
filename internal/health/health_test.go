package health

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

// mockPlugin implements framework.Plugin for testing
type mockPlugin struct {
	name   string
	status framework.PluginStatus
}

func (m *mockPlugin) Init(config json.RawMessage, bus framework.EventBus) error {
	return nil
}

func (m *mockPlugin) Start() error {
	return nil
}

func (m *mockPlugin) Stop() error {
	return nil
}

func (m *mockPlugin) HandleEvent(event framework.Event) error {
	return nil
}

func (m *mockPlugin) Status() framework.PluginStatus {
	return m.status
}

func (m *mockPlugin) Name() string {
	return m.name
}

// mockEventBus implements framework.EventBus for testing
type mockEventBus struct{}

func (m *mockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	return nil
}

func (m *mockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	return nil
}

func (m *mockEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return m.Broadcast(eventType, data)
}

func (m *mockEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return m.Send(target, eventType, data)
}

func (m *mockEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	// For health check, return a successful response
	if eventType == "sql.query.request" && data != nil && data.SQLQueryRequest != nil {
		response := &framework.EventData{
			SQLQueryResponse: &framework.SQLQueryResponse{
				ID:            data.SQLQueryRequest.ID,
				CorrelationID: data.SQLQueryRequest.CorrelationID,
				Success:       true,
				Columns:       []string{"?column?"},
				Rows:          [][]json.RawMessage{{json.RawMessage("1")}},
			},
		}
		return response, nil
	}
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

func (m *mockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	return nil
}

func (m *mockEventBus) SetSQLHandlers(queryHandler, execHandler framework.EventHandler) {
}

func (m *mockEventBus) GetDroppedEventCounts() map[string]int64 {
	return map[string]int64{
		"test.event": 10,
	}
}

func (m *mockEventBus) GetDroppedEventCount(eventType string) int64 {
	if eventType == "test.event" {
		return 10
	}
	return 0
}

// mockHealthChecker implements Checker for testing
type mockHealthChecker struct {
	health ComponentHealth
}

func (m *mockHealthChecker) HealthCheck(ctx context.Context) ComponentHealth {
	return m.health
}

func TestNewService(t *testing.T) {
	eventBus := &mockEventBus{}
	service := NewService(eventBus)

	if service == nil {
		t.Fatal("NewService returned nil")
	}

	if service.eventBus != eventBus {
		t.Error("EventBus not set correctly")
	}

	if service.plugins == nil {
		t.Error("Plugins slice not initialized")
	}

	if service.checkers == nil {
		t.Error("Checkers map not initialized")
	}
}

func TestService_RegisterPlugin(t *testing.T) {
	service := NewService(&mockEventBus{})
	plugin := &mockPlugin{name: "test-plugin"}

	service.RegisterPlugin(plugin)

	if len(service.plugins) != 1 {
		t.Errorf("Expected 1 plugin, got %d", len(service.plugins))
	}
}

func TestService_RegisterChecker(t *testing.T) {
	service := NewService(&mockEventBus{})
	checker := &mockHealthChecker{}

	service.RegisterChecker("test-checker", checker)

	if len(service.checkers) != 1 {
		t.Errorf("Expected 1 checker, got %d", len(service.checkers))
	}
}

func TestService_GetHealth(t *testing.T) {
	service := NewService(&mockEventBus{})

	// Register a healthy plugin
	healthyPlugin := &mockPlugin{
		name: "healthy-plugin",
		status: framework.PluginStatus{
			Name:          "healthy-plugin",
			State:         "running",
			EventsHandled: 100,
			Uptime:        5 * time.Minute,
		},
	}
	service.RegisterPlugin(healthyPlugin)

	// Register an unhealthy plugin
	unhealthyPlugin := &mockPlugin{
		name: "unhealthy-plugin",
		status: framework.PluginStatus{
			Name:      "unhealthy-plugin",
			State:     "error",
			LastError: &testError{"connection failed"},
		},
	}
	service.RegisterPlugin(unhealthyPlugin)

	// Get health status
	ctx := context.Background()
	health := service.GetHealth(ctx, true)

	// Check overall status
	if health.Status != StatusDegraded {
		t.Errorf("Expected status %v, got %v", StatusDegraded, health.Status)
	}

	// Check components
	if len(health.Components) != 3 { // 2 plugins + eventbus
		t.Errorf("Expected 3 components, got %d", len(health.Components))
	}

	// Check healthy plugin
	if comp, ok := health.Components["healthy-plugin"]; ok {
		if comp.Status != StatusUp {
			t.Errorf("Expected healthy-plugin status %v, got %v", StatusUp, comp.Status)
		}
	} else {
		t.Error("healthy-plugin not found in components")
	}

	// Check unhealthy plugin
	if comp, ok := health.Components["unhealthy-plugin"]; ok {
		if comp.Status != StatusDown {
			t.Errorf("Expected unhealthy-plugin status %v, got %v", StatusDown, comp.Status)
		}
		if comp.Error == "" {
			t.Error("Expected error message for unhealthy plugin")
		}
	} else {
		t.Error("unhealthy-plugin not found in components")
	}

	// Check eventbus
	if comp, ok := health.Components["eventbus"]; ok {
		if comp.Status != StatusUp {
			t.Errorf("Expected eventbus status %v, got %v", StatusUp, comp.Status)
		}
	} else {
		t.Error("eventbus not found in components")
	}
}

func TestDatabaseChecker_HealthCheck(t *testing.T) {
	eventBus := &mockEventBus{}
	checker := NewDatabaseChecker(eventBus)

	ctx := context.Background()
	health := checker.HealthCheck(ctx)

	if health.Status != StatusUp {
		t.Errorf("Expected status %v, got %v", StatusUp, health.Status)
	}

	if health.Name != "database" {
		t.Errorf("Expected name 'database', got %v", health.Name)
	}

	if health.Details == nil {
		t.Error("Expected Details to be non-nil")
	}
	// ResponseTimeMS can be 0 for very fast operations
}

// testError is a simple error type for testing
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
