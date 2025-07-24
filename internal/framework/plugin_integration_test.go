package framework

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

// MockEventBus implements EventBus interface for testing
type MockEventBus struct {
	mu                 sync.RWMutex
	events             []MockEvent
	subscribers        map[string][]EventHandler
	patternSubscribers map[string][]EventHandler
	plugins            map[string]Plugin
	pendingRequests    map[string]*MockRequest
	responses          map[string]*EventData
	broadcastDelay     time.Duration
	sendDelay          time.Duration
	simulateTimeout    bool
	simulateError      bool
	errorMessage       string
	droppedEventCounts map[string]int64
	requestTimeout     time.Duration
	correlationCounter int64
}

// MockEvent represents a captured event
type MockEvent struct {
	EventType string
	Data      *EventData
	Metadata  *EventMetadata
	Target    string
	Timestamp time.Time
}

// MockRequest represents a pending request
type MockRequest struct {
	ID         string
	Target     string
	EventType  string
	Data       *EventData
	Metadata   *EventMetadata
	ResponseCh chan *EventData
	ErrorCh    chan error
	Timestamp  time.Time
	Context    context.Context
}

// NewMockEventBus creates a new mock event bus
func NewMockEventBus() *MockEventBus {
	return &MockEventBus{
		events:             make([]MockEvent, 0),
		subscribers:        make(map[string][]EventHandler),
		patternSubscribers: make(map[string][]EventHandler),
		plugins:            make(map[string]Plugin),
		pendingRequests:    make(map[string]*MockRequest),
		responses:          make(map[string]*EventData),
		droppedEventCounts: make(map[string]int64),
		requestTimeout:     30 * time.Second,
	}
}

// Broadcast implements EventBus.Broadcast
func (m *MockEventBus) Broadcast(eventType string, data *EventData) error {
	return m.BroadcastWithMetadata(eventType, data, NewEventMetadata("mock", eventType))
}

// BroadcastWithMetadata implements EventBus.BroadcastWithMetadata
func (m *MockEventBus) BroadcastWithMetadata(eventType string, data *EventData, metadata *EventMetadata) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.simulateError {
		return fmt.Errorf("mock error: %s", m.errorMessage)
	}

	// Record the event
	m.events = append(m.events, MockEvent{
		EventType: eventType,
		Data:      data,
		Metadata:  metadata,
		Timestamp: time.Now(),
	})

	// Simulate delay if configured
	if m.broadcastDelay > 0 {
		time.Sleep(m.broadcastDelay)
	}

	// Route to subscribers
	go m.routeToSubscribers(eventType, data, metadata)

	return nil
}

// Send implements EventBus.Send
func (m *MockEventBus) Send(target string, eventType string, data *EventData) error {
	return m.SendWithMetadata(target, eventType, data, NewEventMetadata("mock", eventType).WithTarget(target))
}

// SendWithMetadata implements EventBus.SendWithMetadata
func (m *MockEventBus) SendWithMetadata(target string, eventType string, data *EventData, metadata *EventMetadata) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.simulateError {
		return fmt.Errorf("mock error: %s", m.errorMessage)
	}

	// Record the event
	m.events = append(m.events, MockEvent{
		EventType: eventType,
		Data:      data,
		Metadata:  metadata,
		Target:    target,
		Timestamp: time.Now(),
	})

	// Simulate delay if configured
	if m.sendDelay > 0 {
		time.Sleep(m.sendDelay)
	}

	// Route to target plugin
	go m.routeToTarget(target, eventType, data, metadata)

	return nil
}

