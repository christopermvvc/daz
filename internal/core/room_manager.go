package core

import (
	"context"
	"fmt"
	"github.com/hildolfr/daz/internal/logger"
	"math/rand"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/cytube"
)

// RoomConnection represents a connection to a single room
type RoomConnection struct {
	Room                RoomConfig
	Client              *cytube.WebSocketClient
	EventChan           chan framework.Event
	Connected           bool
	ReconnectAttempt    int
	LastReconnect       time.Time
	LastMediaUpdate     time.Time
	reconnectInProgress bool
	mu                  sync.RWMutex
}

// RoomManager manages multiple room connections
type RoomManager struct {
	connections map[string]*RoomConnection // keyed by room ID
	eventBus    framework.EventBus
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.RWMutex
}

// NewRoomManager creates a new room manager
func NewRoomManager(eventBus framework.EventBus) *RoomManager {
	ctx, cancel := context.WithCancel(context.Background())
	return &RoomManager{
		connections: make(map[string]*RoomConnection),
		eventBus:    eventBus,
		ctx:         ctx,
		cancel:      cancel,
	}
}

// AddRoom adds a new room to be managed
func (rm *RoomManager) AddRoom(room RoomConfig) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Auto-generate room ID from channel name if not provided
	if room.ID == "" {
		if room.Channel == "" {
			return fmt.Errorf("room channel cannot be empty")
		}
		room.ID = room.Channel
		logger.Info("RoomManager", "Auto-generated room ID '%s' from channel name", room.ID)
	}

	if !room.Enabled {
		logger.Info("RoomManager", "Room '%s' is disabled, skipping", room.ID)
		return nil
	}

	if _, exists := rm.connections[room.ID]; exists {
		return fmt.Errorf("room '%s' already exists", room.ID)
	}

	// Create event channel for this room
	// Increased buffer size to handle burst of events when connecting to multiple rooms
	eventChan := make(chan framework.Event, 2000)

	// Create WebSocket client for this room
	client, err := cytube.NewWebSocketClient(room.Channel, room.ID, eventChan)
	if err != nil {
		return fmt.Errorf("failed to create client for room '%s': %w", room.ID, err)
	}

	// Create room connection
	conn := &RoomConnection{
		Room:            room,
		Client:          client,
		EventChan:       eventChan,
		Connected:       false,
		LastMediaUpdate: time.Now(), // Initialize to current time to avoid zero value
	}

	rm.connections[room.ID] = conn
	logger.Info("RoomManager", "Added room '%s' (channel: %s)", room.ID, room.Channel)

	return nil
}

