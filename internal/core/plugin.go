package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/logger"
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

// GenericPayload wraps arbitrary data to implement cytube.EventPayload
type GenericPayload struct {
	Data map[string]interface{}
}

// IsEventPayload implements cytube.EventPayload interface
func (g *GenericPayload) IsEventPayload() {}

// StringPayload wraps a simple string value to implement cytube.EventPayload
// Used for commands that expect just a string value, not an object
type StringPayload string

// IsEventPayload implements cytube.EventPayload interface
func (s StringPayload) IsEventPayload() {}

// MarshalJSON returns the string as a JSON value
func (s StringPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

// IntPayload wraps an integer value to implement cytube.EventPayload
// Used for commands that expect just a number value, not an object
type IntPayload int

// IsEventPayload implements cytube.EventPayload interface
func (i IntPayload) IsEventPayload() {}

// MarshalJSON returns the integer as a JSON number
func (i IntPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(int(i))
}

// MarshalJSON implements json.Marshaler
func (g *GenericPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal(g.Data)
}

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

	// Subscribe to plugin requests
	if err := p.eventBus.Subscribe("plugin.request", p.handlePluginRequest); err != nil {
		p.status.LastError = fmt.Errorf("failed to subscribe to plugin requests: %w", err)
		return p.status.LastError
	}

	return nil
}

// Initialize sets up the plugin with the event bus (used by main.go for core plugin initialization)
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

	// Subscribe to plugin requests
	if err := p.eventBus.Subscribe("plugin.request", p.handlePluginRequest); err != nil {
		return fmt.Errorf("failed to subscribe to plugin requests: %w", err)
	}

	logger.Info("Core", "Subscribed to plugin.request events")

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

	logger.Debug("Core", "Plugin started successfully with %d rooms configured", len(p.config.Rooms))
	return nil
}

// StartRoomConnections starts all room connections
// This should be called after all plugins are ready
func (p *Plugin) StartRoomConnections() {
	logger.Debug("Core", "Starting room connections...")
	p.roomManager.StartAll()
	logger.Debug("Core", "Room connections initiated")
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

// handlePluginRequest handles plugin requests for core plugin information
func (p *Plugin) handlePluginRequest(event framework.Event) error {
	logger.Debug("Core", "Received plugin.request event")

	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil || dataEvent.Data.PluginRequest == nil {
		logger.Debug("Core", "Received plugin.request event with no valid request data")
		return nil
	}

	req := dataEvent.Data.PluginRequest

	logger.Debug("Core", "Plugin request from '%s' to '%s' type '%s'", req.From, req.To, req.Type)

	// Check if this request is targeted at the core plugin
	if req.To != "core" {
		// Not for us
		return nil
	}

	logger.Info("Core", "Handling plugin request from '%s' of type '%s'", req.From, req.Type)

	// Handle different request types
	switch req.Type {
	case "get_configured_channels":
		return p.handleGetConfiguredChannels(req)
	case "get_bot_username":
		return p.handleGetBotUsername(req)
	case "queue_media":
		return p.handleQueueMedia(req)
	case "delete_media":
		return p.handleDeleteMedia(req)
	default:
		return p.respondToPluginRequest(req, &framework.EventData{PluginResponse: &framework.PluginResponse{
			ID:      req.ID,
			From:    "core",
			Success: false,
			Error:   fmt.Sprintf("unknown request type: %s", req.Type),
		}}, fmt.Errorf("unknown request type: %s", req.Type))
	}
}

func (p *Plugin) respondToPluginRequest(req *framework.PluginRequest, response *framework.EventData, err error) error {
	if req == nil {
		return nil
	}

	// If this is a synchronous request (EventBus.Request), deliver directly.
	if req.ReplyTo == "eventbus" && req.ID != "" {
		p.eventBus.DeliverResponse(req.ID, response, err)
		return nil
	}

	// Default async behavior: broadcast to per-plugin response topic.
	return p.eventBus.Broadcast(fmt.Sprintf("plugin.response.%s", req.From), response)
}

// handleGetConfiguredChannels returns the list of configured channels
func (p *Plugin) handleGetConfiguredChannels(req *framework.PluginRequest) error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Build channel list
	type channelInfo struct {
		ID        string `json:"id"`
		Channel   string `json:"channel"`
		Enabled   bool   `json:"enabled"`
		Connected bool   `json:"connected"`
		Username  string `json:"username,omitempty"`
	}

	channels := make([]channelInfo, 0, len(p.config.Rooms))

	for _, room := range p.config.Rooms {
		info := channelInfo{
			ID:       room.ID,
			Channel:  room.Channel,
			Enabled:  room.Enabled,
			Username: room.Username,
		}

		// Get connection status from room manager
		if conn, exists := p.roomManager.GetConnection(room.ID); exists {
			conn.mu.RLock()
			info.Connected = conn.Connected
			conn.mu.RUnlock()
		}

		channels = append(channels, info)
	}

	// Create response
	responseData := map[string]interface{}{
		"channels": channels,
	}

	rawJSON, err := json.Marshal(responseData)
	if err != nil {
		return fmt.Errorf("failed to marshal response data: %w", err)
	}

	response := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			ID:      req.ID,
			From:    "core",
			Success: true,
			Data: &framework.ResponseData{
				RawJSON: rawJSON,
			},
		},
	}

	// Send response back to requesting plugin
	return p.respondToPluginRequest(req, response, nil)
}

