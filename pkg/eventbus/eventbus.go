package eventbus

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
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

	// Mutexes for thread safety
	chanMu sync.RWMutex
	subMu  sync.RWMutex
	plugMu sync.RWMutex

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
		channels:    make(map[string]chan *eventMessage),
		subscribers: make(map[string][]subscriberInfo),
		plugins:     make(map[string]framework.Plugin),
		bufferSizes: bufferSizes,
		ctx:         ctx,
		cancel:      cancel,
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
		log.Printf("[EventBus] WARNING: Dropped event %s - buffer full", eventType)
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
		log.Printf("[EventBus] WARNING: Dropped direct event to %s - buffer full", target)
		return nil
	}
}

// Query delegates SQL queries to the core plugin
func (eb *EventBus) Query(sql string, params ...interface{}) (framework.QueryResult, error) {
	// The EventBus is designed for asynchronous event-driven communication
	// Synchronous queries are not supported in the current architecture
	// Plugins should either:
	// 1. Use their own database connections for queries
	// 2. Use async patterns with request/response correlation
	// 3. Load data during Start() instead of Init()
	return nil, fmt.Errorf("synchronous queries not supported by EventBus - use async patterns or direct DB access")
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
	// For simplicity, we'll start one router per Subscribe call
	// In production, we'd track active routers

	eb.wg.Add(1)
	go func() {
		defer eb.wg.Done()
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
					if err := s.Handler(m.Event); err != nil {
						log.Printf("[EventBus] Handler error for %s: %v", eventType, err)
					}
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
