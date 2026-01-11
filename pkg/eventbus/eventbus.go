package eventbus

import (
	"context"
	"fmt"
	"github.com/hildolfr/daz/internal/logger"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// EventBus implements the framework.EventBus interface using priority queues
type EventBus struct {
	// Priority queues by event type (e.g., "cytube.event", "sql.request")
	queues map[string]*messageQueue

	// Subscribers by event type (exact matches)
	subscribers map[string][]subscriberInfo

	// Pattern subscribers (wildcard matches)
	patternSubscribers []subscriberInfo

	// Plugin registry for direct sends
	plugins map[string]framework.Plugin

	// Buffer size configuration
	bufferSizes map[string]int

	// Track active routers to prevent duplicates
	activeRouters map[string]bool
	routerMu      sync.RWMutex

	// Sync operation tracking
	pendingRequests map[string]chan *pluginResponse
	syncMu          sync.RWMutex

	// Mutexes for thread safety
	queueMu sync.RWMutex
	subMu   sync.RWMutex
	plugMu  sync.RWMutex

	// Metrics
	droppedEvents map[string]int64
	metricsMu     sync.RWMutex

	// Shutdown management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// eventMessage wraps an event with metadata
type eventMessage struct {
	Event    framework.Event
	Type     string
	Target   string // For direct sends
	Data     *framework.EventData
	Metadata *framework.EventMetadata // Priority and other metadata
}

// subscriberInfo holds subscriber details
type subscriberInfo struct {
	Handler framework.EventHandler
	Name    string   // For debugging
	Pattern string   // Event type pattern (e.g., "cytube.event.*")
	Tags    []string // Tags to filter on (empty means no filter)
}

// pluginResponse holds the response from a plugin request
type pluginResponse struct {
	data *framework.EventData
	err  error
}

// Config holds event bus configuration
type Config struct {
	BufferSizes map[string]int
}

// NewEventBus creates a new event bus instance
func NewEventBus(config *Config) *EventBus {
	ctx, cancel := context.WithCancel(context.Background())

	// Default buffer sizes
	bufferSizes := map[string]int{
		"cytube.event": 1000,
		"sql.":         100, // Default for all SQL operations
		"plugin.":      50,  // Default for all plugin operations
	}

	// Override with config values
	if config != nil && config.BufferSizes != nil {
		for k, v := range config.BufferSizes {
			bufferSizes[k] = v
		}
	}

	eb := &EventBus{
		queues:             make(map[string]*messageQueue),
		subscribers:        make(map[string][]subscriberInfo),
		patternSubscribers: make([]subscriberInfo, 0),
		plugins:            make(map[string]framework.Plugin),
		bufferSizes:        bufferSizes,
		activeRouters:      make(map[string]bool),
		pendingRequests:    make(map[string]chan *pluginResponse),
		droppedEvents:      make(map[string]int64),
		ctx:                ctx,
		cancel:             cancel,
	}

	// Initialize queues for known event types
	for eventType := range bufferSizes {
		eb.getOrCreateQueue(eventType)
	}

	return eb
}

// RegisterPlugin registers a plugin with the event bus
func (eb *EventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	eb.plugMu.Lock()
	defer eb.plugMu.Unlock()

	if _, exists := eb.plugins[name]; exists {
		return fmt.Errorf("plugin %s already registered", name)
	}

	eb.plugins[name] = plugin
	logger.Info("EventBus", "Registered plugin: %s", name)
	return nil
}

// Broadcast sends an event to all subscribers of the event type
func (eb *EventBus) Broadcast(eventType string, data *framework.EventData) error {
	var event framework.Event

	// Check if this is a raw event passthrough
	if data != nil && data.RawEvent != nil {
		// Use the raw event directly - this preserves events like PlaylistArrayEvent
		event = data.RawEvent
		// Debug logging removed for cleaner output
	} else {
		// Create a DataEvent to carry the EventData
		event = framework.NewDataEvent(eventType, data)
	}

	// Create metadata with default priority
	metadata := framework.NewEventMetadata("eventbus", eventType)

	msg := &eventMessage{
		Event:    event,
		Type:     eventType,
		Data:     data,
		Metadata: metadata,
	}

	// Get or create queue
	queue := eb.getOrCreateQueue(eventType)

	// Push with default priority (0)
	if !queue.push(msg, metadata.Priority) {
		// Track dropped event
		eb.metricsMu.Lock()
		eb.droppedEvents[eventType]++
		count := eb.droppedEvents[eventType]
		eb.metricsMu.Unlock()

		// Update Prometheus metrics
		metrics.EventsDropped.WithLabelValues(eventType).Inc()

		logger.Warn("EventBus", "Dropped event %s - queue closed (total dropped: %d)", eventType, count)
		return nil // Don't return error for dropped events
	}

	return nil
}

// Send sends an event to a specific target plugin
func (eb *EventBus) Send(target string, eventType string, data *framework.EventData) error {
	// Create a DataEvent to carry the EventData
	event := framework.NewDataEvent(eventType, data)

	// Create metadata with default priority
	metadata := framework.NewEventMetadata("eventbus", eventType).WithTarget(target)

	msg := &eventMessage{
		Event:    event,
		Type:     eventType,
		Target:   target,
		Data:     data,
		Metadata: metadata,
	}

	// Use plugin.request queue for direct sends
	queue := eb.getOrCreateQueue("plugin.request")

	// Push with default priority
	if !queue.push(msg, metadata.Priority) {
		// Track dropped event
		eb.metricsMu.Lock()
		eb.droppedEvents["plugin.request"]++
		count := eb.droppedEvents["plugin.request"]
		eb.metricsMu.Unlock()

		logger.Warn("EventBus", "Dropped direct event to %s - queue closed (total dropped: %d)", target, count)
		return nil
	}

	return nil
}

// Subscribe registers a handler for an event type
func (eb *EventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	return eb.SubscribeWithTags(eventType, handler, nil)
}

// SubscribeWithTags registers a handler for an event pattern with optional tag filtering
func (eb *EventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	eb.subMu.Lock()
	defer eb.subMu.Unlock()

	// Create subscriber info
	sub := subscriberInfo{
		Handler: handler,
		Pattern: pattern,
		Tags:    tags,
	}

	// Check if this is a wildcard pattern
	if strings.Contains(pattern, "*") {
		// Add to pattern subscribers
		sub.Name = fmt.Sprintf("pattern_handler_%d", len(eb.patternSubscribers))
		eb.patternSubscribers = append(eb.patternSubscribers, sub)
		logger.Debug("EventBus", "Subscribed to pattern %s with tags %v", pattern, tags)
	} else {
		// Add to exact match subscribers
		sub.Name = fmt.Sprintf("handler_%d", len(eb.subscribers[pattern]))
		eb.subscribers[pattern] = append(eb.subscribers[pattern], sub)

		// Ensure queue exists and start router if needed
		queue := eb.getOrCreateQueue(pattern)

		// Start router for this event type if not already running
		eb.startRouter(pattern, queue)

		logger.Debug("EventBus", "Subscribed to %s with tags %v", pattern, tags)
	}

	return nil
}

// Start begins event routing
func (eb *EventBus) Start() error {
	logger.Debug("EventBus", "Starting event routing")

	// Pre-start routers for critical event types
	criticalTypes := []string{
		"cytube.event",
		"sql.query.request",
		"sql.exec.request",
		"log.request",
		"plugin.request",
		"plugin.response",
	}

	for _, eventType := range criticalTypes {
		queue := eb.getOrCreateQueue(eventType)
		eb.startRouter(eventType, queue)
	}

	logger.Debug("EventBus", "Critical routers pre-started")
	return nil
}

// Stop gracefully shuts down the event bus
func (eb *EventBus) Stop() error {
	logger.Info("EventBus", "Stopping event bus")

	// Cancel context to signal shutdown
	eb.cancel()

	// Close all queues
	eb.queueMu.Lock()
	for _, queue := range eb.queues {
		queue.close()
	}
	eb.queueMu.Unlock()

	// Wait for routers to finish
	eb.wg.Wait()

	logger.Info("EventBus", "Event bus stopped")
	return nil
}

// getOrCreateQueue gets or creates a priority queue for an event type
func (eb *EventBus) getOrCreateQueue(eventType string) *messageQueue {
	eb.queueMu.Lock()
	defer eb.queueMu.Unlock()

	if queue, exists := eb.queues[eventType]; exists {
		return queue
	}

	queue := newMessageQueue()
	eb.queues[eventType] = queue
	return queue
}

// getBufferSize returns the buffer size for an event type
func (eb *EventBus) getBufferSize(eventType string) int {
	// Check specific size
	if size, ok := eb.bufferSizes[eventType]; ok {
		return size
	}

	// Check prefix patterns by looking for the longest matching prefix
	var longestPrefix string
	var prefixSize int

	for key, size := range eb.bufferSizes {
		if strings.HasPrefix(eventType, key) && len(key) > len(longestPrefix) {
			longestPrefix = key
			prefixSize = size
		}
	}

	if longestPrefix != "" {
		return prefixSize
	}

	// Default size
	return 100
}

// startRouter starts the event router for a specific event type
func (eb *EventBus) startRouter(eventType string, queue *messageQueue) {
	// Check if router already exists
	eb.routerMu.Lock()
	if eb.activeRouters[eventType] {
		eb.routerMu.Unlock()
		return // Router already exists, don't start another
	}
	eb.activeRouters[eventType] = true
	eb.routerMu.Unlock()

	eb.wg.Add(1)
	go func() {
		defer eb.wg.Done()
		defer func() {
			// Clean up when router stops
			eb.routerMu.Lock()
			delete(eb.activeRouters, eventType)
			eb.routerMu.Unlock()
		}()
		eb.routeEvents(eventType, queue)
	}()
}

// routeEvents routes events to subscribers
func (eb *EventBus) routeEvents(eventType string, queue *messageQueue) {
	logger.Debug("EventBus", "Router started for %s", eventType)

	for {
		// Check if we should stop
		select {
		case <-eb.ctx.Done():
			logger.Info("EventBus", "Router stopping for %s", eventType)
			return
		default:
		}

		// Pop message from queue (blocking)
		msg, ok := queue.pop()
		if !ok {
			logger.Info("EventBus", "Queue closed for %s", eventType)
			return
		}

		// Get all matching subscribers
		matching := eb.getMatchingSubscribers(eventType, msg)

		// Dispatch to each matching subscriber
		for _, sub := range matching {
			// Non-blocking dispatch
			go func(s subscriberInfo, m *eventMessage) {
				timer := prometheus.NewTimer(metrics.EventProcessingDuration.WithLabelValues(eventType))
				defer timer.ObserveDuration()

				if err := s.Handler(m.Event); err != nil {
					logger.Error("EventBus", "Handler error for %s: %v", eventType, err)
				}

				// Track successful processing
				metrics.EventsProcessed.WithLabelValues(eventType).Inc()
			}(sub, msg)
		}
	}
}

// getMatchingSubscribers returns all subscribers that match the event type and tags
func (eb *EventBus) getMatchingSubscribers(eventType string, msg *eventMessage) []subscriberInfo {
	eb.subMu.RLock()
	defer eb.subMu.RUnlock()

	var matching []subscriberInfo

	// Add exact match subscribers
	for _, sub := range eb.subscribers[eventType] {
		if eb.matchesTags(sub.Tags, msg.Metadata) {
			matching = append(matching, sub)
		}
	}

	// Add pattern match subscribers
	for _, sub := range eb.patternSubscribers {
		matched, err := path.Match(sub.Pattern, eventType)
		if err != nil {
			logger.Error("EventBus", "Invalid pattern %s: %v", sub.Pattern, err)
			continue
		}
		if matched && eb.matchesTags(sub.Tags, msg.Metadata) {
			matching = append(matching, sub)
		}
	}

	return matching
}

// matchesTags checks if the subscription tags match the event metadata tags
func (eb *EventBus) matchesTags(subTags []string, metadata *framework.EventMetadata) bool {
	// No tag filter means match all
	if len(subTags) == 0 {
		return true
	}

	// No metadata means no tags to match
	if metadata == nil || len(metadata.Tags) == 0 {
		return false
	}

	// Check if all subscription tags are present in event tags
	for _, subTag := range subTags {
		found := false
		for _, eventTag := range metadata.Tags {
			if subTag == eventTag {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// generateID generates a unique ID for requests
func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// GetDroppedEventCounts returns a copy of the dropped event counts
func (eb *EventBus) GetDroppedEventCounts() map[string]int64 {
	eb.metricsMu.RLock()
	defer eb.metricsMu.RUnlock()

	// Create a copy to avoid race conditions
	counts := make(map[string]int64)
	for k, v := range eb.droppedEvents {
		counts[k] = v
	}
	return counts
}

// GetDroppedEventCount returns the dropped event count for a specific event type
func (eb *EventBus) GetDroppedEventCount(eventType string) int64 {
	eb.metricsMu.RLock()
	defer eb.metricsMu.RUnlock()
	return eb.droppedEvents[eventType]
}

// BroadcastWithMetadata broadcasts an event with metadata
func (eb *EventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	var event framework.Event

	// Check if this is a raw event passthrough
	if data != nil && data.RawEvent != nil {
		// Use the raw event directly
		event = data.RawEvent
		// Debug logging removed for cleaner output
	} else {
		// Create enhanced event data
		enhanced := &framework.EnhancedEventData{
			EventData: data,
			Metadata:  metadata,
		}

		// Wrap in DataEvent for compatibility
		event = &framework.DataEvent{
			EventType: eventType,
			EventTime: metadata.Timestamp,
			Data:      enhanced.EventData,
		}
	}

	// Create event message with metadata
	msg := &eventMessage{
		Event:    event,
		Type:     eventType,
		Data:     data,
		Metadata: metadata,
	}

	// Check if there are potential subscribers (exact or pattern)
	eb.subMu.RLock()
	hasExactSubscribers := len(eb.subscribers[eventType]) > 0
	hasPatternSubscribers := len(eb.patternSubscribers) > 0
	eb.subMu.RUnlock()

	// If no subscribers at all, return early
	if !hasExactSubscribers && !hasPatternSubscribers {
		return nil
	}

	queue := eb.getOrCreateQueue(eventType)

	// Start router if needed
	eb.startRouter(eventType, queue)

	// Push with priority from metadata
	priority := 0
	if metadata != nil {
		priority = metadata.Priority
	}

	if !queue.push(msg, priority) {
		eb.metricsMu.Lock()
		eb.droppedEvents[eventType]++
		count := eb.droppedEvents[eventType]
		eb.metricsMu.Unlock()

		logger.Warn("EventBus", "Dropped event %s - queue closed (total dropped: %d)", eventType, count)
		return nil
	}

	return nil
}

// SendWithMetadata sends an event directly to a plugin with metadata
func (eb *EventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	if metadata == nil {
		metadata = &framework.EventMetadata{}
	}

	// Update metadata with target
	metadata.Target = target

	// Use BroadcastWithMetadata but route to plugin.request channel
	return eb.BroadcastWithMetadata("plugin.request", data, metadata)
}

// Request performs a synchronous request to a plugin
func (eb *EventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	if metadata == nil {
		metadata = &framework.EventMetadata{}
	}

	// Use existing correlation ID if present, otherwise generate new one
	correlationID := metadata.CorrelationID
	if correlationID == "" {
		correlationID = generateID()
		metadata.CorrelationID = correlationID
	}
	metadata.Target = target
	metadata.ReplyTo = "eventbus"

	// log.Printf("[EventBus.Request] Starting request: target=%s, eventType=%s, correlationID=%s", target, eventType, correlationID)

	// Create response channel with buffer to prevent blocking
	respCh := make(chan *pluginResponse, 1)

	// Register pending request
	eb.syncMu.Lock()
	eb.pendingRequests[correlationID] = respCh
	// pendingCount := len(eb.pendingRequests)
	eb.syncMu.Unlock()

	// log.Printf("[EventBus.Request] Registered pending request: correlationID=%s, pending requests count=%d", correlationID, pendingCount)

	// Enhanced cleanup function with context cancellation awareness
	cleanup := func() {
		// log.Printf("[EventBus.Request] Cleanup called for correlationID=%s", correlationID)
		eb.syncMu.Lock()
		delete(eb.pendingRequests, correlationID)
		eb.syncMu.Unlock()

		// Close channel safely (check if not already closed)
		select {
		case <-respCh:
			// Channel had pending data, drain it
			// log.Printf("[EventBus.Request] Drained pending data from response channel: correlationID=%s", correlationID)
		default:
			// Channel is empty
		}
		close(respCh)
	}
	defer cleanup()

	// Send request with metadata
	// log.Printf("[EventBus.Request] Sending request to %s: correlationID=%s", target, correlationID)
	if err := eb.SendWithMetadata(target, eventType, data, metadata); err != nil {
		logger.Error("EventBus", "Failed to send request: correlationID=%s, error=%v", correlationID, err)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	// log.Printf("[EventBus.Request] Request sent successfully: correlationID=%s", correlationID)

	// Wait for response with enhanced timeout handling
	// log.Printf("[EventBus.Request] Waiting for response: correlationID=%s", correlationID)
	select {
	case resp := <-respCh:
		// log.Printf("[EventBus.Request] Received response: correlationID=%s, hasData=%v, hasError=%v",
		// 	correlationID, resp != nil && resp.data != nil, resp != nil && resp.err != nil)
		if resp == nil {
			// log.Printf("[EventBus.Request] Received nil response: correlationID=%s", correlationID)
			return nil, fmt.Errorf("received nil response for correlation ID %s", correlationID)
		}
		return resp.data, resp.err
	case <-ctx.Done():
		// Context cancelled - determine the specific cause
		// log.Printf("[EventBus.Request] Context done while waiting for response: correlationID=%s, err=%v", correlationID, ctx.Err())
		switch ctx.Err() {
		case context.DeadlineExceeded:
			logger.Error("EventBus", "Request timed out: correlationID=%s, target=%s", correlationID, target)
			return nil, fmt.Errorf("request to %s timed out: %w", target, ctx.Err())
		case context.Canceled:
			logger.Warn("EventBus", "Request cancelled: correlationID=%s, target=%s", correlationID, target)
			return nil, fmt.Errorf("request to %s was cancelled: %w", target, ctx.Err())
		default:
			logger.Error("EventBus", "Request failed: correlationID=%s, target=%s, err=%v", correlationID, target, ctx.Err())
			return nil, fmt.Errorf("request to %s failed: %w", target, ctx.Err())
		}
	}
}

// DeliverResponse delivers a response to a waiting request
func (eb *EventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
	// log.Printf("[EventBus.DeliverResponse] Called: correlationID=%s, hasResponse=%v, hasError=%v",
	// 	correlationID, response != nil, err != nil)

	eb.syncMu.RLock()
	respCh, exists := eb.pendingRequests[correlationID]
	// pendingCount := len(eb.pendingRequests)
	eb.syncMu.RUnlock()

	// log.Printf("[EventBus.DeliverResponse] Pending request exists: correlationID=%s, exists=%v, pending count=%d",
	// 	correlationID, exists, pendingCount)

	if exists {
		// log.Printf("[EventBus.DeliverResponse] Attempting to send response: correlationID=%s", correlationID)
		select {
		case respCh <- &pluginResponse{data: response, err: err}:
			// Delivered successfully
			// log.Printf("[EventBus.DeliverResponse] Response delivered successfully: correlationID=%s", correlationID)
		default:
			// Channel full or closed, log error
			logger.Error("EventBus", "Failed to deliver response (channel full/closed): correlationID=%s", correlationID)
		}
	}
}

// UnregisterPlugin unregisters a plugin from the event bus
func (eb *EventBus) UnregisterPlugin(name string) error {
	eb.plugMu.Lock()
	defer eb.plugMu.Unlock()

	if _, exists := eb.plugins[name]; !exists {
		return fmt.Errorf("plugin %s not found", name)
	}

	delete(eb.plugins, name)
	logger.Info("EventBus", "Unregistered plugin: %s", name)
	return nil
}