// StartRoom starts the connection for a specific room
func (rm *RoomManager) StartRoom(roomID string) error {
	rm.mu.RLock()
	conn, exists := rm.connections[roomID]
	rm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("room '%s' not found", roomID)
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()

	// Ensure we're disconnected before attempting to connect
	if conn.Client.IsConnected() {
		logger.Info("RoomManager", "Room '%s': Client still connected, disconnecting first", roomID)
		if err := conn.Client.Disconnect(); err != nil {
			logger.Error("RoomManager", "Room '%s': Error during disconnect: %v", roomID, err)
		}
		// Wait for disconnect to complete
		disconnectDone := make(chan struct{})
		go func() {
			select {
			case <-time.After(100 * time.Millisecond):
			case <-rm.ctx.Done():
			}
			close(disconnectDone)
		}()
		<-disconnectDone
	}

	// Create a new client for reconnection to ensure fresh context
	if conn.ReconnectAttempt > 0 {
		logger.Info("RoomManager", "Room '%s': Creating new client for reconnection", roomID)
		newClient, err := cytube.NewWebSocketClient(conn.Room.Channel, conn.Room.ID, conn.EventChan)
		if err != nil {
			return fmt.Errorf("failed to create new client for reconnection: %w", err)
		}
		conn.Client = newClient
	}

	// Connect with retry logic using exponential backoff
	for attempt := 0; attempt < conn.Room.ReconnectAttempts; attempt++ {
		if attempt > 0 {
			// Exponential backoff with jitter
			baseDelay := 2 * time.Second
			maxDelay := 5 * time.Minute

			// Calculate exponential delay
			waitTime := baseDelay * time.Duration(1<<uint(attempt-1))
			if waitTime > maxDelay {
				waitTime = maxDelay
			}

			// Add jitter (±25%)
			jitter := time.Duration(rand.Float64() * 0.5 * float64(waitTime))
			waitTime = waitTime + jitter - (jitter / 2)

			logger.Info("RoomManager", "Room '%s': Waiting %v before retry %d/%d (exponential backoff with jitter)",
				roomID, waitTime, attempt+1, conn.Room.ReconnectAttempts)
			retryTimer := time.NewTimer(waitTime)
			select {
			case <-retryTimer.C:
			case <-rm.ctx.Done():
				retryTimer.Stop()
				return fmt.Errorf("context cancelled during retry wait")
			}
		}

		logger.Info("RoomManager", "Room '%s': Connecting to %s (attempt %d/%d)",
			roomID, conn.Room.Channel, attempt+1, conn.Room.ReconnectAttempts)

		if err := conn.Client.Connect(); err != nil {
			logger.Error("RoomManager", "Room '%s': Connection failed: %v", roomID, err)
			continue
		}

		conn.Connected = true
		conn.ReconnectAttempt = 0
		conn.LastMediaUpdate = time.Now() // Reset timestamp on successful connection
		logger.Info("RoomManager", "Room '%s': Connected successfully", roomID)

		// Start event processing for this room
		go rm.processRoomEvents(roomID)

		// Wait for connection to stabilize
		stabilizeTimer := time.NewTimer(3 * time.Second)
		select {
		case <-stabilizeTimer.C:
		case <-rm.ctx.Done():
			stabilizeTimer.Stop()
			return fmt.Errorf("context cancelled during stabilization")
		}

		// Login if credentials provided
		if conn.Room.Username != "" && conn.Room.Password != "" {
			logger.Info("RoomManager", "Room '%s': Logging in as %s", roomID, conn.Room.Username)
			if err := conn.Client.Login(conn.Room.Username, conn.Room.Password); err != nil {
				logger.Error("RoomManager", "Room '%s': Login failed: %v", roomID, err)
			} else {
				logger.Info("RoomManager", "Room '%s': Login successful", roomID)

				// Now join the channel after successful login
				logger.Info("RoomManager", "Room '%s': Joining channel as authenticated user", roomID)
				if err := conn.Client.JoinChannel(); err != nil {
					logger.Error("RoomManager", "Room '%s': Failed to join channel: %v", roomID, err)
					return fmt.Errorf("failed to join channel: %w", err)
				}

				// Request playlist after joining
				if err := conn.Client.RequestPlaylist(); err != nil {
					logger.Error("RoomManager", "Room '%s': Failed to request playlist: %v", roomID, err)
				}
			}
		} else {
			// Join channel as anonymous if no credentials
			logger.Info("RoomManager", "Room '%s': Joining channel as anonymous user", roomID)
			if err := conn.Client.JoinChannel(); err != nil {
				logger.Error("RoomManager", "Room '%s': Failed to join channel: %v", roomID, err)
				return fmt.Errorf("failed to join channel: %w", err)
			}
		}

		return nil
	}

	return fmt.Errorf("failed to connect after %d attempts", conn.Room.ReconnectAttempts)
}