// Request implements EventBus.Request
func (m *MockEventBus) Request(ctx context.Context, target string, eventType string, data *EventData, metadata *EventMetadata) (*EventData, error) {
	m.mu.Lock()

	if m.simulateError {
		m.mu.Unlock()
		return nil, fmt.Errorf("mock error: %s", m.errorMessage)
	}

	// Generate correlation ID if not provided
	if metadata.CorrelationID == "" {
		m.correlationCounter++
		metadata.CorrelationID = fmt.Sprintf("mock-req-%d", m.correlationCounter)
	}

	// Create request tracking
	request := &MockRequest{
		ID:         metadata.CorrelationID,
		Target:     target,
		EventType:  eventType,
		Data:       data,
		Metadata:   metadata,
		ResponseCh: make(chan *EventData, 1),
		ErrorCh:    make(chan error, 1),
		Timestamp:  time.Now(),
		Context:    ctx,
	}

	m.pendingRequests[metadata.CorrelationID] = request
	m.mu.Unlock()

	// Send the request
	if err := m.SendWithMetadata(target, eventType, data, metadata); err != nil {
		m.mu.Lock()
		delete(m.pendingRequests, metadata.CorrelationID)
		m.mu.Unlock()
		return nil, err
	}

	// Wait for response with timeout
	timeout := m.requestTimeout
	if metadata.Timeout > 0 {
		timeout = metadata.Timeout
	}

	select {
	case response := <-request.ResponseCh:
		m.mu.Lock()
		delete(m.pendingRequests, metadata.CorrelationID)
		m.mu.Unlock()
		return response, nil
	case err := <-request.ErrorCh:
		m.mu.Lock()
		delete(m.pendingRequests, metadata.CorrelationID)
		m.mu.Unlock()
		return nil, err
	case <-time.After(timeout):
		m.mu.Lock()
		delete(m.pendingRequests, metadata.CorrelationID)
		m.mu.Unlock()
		return nil, fmt.Errorf("request timeout after %v", timeout)
	case <-ctx.Done():
		m.mu.Lock()
		delete(m.pendingRequests, metadata.CorrelationID)
		m.mu.Unlock()
		return nil, ctx.Err()
	}
}

// DeliverResponse implements EventBus.DeliverResponse
func (m *MockEventBus) DeliverResponse(correlationID string, response *EventData, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if request, exists := m.pendingRequests[correlationID]; exists {
		if err != nil {
			select {
			case request.ErrorCh <- err:
			default:
			}
		} else {
			select {
			case request.ResponseCh <- response:
			default:
			}
		}
	}
}

// Subscribe implements EventBus.Subscribe
func (m *MockEventBus) Subscribe(eventType string, handler EventHandler) error {
	return m.SubscribeWithTags(eventType, handler, nil)
}

// SubscribeWithTags implements EventBus.SubscribeWithTags
func (m *MockEventBus) SubscribeWithTags(pattern string, handler EventHandler, tags []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if containsSubstring(pattern, "*") {
		m.patternSubscribers[pattern] = append(m.patternSubscribers[pattern], handler)
	} else {
		m.subscribers[pattern] = append(m.subscribers[pattern], handler)
	}

	return nil
}

// RegisterPlugin implements EventBus.RegisterPlugin
func (m *MockEventBus) RegisterPlugin(name string, plugin Plugin) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.plugins[name] = plugin
	return nil
}

// UnregisterPlugin implements EventBus.UnregisterPlugin
func (m *MockEventBus) UnregisterPlugin(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.plugins, name)
	return nil
}

// GetDroppedEventCounts implements EventBus.GetDroppedEventCounts
func (m *MockEventBus) GetDroppedEventCounts() map[string]int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	counts := make(map[string]int64)
	for k, v := range m.droppedEventCounts {
		counts[k] = v
	}
	return counts
}

// GetDroppedEventCount implements EventBus.GetDroppedEventCount
func (m *MockEventBus) GetDroppedEventCount(eventType string) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.droppedEventCounts[eventType]
}

// Helper methods for testing

// GetEvents returns all captured events
func (m *MockEventBus) GetEvents() []MockEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	events := make([]MockEvent, len(m.events))
	copy(events, m.events)
	return events
}

