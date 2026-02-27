package games

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

func TestNew(t *testing.T) {
	plugin := New()
	assert.NotNil(t, plugin)
	assert.Equal(t, "games", plugin.Name())
}

func TestInit(t *testing.T) {
	plugin := New().(*Plugin)
	mockBus := new(MockEventBus)

	err := plugin.Init(nil, mockBus)
	assert.NoError(t, err)
	assert.Equal(t, 5*time.Second, plugin.cooldown)

	config := Config{CooldownSeconds: 12}
	raw, marshalErr := json.Marshal(config)
	assert.NoError(t, marshalErr)

	err = plugin.Init(raw, mockBus)
	assert.NoError(t, err)
	assert.Equal(t, 12*time.Second, plugin.cooldown)
}

func TestStart(t *testing.T) {
	plugin := New()
	mockBus := new(MockEventBus)
	_ = plugin.Init(nil, mockBus)

	mockBus.On("Broadcast", "command.register", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.8ball.execute", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.eightball.execute", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.coinflip.execute", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.flip.execute", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.rps.execute", mock.Anything).Return(nil)

	err := plugin.Start()
	assert.NoError(t, err)
	mockBus.AssertExpectations(t)
}

func TestCheckRateLimit(t *testing.T) {
	plugin := New().(*Plugin)
	assert.True(t, plugin.checkRateLimit("alice"))
	assert.False(t, plugin.checkRateLimit("alice"))
}

func TestGenerate8BallResponse(t *testing.T) {
	plugin := New().(*Plugin)

	response := plugin.generate8BallResponse([]string{})
	assert.Equal(t, "Usage: !8ball <question>", response)

	response = plugin.generate8BallResponse([]string{"will", "this", "work?"})
	assert.True(t, strings.HasPrefix(response, "🎱 "))
}

func TestGenerateCoinFlipResponse(t *testing.T) {
	plugin := New().(*Plugin)

	response := plugin.generateCoinFlipResponse(nil)
	assert.Contains(t, []string{"🪙 Heads!", "🪙 Tails!"}, response)
}

func TestGenerateRPSResponse(t *testing.T) {
	plugin := New().(*Plugin)

	t.Run("usage", func(t *testing.T) {
		response := plugin.generateRPSResponse([]string{})
		assert.Equal(t, "Usage: !rps <rock|paper|scissors>", response)
	})

	t.Run("invalid", func(t *testing.T) {
		response := plugin.generateRPSResponse([]string{"lizard"})
		assert.Equal(t, "Please choose rock, paper, or scissors.", response)
	})

	t.Run("valid", func(t *testing.T) {
		response := plugin.generateRPSResponse([]string{"rock"})
		assert.True(t, strings.HasPrefix(response, "✂️ You played rock, I played "))
	})
}