func (p *Plugin) handleGetBotUsername(req *framework.PluginRequest) error {
	channel := ""
	if req.Data != nil && req.Data.KeyValue != nil {
		channel = req.Data.KeyValue["channel"]
	}
	channel = strings.TrimSpace(channel)
	if channel == "" {
		resp := &framework.EventData{PluginResponse: &framework.PluginResponse{
			ID:      req.ID,
			From:    "core",
			Success: false,
			Error:   "channel is required",
		}}
		return p.respondToPluginRequest(req, resp, fmt.Errorf("channel is required"))
	}

	botUsername := ""
	p.mu.RLock()
	for _, room := range p.config.Rooms {
		if room.Enabled && room.Channel == channel {
			botUsername = room.Username
			break
		}
	}
	p.mu.RUnlock()

	resp := &framework.EventData{PluginResponse: &framework.PluginResponse{
		ID:      req.ID,
		From:    "core",
		Success: true,
		Data: &framework.ResponseData{
			KeyValue: map[string]string{
				"channel":      channel,
				"bot_username": botUsername,
			},
		},
	}}
	return p.respondToPluginRequest(req, resp, nil)
}

// handleQueueMedia handles requests to queue media in CyTube
func (p *Plugin) handleQueueMedia(req *framework.PluginRequest) error {
	logger.Info("Core", "handleQueueMedia called from plugin: %s", req.From)

	if req.Data == nil || req.Data.KeyValue == nil {
		logger.Error("Core", "Missing queue data in request from %s", req.From)
		return fmt.Errorf("missing queue data")
	}

	data := req.Data.KeyValue
	channel := data["channel"]
	mediaType := data["type"]
	mediaID := data["id"]
	pos := data["pos"]
	temp := data["temp"] == "true"

	logger.Info("Core", "Queue media request - Channel: %s, Type: %s, ID: %s, Pos: %s, Temp: %v",
		channel, mediaType, mediaID, pos, temp)

	// Find the room ID for this channel
	roomID := ""
	for _, room := range p.config.Rooms {
		if room.Channel == channel {
			roomID = room.ID
			break
		}
	}

	if roomID == "" {
		return fmt.Errorf("channel '%s' not found", channel)
	}

	// Get the connection for this room
	p.mu.RLock()
	conn, exists := p.roomManager.connections[roomID]
	p.mu.RUnlock()

	if !exists || conn == nil {
		return fmt.Errorf("no connection for room '%s'", roomID)
	}

	conn.mu.RLock()
	client := conn.Client
	connected := conn.Connected
	conn.mu.RUnlock()

	if !connected || client == nil {
		return fmt.Errorf("room '%s' is not connected", roomID)
	}

	// Create the queue payload for CyTube matching the JS client format
	// From CyTube's ui.js: socket.emit("queue", { id, type, pos, temp, title, duration })
	queuePayload := map[string]interface{}{
		"id":   mediaID,
		"type": mediaType,
		"pos":  pos,
		"temp": temp,
	}

	// Add title if provided and not default
	title := data["title"]
	if title != "" && title != "Default Title" {
		queuePayload["title"] = title
	}

	// For direct files (type "fi"), we pass the full URL as the ID
	if mediaType == "fi" {
		queuePayload["id"] = mediaID
	}

	logger.Info("Core", "Sending queue payload: %+v", queuePayload)

	// Create a generic payload that implements EventPayload
	genericPayload := &GenericPayload{Data: queuePayload}

	// Send the queue event via WebSocket
	if err := client.Send("queue", genericPayload); err != nil {
		logger.Error("Core", "Failed to send queue event: %v", err)
		return fmt.Errorf("failed to queue media: %w", err)
	}

	logger.Info("Core", "Successfully sent queue event for %s:%s", mediaType, mediaID)

	// Send success response
	response := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			ID:      req.ID,
			From:    "core",
			Success: true,
		},
	}

	return p.eventBus.Broadcast(fmt.Sprintf("plugin.response.%s", req.From), response)
}

