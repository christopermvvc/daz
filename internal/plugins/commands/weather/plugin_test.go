package weather

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

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
	return args.Get(0).(*framework.EventData), args.Error(1)
}

func (m *MockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	args := m.Called(eventType, handler)
	return args.Error(0)
}

func (m *MockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	args := m.Called(pattern, handler, tags)
	return args.Error(0)
}

func (m *MockEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
	m.Called(correlationID, response, err)
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
	return args.Get(0).(map[string]int64)
}

func (m *MockEventBus) GetDroppedEventCount(eventType string) int64 {
	args := m.Called(eventType)
	return args.Get(0).(int64)
}

func TestNew(t *testing.T) {
	plugin := New()
	assert.NotNil(t, plugin)
	assert.Equal(t, "weather", plugin.Name())
}

func TestInit(t *testing.T) {
	plugin := New()
	mockBus := new(MockEventBus)

	t.Run("default config", func(t *testing.T) {
		err := plugin.Init(nil, mockBus)
		assert.NoError(t, err)
	})

	t.Run("custom config", func(t *testing.T) {
		config := Config{RateLimitSeconds: 30}
		configData, _ := json.Marshal(config)
		err := plugin.Init(configData, mockBus)
		assert.NoError(t, err)
	})
}

func TestStart(t *testing.T) {
	plugin := New()
	mockBus := new(MockEventBus)
	plugin.Init(nil, mockBus)

	mockBus.On("Broadcast", "command.register", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.weather.execute", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.w.execute", mock.Anything).Return(nil)

	err := plugin.Start()
	assert.NoError(t, err)
	mockBus.AssertExpectations(t)
}

func TestStop(t *testing.T) {
	plugin := New()
	mockBus := new(MockEventBus)
	plugin.Init(nil, mockBus)

	mockBus.On("Broadcast", "command.register", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.weather.execute", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.w.execute", mock.Anything).Return(nil)

	plugin.Start()
	err := plugin.Stop()
	assert.NoError(t, err)
}

func TestStatus(t *testing.T) {
	plugin := New()
	mockBus := new(MockEventBus)
	plugin.Init(nil, mockBus)

	status := plugin.Status()
	assert.Equal(t, "weather", status.Name)
	assert.Equal(t, "stopped", status.State)

	mockBus.On("Broadcast", "command.register", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.weather.execute", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.w.execute", mock.Anything).Return(nil)

	plugin.Start()
	status = plugin.Status()
	assert.Equal(t, "running", status.State)
}

func TestRateLimit(t *testing.T) {
	plugin := New().(*Plugin)
	mockBus := new(MockEventBus)
	plugin.Init(nil, mockBus)
	plugin.rateLimitSecs = 1

	username := "testuser"

	// First request should succeed
	assert.True(t, plugin.checkRateLimit(username))

	// Immediate second request should fail
	assert.False(t, plugin.checkRateLimit(username))

	// After waiting, should succeed
	time.Sleep(1100 * time.Millisecond)
	assert.True(t, plugin.checkRateLimit(username))
}

func TestHandleEvent(t *testing.T) {
	plugin := New()
	mockBus := new(MockEventBus)
	plugin.Init(nil, mockBus)

	event := &framework.DataEvent{}
	err := plugin.HandleEvent(event)
	assert.NoError(t, err)
}