// processRoomEvents handles events from a specific room
func (rm *RoomManager) processRoomEvents(roomID string) {
	rm.mu.RLock()
	conn, exists := rm.connections[roomID]
	rm.mu.RUnlock()

	if !exists {
		return
	}

	for {
		select {
		case <-rm.ctx.Done():
			return
		case event := <-conn.EventChan:
			// Events from Cytube should already have channel name set
			// For events that don't, we wrap them in a CytubeEvent
			if cytubeEvent, ok := event.(*framework.CytubeEvent); ok {
				// Update channel name to ensure it matches our config
				cytubeEvent.ChannelName = conn.Room.Channel
				// Add room ID to metadata
				if cytubeEvent.Metadata == nil {
					cytubeEvent.Metadata = make(map[string]string)
				}
				cytubeEvent.Metadata["room_id"] = roomID
			}

			// Handle disconnect events immediately
			if event.Type() == "disconnect" {
				logger.Info("RoomManager", "Room '%s': Received disconnect event, marking as disconnected", roomID)
				conn.mu.Lock()
				conn.Connected = false
				conn.mu.Unlock()

				// Check if this was an unexpected disconnect
				if cytubeEvent, ok := event.(*framework.CytubeEvent); ok {
					if reason, exists := cytubeEvent.Metadata["reason"]; exists && reason == "connection_lost" {
						logger.Warn("RoomManager", "Room '%s': Connection lost unexpectedly, triggering immediate reconnection", roomID)
						// Trigger immediate reconnection in a goroutine
						go func() {
							// Small delay to avoid immediate reconnection storms
							reconnectTimer := time.NewTimer(2 * time.Second)
							select {
							case <-reconnectTimer.C:
								rm.handleReconnection(roomID)
							case <-rm.ctx.Done():
								reconnectTimer.Stop()
							}
						}()
					}
				}
			}

			// Update last media update time for MediaUpdate events
			if event.Type() == "mediaUpdate" {
				conn.mu.Lock()
				conn.LastMediaUpdate = time.Now()
				conn.mu.Unlock()
			}

			// Broadcast to event bus with proper event type prefix
			eventType := fmt.Sprintf("cytube.event.%s", event.Type())
			eventData := &framework.EventData{
				RawEventType: event.Type(),
			}

			// For chat messages, populate the ChatMessage field
			if chatEvent, ok := event.(*framework.ChatMessageEvent); ok && event.Type() == "chatMsg" {
				eventData.ChatMessage = &framework.ChatMessageData{
					Username:    chatEvent.Username,
					Message:     chatEvent.Message,
					UserRank:    chatEvent.UserRank,
					UserID:      chatEvent.UserID,
					Channel:     chatEvent.ChannelName,
					MessageTime: chatEvent.MessageTime,
				}
			} else if pmEvent, ok := event.(*framework.PrivateMessageEvent); ok && event.Type() == "pm" {
				// For private messages, populate the PrivateMessage field
				eventData.PrivateMessage = &framework.PrivateMessageData{
					FromUser:    pmEvent.FromUser,
					ToUser:      pmEvent.ToUser,
					Message:     pmEvent.Message,
					MessageTime: pmEvent.MessageTime,
					Channel:     pmEvent.ChannelName,
				}
			} else {
				// For non-chat events, pass the raw event
				eventData.RawEvent = event
			}

			// Create metadata with appropriate tags
			metadata := rm.createEventMetadata(event.Type(), roomID)

			// Broadcast with metadata
			if err := rm.eventBus.BroadcastWithMetadata(eventType, eventData, metadata); err != nil {
				logger.Error("RoomManager", "Room '%s': Failed to broadcast event: %v", roomID, err)
			}
		}
	}
}

// StopRoom stops the connection for a specific room
func (rm *RoomManager) StopRoom(roomID string) error {
	rm.mu.RLock()
	conn, exists := rm.connections[roomID]
	rm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("room '%s' not found", roomID)
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()

	if conn.Connected {
		if err := conn.Client.Disconnect(); err != nil {
			logger.Error("RoomManager", "Room '%s': Error disconnecting: %v", roomID, err)
		}
		conn.Connected = false
	}

	return nil
}

// StartAll starts all enabled rooms
func (rm *RoomManager) StartAll() {
	rm.mu.RLock()
	roomIDs := make([]string, 0, len(rm.connections))
	for id := range rm.connections {
		roomIDs = append(roomIDs, id)
	}
	rm.mu.RUnlock()

	// Start all rooms concurrently
	var wg sync.WaitGroup
	wg.Add(len(roomIDs))

	for _, roomID := range roomIDs {
		go func(id string) {
			defer wg.Done()
			if err := rm.StartRoom(id); err != nil {
				logger.Error("RoomManager", "Failed to start room '%s': %v", id, err)
			}
		}(roomID)
	}

	// Wait for all rooms to finish connecting
	wg.Wait()
}

// StopAll stops all room connections
func (rm *RoomManager) StopAll() {
	rm.cancel() // Cancel context to stop event processors

	rm.mu.RLock()
	roomIDs := make([]string, 0, len(rm.connections))
	for id := range rm.connections {
		roomIDs = append(roomIDs, id)
	}
	rm.mu.RUnlock()

	for _, roomID := range roomIDs {
		if err := rm.StopRoom(roomID); err != nil {
			logger.Error("RoomManager", "Failed to stop room '%s': %v", roomID, err)
		}
	}
}

// GetConnection returns the connection for a specific room
func (rm *RoomManager) GetConnection(roomID string) (*RoomConnection, bool) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	conn, exists := rm.connections[roomID]
	return conn, exists
}

