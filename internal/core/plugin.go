package core

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/cytube"
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
				eventType := event.Type()

				switch e := event.(type) {
				case *framework.ChatMessageEvent:
					eventData.ChatMessage = &framework.ChatMessageData{
						Username: e.Username,
						Message:  e.Message,
						UserRank: e.UserRank,
						UserID:   e.UserID,
					}
				case *framework.UserJoinEvent:
					eventData.UserJoin = &framework.UserJoinData{
						Username: e.Username,
						UserRank: e.UserRank,
					}
				case *framework.UserLeaveEvent:
					eventData.UserLeave = &framework.UserLeaveData{
						Username: e.Username,
					}
				case *framework.VideoChangeEvent:
					eventData.VideoChange = &framework.VideoChangeData{
						VideoID:   e.VideoID,
						VideoType: e.VideoType,
						Duration:  e.Duration,
						Title:     e.Title,
					}
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
