package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/metrics"
	"github.com/hildolfr/daz/pkg/cytube"
	"github.com/hildolfr/daz/pkg/eventbus"
)

// Plugin implements the core plugin functionality
type Plugin struct {
	config           *Config
	eventBus         framework.EventBus
	cytubeConn       *cytube.WebSocketClient
	sqlModule        *SQLModule
	mu               sync.RWMutex
	ctx              context.Context
	cancel           context.CancelFunc
	eventChan        chan framework.Event
	status           framework.PluginStatus
	startTime        time.Time
	reconnectAttempt int
	lastReconnect    time.Time
	eventHandlers    map[string]eventHandler
}

// eventHandler is a function that processes a specific event type
type eventHandler func(event framework.Event) (string, *framework.EventData, bool)

// NewPlugin creates a new instance of the core plugin
func NewPlugin(config *Config) *Plugin {
	if config == nil {
		config = &Config{}
	}
	config.SetDefaults()

	ctx, cancel := context.WithCancel(context.Background())
	return &Plugin{
		config: config,
		ctx:    ctx,
		cancel: cancel,
		status: framework.PluginStatus{
			Name:  "core",
			State: "initialized",
		},
	}
}

// New creates a new plugin instance implementing the framework.Plugin interface
func New() framework.Plugin {
	return &Plugin{
		status: framework.PluginStatus{
			Name:  "core",
			State: "initialized",
		},
	}
}

// Init initializes the plugin with configuration and event bus
func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Parse configuration if provided
	if len(config) > 0 {
		var cfg Config
		if err := json.Unmarshal(config, &cfg); err != nil {
			p.status.LastError = fmt.Errorf("failed to parse config: %w", err)
			return p.status.LastError
		}
		p.config = &cfg
	} else {
		p.config = &Config{}
	}
	p.config.SetDefaults()

	// Initialize context if not already done
	if p.ctx == nil {
		p.ctx, p.cancel = context.WithCancel(context.Background())
	}

	// Store event bus reference
	p.eventBus = bus

	// Initialize SQL module
	sqlModule, err := NewSQLModule(p.config.Database, bus)
	if err != nil {
		p.status.LastError = fmt.Errorf("failed to initialize SQL module: %w", err)
		return p.status.LastError
	}
	p.sqlModule = sqlModule

	// Create event channel for Cytube events
	p.eventChan = make(chan framework.Event, 100)

	// Initialize Cytube client
	cytubeClient, err := cytube.NewWebSocketClient(p.config.Cytube.Channel, p.eventChan)
	if err != nil {
		p.status.LastError = fmt.Errorf("failed to create Cytube client: %w", err)
		return p.status.LastError
	}
	p.cytubeConn = cytubeClient

	// Initialize event handlers
	p.initEventHandlers()

	// Set up Cytube event handlers
	p.setupCytubeHandlers()

	// Set up connection monitoring
	p.setupConnectionMonitor()

	// Subscribe to SQL events
	if err := p.eventBus.Subscribe("sql.exec", p.handleSQLExec); err != nil {
		p.status.LastError = fmt.Errorf("failed to subscribe to SQL exec events: %w", err)
		return p.status.LastError
	}

	if err := p.eventBus.Subscribe("sql.query", p.handleSQLQuery); err != nil {
		p.status.LastError = fmt.Errorf("failed to subscribe to SQL query events: %w", err)
		return p.status.LastError
	}

	// Subscribe to chat message sending
	if err := p.eventBus.Subscribe("cytube.send", p.handleCytubeSend); err != nil {
		p.status.LastError = fmt.Errorf("failed to subscribe to cytube send events: %w", err)
		return p.status.LastError
	}

	// Register ourselves as the SQL handler for the EventBus
	p.eventBus.SetSQLHandlers(p.handleSQLQuery, p.handleSQLExec)

	return nil
}

// Initialize sets up the plugin with the event bus (deprecated, use Init)
func (p *Plugin) Initialize(eventBus framework.EventBus) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.eventBus = eventBus

	// Initialize SQL module
	sqlModule, err := NewSQLModule(p.config.Database, eventBus)
	if err != nil {
		return fmt.Errorf("failed to initialize SQL module: %w", err)
	}
	p.sqlModule = sqlModule

	// Create event channel for Cytube events
	p.eventChan = make(chan framework.Event, 100)

	// Initialize Cytube client
	cytubeClient, err := cytube.NewWebSocketClient(p.config.Cytube.Channel, p.eventChan)
	if err != nil {
		return fmt.Errorf("failed to create Cytube client: %w", err)
	}
	p.cytubeConn = cytubeClient

	// Initialize event handlers
	p.initEventHandlers()

	// Set up Cytube event handlers
	p.setupCytubeHandlers()

	// Subscribe to SQL events
	if err := p.eventBus.Subscribe("sql.exec", p.handleSQLExec); err != nil {
		return fmt.Errorf("failed to subscribe to SQL exec events: %w", err)
	}

	if err := p.eventBus.Subscribe("sql.query", p.handleSQLQuery); err != nil {
		return fmt.Errorf("failed to subscribe to SQL query events: %w", err)
	}

	// Subscribe to chat message sending
	if err := p.eventBus.Subscribe("cytube.send", p.handleCytubeSend); err != nil {
		return fmt.Errorf("failed to subscribe to cytube send events: %w", err)
	}

	// Register ourselves as the SQL handler for the EventBus
	p.eventBus.SetSQLHandlers(p.handleSQLQuery, p.handleSQLExec)

	return nil
}