// GetEventsByType returns events of a specific type
func (m *MockEventBus) GetEventsByType(eventType string) []MockEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var filtered []MockEvent
	for _, event := range m.events {
		if event.EventType == eventType {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

// GetEventsByTarget returns events sent to a specific target
func (m *MockEventBus) GetEventsByTarget(target string) []MockEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var filtered []MockEvent
	for _, event := range m.events {
		if event.Target == target {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

// GetPendingRequests returns all pending requests
func (m *MockEventBus) GetPendingRequests() map[string]*MockRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	requests := make(map[string]*MockRequest)
	for k, v := range m.pendingRequests {
		requests[k] = v
	}
	return requests
}

// ClearEvents clears all captured events
func (m *MockEventBus) ClearEvents() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.events = make([]MockEvent, 0)
}

// SetBroadcastDelay sets delay for broadcasts
func (m *MockEventBus) SetBroadcastDelay(delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.broadcastDelay = delay
}

// SetSendDelay sets delay for sends
func (m *MockEventBus) SetSendDelay(delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sendDelay = delay
}

// SetSimulateTimeout enables/disables timeout simulation
func (m *MockEventBus) SetSimulateTimeout(enable bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.simulateTimeout = enable
}

// SetSimulateError enables/disables error simulation
func (m *MockEventBus) SetSimulateError(enable bool, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.simulateError = enable
	m.errorMessage = message
}

// SetRequestTimeout sets the request timeout
func (m *MockEventBus) SetRequestTimeout(timeout time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requestTimeout = timeout
}

// routeToSubscribers routes events to subscribers
func (m *MockEventBus) routeToSubscribers(eventType string, data *EventData, metadata *EventMetadata) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Route to exact match subscribers
	if handlers, exists := m.subscribers[eventType]; exists {
		for _, handler := range handlers {
			go func(h EventHandler, meta *EventMetadata) {
				event := NewDataEvent(eventType, data)
				// Store correlation ID in a way the handler can access it
				if meta != nil && meta.CorrelationID != "" && data != nil && data.PluginRequest != nil {
					data.PluginRequest.ID = meta.CorrelationID
				}
				_ = h(event)
			}(handler, metadata)
		}
	}

	// Route to pattern subscribers
	for pattern, handlers := range m.patternSubscribers {
		if matchesPattern(pattern, eventType) {
			for _, handler := range handlers {
				go func(h EventHandler, meta *EventMetadata) {
					event := NewDataEvent(eventType, data)
					// Store correlation ID in a way the handler can access it
					if meta != nil && meta.CorrelationID != "" && data != nil && data.PluginRequest != nil {
						data.PluginRequest.ID = meta.CorrelationID
					}
					if err := h(event); err != nil {
						// Log error but don't panic in async handler
						fmt.Printf("Error in event handler: %v\n", err)
					}
				}(handler, metadata)
			}
		}
	}
}

// routeToTarget routes events to target plugin
func (m *MockEventBus) routeToTarget(target string, eventType string, data *EventData, metadata *EventMetadata) {
	m.mu.RLock()
	plugin, exists := m.plugins[target]
	m.mu.RUnlock()

	if exists {
		event := NewDataEvent(eventType, data)
		_ = plugin.HandleEvent(event)
	}

	// Also route to subscribers of the event type
	m.routeToSubscribers(eventType, data, metadata)
}

// Helper functions

func containsSubstring(s string, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && findIndexOf(s, substr) >= 0
}

func findIndexOf(s string, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func matchesPattern(pattern string, eventType string) bool {
	if !containsSubstring(pattern, "*") {
		return pattern == eventType
	}

	// Simple wildcard matching
	if pattern == "*" {
		return true
	}

	if pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(eventType) >= len(prefix) && eventType[:len(prefix)] == prefix
	}

	return false
}

// Test Plugin Implementation for Testing

type TestPlugin struct {
	name           string
	initCalled     bool
	startCalled    bool
	stopCalled     bool
	eventsHandled  int
	eventBus       EventBus
	requestHandler func(*PluginRequest) (*EventData, error)
	mu             sync.Mutex
}

func NewTestPlugin(name string) *TestPlugin {
	return &TestPlugin{
		name: name,
	}
}

func (p *TestPlugin) Init(config json.RawMessage, bus EventBus) error {
	p.initCalled = true
	p.eventBus = bus
	return nil
}

func (p *TestPlugin) Start() error {
	p.startCalled = true
	return nil
}

func (p *TestPlugin) Stop() error {
	p.stopCalled = true
	return nil
}

func (p *TestPlugin) HandleEvent(event Event) error {
	p.mu.Lock()
	p.eventsHandled++
	p.mu.Unlock()

	// Handle plugin requests
	if dataEvent, ok := event.(*DataEvent); ok {
		if dataEvent.Data != nil && dataEvent.Data.PluginRequest != nil {
			// Use the request ID as correlation ID
			correlationID := dataEvent.Data.PluginRequest.ID

			if p.requestHandler != nil {
				response, err := p.requestHandler(dataEvent.Data.PluginRequest)
				if p.eventBus != nil {
					if err != nil {
						p.eventBus.DeliverResponse(correlationID, nil, err)
					} else {
						p.eventBus.DeliverResponse(correlationID, response, nil)
					}
				}
			} else {
				// Default response if no handler is set
				defaultResponse := &EventData{
					PluginResponse: &PluginResponse{
						ID:      dataEvent.Data.PluginRequest.ID,
						From:    p.name,
						Success: false,
						Error:   "No request handler configured",
					},
				}
				if p.eventBus != nil {
					p.eventBus.DeliverResponse(correlationID, defaultResponse, nil)
				}
			}
		}
	}

	return nil
}

func (p *TestPlugin) Status() PluginStatus {
	return PluginStatus{
		Name:          p.name,
		State:         "running",
		EventsHandled: int64(p.eventsHandled),
	}
}

func (p *TestPlugin) Name() string {
	return p.name
}

func (p *TestPlugin) SetRequestHandler(handler func(*PluginRequest) (*EventData, error)) {
	p.requestHandler = handler
}

// Integration Tests

func TestMockEventBusBasicFlow(t *testing.T) {
	mockBus := NewMockEventBus()

	// Test basic broadcast
	testData := &EventData{
		KeyValue: map[string]string{
			"test": "value",
		},
	}

	metadata := NewEventMetadata("test", "test.event")
	if err := mockBus.BroadcastWithMetadata("test.event", testData, metadata); err != nil {
		t.Fatalf("Failed to broadcast: %v", err)
	}

	// Verify event was captured
	events := mockBus.GetEvents()
	if len(events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(events))
	}

	if events[0].EventType != "test.event" {
		t.Errorf("Expected event type 'test.event', got '%s'", events[0].EventType)
	}

	if events[0].Data.KeyValue["test"] != "value" {
		t.Errorf("Expected key 'test' to have value 'value', got '%s'", events[0].Data.KeyValue["test"])
	}
}

