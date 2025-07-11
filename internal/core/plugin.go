package core

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hildolfr/daz/internal/logger"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/metrics"
	"github.com/hildolfr/daz/pkg/eventbus"
)

// Plugin implements the core plugin functionality
type Plugin struct {
	config        *Config
	eventBus      framework.EventBus
	roomManager   *RoomManager
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	status        framework.PluginStatus
	startTime     time.Time
	eventHandlers map[string]eventHandler
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

	// Create room manager
	p.roomManager = NewRoomManager(bus)

	// Add all configured rooms
	for _, room := range p.config.Rooms {
		if err := p.roomManager.AddRoom(room); err != nil {
			p.status.LastError = fmt.Errorf("failed to add room '%s': %w", room.ID, err)
			return p.status.LastError
		}
	}

	// Initialize event handlers
	p.initEventHandlers()

	// Note: Event handling and connection monitoring are now managed by RoomManager

	// Subscribe to chat message sending
	if err := p.eventBus.Subscribe("cytube.send", p.handleCytubeSend); err != nil {
		p.status.LastError = fmt.Errorf("failed to subscribe to cytube send events: %w", err)
		return p.status.LastError
	}

	// Subscribe to playlist request command
	if err := p.eventBus.Subscribe("cytube.command.requestPlaylist", p.handlePlaylistRequest); err != nil {
		p.status.LastError = fmt.Errorf("failed to subscribe to playlist request command: %w", err)
		return p.status.LastError
	}

	return nil
}

// Initialize sets up the plugin with the event bus (deprecated, use Init)
func (p *Plugin) Initialize(eventBus framework.EventBus) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.eventBus = eventBus

	// Create room manager
	p.roomManager = NewRoomManager(eventBus)

	// Add all configured rooms
	for _, room := range p.config.Rooms {
		if err := p.roomManager.AddRoom(room); err != nil {
			return fmt.Errorf("failed to add room '%s': %w", room.ID, err)
		}
	}

	// Initialize event handlers
	p.initEventHandlers()

	// Note: Event handling is now managed by RoomManager

	// Subscribe to chat message sending
	if err := p.eventBus.Subscribe("cytube.send", p.handleCytubeSend); err != nil {
		return fmt.Errorf("failed to subscribe to cytube send events: %w", err)
	}

	// Subscribe to private message sending
	if err := p.eventBus.Subscribe("cytube.send.pm", p.handleCytubeSendPM); err != nil {
		return fmt.Errorf("failed to subscribe to cytube send pm events: %w", err)
	}

	return nil
}

// Start begins the plugin operation
func (p *Plugin) Start() error {
	// Update status
	p.mu.Lock()
	p.status.State = "running"
	p.startTime = time.Now()
	p.mu.Unlock()

	// Start connection monitoring (but don't connect yet)
	go p.roomManager.MonitorConnections(p.ctx)

	logger.Info("Core", "Plugin started successfully with %d rooms configured", len(p.config.Rooms))
	return nil
}

// StartRoomConnections starts all room connections
// This should be called after all plugins are ready
func (p *Plugin) StartRoomConnections() {
	logger.Info("Core", "Starting room connections...")
	p.roomManager.StartAll()
	logger.Info("Core", "Room connections initiated")
}

// Stop gracefully shuts down the plugin
func (p *Plugin) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Update status
	p.status.State = "stopped"

	// Cancel context to stop all operations
	p.cancel()

	// Stop all room connections
	if p.roomManager != nil {
		p.roomManager.StopAll()
	}

	logger.Info("Core", "Plugin stopped")
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

// handleCytubeSend handles requests to send messages to Cytube
func (p *Plugin) handleCytubeSend(event framework.Event) error {
	logger.Debug("Core", "Received cytube.send event")

	// Check if this is a DataEvent which carries EventData
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		logger.Error("Core", "Event is not DataEvent, got %T", event)
		return nil
	}

	if dataEvent.Data == nil || dataEvent.Data.RawMessage == nil {
		logger.Error("Core", "No RawMessage in event data")
		return nil
	}

	// Send chat message to Cytube
	msg := dataEvent.Data.RawMessage.Message
	logger.Debug("Core", "Sending message to Cytube: %d chars", len(msg))

	// Determine which room to send to
	roomID := ""

	// Channel must be provided to know where to send the message
	channel := dataEvent.Data.RawMessage.Channel
	if channel == "" {
		return fmt.Errorf("no channel specified for message")
	}

	// Find room by channel name
	for _, room := range p.config.Rooms {
		if room.Enabled && room.Channel == channel {
			roomID = room.ID
			break
		}
	}

	if roomID == "" {
		return fmt.Errorf("channel '%s' not found in configured rooms", channel)
	}

	// Send message to the room
	err := p.roomManager.SendToRoom(roomID, msg)
	if err != nil {
		logger.Error("Core", "Failed to send message to room '%s': %v", roomID, err)
		return err
	}

	// Track message sent
	metrics.CytubeMessagesSent.Inc()

	logger.Debug("Core", "Message sent successfully to room '%s'", roomID)
	return nil
}