// Start begins the plugin operation
func (p *Plugin) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Start SQL module
	if err := p.sqlModule.Start(); err != nil {
		p.status.LastError = fmt.Errorf("failed to start SQL module: %w", err)
		return p.status.LastError
	}

	// Update status
	p.status.State = "running"
	p.startTime = time.Now()

	// Connect to Cytube with retry logic
	if err := p.connectWithRetry(); err != nil {
		p.status.LastError = fmt.Errorf("failed to connect to Cytube after retries: %w", err)
		return p.status.LastError
	}

	// Login if credentials are provided
	if p.config.Cytube.Username != "" && p.config.Cytube.Password != "" {
		log.Printf("[Core] Logging in as %s...", p.config.Cytube.Username)
		if err := p.cytubeConn.Login(p.config.Cytube.Username, p.config.Cytube.Password); err != nil {
			log.Printf("[Core] Login failed: %v", err)
		} else {
			log.Printf("[Core] Login successful!")
		}
	}

	log.Printf("[Core] Plugin started successfully")
	return nil
}

// Stop gracefully shuts down the plugin
func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Update status
	p.status.State = "stopped"

	// Cancel context to stop all operations
	p.cancel()

	// Disconnect from Cytube
	if p.cytubeConn != nil {
		if err := p.cytubeConn.Disconnect(); err != nil {
			log.Printf("[Core] Error disconnecting from Cytube: %v", err)
		}
	}

	// Stop SQL module
	if p.sqlModule != nil {
		if err := p.sqlModule.Stop(); err != nil {
			log.Printf("[Core] Error stopping SQL module: %v", err)
		}
	}

	log.Printf("[Core] Plugin stopped")
	return nil
}

// HandleEvent processes incoming events
func (p *Plugin) HandleEvent(event framework.Event) error {
	p.mu.Lock()
	p.status.EventsHandled++
	p.mu.Unlock()

	// Core plugin primarily publishes events, doesn't handle many
	return nil
}

// handleSQLExec handles SQL exec requests from other plugins
func (p *Plugin) handleSQLExec(event framework.Event) error {
	// Check if this is a DataEvent which carries EventData
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		return nil
	}

	if dataEvent.Data == nil || dataEvent.Data.SQLRequest == nil {
		return nil
	}

	req := dataEvent.Data.SQLRequest

	// Check if this is a synchronous exec
	if req.IsSync && req.ResponseCh != "" {
		// Execute the query
		err := p.sqlModule.HandleSQLExec(p.ctx, *req)

		// Deliver the response through the EventBus
		if eventBus, ok := p.eventBus.(interface {
			DeliverExecResponse(string, sql.Result, error)
		}); ok {
			// For now, we'll return an error since we can't pass the result directly
			// This needs to be enhanced to serialize the exec result
			eventBus.DeliverExecResponse(req.ID, nil, fmt.Errorf("sync exec requires result serialization"))
		}
		return err
	}

	// For async exec, just execute and return
	return p.sqlModule.HandleSQLExec(p.ctx, *req)
}

// handleSQLQuery handles SQL query requests from other plugins
func (p *Plugin) handleSQLQuery(event framework.Event) error {
	// Check if this is a DataEvent which carries EventData
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		return nil
	}

	if dataEvent.Data == nil || dataEvent.Data.SQLRequest == nil {
		return nil
	}

	req := dataEvent.Data.SQLRequest

	// Check if this is a synchronous query
	if req.IsSync && req.ResponseCh != "" {
		// For sync queries, we need to use the standard database/sql connection
		// because the EventBus expects *sql.Rows, not pgx.Rows

		// Get the database connection from sql module
		db := p.sqlModule.GetDB()
		if db == nil {
			if eventBus, ok := p.eventBus.(interface {
				DeliverQueryResponse(string, *sql.Rows, error)
			}); ok {
				eventBus.DeliverQueryResponse(req.ID, nil, fmt.Errorf("database connection not available"))
			}
			return fmt.Errorf("database connection not available")
		}

		// Execute the query using standard database/sql
		rows, err := db.QueryContext(p.ctx, req.Query, req.Params...)

		// Deliver the response through the EventBus
		if eventBus, ok := p.eventBus.(interface {
			DeliverQueryResponse(string, *sql.Rows, error)
		}); ok {
			eventBus.DeliverQueryResponse(req.ID, rows, err)
		}

		return nil
	}

	// For async queries, just execute and return
	_, err := p.sqlModule.HandleSQLRequest(p.ctx, *req)
	return err
}