func TestMockEventBusRequestResponseFlow(t *testing.T) {
	mockBus := NewMockEventBus()

	// Create test plugin that responds to requests
	testPlugin := NewTestPlugin("test-plugin")
	testPlugin.SetRequestHandler(func(req *PluginRequest) (*EventData, error) {
		return &EventData{
			PluginResponse: &PluginResponse{
				ID:      req.ID,
				From:    "test-plugin",
				Success: true,
				Data: &ResponseData{
					KeyValue: map[string]string{
						"response": "success",
					},
				},
			},
		}, nil
	})

	// Initialize and register plugin
	if err := testPlugin.Init(nil, mockBus); err != nil {
		t.Fatalf("Failed to initialize plugin: %v", err)
	}

	if err := mockBus.RegisterPlugin("test-plugin", testPlugin); err != nil {
		t.Fatalf("Failed to register plugin: %v", err)
	}

	// Subscribe to plugin requests
	if err := mockBus.Subscribe("plugin.request", testPlugin.HandleEvent); err != nil {
		t.Fatalf("Failed to subscribe to plugin requests: %v", err)
	}

	// Test request/response
	queryData := &EventData{
		PluginRequest: &PluginRequest{
			ID:   "test-request-1",
			From: "test",
			To:   "test-plugin",
			Type: "test-request",
		},
	}

	metadata := NewEventMetadata("test", "plugin.request").
		WithCorrelationID("test-correlation-1").
		WithTarget("test-plugin").
		WithTimeout(5 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	response, err := mockBus.Request(ctx, "test-plugin", "plugin.request", queryData, metadata)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if response.PluginResponse.ID != "test-correlation-1" {
		t.Errorf("Expected response ID 'test-correlation-1', got '%s'", response.PluginResponse.ID)
	}

	if !response.PluginResponse.Success {
		t.Error("Expected successful response")
	}

	if response.PluginResponse.Data.KeyValue["response"] != "success" {
		t.Errorf("Expected response 'success', got '%s'", response.PluginResponse.Data.KeyValue["response"])
	}
}

func TestMockEventBusTimeoutHandling(t *testing.T) {
	mockBus := NewMockEventBus()

	// Set short timeout
	mockBus.SetRequestTimeout(100 * time.Millisecond)

	// Create plugin that doesn't respond
	testPlugin := NewTestPlugin("slow-plugin")
	if err := mockBus.RegisterPlugin("slow-plugin", testPlugin); err != nil {
		t.Fatalf("Failed to register plugin: %v", err)
	}

	// Test timeout
	queryData := &EventData{
		PluginRequest: &PluginRequest{
			ID:   "timeout-test-1",
			From: "test",
			To:   "slow-plugin",
			Type: "test-request",
		},
	}

	metadata := NewEventMetadata("test", "plugin.request").
		WithCorrelationID("timeout-correlation-1").
		WithTarget("slow-plugin").
		WithTimeout(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	start := time.Now()
	_, err := mockBus.Request(ctx, "slow-plugin", "plugin.request", queryData, metadata)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("Expected timeout error")
	}

	if elapsed < 50*time.Millisecond {
		t.Errorf("Request returned too quickly: %v", elapsed)
	}

	if elapsed > 200*time.Millisecond {
		t.Errorf("Request took too long: %v", elapsed)
	}
}

func TestMockEventBusErrorSimulation(t *testing.T) {
	mockBus := NewMockEventBus()

	// Enable error simulation
	mockBus.SetSimulateError(true, "simulated network error")

	// Test error propagation
	queryData := &EventData{
		PluginRequest: &PluginRequest{
			ID:   "error-test-1",
			From: "test",
			To:   "target-plugin",
			Type: "test-request",
		},
	}

	metadata := NewEventMetadata("test", "plugin.request").
		WithCorrelationID("error-correlation-1").
		WithTarget("target-plugin")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := mockBus.Request(ctx, "target-plugin", "plugin.request", queryData, metadata)
	if err == nil {
		t.Error("Expected error")
	}

	if !containsSubstring(err.Error(), "simulated network error") {
		t.Errorf("Expected simulated error message, got: %v", err)
	}
}

func TestMockEventBusCorrelationIDGeneration(t *testing.T) {
	mockBus := NewMockEventBus()

	// Test automatic correlation ID generation
	queryData := &EventData{
		PluginRequest: &PluginRequest{
			ID:   "correlation-test-1",
			From: "test",
			To:   "target-plugin",
			Type: "test-request",
		},
	}

	metadata := NewEventMetadata("test", "plugin.request").
		WithTarget("target-plugin").
		WithTimeout(1 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// This should generate a correlation ID and timeout
	_, err := mockBus.Request(ctx, "target-plugin", "plugin.request", queryData, metadata)
	if err == nil {
		t.Error("Expected timeout error")
	}

	// Verify correlation ID was generated
	if metadata.CorrelationID == "" {
		t.Error("Expected correlation ID to be generated")
	}

	// Test with existing correlation ID
	existingID := "existing-correlation-id"
	metadata2 := NewEventMetadata("test", "plugin.request").
		WithCorrelationID(existingID).
		WithTarget("target-plugin").
		WithTimeout(1 * time.Second)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()

	_, err2 := mockBus.Request(ctx2, "target-plugin", "plugin.request", queryData, metadata2)
	if err2 == nil {
		t.Error("Expected timeout error")
	}

	// Verify correlation ID was preserved
	if metadata2.CorrelationID != existingID {
		t.Errorf("Expected correlation ID '%s', got '%s'", existingID, metadata2.CorrelationID)
	}
}

func TestMockEventBusSubscriptionAndRouting(t *testing.T) {
	mockBus := NewMockEventBus()

	// Test subscription and event routing
	var receivedEvents []Event
	var mu sync.Mutex

	handler := func(event Event) error {
		mu.Lock()
		receivedEvents = append(receivedEvents, event)
		mu.Unlock()
		return nil
	}

	// Subscribe to events
	if err := mockBus.Subscribe("test.event", handler); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Send test event
	testData := &EventData{
		KeyValue: map[string]string{
			"test": "value",
		},
	}

	metadata := NewEventMetadata("test", "test.event")
	if err := mockBus.BroadcastWithMetadata("test.event", testData, metadata); err != nil {
		t.Fatalf("Failed to broadcast: %v", err)
	}

	// Give time for async processing
	time.Sleep(100 * time.Millisecond)

	// Verify event was received
	mu.Lock()
	eventCount := len(receivedEvents)
	mu.Unlock()

	if eventCount != 1 {
		t.Errorf("Expected 1 received event, got %d", eventCount)
	}

	if eventCount > 0 {
		if receivedEvents[0].Type() != "test.event" {
			t.Errorf("Expected event type 'test.event', got '%s'", receivedEvents[0].Type())
		}
	}
}

func TestMockEventBusPatternSubscription(t *testing.T) {
	mockBus := NewMockEventBus()

	// Test pattern subscription
	var receivedEvents []Event
	var mu sync.Mutex

	handler := func(event Event) error {
		mu.Lock()
		receivedEvents = append(receivedEvents, event)
		mu.Unlock()
		return nil
	}

	// Subscribe to pattern
	if err := mockBus.SubscribeWithTags("test.*", handler, nil); err != nil {
		t.Fatalf("Failed to subscribe to pattern: %v", err)
	}

	// Send events matching pattern
	testEvents := []string{"test.event1", "test.event2", "test.event3"}
	for _, eventType := range testEvents {
		testData := &EventData{
			KeyValue: map[string]string{
				"event": eventType,
			},
		}

		metadata := NewEventMetadata("test", eventType)
		if err := mockBus.BroadcastWithMetadata(eventType, testData, metadata); err != nil {
			t.Fatalf("Failed to broadcast %s: %v", eventType, err)
		}
	}

	// Send event not matching pattern
	nonMatchingData := &EventData{
		KeyValue: map[string]string{
			"event": "other.event",
		},
	}
	metadata := NewEventMetadata("test", "other.event")
	if err := mockBus.BroadcastWithMetadata("other.event", nonMatchingData, metadata); err != nil {
		t.Fatalf("Failed to broadcast non-matching event: %v", err)
	}

	// Give time for async processing
	time.Sleep(200 * time.Millisecond)

	// Verify only matching events were received
	mu.Lock()
	eventCount := len(receivedEvents)
	mu.Unlock()

	if eventCount != 3 {
		t.Errorf("Expected 3 received events, got %d", eventCount)
	}
}

func TestMockEventBusConcurrentRequests(t *testing.T) {
	mockBus := NewMockEventBus()

	// Set reasonable timeout
	mockBus.SetRequestTimeout(2 * time.Second)

	// Create multiple test plugins
	plugins := []string{"plugin1", "plugin2", "plugin3"}
	for _, name := range plugins {
		plugin := NewTestPlugin(name)
		if err := mockBus.RegisterPlugin(name, plugin); err != nil {
			t.Fatalf("Failed to register plugin %s: %v", name, err)
		}
	}

	// Test concurrent requests
	numRequests := 10
	var wg sync.WaitGroup
	results := make([]error, numRequests)

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			pluginName := plugins[idx%len(plugins)]
			queryData := &EventData{
				PluginRequest: &PluginRequest{
					ID:   fmt.Sprintf("concurrent-request-%d", idx),
					From: "test",
					To:   pluginName,
					Type: "test-request",
				},
			}

			metadata := NewEventMetadata("test", "plugin.request").
				WithCorrelationID(fmt.Sprintf("concurrent-corr-%d", idx)).
				WithTarget(pluginName).
				WithTimeout(1 * time.Second)

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			_, err := mockBus.Request(ctx, pluginName, "plugin.request", queryData, metadata)
			results[idx] = err
		}(i)
	}

	wg.Wait()

	// Verify all requests completed (with timeout errors expected since plugins don't respond)
	for i, err := range results {
		if err == nil {
			t.Errorf("Request %d expected timeout error, got nil", i)
		}
	}

	// Verify all events were captured
	allEvents := mockBus.GetEvents()
	if len(allEvents) != numRequests {
		t.Errorf("Expected %d events, got %d", numRequests, len(allEvents))
	}
}

