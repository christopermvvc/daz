package core

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/cytube"
	"github.com/hildolfr/daz/pkg/eventbus"
)

// Plugin implements the core plugin functionality
type Plugin struct {
	config     *Config
	eventBus   framework.EventBus
	cytubeConn *cytube.WebSocketClient
	sqlModule  *SQLModule
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	eventChan  chan framework.Event
}

// NewPlugin creates a new instance of the core plugin
func NewPlugin(config *Config) *Plugin {
	config.SetDefaults()

	ctx, cancel := context.WithCancel(context.Background())
	return &Plugin{
		config: config,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Initialize sets up the plugin with the event bus
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
		return fmt.Errorf("failed to start SQL module: %w", err)
	}

	// Connect to Cytube
	if err := p.cytubeConn.Connect(); err != nil {
		return fmt.Errorf("failed to connect to Cytube: %w", err)
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
func (p *Plugin) HandleEvent(ctx context.Context, event framework.Event) error {
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

	log.Printf("[Core] Message sent successfully")
	return nil
}

// setupCytubeHandlers starts a goroutine to process Cytube events
func (p *Plugin) setupCytubeHandlers() {
	go func() {
		for {
			select {
			case event := <-p.eventChan:
				// Log event to database
				if err := p.sqlModule.LogEvent(p.ctx, event); err != nil {
					log.Printf("[Core] Error logging event: %v", err)
				}

				// Re-publish to event bus for other plugins
				eventData := &framework.EventData{}
				var eventType string

				switch e := event.(type) {
				case *framework.ChatMessageEvent:
					eventType = eventbus.EventCytubeChatMsg
					eventData.ChatMessage = &framework.ChatMessageData{
						Username: e.Username,
						Message:  e.Message,
						UserRank: e.UserRank,
						UserID:   e.UserID,
						Channel:  e.ChannelName,
					}
				case *framework.UserJoinEvent:
					eventType = eventbus.EventCytubeUserJoin
					eventData.UserJoin = &framework.UserJoinData{
						Username: e.Username,
						UserRank: e.UserRank,
					}
				case *framework.UserLeaveEvent:
					eventType = eventbus.EventCytubeUserLeave
					eventData.UserLeave = &framework.UserLeaveData{
						Username: e.Username,
					}
				case *framework.VideoChangeEvent:
					eventType = eventbus.EventCytubeVideoChange
					eventData.VideoChange = &framework.VideoChangeData{
						VideoID:   e.VideoID,
						VideoType: e.VideoType,
						Duration:  e.Duration,
						Title:     e.Title,
					}
				default:
					// For other events, use the original type
					eventType = event.Type()
				}

				if err := p.eventBus.Broadcast(eventType, eventData); err != nil {
					log.Printf("[Core] Error broadcasting event: %v", err)
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

// SupportsStream indicates if the plugin supports streaming events
func (p *Plugin) SupportsStream() bool {
	return false
}