// handleCytubeSend handles requests to send messages to Cytube
func (p *Plugin) handleCytubeSend(event framework.Event) error {
	log.Printf("[Core] Received cytube.send event")

	// Check if this is a DataEvent which carries EventData
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		log.Printf("[Core] Event is not DataEvent, got %T", event)
		return nil
	}

	if dataEvent.Data == nil || dataEvent.Data.RawMessage == nil {
		log.Printf("[Core] No RawMessage in event data")
		return nil
	}

	// Send chat message to Cytube
	msg := dataEvent.Data.RawMessage.Message
	log.Printf("[Core] Sending message to Cytube: %d chars", len(msg))

	msgData := map[string]string{
		"msg": msg,
	}

	// Check if we have a connection
	if p.cytubeConn == nil {
		return fmt.Errorf("not connected to Cytube")
	}

	err := p.cytubeConn.Send("chatMsg", msgData)
	if err != nil {
		log.Printf("[Core] Failed to send message: %v", err)
		return err
	}

	// Track message sent
	metrics.CytubeMessagesSent.Inc()

	log.Printf("[Core] Message sent successfully")
	return nil
}

// initEventHandlers initializes the map of event handlers
func (p *Plugin) initEventHandlers() {
	p.eventHandlers = map[string]eventHandler{
		"*framework.ChatMessageEvent": p.handleChatMessage,
		"*framework.UserJoinEvent":    p.handleUserJoin,
		"*framework.UserLeaveEvent":   p.handleUserLeave,
		"*framework.VideoChangeEvent": p.handleVideoChange,
	}
}

// Event handler methods
func (p *Plugin) handleChatMessage(event framework.Event) (string, *framework.EventData, bool) {
	e := event.(*framework.ChatMessageEvent)
	eventData := &framework.EventData{
		ChatMessage: &framework.ChatMessageData{
			Username: e.Username,
			Message:  e.Message,
			UserRank: e.UserRank,
			UserID:   e.UserID,
			Channel:  e.ChannelName,
		},
	}
	return eventbus.EventCytubeChatMsg, eventData, true
}

func (p *Plugin) handleUserJoin(event framework.Event) (string, *framework.EventData, bool) {
	e := event.(*framework.UserJoinEvent)
	eventData := &framework.EventData{
		UserJoin: &framework.UserJoinData{
			Username: e.Username,
			UserRank: e.UserRank,
			Channel:  e.ChannelName,
		},
	}
	return eventbus.EventCytubeUserJoin, eventData, true
}

func (p *Plugin) handleUserLeave(event framework.Event) (string, *framework.EventData, bool) {
	e := event.(*framework.UserLeaveEvent)
	eventData := &framework.EventData{
		UserLeave: &framework.UserLeaveData{
			Username: e.Username,
			Channel:  e.ChannelName,
		},
	}
	return eventbus.EventCytubeUserLeave, eventData, true
}

func (p *Plugin) handleVideoChange(event framework.Event) (string, *framework.EventData, bool) {
	e := event.(*framework.VideoChangeEvent)
	eventData := &framework.EventData{
		VideoChange: &framework.VideoChangeData{
			VideoID:   e.VideoID,
			VideoType: e.VideoType,
			Duration:  e.Duration,
			Title:     e.Title,
		},
	}
	return eventbus.EventCytubeVideoChange, eventData, true
}