// handleCytubeSendPM handles private message send requests
func (p *Plugin) handleCytubeSendPM(event framework.Event) error {
	// Extract data event
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok {
		return fmt.Errorf("invalid event type for cytube send pm")
	}

	// Check for PM data
	if dataEvent.Data == nil || dataEvent.Data.PrivateMessage == nil {
		logger.Error("Core", "No PrivateMessage in event data")
		return nil
	}

	// Get PM details
	pm := dataEvent.Data.PrivateMessage
	toUser := pm.ToUser
	msg := pm.Message
	channel := pm.Channel

	logger.Debug("Core", "Sending PM to user '%s': %d chars", toUser, len(msg))

	// Determine which room to send to
	roomID := ""
	botUsername := ""

	// Channel must be provided to know where to send the PM
	if channel == "" {
		return fmt.Errorf("no channel specified for PM")
	}

	// Find room by channel name
	for _, room := range p.config.Rooms {
		if room.Enabled && room.Channel == channel {
			roomID = room.ID
			botUsername = room.Username
			break
		}
	}

	if roomID == "" {
		return fmt.Errorf("channel '%s' not found in configured rooms", channel)
	}

	// Send PM to the room
	err := p.roomManager.SendPMToRoom(roomID, toUser, msg)
	if err != nil {
		logger.Error("Core", "Failed to send PM to room '%s': %v", roomID, err)
		return err
	}

	// Get the actual channel name for logging
	channelName := ""
	for _, room := range p.config.Rooms {
		if room.ID == roomID {
			channelName = room.Channel
			break
		}
	}

	logger.Debug("Core", "PM sent successfully to user '%s' in channel '%s'", toUser, channelName)

	// Log the outgoing PM to SQL
	logEvent := framework.NewDataEvent("cytube.event.pm.sent", &framework.EventData{
		PrivateMessage: &framework.PrivateMessageData{
			FromUser:    botUsername,
			ToUser:      toUser,
			Message:     msg,
			MessageTime: time.Now().Unix(),
			Channel:     channelName,
		},
	})
	if err := p.eventBus.Broadcast("cytube.event.pm.sent", logEvent.Data); err != nil {
		logger.Error("Core", "Failed to log outgoing PM: %v", err)
	}

	return nil
}

// handlePlaylistRequest handles requests to fetch the playlist
func (p *Plugin) handlePlaylistRequest(event framework.Event) error {
	logger.Debug("Core", "Received playlist request command")

	// For now, request playlist from all connected rooms
	// In the future, we might want to specify which room
	connections := p.roomManager.GetAllConnections()

	successCount := 0
	for roomID, conn := range connections {
		conn.mu.RLock()
		connected := conn.Connected
		client := conn.Client
		conn.mu.RUnlock()

		if connected && client != nil {
			if err := client.RequestPlaylist(); err != nil {
				logger.Error("Core", "Failed to request playlist for room '%s': %v", roomID, err)
			} else {
				logger.Debug("Core", "Playlist request sent for room '%s'", roomID)
				successCount++
			}
		}
	}

	if successCount == 0 {
		return fmt.Errorf("no rooms available for playlist request")
	}

	logger.Info("Core", "Playlist request sent to %d room(s)", successCount)
	return nil
}

// initEventHandlers initializes the map of event handlers
func (p *Plugin) initEventHandlers() {
	p.eventHandlers = map[string]eventHandler{
		"*framework.ChatMessageEvent":    p.handleChatMessage,
		"*framework.UserJoinEvent":       p.handleUserJoin,
		"*framework.UserLeaveEvent":      p.handleUserLeave,
		"*framework.VideoChangeEvent":    p.handleVideoChange,
		"*framework.MediaUpdateEvent":    p.handleMediaUpdate,
		"*framework.PrivateMessageEvent": p.handlePrivateMessage,
		"*framework.PlaylistArrayEvent":  p.handlePlaylistArray,
	}
}

// Event handler methods
func (p *Plugin) handleChatMessage(event framework.Event) (string, *framework.EventData, bool) {
	e := event.(*framework.ChatMessageEvent)
	eventData := &framework.EventData{
		ChatMessage: &framework.ChatMessageData{
			Username:    e.Username,
			Message:     e.Message,
			UserRank:    e.UserRank,
			UserID:      e.UserID,
			Channel:     e.ChannelName,
			MessageTime: e.MessageTime,
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
			Channel:   e.ChannelName,
		},
	}
	return eventbus.EventCytubeVideoChange, eventData, true
}

func (p *Plugin) handleMediaUpdate(event framework.Event) (string, *framework.EventData, bool) {
	e := event.(*framework.MediaUpdateEvent)

	// Note: MediaUpdate timestamps are now tracked by RoomManager per room

	eventData := &framework.EventData{
		MediaUpdate: &framework.MediaUpdateData{
			CurrentTime: e.CurrentTime,
			Paused:      e.Paused,
		},
	}
	return eventbus.EventCytubeMediaUpdate, eventData, true
}

func (p *Plugin) handlePrivateMessage(event framework.Event) (string, *framework.EventData, bool) {
	e := event.(*framework.PrivateMessageEvent)
	eventData := &framework.EventData{
		PrivateMessage: &framework.PrivateMessageData{
			FromUser:    e.FromUser,
			ToUser:      e.ToUser,
			Message:     e.Message,
			MessageTime: e.MessageTime,
			Channel:     e.ChannelName,
		},
	}
	return eventbus.EventCytubePM, eventData, true
}

func (p *Plugin) handlePlaylistArray(event framework.Event) (string, *framework.EventData, bool) {
	// For PlaylistArrayEvent, we need to pass the raw event through
	if _, ok := event.(*framework.PlaylistArrayEvent); ok {
		// Create EventData with raw event passthrough
		eventData := &framework.EventData{
			RawEvent:     event,
			RawEventType: "PlaylistArrayEvent",
		}

		return "cytube.event.playlist", eventData, true
	}

	// Fallback
	return "", nil, false
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