// handleDeleteMedia handles requests to delete media from CyTube playlist
func (p *Plugin) handleDeleteMedia(req *framework.PluginRequest) error {
	logger.Info("Core", "handleDeleteMedia called from plugin: %s", req.From)

	if req.Data == nil || req.Data.KeyValue == nil {
		logger.Error("Core", "Missing delete data in request from %s", req.From)
		return fmt.Errorf("missing delete data")
	}

	data := req.Data.KeyValue
	channel := data["channel"]
	uid := data["uid"]

	logger.Info("Core", "Delete media request - Channel: %s, UID: %s", channel, uid)

	// Find the room ID for this channel
	roomID := ""
	for _, room := range p.config.Rooms {
		if room.Channel == channel {
			roomID = room.ID
			break
		}
	}

	if roomID == "" {
		return fmt.Errorf("channel '%s' not found", channel)
	}

	// Get the connection for this room
	p.mu.RLock()
	conn, exists := p.roomManager.connections[roomID]
	p.mu.RUnlock()

	if !exists || conn == nil {
		return fmt.Errorf("no connection for room '%s'", roomID)
	}

	conn.mu.RLock()
	client := conn.Client
	connected := conn.Connected
	conn.mu.RUnlock()

	if !connected || client == nil {
		return fmt.Errorf("room '%s' is not connected", roomID)
	}

	// Create delete payload - CyTube expects just the UID value directly
	// From util.js: socket.emit("delete", li.data("uid"));
	// CyTube's delete event expects the UID as a number, not a string
	// Convert the UID string to an integer
	uidInt, err := strconv.Atoi(uid)
	if err != nil {
		logger.Error("Core", "Invalid UID format: %s", uid)
		return fmt.Errorf("invalid UID: %w", err)
	}

	// Use the IntPayload type to send the UID as a JSON number
	payload := IntPayload(uidInt)

	logger.Info("Core", "Sending delete event for UID: %d", uidInt)

	// Send the delete event via WebSocket
	if err := client.Send("delete", payload); err != nil {
		logger.Error("Core", "Failed to send delete event: %v", err)
		return fmt.Errorf("failed to delete media: %w", err)
	}

	logger.Info("Core", "Successfully sent delete event for UID %s", uid)

	// Send success response
	response := &framework.EventData{
		PluginResponse: &framework.PluginResponse{
			ID:      req.ID,
			From:    "core",
			Success: true,
		},
	}

	return p.eventBus.Broadcast(fmt.Sprintf("plugin.response.%s", req.From), response)
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
