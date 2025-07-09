package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
)

// Mock plugin for testing with dependencies
type mockPluginDep struct {
	name         string
	initialized  bool
	started      bool
	stopped      bool
	initError    error
	startError   error
	stopError    error
	dependencies []string
	ready        bool
}

func (m *mockPluginDep) Init(config json.RawMessage, bus EventBus) error {
	if m.initError != nil {
		return m.initError
	}
	m.initialized = true
	return nil
}

func (m *mockPluginDep) Start() error {
	if m.startError != nil {
		return m.startError
	}
	m.started = true
	m.ready = true
	return nil
}

func (m *mockPluginDep) Stop() error {
	if m.stopError != nil {
		return m.stopError
	}
	m.stopped = true
	m.ready = false
	return nil
}

func (m *mockPluginDep) HandleEvent(event Event) error {
	return nil
}

func (m *mockPluginDep) Status() PluginStatus {
	return PluginStatus{
		Name:  m.name,
		State: "running",
	}
}

func (m *mockPluginDep) Name() string {
	return m.name
}

func (m *mockPluginDep) Dependencies() []string {
	return m.dependencies
}

func (m *mockPluginDep) Ready() bool {
	return m.ready
}

// Mock EventBus for testing
type mockEventBus struct{}

func (m *mockEventBus) Broadcast(eventType string, data *EventData) error { return nil }
func (m *mockEventBus) BroadcastWithMetadata(eventType string, data *EventData, metadata *EventMetadata) error {
	return nil
}
func (m *mockEventBus) Send(target string, eventType string, data *EventData) error { return nil }
func (m *mockEventBus) SendWithMetadata(target string, eventType string, data *EventData, metadata *EventMetadata) error {
	return nil
}
func (m *mockEventBus) Request(ctx context.Context, target string, eventType string, data *EventData, metadata *EventMetadata) (*EventData, error) {
	return nil, nil
}
func (m *mockEventBus) DeliverResponse(correlationID string, response *EventData, err error) {}
func (m *mockEventBus) Query(sql string, params ...SQLParam) (QueryResult, error)            { return nil, nil }
func (m *mockEventBus) Exec(sql string, params ...SQLParam) error                            { return nil }
func (m *mockEventBus) QuerySync(ctx context.Context, sql string, params ...interface{}) (*sql.Rows, error) {
	return nil, nil
}
func (m *mockEventBus) ExecSync(ctx context.Context, sql string, params ...interface{}) (sql.Result, error) {
	return nil, nil
}
func (m *mockEventBus) Subscribe(eventType string, handler EventHandler) error { return nil }
func (m *mockEventBus) SetSQLHandlers(queryHandler, execHandler EventHandler)  {}
func (m *mockEventBus) RegisterPlugin(name string, plugin Plugin) error        { return nil }
func (m *mockEventBus) UnregisterPlugin(name string) error                     { return nil }
func (m *mockEventBus) GetDroppedEventCounts() map[string]int64                { return nil }
func (m *mockEventBus) GetDroppedEventCount(eventType string) int64            { return 0 }

func TestPluginManagerRegisterPlugin(t *testing.T) {
	pm := NewPluginManager()
	plugin := &mockPluginDep{name: "test"}

	// Test successful registration
	err := pm.RegisterPlugin("test", plugin)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Test duplicate registration
	err = pm.RegisterPlugin("test", plugin)
	if err == nil {
		t.Error("Expected error for duplicate registration, got nil")
	}
}

