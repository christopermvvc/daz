package eventbus

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// EventBus implements the framework.EventBus interface using Go channels
type EventBus struct {
	// Event channels by type (e.g., "cytube.event", "sql.request")
	channels map[string]chan *eventMessage

	// Subscribers by event type
	subscribers map[string][]subscriberInfo

	// Plugin registry for direct sends
	plugins map[string]framework.Plugin

	// SQL handler references (set by core plugin)
	sqlQueryHandler framework.EventHandler
	sqlExecHandler  framework.EventHandler

	// Buffer size configuration
	bufferSizes map[string]int

	// Track active routers to prevent duplicates
	activeRouters map[string]bool
	routerMu      sync.RWMutex

	// Sync operation tracking
	pendingQueries map[string]chan *queryResponse
	pendingExecs   map[string]chan *execResponse
	syncMu         sync.RWMutex

	// Mutexes for thread safety
	chanMu sync.RWMutex
	subMu  sync.RWMutex
	plugMu sync.RWMutex

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
	Event  framework.Event
	Type   string
	Target string // For direct sends
	Data   *framework.EventData
}

// subscriberInfo holds subscriber details
type subscriberInfo struct {
	Handler framework.EventHandler
	Name    string // For debugging
}

// queryResponse holds the response from a synchronous query
type queryResponse struct {
	rows *sql.Rows
	err  error
}

// execResponse holds the response from a synchronous exec
type execResponse struct {
	result sql.Result
	err    error
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
		channels:       make(map[string]chan *eventMessage),
		subscribers:    make(map[string][]subscriberInfo),
		plugins:        make(map[string]framework.Plugin),
		bufferSizes:    bufferSizes,
		activeRouters:  make(map[string]bool),
		pendingQueries: make(map[string]chan *queryResponse),
		pendingExecs:   make(map[string]chan *execResponse),
		droppedEvents:  make(map[string]int64),
		ctx:            ctx,
		cancel:         cancel,
	}

	// Initialize channels for known event types
	for eventType, size := range bufferSizes {
		eb.getOrCreateChannel(eventType, size)
	}

	return eb
}

// RegisterPlugin registers a plugin with the event bus
func (eb *EventBus) RegisterPlugin(name string, plugin framework.Plugin) {
	eb.plugMu.Lock()
	defer eb.plugMu.Unlock()

	eb.plugins[name] = plugin
	log.Printf("[EventBus] Registered plugin: %s", name)
}

// SetSQLHandlers sets the SQL query and exec handlers (called by core plugin)
func (eb *EventBus) SetSQLHandlers(queryHandler, execHandler framework.EventHandler) {
	eb.sqlQueryHandler = queryHandler
	eb.sqlExecHandler = execHandler
}

// Broadcast sends an event to all subscribers of the event type
func (eb *EventBus) Broadcast(eventType string, data *framework.EventData) error {
	// Create a DataEvent to carry the EventData
	event := framework.NewDataEvent(eventType, data)

	msg := &eventMessage{
		Event: event,
		Type:  eventType,
		Data:  data,
	}

	// Get or create channel
	ch := eb.getOrCreateChannel(eventType, eb.getBufferSize(eventType))

	// Non-blocking send - drop if buffer full
	select {
	case ch <- msg:
		return nil
	default:
		// Track dropped event
		eb.metricsMu.Lock()
		eb.droppedEvents[eventType]++
		count := eb.droppedEvents[eventType]
		eb.metricsMu.Unlock()

		// Update Prometheus metrics
		metrics.EventsDropped.WithLabelValues(eventType).Inc()

		log.Printf("[EventBus] WARNING: Dropped event %s - buffer full (total dropped: %d)", eventType, count)
		return nil // Don't return error for dropped events
	}
}

// Send sends an event to a specific target plugin
func (eb *EventBus) Send(target string, eventType string, data *framework.EventData) error {
	// Create a DataEvent to carry the EventData
	event := framework.NewDataEvent(eventType, data)

	msg := &eventMessage{
		Event:  event,
		Type:   eventType,
		Target: target,
		Data:   data,
	}

	// Use plugin.request channel for direct sends
	ch := eb.getOrCreateChannel("plugin.request", eb.getBufferSize("plugin.request"))

	// Non-blocking send
	select {
	case ch <- msg:
		return nil
	default:
		// Track dropped event
		eb.metricsMu.Lock()
		eb.droppedEvents["plugin.request"]++
		count := eb.droppedEvents["plugin.request"]
		eb.metricsMu.Unlock()

		log.Printf("[EventBus] WARNING: Dropped direct event to %s - buffer full (total dropped: %d)", target, count)
		return nil
	}
}

