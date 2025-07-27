package framework

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// testEventBus implements EventBus interface for testing request functionality
type testEventBus struct {
	requestCount int
	shouldFail   bool
	failCount    int
	delay        time.Duration
}

func newTestEventBus() *testEventBus {
	return &testEventBus{}
}

func (t *testEventBus) Request(ctx context.Context, target string, eventType string, data *EventData, metadata *EventMetadata) (*EventData, error) {
	t.requestCount++

	// Add delay if configured
	if t.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(t.delay):
		}
	}

	// Simulate failures
	if t.shouldFail && t.requestCount <= t.failCount {
		return nil, errors.New("temporary failure")
	}

	// Return success response
	return &EventData{
		KeyValue: map[string]string{"result": "success"},
	}, nil
}

// Stub implementations for other EventBus methods
func (t *testEventBus) Broadcast(eventType string, data *EventData) error { return nil }
func (t *testEventBus) BroadcastWithMetadata(eventType string, data *EventData, metadata *EventMetadata) error {
	return nil
}
func (t *testEventBus) Send(target string, eventType string, data *EventData) error { return nil }
func (t *testEventBus) SendWithMetadata(target string, eventType string, data *EventData, metadata *EventMetadata) error {
	return nil
}
func (t *testEventBus) DeliverResponse(correlationID string, response *EventData, err error) {}
func (t *testEventBus) Subscribe(eventType string, handler EventHandler) error               { return nil }
func (t *testEventBus) SubscribeWithTags(pattern string, handler EventHandler, tags []string) error {
	return nil
}
func (t *testEventBus) RegisterPlugin(name string, plugin Plugin) error { return nil }
func (t *testEventBus) UnregisterPlugin(name string) error              { return nil }
func (t *testEventBus) GetDroppedEventCounts() map[string]int64         { return nil }
func (t *testEventBus) GetDroppedEventCount(eventType string) int64     { return 0 }

func TestRequestHelper_FastRequest(t *testing.T) {
	bus := newTestEventBus()
	helper := NewRequestHelper(bus, "test-source")

	ctx := context.Background()
	data := &EventData{KeyValue: map[string]string{"test": "data"}}

	result, err := helper.FastRequest(ctx, "target", "test.event", data)
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	if bus.requestCount != 1 {
		t.Errorf("Expected 1 request, got %d", bus.requestCount)
	}
}

func TestRequestHelper_NormalRequest(t *testing.T) {
	bus := newTestEventBus()
	helper := NewRequestHelper(bus, "test-source")

	ctx := context.Background()
	data := &EventData{KeyValue: map[string]string{"test": "data"}}

	result, err := helper.NormalRequest(ctx, "target", "test.event", data)
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	if bus.requestCount != 1 {
		t.Errorf("Expected 1 request, got %d", bus.requestCount)
	}
}

func TestRequestHelper_SlowRequest(t *testing.T) {
	bus := newTestEventBus()
	helper := NewRequestHelper(bus, "test-source")

	ctx := context.Background()
	data := &EventData{KeyValue: map[string]string{"test": "data"}}

	result, err := helper.SlowRequest(ctx, "target", "test.event", data)
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	if bus.requestCount != 1 {
		t.Errorf("Expected 1 request, got %d", bus.requestCount)
	}
}

func TestRequestHelper_CriticalRequest(t *testing.T) {
	bus := newTestEventBus()
	helper := NewRequestHelper(bus, "test-source")

	ctx := context.Background()
	data := &EventData{KeyValue: map[string]string{"test": "data"}}

	result, err := helper.CriticalRequest(ctx, "target", "test.event", data)
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	if bus.requestCount != 1 {
		t.Errorf("Expected 1 request, got %d", bus.requestCount)
	}
}

func TestRequestHelper_RequestWithRetry(t *testing.T) {
	bus := newTestEventBus()
	bus.shouldFail = true
	bus.failCount = 2 // Fail first 2 requests, succeed on 3rd

	helper := NewRequestHelper(bus, "test-source")

	ctx := context.Background()
	data := &EventData{KeyValue: map[string]string{"test": "data"}}

	result, err := helper.NormalRequest(ctx, "target", "test.event", data)
	if err != nil {
		t.Fatalf("Expected success after retries, got error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	if bus.requestCount != 3 {
		t.Errorf("Expected 3 requests (2 failures + 1 success), got %d", bus.requestCount)
	}
}

func TestRequestHelper_ContextTimeout(t *testing.T) {
	bus := newTestEventBus()
	bus.delay = 100 * time.Millisecond

	helper := NewRequestHelper(bus, "test-source")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	data := &EventData{KeyValue: map[string]string{"test": "data"}}

	_, err := helper.FastRequest(ctx, "target", "test.event", data)
	if err == nil {
		t.Fatal("Expected timeout error, got nil")
	}

	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "timeout") {
		t.Errorf("Expected timeout error, got: %v", err)
	}
}

