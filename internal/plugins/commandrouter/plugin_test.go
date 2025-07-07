package commandrouter

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

type mockEventBus struct {
	subscriptions map[string][]framework.EventHandler
	broadcasts    []broadcastCall
	sends         []sendCall
	execCalls     []execCall
	queryCalls    []queryCall
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

type execCall struct {
	sql    string
	params []interface{}
}

type queryCall struct {
	sql    string
	params []interface{}
}

func newMockEventBus() *mockEventBus {
	return &mockEventBus{
		subscriptions: make(map[string][]framework.EventHandler),
	}
}

func (m *mockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	m.broadcasts = append(m.broadcasts, broadcastCall{eventType: eventType, data: data})
	return nil
}

func (m *mockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	m.sends = append(m.sends, sendCall{target: target, eventType: eventType, data: data})
	return nil
}

func (m *mockEventBus) Query(sql string, params ...interface{}) (framework.QueryResult, error) {
	m.queryCalls = append(m.queryCalls, queryCall{sql: sql, params: params})
	return &mockQueryResult{}, nil
}

func (m *mockEventBus) Exec(sql string, params ...interface{}) error {
	m.execCalls = append(m.execCalls, execCall{sql: sql, params: params})
	return nil
}

func (m *mockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	m.subscriptions[eventType] = append(m.subscriptions[eventType], handler)
	return nil
}

type mockQueryResult struct {
	rows [][]interface{}
	idx  int
}

func (m *mockQueryResult) Next() bool {
	return m.idx < len(m.rows)
}

func (m *mockQueryResult) Scan(dest ...interface{}) error {
	if m.idx >= len(m.rows) {
		return nil
	}
	row := m.rows[m.idx]
	for i, v := range row {
		if i < len(dest) {
			switch d := dest[i].(type) {
			case *string:
				if s, ok := v.(string); ok {
					*d = s
				}
			case *int:
				if n, ok := v.(int); ok {
					*d = n
				}
			case *bool:
				if b, ok := v.(bool); ok {
					*d = b
				}
			}
		}
	}
	m.idx++
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

	if p.name != "commandrouter" {
		t.Errorf("Expected name 'commandrouter', got '%s'", p.name)
	}

	if p.registry == nil {
		t.Error("Registry map not initialized")
	}

	if p.cooldowns == nil {
		t.Error("Cooldowns map not initialized")
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
				DefaultCooldown: 5,
				AdminUsers:      []string{},
			},
		},
		{
			name:   "custom config",
			config: json.RawMessage(`{"default_cooldown": 10, "admin_users": ["admin1", "admin2"]}`),
			want: &Config{
				DefaultCooldown: 10,
				AdminUsers:      []string{"admin1", "admin2"},
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
				if plugin.config.DefaultCooldown != tt.want.DefaultCooldown {
					t.Errorf("DefaultCooldown = %d, want %d", plugin.config.DefaultCooldown, tt.want.DefaultCooldown)
				}

				if len(plugin.config.AdminUsers) != len(tt.want.AdminUsers) {
					t.Errorf("AdminUsers length = %d, want %d", len(plugin.config.AdminUsers), len(tt.want.AdminUsers))
				}
			}

			// Check that schema creation was attempted
			if len(bus.execCalls) == 0 {
				t.Error("Expected schema creation exec call")
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

	// Check subscriptions
	if len(bus.subscriptions) != 2 {
		t.Errorf("Expected 2 subscriptions, got %d", len(bus.subscriptions))
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

	// Test double stop
	err = plugin.Stop()
	if err != nil {
		t.Error("Stop() on stopped plugin should not error")
	}
}

func TestHandleEvent(t *testing.T) {
	plugin := New().(*Plugin)
	bus := newMockEventBus()

	err := plugin.Init(nil, bus)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// HandleEvent should always return nil in this implementation
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
	if status.Name != "commandrouter" {
		t.Errorf("Status.Name = %s, want commandrouter", status.Name)
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

func TestParseRank(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		hasError bool
	}{
		{"0", 0, false},
		{"1", 1, false},
		{"10", 10, false},
		{"abc", 0, true},
		{"", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			rank, err := parseRank(tt.input)
			if tt.hasError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.hasError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
			if rank != tt.expected {
				t.Errorf("parseRank(%s) = %d, want %d", tt.input, rank, tt.expected)
			}
		})
	}
}