// Query delegates SQL queries to the core plugin
func (eb *EventBus) Query(sql string, params ...interface{}) (framework.QueryResult, error) {
	// Delegate to QuerySync with a 30-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rows, err := eb.QuerySync(ctx, sql, params...)
	if err != nil {
		return nil, err
	}

	return &sqlRowsAdapter{rows: rows}, nil
}

// Exec delegates SQL exec operations to the core plugin
func (eb *EventBus) Exec(sql string, params ...interface{}) error {
	if eb.sqlExecHandler == nil {
		return fmt.Errorf("SQL exec handler not configured")
	}

	// Create SQL request
	request := &framework.SQLRequest{
		ID:        generateID(),
		Query:     sql,
		Params:    params,
		Timeout:   30 * time.Second,
		RequestBy: "unknown",
	}

	// Create event data
	data := &framework.EventData{
		SQLRequest: request,
	}

	// Broadcast as sql.exec event
	return eb.Broadcast("sql.exec", data)
}

// QuerySync performs a synchronous SQL query with context support
func (eb *EventBus) QuerySync(ctx context.Context, sql string, params ...interface{}) (*sql.Rows, error) {
	// Generate correlation ID
	correlationID := generateID()

	// Create response channel
	respCh := make(chan *queryResponse, 1)

	// Register pending query
	eb.syncMu.Lock()
	eb.pendingQueries[correlationID] = respCh
	eb.syncMu.Unlock()

	// Clean up on exit
	defer func() {
		eb.syncMu.Lock()
		delete(eb.pendingQueries, correlationID)
		eb.syncMu.Unlock()
		close(respCh)
	}()

	// Create sync request
	request := &framework.SQLRequest{
		ID:         correlationID,
		Query:      sql,
		Params:     params,
		IsSync:     true,
		ResponseCh: correlationID,
		RequestBy:  "sync_query",
	}

	// Create event data
	data := &framework.EventData{
		SQLRequest: request,
	}

	// Send request
	if err := eb.Broadcast("sql.query", data); err != nil {
		return nil, fmt.Errorf("failed to broadcast query: %w", err)
	}

	// Wait for response with timeout
	select {
	case resp := <-respCh:
		return resp.rows, resp.err
	case <-ctx.Done():
		return nil, fmt.Errorf("query timeout: %w", ctx.Err())
	}
}

// ExecSync performs a synchronous SQL exec with context support
func (eb *EventBus) ExecSync(ctx context.Context, sql string, params ...interface{}) (sql.Result, error) {
	// Generate correlation ID
	correlationID := generateID()

	// Create response channel
	respCh := make(chan *execResponse, 1)

	// Register pending exec
	eb.syncMu.Lock()
	eb.pendingExecs[correlationID] = respCh
	eb.syncMu.Unlock()

	// Clean up on exit
	defer func() {
		eb.syncMu.Lock()
		delete(eb.pendingExecs, correlationID)
		eb.syncMu.Unlock()
		close(respCh)
	}()

	// Create sync request
	request := &framework.SQLRequest{
		ID:         correlationID,
		Query:      sql,
		Params:     params,
		IsSync:     true,
		ResponseCh: correlationID,
		RequestBy:  "sync_exec",
	}

	// Create event data
	data := &framework.EventData{
		SQLRequest: request,
	}

	// Send request
	if err := eb.Broadcast("sql.exec", data); err != nil {
		return nil, fmt.Errorf("failed to broadcast exec: %w", err)
	}

	// Wait for response with timeout
	select {
	case resp := <-respCh:
		return resp.result, resp.err
	case <-ctx.Done():
		return nil, fmt.Errorf("exec timeout: %w", ctx.Err())
	}
}

// Subscribe registers a handler for an event type
func (eb *EventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	eb.subMu.Lock()
	defer eb.subMu.Unlock()

	// Add subscriber
	sub := subscriberInfo{
		Handler: handler,
		Name:    fmt.Sprintf("handler_%d", len(eb.subscribers[eventType])),
	}

	eb.subscribers[eventType] = append(eb.subscribers[eventType], sub)

	// Ensure channel exists and start router if needed
	ch := eb.getOrCreateChannel(eventType, eb.getBufferSize(eventType))

	// Start router for this event type if not already running
	eb.startRouter(eventType, ch)

	log.Printf("[EventBus] Subscribed to %s", eventType)
	return nil
}

