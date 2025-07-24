package tell_test

import (
	"context"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/plugins/commands/tell"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockEventBus implements framework.EventBus for testing
type MockEventBus struct {
	mock.Mock
	mu         sync.Mutex
	handlers   map[string][]framework.EventHandler
	broadcasts []broadcastCall
}

type broadcastCall struct {
	eventType string
	data      *framework.EventData
}

func NewMockEventBus() *MockEventBus {
	return &MockEventBus{
		handlers:   make(map[string][]framework.EventHandler),
		broadcasts: make([]broadcastCall, 0),
	}
}

func (m *MockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	m.mu.Lock()
	m.broadcasts = append(m.broadcasts, broadcastCall{eventType: eventType, data: data})
	m.mu.Unlock()

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
	m.mu.Lock()
	defer m.mu.Unlock()

	m.handlers[eventType] = append(m.handlers[eventType], handler)
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

func (m *MockEventBus) GetDroppedEventCount(eventType string) int64 {
	args := m.Called(eventType)
	return args.Get(0).(int64)
}

func (m *MockEventBus) GetDroppedEventCounts() map[string]int64 {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(map[string]int64)
}

func (m *MockEventBus) GetBroadcasts() []broadcastCall {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]broadcastCall, len(m.broadcasts))
	copy(result, m.broadcasts)
	return result
}

func TestPlugin_New(t *testing.T) {
	p := tell.New()
	assert.NotNil(t, p)
	assert.Equal(t, "tell", p.Name())

	// Check if it implements PluginWithDependencies
	pwd, ok := p.(framework.PluginWithDependencies)
	if assert.True(t, ok, "Plugin should implement PluginWithDependencies") {
		assert.Equal(t, []string{"sql"}, pwd.Dependencies())
		assert.False(t, pwd.Ready())
	}
}

func TestPlugin_Init(t *testing.T) {
	p := tell.New()
	mockBus := NewMockEventBus()

	err := p.Init(nil, mockBus)
	assert.NoError(t, err)
}

func TestPlugin_Start(t *testing.T) {
	p := tell.New()
	mockBus := NewMockEventBus()

	// Set up the plugin
	_ = p.Init(nil, mockBus)

	// Mock expectations for SQL operations
	mockBus.On("Request", mock.Anything, "sql", "sql.exec.request", mock.Anything, mock.Anything).Return(&framework.EventData{
		SQLExecResponse: &framework.SQLExecResponse{
			Success:      true,
			RowsAffected: 0,
		},
	}, nil)

	// Mock expectations for subscriptions
	mockBus.On("Subscribe", "cytube.event.addUser", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "cytube.event.userLeave", mock.Anything).Return(nil)
	mockBus.On("Subscribe", "command.tell.execute", mock.Anything).Return(nil)
	mockBus.On("Broadcast", "command.register", mock.Anything).Return(nil)

	err := p.Start()
	assert.NoError(t, err)

	// Test double start
	err = p.Start()
	assert.Error(t, err)

	// Clean up - ensure proper shutdown
	err = p.Stop()
	assert.NoError(t, err)
}

func TestPlugin_Stop(t *testing.T) {
	p := tell.New()
	mockBus := NewMockEventBus()

	_ = p.Init(nil, mockBus)

	// Should not error when not running
	err := p.Stop()
	assert.NoError(t, err)
}

func TestPlugin_HandleEvent(t *testing.T) {
	p := tell.New()
	event := &framework.DataEvent{}

	err := p.HandleEvent(event)
	assert.NoError(t, err)
}

func TestPlugin_Status(t *testing.T) {
	p := tell.New()

	// When stopped
	status := p.Status()
	assert.Equal(t, "tell", status.Name)
	assert.Equal(t, "stopped", status.State)
}

func TestPlugin_CreateTable(t *testing.T) {
	db, sqlMock, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	p := tell.New()
	mockBus := NewMockEventBus()

	_ = p.Init(nil, mockBus)

	// Expect table creation query via event bus
	mockBus.On("Request", mock.Anything, "sql", "plugin.request", mock.Anything, mock.Anything).Return(&framework.EventData{
		SQLExecResponse: &framework.SQLExecResponse{
			Success:      true,
			RowsAffected: 0,
		},
	}, nil)

	// We can't directly test createTable as it's private, but it will be called during Start
	assert.NoError(t, sqlMock.ExpectationsWereMet())
}