// setupCytubeHandlers starts a goroutine to process Cytube events
func (p *Plugin) setupCytubeHandlers() {
	// Define known event types that should be broadcast
	knownEventTypes := map[string]bool{
		"chatMsg":           true,
		"userJoin":          true,
		"userLeave":         true,
		"videoChange":       true,
		"userlist":          true,
		"setUserRank":       true,
		"rank":              true,
		"login":             true,
		"addUser":           true,
		"setPlaylistMeta":   true,
		"playlist":          true,
		"setCurrent":        true,
		"usercount":         true,
		"setUserMeta":       true,
		"setAFK":            true,
		"setPermissions":    true,
		"setPlaylistLocked": true,
		"emoteList":         true,
		"drinkCount":        true,
		"channelCSSJS":      true,
		"setMotd":           true,
		"channelOpts":       true,
	}

	go func() {
		for {
			select {
			case event := <-p.eventChan:
				// Increment event counter
				p.mu.Lock()
				p.status.EventsHandled++
				p.mu.Unlock()

				// Track message received
				metrics.CytubeMessagesReceived.Inc()

				// Log event to database
				if err := p.sqlModule.LogEvent(p.ctx, event); err != nil {
					log.Printf("[Core] Error logging event %s: %v", event.Type(), err)
					p.mu.Lock()
					p.status.LastError = fmt.Errorf("SQL log error: %w", err)
					p.mu.Unlock()
				}

				// Process event based on type
				var eventType string
				var eventData *framework.EventData
				var shouldBroadcast bool

				// Check if we have a specific handler for this event type
				eventTypeName := fmt.Sprintf("%T", event)
				if handler, ok := p.eventHandlers[eventTypeName]; ok {
					eventType, eventData, shouldBroadcast = handler(event)
				} else {
					// For other events, check if they're in the known list
					eventType = event.Type()
					eventData = &framework.EventData{}
					shouldBroadcast = knownEventTypes[eventType]

					if !shouldBroadcast && eventType != "mediaUpdate" {
						// Log unknown events except mediaUpdate (too noisy)
						log.Printf("[Core] Skipping broadcast of unknown event type: %s", eventType)
					}
				}

				if shouldBroadcast {
					if err := p.eventBus.Broadcast(eventType, eventData); err != nil {
						log.Printf("[Core] Error broadcasting event %s: %v", eventType, err)
						p.mu.Lock()
						p.status.LastError = fmt.Errorf("broadcast error for %s: %w", eventType, err)
						p.mu.Unlock()
					}
				}

			case <-p.ctx.Done():
				return
			}
		}
	}()
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return "core"
}

// Status returns the current plugin status
func (p *Plugin) Status() framework.PluginStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	status := p.status
	if p.status.State == "running" && p.startTime.Unix() > 0 {
		status.Uptime = time.Since(p.startTime)
	}
	return status
}

// SupportsStream indicates if the plugin supports streaming events
func (p *Plugin) SupportsStream() bool {
	return false
}

// connectWithRetry attempts to connect to Cytube with exponential backoff
func (p *Plugin) connectWithRetry() error {
	maxRetries := 5
	baseDelay := time.Second
	maxDelay := 30 * time.Second

	for i := 0; i < maxRetries; i++ {
		err := p.cytubeConn.Connect()
		if err == nil {
			p.reconnectAttempt = 0
			// Update connection status metric
			metrics.CytubeConnectionStatus.Set(1)
			return nil
		}

		if i == maxRetries-1 {
			return fmt.Errorf("all connection attempts failed: %w", err)
		}

		// Calculate delay with exponential backoff
		delay := baseDelay * time.Duration(1<<uint(i))
		if delay > maxDelay {
			delay = maxDelay
		}

		log.Printf("[Core] Connection attempt %d/%d failed: %v. Retrying in %v...", i+1, maxRetries, err, delay)
		p.status.RetryCount++
		p.reconnectAttempt = i + 1
		p.lastReconnect = time.Now()

		// Use a timer with context to allow cancellation during retry
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
			// Continue to next attempt
		case <-p.ctx.Done():
			timer.Stop()
			return fmt.Errorf("connection cancelled during retry: %w", p.ctx.Err())
		}
	}

	return fmt.Errorf("should not reach here")
}

// setupConnectionMonitor monitors the Cytube connection and attempts reconnection if needed
func (p *Plugin) setupConnectionMonitor() {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Check if we're still connected
				if p.cytubeConn != nil && !p.cytubeConn.IsConnected() {
					log.Printf("[Core] Connection lost, attempting to reconnect...")
					p.mu.Lock()
					p.status.State = "reconnecting"
					p.mu.Unlock()

					// Update connection status metric
					metrics.CytubeConnectionStatus.Set(0)
					metrics.CytubeReconnects.Inc()

					if err := p.connectWithRetry(); err != nil {
						log.Printf("[Core] Reconnection failed: %v", err)
						p.mu.Lock()
						p.status.State = "error"
						p.status.LastError = err
						p.mu.Unlock()
					} else {
						log.Printf("[Core] Successfully reconnected to Cytube")
						p.mu.Lock()
						p.status.State = "running"
						p.mu.Unlock()

						// Re-login if credentials are provided
						if p.config.Cytube.Username != "" && p.config.Cytube.Password != "" {
							if err := p.cytubeConn.Login(p.config.Cytube.Username, p.config.Cytube.Password); err != nil {
								log.Printf("[Core] Re-login failed: %v", err)
							} else {
								log.Printf("[Core] Re-login successful")
							}
						}
					}
				}
			case <-p.ctx.Done():
				return
			}
		}
	}()
}