// Start begins event routing
func (eb *EventBus) Start() error {
	log.Println("[EventBus] Starting event routing")

	// Routers are started on-demand when subscribers are added
	return nil
}

// Stop gracefully shuts down the event bus
func (eb *EventBus) Stop() error {
	log.Println("[EventBus] Stopping event bus")

	// Cancel context to signal shutdown
	eb.cancel()

	// Close all channels
	eb.chanMu.Lock()
	for _, ch := range eb.channels {
		close(ch)
	}
	eb.chanMu.Unlock()

	// Wait for routers to finish
	eb.wg.Wait()

	log.Println("[EventBus] Event bus stopped")
	return nil
}

// getOrCreateChannel gets or creates a channel for an event type
func (eb *EventBus) getOrCreateChannel(eventType string, bufferSize int) chan *eventMessage {
	eb.chanMu.Lock()
	defer eb.chanMu.Unlock()

	if ch, exists := eb.channels[eventType]; exists {
		return ch
	}

	ch := make(chan *eventMessage, bufferSize)
	eb.channels[eventType] = ch
	return ch
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
func (eb *EventBus) startRouter(eventType string, ch chan *eventMessage) {
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
		eb.routeEvents(eventType, ch)
	}()
}

// routeEvents routes events to subscribers
func (eb *EventBus) routeEvents(eventType string, ch chan *eventMessage) {
	log.Printf("[EventBus] Router started for %s", eventType)

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				log.Printf("[EventBus] Channel closed for %s", eventType)
				return
			}

			// Get subscribers
			eb.subMu.RLock()
			subscribers := eb.subscribers[eventType]
			eb.subMu.RUnlock()

			// Dispatch to each subscriber
			for _, sub := range subscribers {
				// Non-blocking dispatch
				go func(s subscriberInfo, m *eventMessage) {
					timer := prometheus.NewTimer(metrics.EventProcessingDuration.WithLabelValues(eventType))
					defer timer.ObserveDuration()

					if err := s.Handler(m.Event); err != nil {
						log.Printf("[EventBus] Handler error for %s: %v", eventType, err)
					}

					// Track successful processing
					metrics.EventsProcessed.WithLabelValues(eventType).Inc()
				}(sub, msg)
			}

		case <-eb.ctx.Done():
			log.Printf("[EventBus] Router stopping for %s", eventType)
			return
		}
	}
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

// DeliverQueryResponse delivers a query response to a waiting sync operation
func (eb *EventBus) DeliverQueryResponse(correlationID string, rows *sql.Rows, err error) {
	eb.syncMu.RLock()
	respCh, exists := eb.pendingQueries[correlationID]
	eb.syncMu.RUnlock()

	if exists {
		select {
		case respCh <- &queryResponse{rows: rows, err: err}:
			// Delivered successfully
		default:
			// Channel full or closed, log error
			log.Printf("[EventBus] Failed to deliver query response for %s", correlationID)
		}
	}
}

// DeliverExecResponse delivers an exec response to a waiting sync operation
func (eb *EventBus) DeliverExecResponse(correlationID string, result sql.Result, err error) {
	eb.syncMu.RLock()
	respCh, exists := eb.pendingExecs[correlationID]
	eb.syncMu.RUnlock()

	if exists {
		select {
		case respCh <- &execResponse{result: result, err: err}:
			// Delivered successfully
		default:
			// Channel full or closed, log error
			log.Printf("[EventBus] Failed to deliver exec response for %s", correlationID)
		}
	}
}

// sqlRowsAdapter adapts *sql.Rows to framework.QueryResult
type sqlRowsAdapter struct {
	rows *sql.Rows
}

func (a *sqlRowsAdapter) Next() bool {
	return a.rows.Next()
}

func (a *sqlRowsAdapter) Scan(dest ...interface{}) error {
	return a.rows.Scan(dest...)
}

func (a *sqlRowsAdapter) Close() error {
	return a.rows.Close()
}

func (a *sqlRowsAdapter) Columns() ([]string, error) {
	return a.rows.Columns()
}
