package retry

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockEventBus implements a mock EventBus for testing
type MockEventBus struct {
	broadcastCalls []broadcastCall
	requestCalls   []requestCall
	subscriptions  map[string][]framework.EventHandler
	requestFunc    func(ctx context.Context, target, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error)
}

type broadcastCall struct {
	eventType string
	data      *framework.EventData
}

type requestCall struct {
	target    string
	eventType string
	data      *framework.EventData
	metadata  *framework.EventMetadata
}

func NewMockEventBus() *MockEventBus {
	return &MockEventBus{
		subscriptions: make(map[string][]framework.EventHandler),
	}
}

func (m *MockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	m.broadcastCalls = append(m.broadcastCalls, broadcastCall{eventType, data})
	return nil
}

func (m *MockEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return m.Broadcast(eventType, data)
}

func (m *MockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	return nil
}

func (m *MockEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return nil
}

func (m *MockEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	m.requestCalls = append(m.requestCalls, requestCall{target, eventType, data, metadata})
	if m.requestFunc != nil {
		return m.requestFunc(ctx, target, eventType, data, metadata)
	}
	return nil, errors.New("request not implemented")
}

func (m *MockEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
}

func (m *MockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	m.subscriptions[eventType] = append(m.subscriptions[eventType], handler)
	return nil
}

func (m *MockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	return m.Subscribe(pattern, handler)
}

func (m *MockEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	return nil
}

func (m *MockEventBus) UnregisterPlugin(name string) error {
	return nil
}

func (m *MockEventBus) GetDroppedEventCounts() map[string]int64 {
	return make(map[string]int64)
}