func TestMockEventBusCleanupOnTimeout(t *testing.T) {
	mockBus := NewMockEventBus()

	// Create multiple requests that will timeout
	numRequests := 5
	correlationIDs := make([]string, numRequests)

	for i := 0; i < numRequests; i++ {
		correlationID := fmt.Sprintf("cleanup-test-%d", i)
		correlationIDs[i] = correlationID

		queryData := &EventData{
			PluginRequest: &PluginRequest{
				ID:   correlationID,
				From: "test",
				To:   "nonexistent-plugin",
				Type: "test-request",
			},
		}

		metadata := NewEventMetadata("test", "plugin.request").
			WithCorrelationID(correlationID).
			WithTarget("nonexistent-plugin").
			WithTimeout(50 * time.Millisecond)

		go func(id string) {
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
			defer cancel()

			_, _ = mockBus.Request(ctx, "nonexistent-plugin", "plugin.request", queryData, metadata)
		}(correlationID)
	}

	// Wait for all requests to timeout
	time.Sleep(200 * time.Millisecond)

	// Verify all pending requests were cleaned up
	pendingRequests := mockBus.GetPendingRequests()
	if len(pendingRequests) != 0 {
		t.Errorf("Expected 0 pending requests after timeout, got %d", len(pendingRequests))
	}
}