func TestRequestHelper_IsRetryableError(t *testing.T) {
	helper := NewRequestHelper(nil, "test-source")

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "context timeout",
			err:      context.DeadlineExceeded,
			expected: true,
		},
		{
			name:     "context canceled",
			err:      context.Canceled,
			expected: true,
		},
		{
			name:     "connection refused",
			err:      errors.New("connection refused"),
			expected: true,
		},
		{
			name:     "request timeout",
			err:      errors.New("request timeout"),
			expected: true,
		},
		{
			name:     "database timeout",
			err:      errors.New("database timeout error"),
			expected: true,
		},
		{
			name:     "database connection error",
			err:      errors.New("database connection failed"),
			expected: true,
		},
		{
			name:     "invalid request format",
			err:      errors.New("invalid request format"),
			expected: false,
		},
		{
			name:     "permission denied",
			err:      errors.New("permission denied"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := helper.isRetryableError(tt.err)
			if result != tt.expected {
				t.Errorf("isRetryableError(%v) = %v, expected %v", tt.err, result, tt.expected)
			}
		})
	}
}

func TestRequestHelper_RequestWithConfig(t *testing.T) {
	bus := newTestEventBus()
	helper := NewRequestHelper(bus, "test-source")

	ctx := context.Background()
	data := &EventData{KeyValue: map[string]string{"test": "data"}}

	// Test with valid config
	result, err := helper.RequestWithConfig(ctx, "target", "test.event", data, "fast")
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	// Test with invalid config
	_, err = helper.RequestWithConfig(ctx, "target", "test.event", data, "invalid")
	if err == nil {
		t.Fatal("Expected error for invalid config, got nil")
	}

	if !strings.Contains(err.Error(), "unknown request config") {
		t.Errorf("Expected unknown config error, got: %v", err)
	}
}

func TestDefaultRequestConfigs(t *testing.T) {
	// Test that all default configs are properly defined
	expectedConfigs := []string{"fast", "normal", "slow", "critical"}

	for _, configName := range expectedConfigs {
		config, exists := DefaultRequestConfigs[configName]
		if !exists {
			t.Errorf("Expected config %s to exist", configName)
			continue
		}

		if config.Timeout <= 0 {
			t.Errorf("Config %s has invalid timeout: %v", configName, config.Timeout)
		}

		if config.MaxRetries < 0 {
			t.Errorf("Config %s has invalid max retries: %d", configName, config.MaxRetries)
		}

		if config.RetryDelay <= 0 {
			t.Errorf("Config %s has invalid retry delay: %v", configName, config.RetryDelay)
		}

		if config.BackoffRate <= 0 {
			t.Errorf("Config %s has invalid backoff rate: %f", configName, config.BackoffRate)
		}

		if config.MetricName == "" {
			t.Errorf("Config %s has empty metric name", configName)
		}
	}
}

func TestRequestHelper_ConfigTimeouts(t *testing.T) {
	tests := []struct {
		name            string
		configName      string
		expectedTimeout time.Duration
	}{
		{
			name:            "fast config",
			configName:      "fast",
			expectedTimeout: 5 * time.Second,
		},
		{
			name:            "normal config",
			configName:      "normal",
			expectedTimeout: 15 * time.Second,
		},
		{
			name:            "slow config",
			configName:      "slow",
			expectedTimeout: 30 * time.Second,
		},
		{
			name:            "critical config",
			configName:      "critical",
			expectedTimeout: 45 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, exists := DefaultRequestConfigs[tt.configName]
			if !exists {
				t.Fatalf("Config %s does not exist", tt.configName)
			}

			if config.Timeout != tt.expectedTimeout {
				t.Errorf("Config %s timeout = %v, expected %v", tt.configName, config.Timeout, tt.expectedTimeout)
			}
		})
	}
}

func TestSQLRequestHelper_Creation(t *testing.T) {
	bus := newTestEventBus()
	helper := NewSQLRequestHelper(bus, "test-source")

	if helper == nil {
		t.Fatal("Expected SQL helper to be created")
	}

	if helper.RequestHelper == nil {
		t.Fatal("Expected SQL helper to have embedded RequestHelper")
	}

	if helper.source != "test-source" {
		t.Errorf("Expected source 'test-source', got %s", helper.source)
	}
}

func TestRequestHelper_MaxRetries(t *testing.T) {
	bus := newTestEventBus()
	bus.shouldFail = true
	bus.failCount = 10 // Fail more times than max retries

	helper := NewRequestHelper(bus, "test-source")

	ctx := context.Background()
	data := &EventData{KeyValue: map[string]string{"test": "data"}}

	_, err := helper.FastRequest(ctx, "target", "test.event", data)
	if err == nil {
		t.Fatal("Expected failure after max retries, got success")
	}

	// Fast config has MaxRetries = 1, so should make 2 requests total (1 initial + 1 retry)
	expectedRequests := 2
	if bus.requestCount != expectedRequests {
		t.Errorf("Expected %d requests, got %d", expectedRequests, bus.requestCount)
	}
}