func (m *MockEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

// Test helpers
func createTestConfig() json.RawMessage {
	config := Config{
		Enabled:             true,
		PollIntervalSeconds: 1,
		BatchSize:           10,
		WorkerCount:         2,
		DefaultMaxRetries:   3,
		DefaultTimeout:      30,
		RetryPolicies: map[string]Policy{
			"test_operation": {
				MaxRetries:          3,
				InitialDelaySeconds: 1,
				MaxDelaySeconds:     60,
				BackoffMultiplier:   2.0,
				TimeoutSeconds:      30,
			},
		},
	}

	data, _ := json.Marshal(config)
	return data
}

func TestPluginInit(t *testing.T) {
	t.Run("successful init with defaults", func(t *testing.T) {
		plugin := NewPlugin()
		eventBus := NewMockEventBus()

		err := plugin.Init(json.RawMessage(`{}`), eventBus)
		assert.NoError(t, err)
		assert.Equal(t, 10, plugin.config.PollIntervalSeconds)
		assert.Equal(t, 10, plugin.config.BatchSize)
		assert.Equal(t, 3, plugin.config.WorkerCount)
	})

	t.Run("successful init with custom config", func(t *testing.T) {
		plugin := NewPlugin()
		eventBus := NewMockEventBus()

		config := createTestConfig()
		err := plugin.Init(config, eventBus)
		assert.NoError(t, err)
		assert.Equal(t, 1, plugin.config.PollIntervalSeconds)
		assert.Equal(t, 10, plugin.config.BatchSize)
		assert.Equal(t, 2, plugin.config.WorkerCount)
	})

	t.Run("invalid config", func(t *testing.T) {
		plugin := NewPlugin()
		eventBus := NewMockEventBus()

		err := plugin.Init(json.RawMessage(`invalid json`), eventBus)
		assert.Error(t, err)
	})
}

func TestPluginStart(t *testing.T) {
	t.Run("successful start", func(t *testing.T) {
		plugin := NewPlugin()
		eventBus := NewMockEventBus()

		config := createTestConfig()
		require.NoError(t, plugin.Init(config, eventBus))

		err := plugin.Start()
		assert.NoError(t, err)

		// Check subscriptions
		assert.Contains(t, eventBus.subscriptions, "*.failed")
		assert.Contains(t, eventBus.subscriptions, "*.error")
		assert.Contains(t, eventBus.subscriptions, "*.timeout")
		assert.Contains(t, eventBus.subscriptions, "retry.schedule")

		// Plugin should be started
		assert.NotNil(t, plugin.ctx)

		// Clean up
		plugin.Stop()
	})
}

func TestIsRetryable(t *testing.T) {
	plugin := NewPlugin()
	eventBus := NewMockEventBus()

	config := createTestConfig()
	require.NoError(t, plugin.Init(config, eventBus))

	testCases := []struct {
		name     string
		event    *framework.DataEvent
		expected bool
	}{
		{
			name: "retryable timeout error",
			event: &framework.DataEvent{
				EventType: "sql.query.failed",
				Data: &framework.EventData{
					KeyValue: map[string]string{
						"error": "connection timeout",
					},
				},
			},
			expected: true,
		},
		{
			name: "non-retryable validation error",
			event: &framework.DataEvent{
				EventType: "sql.query.failed",
				Data: &framework.EventData{
					KeyValue: map[string]string{
						"error": "validation error: invalid input",
					},
				},
			},
			expected: false,
		},
		{
			name: "event without error key",
			event: &framework.DataEvent{
				EventType: "something.failed",
				Data: &framework.EventData{
					KeyValue: map[string]string{},
				},
			},
			expected: true,
		},
		{
			name: "event marked as no-retry",
			event: &framework.DataEvent{
				EventType: "sql.query.failed",
				Data: &framework.EventData{
					KeyValue: map[string]string{
						"error":    "connection timeout",
						"no-retry": "true",
					},
				},
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := plugin.isRetryable(tc.event)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// Test removed - createRetryableOperation was refactored

func TestHandleEvent(t *testing.T) {
	t.Run("handle retry request", func(t *testing.T) {
		plugin := NewPlugin()
		eventBus := NewMockEventBus()

		config := createTestConfig()
		require.NoError(t, plugin.Init(config, eventBus))
		require.NoError(t, plugin.Start())
		defer plugin.Stop()

		// Send retry request
		event := &framework.DataEvent{
			EventType: "retry.operation.request",
			Data: &framework.EventData{
				KeyValue: map[string]string{
					"operation_id":   "test-op-1",
					"operation_type": "test.operation",
					"event_type":     "test.event",
					"target_plugin":  "test",
				},
			},
		}

		err := plugin.HandleEvent(event)
		assert.NoError(t, err)

		// Verify handler was called (no queue in new implementation)
		// The retry is scheduled to database instead
	})

	t.Run("handle failure event", func(t *testing.T) {
		plugin := NewPlugin()
		eventBus := NewMockEventBus()

		config := createTestConfig()
		require.NoError(t, plugin.Init(config, eventBus))
		require.NoError(t, plugin.Start())
		defer plugin.Stop()

		// Send failure event
		event := &framework.DataEvent{
			EventType: "sql.query.failed",
			Data: &framework.EventData{
				KeyValue: map[string]string{
					"operation_type":      "sql.query",
					"target_plugin":       "sql",
					"original_event_type": "sql.query.request",
					"error":               "connection timeout",
				},
			},
		}

		err := plugin.HandleEvent(event)
		assert.NoError(t, err)

		// Verify handler was called (no queue in new implementation)
		// The retry is scheduled to database instead
	})
}

// TestRetryProcessing removed - the new implementation uses database-backed queue

func TestGetPolicy(t *testing.T) {
	plugin := NewPlugin()
	eventBus := NewMockEventBus()

	config := Config{
		Enabled:           true,
		DefaultMaxRetries: 3,
		DefaultTimeout:    30,
		RetryPolicies: map[string]Policy{
			"critical.operation": {
				MaxRetries:          5,
				InitialDelaySeconds: 1,
				MaxDelaySeconds:     60,
				BackoffMultiplier:   1.5,
				TimeoutSeconds:      60,
			},
		},
	}

	configData, _ := json.Marshal(config)
	require.NoError(t, plugin.Init(configData, eventBus))

	t.Run("get custom policy", func(t *testing.T) {
		policy := plugin.getPolicy("critical.operation")
		assert.Equal(t, 5, policy.MaxRetries)
		assert.Equal(t, 1, policy.InitialDelaySeconds)
		assert.Equal(t, 1.5, policy.BackoffMultiplier)
	})

	t.Run("get default policy", func(t *testing.T) {
		policy := plugin.getPolicy("regular.operation")
		assert.Equal(t, 3, policy.MaxRetries)
		assert.Equal(t, 5, policy.InitialDelaySeconds)
		assert.Equal(t, 2.0, policy.BackoffMultiplier)
	})
}

// TestPowFloat removed - no longer needed
