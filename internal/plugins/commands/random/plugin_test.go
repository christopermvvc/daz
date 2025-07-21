package random

import (
	"context"
	"strings"
	"testing"

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
	assert.Equal(t, "random", plugin.Name())
}

func TestInit(t *testing.T) {
	plugin := New()
	mockBus := new(MockEventBus)

	err := plugin.Init(nil, mockBus)
	assert.NoError(t, err)
}

func TestStart(t *testing.T) {
	plugin := New()
	mockBus := new(MockEventBus)
	plugin.Init(nil, mockBus)

	mockBus.On("Broadcast", "command.register", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.random.execute", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.rand.execute", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.r.execute", mock.Anything).Return(nil)

	err := plugin.Start()
	assert.NoError(t, err)
	mockBus.AssertExpectations(t)
}

func TestStop(t *testing.T) {
	plugin := New()
	mockBus := new(MockEventBus)
	plugin.Init(nil, mockBus)

	mockBus.On("Broadcast", "command.register", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.random.execute", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.rand.execute", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.r.execute", mock.Anything).Return(nil)

	plugin.Start()
	err := plugin.Stop()
	assert.NoError(t, err)
}

func TestStatus(t *testing.T) {
	plugin := New()
	mockBus := new(MockEventBus)
	plugin.Init(nil, mockBus)

	status := plugin.Status()
	assert.Equal(t, "random", status.Name)
	assert.Equal(t, "stopped", status.State)

	mockBus.On("Broadcast", "command.register", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.random.execute", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.rand.execute", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.r.execute", mock.Anything).Return(nil)

	plugin.Start()
	status = plugin.Status()
	assert.Equal(t, "running", status.State)
}

func TestGenerateRandomResponse(t *testing.T) {
	plugin := New().(*Plugin)

	t.Run("no args", func(t *testing.T) {
		response := plugin.generateRandomResponse([]string{})
		assert.Contains(t, response, "Usage:")
	})

	t.Run("invalid number", func(t *testing.T) {
		response := plugin.generateRandomResponse([]string{"abc"})
		assert.Equal(t, "Please provide a valid positive number", response)
	})

	t.Run("negative number", func(t *testing.T) {
		response := plugin.generateRandomResponse([]string{"-5"})
		assert.Equal(t, "Please provide a valid positive number", response)
	})

	t.Run("valid number", func(t *testing.T) {
		response := plugin.generateRandomResponse([]string{"10"})
		assert.True(t, strings.HasPrefix(response, "🎲 Random number (0-10):"))
		// Extract the number from response
		parts := strings.Split(response, ": ")
		if len(parts) == 2 {
			numStr := parts[1]
			// Should be a number between 0 and 10
			assert.NotEmpty(t, numStr)
		}
	})
}

func TestHandleEvent(t *testing.T) {
	plugin := New()
	mockBus := new(MockEventBus)
	plugin.Init(nil, mockBus)

	event := &framework.DataEvent{}
	err := plugin.HandleEvent(event)
	assert.NoError(t, err)
}
