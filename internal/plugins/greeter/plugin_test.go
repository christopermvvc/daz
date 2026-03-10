package greeter

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockEventBus implements framework.EventBus for testing
type MockEventBus struct {
	mock.Mock
}

func (m *MockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	args := m.Called(eventType, data)
	return args.Error(0)
}

func (m *MockEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	args := m.Called(eventType, data, metadata)
	return args.Error(0)
}

func (m *MockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	args := m.Called(target, eventType, data)
	return args.Error(0)
}

func (m *MockEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	args := m.Called(target, eventType, data, metadata)
	return args.Error(0)
}

func (m *MockEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	args := m.Called(ctx, target, eventType, data, metadata)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*framework.EventData), args.Error(1)
}

func (m *MockEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
	m.Called(correlationID, response, err)
}

func (m *MockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	args := m.Called(eventType, handler)
	return args.Error(0)
}

func (m *MockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	args := m.Called(pattern, handler, tags)
	return args.Error(0)
}

func (m *MockEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	args := m.Called(name, plugin)
	return args.Error(0)
}

func (m *MockEventBus) UnregisterPlugin(name string) error {
	args := m.Called(name)
	return args.Error(0)
}

func (m *MockEventBus) GetDroppedEventCounts() map[string]int64 {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(map[string]int64)
}

func (m *MockEventBus) GetDroppedEventCount(eventType string) int64 {
	args := m.Called(eventType)
	return args.Get(0).(int64)
}

func TestNewPlugin(t *testing.T) {
	plugin := New()
	assert.NotNil(t, plugin)
	assert.Equal(t, "greeter", plugin.Name())
}