// GetAllConnections returns all room connections
func (rm *RoomManager) GetAllConnections() map[string]*RoomConnection {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	// Return a copy to prevent external modifications
	result := make(map[string]*RoomConnection)
	for k, v := range rm.connections {
		result[k] = v
	}
	return result
}

// SendToRoom sends a message to a specific room
func (rm *RoomManager) SendToRoom(roomID string, message string) error {
	rm.mu.RLock()
	conn, exists := rm.connections[roomID]
	rm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("room '%s' not found", roomID)
	}

	conn.mu.RLock()
	defer conn.mu.RUnlock()

	if !conn.Connected {
		return fmt.Errorf("room '%s' is not connected", roomID)
	}

	// Send chat message using the cytube protocol
	chatData := cytube.ChatMessageSendPayload{
		Message: message,
	}
	return conn.Client.Send("chatMsg", chatData)
}

// SendPMToRoom sends a private message to a specific user in a room
func (rm *RoomManager) SendPMToRoom(roomID string, toUser string, message string) error {
	rm.mu.RLock()
	conn, exists := rm.connections[roomID]
	rm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("room '%s' not found", roomID)
	}

	conn.mu.RLock()
	defer conn.mu.RUnlock()

	if !conn.Connected {
		return fmt.Errorf("room '%s' is not connected", roomID)
	}

	// Send PM using the cytube protocol
	pmData := cytube.PrivateMessageSendPayload{
		To:   toUser,
		Msg:  message,
		Meta: make(map[string]interface{}),
	}

	// Debug log with actual channel name
	logger.Debug("RoomManager", "Sending PM via websocket - Channel: '%s', To: '%s', Message length: %d",
		conn.Room.Channel, toUser, len(message))

	return conn.Client.Send("pm", pmData)
}

// MonitorConnections monitors all connections and reconnects if needed
func (rm *RoomManager) MonitorConnections(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rm.checkConnections()
		}
	}
}

// handleReconnection handles reconnection for a specific room
func (rm *RoomManager) handleReconnection(roomID string) {
	rm.mu.RLock()
	conn, exists := rm.connections[roomID]
	rm.mu.RUnlock()

	if !exists {
		logger.Error("RoomManager", "Room '%s': Cannot reconnect - room not found", roomID)
		return
	}

	// Check if reconnection is already in progress
	conn.mu.Lock()
	if conn.reconnectInProgress {
		conn.mu.Unlock()
		logger.Info("RoomManager", "Room '%s': Reconnection already in progress, skipping", roomID)
		return
	}
	conn.reconnectInProgress = true
	conn.mu.Unlock()

	// Ensure we clear the flag when done
	defer func() {
		conn.mu.Lock()
		conn.reconnectInProgress = false
		conn.mu.Unlock()
	}()

	// Check cooldown period
	conn.mu.RLock()
	timeSinceLastReconnect := time.Since(conn.LastReconnect)
	cooldownMinutes := time.Duration(conn.Room.CooldownMinutes) * time.Minute
	conn.mu.RUnlock()

	if timeSinceLastReconnect < cooldownMinutes {
		logger.Info("RoomManager", "Room '%s': Still in cooldown period (%v remaining)",
			roomID, cooldownMinutes-timeSinceLastReconnect)
		return
	}

	// Update reconnection tracking
	conn.mu.Lock()
	conn.Connected = false
	conn.LastReconnect = time.Now()
	conn.ReconnectAttempt++
	conn.mu.Unlock()

	logger.Info("RoomManager", "Room '%s': Attempting reconnection (attempt %d)", roomID, conn.ReconnectAttempt)
	if err := rm.StartRoom(roomID); err != nil {
		logger.Error("RoomManager", "Room '%s': Reconnection failed: %v", roomID, err)
	}
}

