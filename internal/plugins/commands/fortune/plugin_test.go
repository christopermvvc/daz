package fortune

import (
	"context"
	"encoding/json"
	"strings"
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

func TestPlugin_Init(t *testing.T) {
	p := New()
	mockBus := new(MockEventBus)

	err := p.Init(json.RawMessage{}, mockBus)
	assert.NoError(t, err)
}

func TestPlugin_InitWithConfig(t *testing.T) {
	p := New()
	mockBus := new(MockEventBus)

	config := json.RawMessage(`{"cooldown_seconds": 60}`)
	err := p.Init(config, mockBus)
	assert.NoError(t, err)

	// Check that cooldown was set correctly
	plugin := p.(*Plugin)
	assert.Equal(t, 60*time.Second, plugin.cooldown)
}

func TestPlugin_StartStop(t *testing.T) {
	p := New()
	mockBus := new(MockEventBus)

	err := p.Init(json.RawMessage{}, mockBus)
	assert.NoError(t, err)

	mockBus.On("Broadcast", "command.register", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.fortune.execute", mock.Anything).Return(nil)

	err = p.Start()
	assert.NoError(t, err)

	status := p.Status()
	assert.Equal(t, "running", status.State)

	err = p.Stop()
	assert.NoError(t, err)

	status = p.Status()
	assert.Equal(t, "stopped", status.State)

	mockBus.AssertExpectations(t)
}

func TestPlugin_Name(t *testing.T) {
	p := New()
	assert.Equal(t, "fortune", p.Name())
}

func TestPlugin_Cooldown(t *testing.T) {
	p := &Plugin{
		name:           "fortune",
		cooldown:       100 * time.Millisecond, // Short cooldown for testing
		lastRequestMap: make(map[string]time.Time),
	}

	// First check should pass for user1
	assert.True(t, p.checkRateLimit("user1"), "First cooldown check should pass for user1")

	// Immediate second check should fail for user1
	assert.False(t, p.checkRateLimit("user1"), "Second cooldown check should fail for user1")

	// But should pass for user2
	assert.True(t, p.checkRateLimit("user2"), "First cooldown check should pass for user2")

	// Wait for cooldown to expire
	time.Sleep(150 * time.Millisecond)

	// Check should pass again for user1
	assert.True(t, p.checkRateLimit("user1"), "Cooldown check should pass after waiting for user1")
}

func TestPlugin_HandleFortuneCommand(t *testing.T) {
	p := &Plugin{
		name:           "fortune",
		running:        true,
		cooldown:       30 * time.Second,
		lastRequestMap: make(map[string]time.Time),
	}

	mockBus := new(MockEventBus)
	p.eventBus = mockBus

	// Create a fortune command request
	req := &framework.PluginRequest{
		From: "testuser",
		Data: &framework.RequestData{
			Command: &framework.CommandData{
				Params: map[string]string{
					"channel":  "test-channel",
					"username": "testuser",
				},
			},
		},
	}

	event := &framework.DataEvent{
		Data: &framework.EventData{
			PluginRequest: req,
		},
	}

	// Expect broadcast to be called for sending the fortune
	mockBus.On("Broadcast", "cytube.send", mock.Anything).Return(nil)

	// Handle the command
	err := p.handleFortuneCommand(event)
	assert.NoError(t, err)

	// Verify expectations
	mockBus.AssertExpectations(t)
}

func TestPlugin_HandleFortuneCommandPM(t *testing.T) {
	p := &Plugin{
		name:           "fortune",
		running:        true,
		cooldown:       30 * time.Second,
		lastRequestMap: make(map[string]time.Time),
	}

	mockBus := new(MockEventBus)
	p.eventBus = mockBus

	// Create a fortune PM command request
	req := &framework.PluginRequest{
		From: "testuser",
		Data: &framework.RequestData{
			Command: &framework.CommandData{
				Params: map[string]string{
					"channel":  "test-channel",
					"username": "testuser",
					"is_pm":    "true",
				},
			},
		},
	}

	event := &framework.DataEvent{
		Data: &framework.EventData{
			PluginRequest: req,
		},
	}

	// Expect broadcast to be called for PM response
	mockBus.On("Broadcast", "plugin.response", mock.Anything).Return(nil)

	// Handle the command
	err := p.handleFortuneCommand(event)
	assert.NoError(t, err)

	// Verify expectations
	mockBus.AssertExpectations(t)
}

func TestPlugin_HandleFortuneCommandRateLimit(t *testing.T) {
	p := &Plugin{
		name:           "fortune",
		running:        true,
		cooldown:       30 * time.Second,
		lastRequestMap: make(map[string]time.Time),
	}

	mockBus := new(MockEventBus)
	p.eventBus = mockBus

	// Create a fortune command request
	req := &framework.PluginRequest{
		From: "testuser",
		Data: &framework.RequestData{
			Command: &framework.CommandData{
				Params: map[string]string{
					"channel":  "test-channel",
					"username": "testuser",
				},
			},
		},
	}

	event := &framework.DataEvent{
		Data: &framework.EventData{
			PluginRequest: req,
		},
	}

	// First request should succeed
	mockBus.On("Broadcast", "cytube.send", mock.Anything).Return(nil).Once()
	err := p.handleFortuneCommand(event)
	assert.NoError(t, err)

	// Second request should be rate limited
	mockBus.On("Broadcast", "cytube.send", mock.MatchedBy(func(data *framework.EventData) bool {
		return data.RawMessage != nil && strings.Contains(data.RawMessage.Message, "Please wait")
	})).Return(nil).Once()

	err = p.handleFortuneCommand(event)
	assert.NoError(t, err)

	// Verify expectations
	mockBus.AssertExpectations(t)
}

func TestPlugin_HandleEvent(t *testing.T) {
	p := New()
	mockBus := new(MockEventBus)
	_ = p.Init(nil, mockBus)

	event := &framework.DataEvent{}
	err := p.HandleEvent(event)
	assert.NoError(t, err)
}

func TestPlugin_GetFortune(t *testing.T) {
	p := &Plugin{
		name: "fortune",
	}

	// Test getting a fortune
	fortune := p.getFortune()
	assert.NotEmpty(t, fortune)

	// Check for error messages
	if strings.Contains(fortune, "Sorry") {
		// If fortune is not installed, we should get a helpful error message
		assert.True(t,
			strings.Contains(fortune, "not installed") ||
				strings.Contains(fortune, "couldn't fetch"),
			"Expected helpful error message, got: %s", fortune)
	}
}

func TestIsFortuneStyle(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "fortune proverb", input: "A calm sea does not make a skilled sailor.", want: true},
		{name: "q and a joke", input: "Q: Why did the chicken cross the road?\nA: To get to the other side.", want: false},
		{name: "riddle text", input: "Here is a riddle for you", want: false},
		{name: "knock knock", input: "Knock knock. Who's there?", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isFortuneStyle(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