func TestMockEventBusDirectResponseDelivery(t *testing.T) {
	mockBus := NewMockEventBus()

	// Test direct response delivery
	correlationID := "direct-response-test"
	responseData := &EventData{
		PluginResponse: &PluginResponse{
			ID:      correlationID,
			From:    "test-plugin",
			Success: true,
			Data: &ResponseData{
				KeyValue: map[string]string{
					"result": "success",
				},
			},
		},
	}

	// Create pending request manually
	mockBus.mu.Lock()
	request := &MockRequest{
		ID:         correlationID,
		ResponseCh: make(chan *EventData, 1),
		ErrorCh:    make(chan error, 1),
	}
	mockBus.pendingRequests[correlationID] = request
	mockBus.mu.Unlock()

	// Deliver response
	mockBus.DeliverResponse(correlationID, responseData, nil)

	// Verify response was delivered
	select {
	case response := <-request.ResponseCh:
		if response.PluginResponse.ID != correlationID {
			t.Errorf("Expected response ID '%s', got '%s'", correlationID, response.PluginResponse.ID)
		}
		if !response.PluginResponse.Success {
			t.Error("Expected successful response")
		}
	case <-time.After(1 * time.Second):
		t.Error("Response delivery timeout")
	}
}

func TestMockEventBusDirectErrorDelivery(t *testing.T) {
	mockBus := NewMockEventBus()

	// Test error delivery
	correlationID := "direct-error-test"
	testError := fmt.Errorf("test plugin error")

	// Create pending request manually
	mockBus.mu.Lock()
	request := &MockRequest{
		ID:         correlationID,
		ResponseCh: make(chan *EventData, 1),
		ErrorCh:    make(chan error, 1),
	}
	mockBus.pendingRequests[correlationID] = request
	mockBus.mu.Unlock()

	// Deliver error
	mockBus.DeliverResponse(correlationID, nil, testError)

	// Verify error was delivered
	select {
	case err := <-request.ErrorCh:
		if err.Error() != testError.Error() {
			t.Errorf("Expected error '%s', got '%s'", testError.Error(), err.Error())
		}
	case <-time.After(1 * time.Second):
		t.Error("Error delivery timeout")
	}
}

// Benchmark tests

func BenchmarkMockEventBusRequest(b *testing.B) {
	mockBus := NewMockEventBus()

	// Create test plugin
	plugin := NewTestPlugin("benchmark-plugin")
	_ = mockBus.RegisterPlugin("benchmark-plugin", plugin)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			queryData := &EventData{
				PluginRequest: &PluginRequest{
					ID:   "benchmark-request",
					From: "test",
					To:   "benchmark-plugin",
					Type: "test-request",
				},
			}

			metadata := NewEventMetadata("test", "plugin.request").
				WithTarget("benchmark-plugin").
				WithTimeout(1 * time.Second)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, _ = mockBus.Request(ctx, "benchmark-plugin", "plugin.request", queryData, metadata)
			cancel()
		}
	})
}

func BenchmarkMockEventBusBroadcast(b *testing.B) {
	mockBus := NewMockEventBus()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			testData := &EventData{
				KeyValue: map[string]string{
					"benchmark": "test",
				},
			}

			metadata := NewEventMetadata("test", "benchmark.event")
			_ = mockBus.BroadcastWithMetadata("benchmark.event", testData, metadata)
		}
	})
}
