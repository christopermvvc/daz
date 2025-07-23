package greeter_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/plugins/greeter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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
	plugin := greeter.New()
	assert.NotNil(t, plugin)
	assert.Equal(t, "greeter", plugin.Name())
}

func TestPluginDependencies(t *testing.T) {
	plugin := greeter.New()
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
	plugin := greeter.New()
	mockBus := new(MockEventBus)

	// Test with no config
	err := plugin.Init(nil, mockBus)
	assert.NoError(t, err)

	// Test with valid config
	config := json.RawMessage(`{"cooldown_minutes": 60, "enabled": false}`)
	err = plugin.Init(config, mockBus)
	assert.NoError(t, err)

	// Test with invalid config
	badConfig := json.RawMessage(`{"cooldown_minutes": "invalid"}`)
	err = plugin.Init(badConfig, mockBus)
	assert.Error(t, err)
}

func TestPluginStartStop(t *testing.T) {
	plugin := greeter.New()
	mockBus := new(MockEventBus)

	// Initialize first
	err := plugin.Init(nil, mockBus)
	assert.NoError(t, err)

	// Mock expectations for Start
	mockBus.On("Subscribe", "cytube.event.addUser", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "cytube.event.userJoin", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "cytube.event.userLeave", mock.Anything).Return(nil)
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
	plugin := greeter.New()

	// Check initial status
	status := plugin.Status()
	assert.Equal(t, "greeter", status.Name)
	assert.Equal(t, "stopped", status.State)
}

func TestHandleEvent(t *testing.T) {
	plugin := greeter.New()

	// HandleEvent should always return nil
	err := plugin.HandleEvent(nil)
	assert.NoError(t, err)
}