func TestPluginManagerDependencyResolution(t *testing.T) {
	tests := []struct {
		name        string
		plugins     map[string]*mockPluginDep
		expectError bool
		errorMsg    string
	}{
		{
			name: "simple dependency chain",
			plugins: map[string]*mockPluginDep{
				"a": {name: "a", dependencies: []string{"b"}},
				"b": {name: "b", dependencies: []string{"c"}},
				"c": {name: "c", dependencies: []string{}},
			},
			expectError: false,
		},
		{
			name: "circular dependency",
			plugins: map[string]*mockPluginDep{
				"a": {name: "a", dependencies: []string{"b"}},
				"b": {name: "b", dependencies: []string{"a"}},
			},
			expectError: true,
			errorMsg:    "circular dependency",
		},
		{
			name: "missing dependency",
			plugins: map[string]*mockPluginDep{
				"a": {name: "a", dependencies: []string{"missing"}},
			},
			expectError: true,
			errorMsg:    "non-existent plugin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm := NewPluginManager()

			// Register plugins
			for name, plugin := range tt.plugins {
				if err := pm.RegisterPlugin(name, plugin); err != nil {
					t.Fatalf("Failed to register plugin %s: %v", name, err)
				}
			}

			// Resolve dependencies
			err := pm.resolveStartupOrder()

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got nil")
				} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}
		})
	}
}

func TestPluginManagerInitializeAll(t *testing.T) {
	pm := NewPluginManager()
	bus := &mockEventBus{}

	plugin1 := &mockPluginDep{name: "plugin1"}
	plugin2 := &mockPluginDep{name: "plugin2", initError: errors.New("init failed")}

	pm.RegisterPlugin("plugin1", plugin1)
	pm.RegisterPlugin("plugin2", plugin2)

	configs := map[string]interface{}{
		"plugin1": nil,
		"plugin2": nil,
	}

	err := pm.InitializeAll(configs, bus)
	if err == nil {
		t.Error("Expected error from plugin2 init failure")
	}

	if !plugin1.initialized {
		t.Error("Expected plugin1 to be initialized")
	}
}

func TestPluginManagerStartAll(t *testing.T) {
	pm := NewPluginManager()
	bus := &mockEventBus{}

	plugin1 := &mockPluginDep{name: "plugin1"}
	plugin2 := &mockPluginDep{name: "plugin2", dependencies: []string{"plugin1"}}

	pm.RegisterPlugin("plugin1", plugin1)
	pm.RegisterPlugin("plugin2", plugin2)

	configs := map[string]interface{}{
		"plugin1": nil,
		"plugin2": nil,
	}

	// Initialize first
	if err := pm.InitializeAll(configs, bus); err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}

	// Start all
	if err := pm.StartAll(); err != nil {
		t.Errorf("Failed to start: %v", err)
	}

	if !plugin1.started {
		t.Error("Expected plugin1 to be started")
	}

	if !plugin2.started {
		t.Error("Expected plugin2 to be started")
	}
}

func TestPluginManagerStopAll(t *testing.T) {
	pm := NewPluginManager()
	bus := &mockEventBus{}

	plugin1 := &mockPluginDep{name: "plugin1"}
	plugin2 := &mockPluginDep{name: "plugin2", dependencies: []string{"plugin1"}}

	pm.RegisterPlugin("plugin1", plugin1)
	pm.RegisterPlugin("plugin2", plugin2)

	configs := map[string]interface{}{
		"plugin1": nil,
		"plugin2": nil,
	}

	// Initialize and start
	pm.InitializeAll(configs, bus)
	pm.StartAll()

	// Stop all
	pm.StopAll()

	if !plugin1.stopped {
		t.Error("Expected plugin1 to be stopped")
	}

	if !plugin2.stopped {
		t.Error("Expected plugin2 to be stopped")
	}
}

func TestConvertToJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			name:     "nil input",
			input:    nil,
			expected: "{}",
		},
		{
			name:     "byte array",
			input:    []byte(`{"key":"value"}`),
			expected: `{"key":"value"}`,
		},
		{
			name:     "map",
			input:    map[string]string{"key": "value"},
			expected: `{"key":"value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertToJSON(tt.input)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if string(result) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, string(result))
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[0:len(substr)] == substr || contains(s[1:], substr)))
}