func TestMatchesBotIdentity(t *testing.T) {
	tests := []struct {
		name     string
		username string
		botName  string
		expected bool
	}{
		{name: "canonical", username: "Dazza", botName: "Dazza", expected: true},
		{name: "intermediate alias", username: "dazz", botName: "Dazza", expected: true},
		{name: "short alias", username: "daz", botName: "Dazza", expected: true},
		{name: "collapsed mutation", username: "daza", botName: "Dazza", expected: true},
		{name: "different user", username: "dazzler", botName: "Dazza", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesBotIdentity(tt.username, tt.botName)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestBuildGreeterBotAliasesSkipsCommonWords(t *testing.T) {
	aliases := buildGreeterBotAliases("Theree")
	if _, exists := aliases["there"]; exists {
		t.Fatalf("expected common word alias to be skipped")
	}
}

func TestPluginDependencies(t *testing.T) {
	plugin := New()
	// Cast to PluginWithDependencies interface
	if p, ok := plugin.(interface {
		Dependencies() []string
	}); ok {
		deps := p.Dependencies()
		assert.Contains(t, deps, "sql")
	} else {
		t.Fatal("Plugin does not implement Dependencies method")
	}
}

func TestPluginInit(t *testing.T) {
	plugin := New().(*Plugin)
	mockBus := new(MockEventBus)

	// Test with no config
	err := plugin.Init(nil, mockBus)
	assert.NoError(t, err)
	assert.Equal(t, defaultCooldownMinutes, plugin.config.CooldownMinutes)

	// Test with valid config
	config := json.RawMessage(`{"cooldown_minutes": 60, "enabled": false}`)
	err = plugin.Init(config, mockBus)
	assert.NoError(t, err)
	assert.Equal(t, 60, plugin.config.CooldownMinutes)

	// Test with invalid config
	badConfig := json.RawMessage(`{"cooldown_minutes": "invalid"}`)
	err = plugin.Init(badConfig, mockBus)
	assert.Error(t, err)
}

func TestHandleUserJoinRecentGreetingCutoffUsesConfiguredWindow(t *testing.T) {
	mockBus := new(MockEventBus)
	plugin := New().(*Plugin)

	if err := plugin.Init([]byte(`{"cooldown_minutes": 90}`), mockBus); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Force skip path after cooldown checks so we don't enqueue a greeting.
	plugin.skipProbability = 1.0

	var greetingCutoff time.Time
	mockBus.On("Request", mock.Anything, "sql", "sql.query.request", mock.MatchedBy(func(data *framework.EventData) bool {
		if data == nil || data.SQLQueryRequest == nil {
			return false
		}

		query := data.SQLQueryRequest.Query
		if query == "" {
			return false
		}

		if strings.Contains(query, "daz_greeter_history") {
			if len(data.SQLQueryRequest.Params) < 2 {
				return false
			}

			cutoff, ok := data.SQLQueryRequest.Params[1].Value.(time.Time)
			if !ok {
				return false
			}
			greetingCutoff = cutoff
		}

		return true
	}), mock.Anything).
		Return(&framework.EventData{
			SQLQueryResponse: &framework.SQLQueryResponse{
				Success: true,
				Rows:    [][]json.RawMessage{},
			},
		}, nil)

	event := &framework.AddUserEvent{
		CytubeEvent: framework.CytubeEvent{ChannelName: "testchannel"},
		Username:    "alice",
		UserRank:    0,
	}

	when := time.Now()
	if err := plugin.handleUserJoin(event); err != nil {
		t.Fatalf("handleUserJoin failed: %v", err)
	}
	mockBus.AssertNumberOfCalls(t, "Request", 3)
	require.False(t, greetingCutoff.IsZero())

	expectedWindow := time.Duration(plugin.config.CooldownMinutes) * time.Minute
	min := when.Add(-expectedWindow).Add(-3 * time.Second)
	max := time.Now().Add(-expectedWindow).Add(3 * time.Second)
	require.GreaterOrEqual(t, greetingCutoff.Unix(), min.Unix())
	require.LessOrEqual(t, greetingCutoff.Unix(), max.Unix())
}

func TestPluginStartStop(t *testing.T) {
	plugin := New()
	mockBus := new(MockEventBus)

	// Initialize first
	err := plugin.Init(nil, mockBus)
	assert.NoError(t, err)

	// Mock expectations for Start
	mockBus.On("Subscribe", "cytube.event.addUser", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "cytube.event.userJoin", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "cytube.event.userLeave", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "cytube.event.userlist.start", mock.Anything).Return(nil)
	mockBus.On("Broadcast", "command.register", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.greeter.execute", mock.Anything).Return(nil)

	// Mock SQL request for creating tables and loading cooldowns
	mockBus.On("Request", mock.Anything, "sql", mock.Anything, mock.Anything, mock.Anything).Return(&framework.EventData{
		SQLExecResponse: &framework.SQLExecResponse{
			Success:      true,
			RowsAffected: 0,
		},
		SQLQueryResponse: &framework.SQLQueryResponse{
			Success: true,
			Rows:    [][]json.RawMessage{},
		},
	}, nil).Maybe()

	// Start the plugin
	err = plugin.Start()
	assert.NoError(t, err)

	// Trying to start again should error
	err = plugin.Start()
	assert.Error(t, err)

	// Stop the plugin
	err = plugin.Stop()
	assert.NoError(t, err)

	// Stopping again should not error
	err = plugin.Stop()
	assert.NoError(t, err)
}

func TestPluginStatus(t *testing.T) {
	plugin := New()

	// Check initial status
	status := plugin.Status()
	assert.Equal(t, "greeter", status.Name)
	assert.Equal(t, "stopped", status.State)
}

func TestHandleEvent(t *testing.T) {
	plugin := New()

	// HandleEvent should always return nil
	err := plugin.HandleEvent(nil)
	assert.NoError(t, err)
}

func TestPluginStartRegistersGreetmeWithAdminOnly(t *testing.T) {
	plugin := New()
	mockBus := new(MockEventBus)

	if err := plugin.Init(nil, mockBus); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	expectedCommands := ""
	expectedAdminOnly := ""
	gotRegistration := false

	mockBus.On("Subscribe", "cytube.event.addUser", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "cytube.event.userJoin", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "cytube.event.userLeave", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "cytube.event.userlist.start", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.greeter.execute", mock.Anything).Return(nil)
	mockBus.On("Broadcast", "command.register", mock.MatchedBy(func(data *framework.EventData) bool {
		if data == nil || data.PluginRequest == nil || data.PluginRequest.Data == nil {
			return false
		}

		gotRegistration = true
		expectedCommands = data.PluginRequest.Data.KeyValue["commands"]
		expectedAdminOnly = data.PluginRequest.Data.KeyValue["admin_only"]
		return true
	})).Return(nil)
	mockBus.On("Request", mock.Anything, "sql", mock.Anything, mock.Anything, mock.Anything).Return(&framework.EventData{
		SQLExecResponse: &framework.SQLExecResponse{
			Success:      true,
			RowsAffected: 0,
		},
		SQLQueryResponse: &framework.SQLQueryResponse{
			Success: true,
			Rows:    [][]json.RawMessage{},
		},
	}, nil).Maybe()

	greeterPlugin, ok := plugin.(*Plugin)
	if !ok {
		t.Fatal("plugin is not *Plugin")
	}

	if err := greeterPlugin.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !gotRegistration {
		t.Fatal("expected command.register broadcast")
	}
	assert.Equal(t, "greeter,greetme", expectedCommands)
	assert.Equal(t, "greetme", expectedAdminOnly)

	if err := greeterPlugin.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestHandleGreeterCommand_GreetmeRequiresAdmin(t *testing.T) {
	plugin := New()
	mockBus := new(MockEventBus)

	if err := plugin.Init(nil, mockBus); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	greeterPlugin, ok := plugin.(*Plugin)
	if !ok {
		t.Fatal("plugin is not *Plugin")
	}

	mockBus.On("Broadcast", "cytube.send.pm", mock.MatchedBy(func(data *framework.EventData) bool {
		if data == nil || data.PrivateMessage == nil {
			return false
		}
		return data.PrivateMessage.ToUser == "alice" &&
			data.PrivateMessage.Channel == "testchannel" &&
			data.PrivateMessage.Message == "Sorry, only admins can use !greetme."
	})).Return(nil)

	event := &framework.DataEvent{
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				Data: &framework.RequestData{
					Command: &framework.CommandData{
						Name: "greetme",
						Params: map[string]string{
							"username": "alice",
							"channel":  "testchannel",
							"is_pm":    "true",
							"is_admin": "false",
							"rank":     "3",
						},
					},
				},
			},
		},
	}

	if err := greeterPlugin.handleGreeterCommand(event); err != nil {
		t.Fatalf("handleGreeterCommand failed: %v", err)
	}

	mockBus.AssertCalled(t, "Broadcast", "cytube.send.pm", mock.MatchedBy(func(data *framework.EventData) bool {
		if data == nil || data.PrivateMessage == nil {
			return false
		}
		return data.PrivateMessage.ToUser == "alice" &&
			data.PrivateMessage.Channel == "testchannel" &&
			data.PrivateMessage.Message == "Sorry, only admins can use !greetme."
	}))
	mockBus.AssertNotCalled(t, "Broadcast", "cytube.send", mock.Anything, mock.Anything)
}