// createEventMetadata creates appropriate metadata for a Cytube event
func (rm *RoomManager) createEventMetadata(eventType string, roomID string) *framework.EventMetadata {
	metadata := framework.NewEventMetadata("core", fmt.Sprintf("cytube.event.%s", eventType))

	// Add room ID as a tag
	metadata.WithTags("room:" + roomID)

	// Set tags and logging based on event type
	switch eventType {
	// Chat Events
	case "chatMsg":
		metadata.WithTags("chat", "public", "user-content")
		metadata.WithLogging("info")
		metadata.WithPriority(0)
	case "pm":
		metadata.WithTags("chat", "private", "user-content")
		metadata.WithLogging("info")
		metadata.WithPriority(1) // Higher priority for PMs

	// User Events
	case "userJoin":
		metadata.WithTags("user", "presence", "join")
		metadata.WithLogging("info")
	case "userLeave":
		metadata.WithTags("user", "presence", "leave")
		metadata.WithLogging("info")
	case "setAFK":
		metadata.WithTags("user", "status")
	case "setUserRank":
		metadata.WithTags("user", "permission")
		metadata.WithLogging("info")
	case "setUserMeta":
		metadata.WithTags("user", "metadata")

	// Media Events
	case "changeMedia":
		metadata.WithTags("media", "playlist", "change")
		metadata.WithLogging("info")
	case "mediaUpdate":
		metadata.WithTags("media", "sync", "playback")
		// Don't log these by default as they're very frequent
	case "playlist":
		metadata.WithTags("media", "playlist", "update")
	case "queue":
		metadata.WithTags("media", "playlist", "queue")
		metadata.WithLogging("info")
	case "queueFail":
		metadata.WithTags("media", "playlist", "error")
		metadata.WithLogging("warn")
		metadata.WithPriority(2)
	case "moveVideo":
		metadata.WithTags("media", "playlist", "reorder")
	case "setCurrent":
		metadata.WithTags("media", "playlist", "current")
	case "setPlaylistLocked":
		metadata.WithTags("media", "playlist", "permission")
		metadata.WithLogging("info")
	case "setPlaylistMeta":
		metadata.WithTags("media", "playlist", "metadata")

	// Channel Events
	case "channelOpts":
		metadata.WithTags("channel", "config")
		metadata.WithLogging("info")
	case "channelCSSJS":
		metadata.WithTags("channel", "style")
	case "setMotd":
		metadata.WithTags("channel", "announcement")
		metadata.WithLogging("info")
	case "setPermissions":
		metadata.WithTags("channel", "permission")
		metadata.WithLogging("warn")
		metadata.WithPriority(2)
	case "meta":
		metadata.WithTags("channel", "metadata")

	// System Events
	case "login":
		metadata.WithTags("system", "auth", "login")
		metadata.WithLogging("info")
		metadata.WithPriority(2)
	case "addUser":
		metadata.WithTags("system", "user", "registration")
		metadata.WithLogging("info")
		metadata.WithPriority(2)
	case "delete":
		metadata.WithTags("system", "moderation")
		metadata.WithLogging("warn")
		metadata.WithPriority(3)
	case "clearVoteskipVote":
		metadata.WithTags("system", "voting")
	case "disconnect":
		metadata.WithTags("system", "connection", "disconnect")
		metadata.WithLogging("warn")
		metadata.WithPriority(2)

	// Default for unknown events
	default:
		metadata.WithTags("unknown")
		metadata.WithLogging("debug")
	}

	return metadata
}

// checkConnections checks all connections and reconnects if needed
func (rm *RoomManager) checkConnections() {
	rm.mu.RLock()
	connections := make([]*RoomConnection, 0, len(rm.connections))
	for _, conn := range rm.connections {
		connections = append(connections, conn)
	}
	rm.mu.RUnlock()

	for _, conn := range connections {
		conn.mu.RLock()
		roomID := conn.Room.ID
		connected := conn.Connected
		lastMediaUpdate := conn.LastMediaUpdate
		conn.mu.RUnlock()

		// Check if we need to reconnect
		needsReconnect := false

		// Also check the actual client connection state
		clientConnected := conn.Client.IsConnected()

		if !connected || !clientConnected {
			needsReconnect = true
			logger.Warn("RoomManager", "Room '%s': Not connected (manager: %v, client: %v), will attempt reconnection",
				roomID, connected, clientConnected)
		} else if time.Since(lastMediaUpdate) > 5*time.Minute {
			needsReconnect = true
			logger.Warn("RoomManager", "Room '%s': No MediaUpdate for %v, assuming disconnected",
				roomID, time.Since(lastMediaUpdate))
		}

		if needsReconnect {
			logger.Info("RoomManager", "Room '%s': Triggering reconnection via handleReconnection", roomID)
			go rm.handleReconnection(roomID)
		}
	}
}
