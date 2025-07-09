package core

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/cytube"
)

// RoomConnection represents a connection to a single room
type RoomConnection struct {
	Room             RoomConfig
	Client           *cytube.WebSocketClient
	EventChan        chan framework.Event
	Connected        bool
	ReconnectAttempt int
	LastReconnect    time.Time
	LastMediaUpdate  time.Time
	mu               sync.RWMutex
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
		log.Printf("[RoomManager] Auto-generated room ID '%s' from channel name", room.ID)
	}

	if !room.Enabled {
		log.Printf("[RoomManager] Room '%s' is disabled, skipping", room.ID)
		return nil
	}

	if _, exists := rm.connections[room.ID]; exists {
		return fmt.Errorf("room '%s' already exists", room.ID)
	}

	// Create event channel for this room
	eventChan := make(chan framework.Event, 100)

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
	log.Printf("[RoomManager] Added room '%s' (channel: %s)", room.ID, room.Channel)

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
		log.Printf("[RoomManager] Room '%s': Client still connected, disconnecting first", roomID)
		if err := conn.Client.Disconnect(); err != nil {
			log.Printf("[RoomManager] Room '%s': Error during disconnect: %v", roomID, err)
		}
		// Wait for disconnect to complete
		time.Sleep(100 * time.Millisecond)
	}

	// Connect with retry logic
	for attempt := 0; attempt < conn.Room.ReconnectAttempts; attempt++ {
		if attempt > 0 {
			waitTime := time.Duration(attempt) * 5 * time.Second
			if waitTime > 30*time.Second {
				waitTime = 30 * time.Second
			}
			log.Printf("[RoomManager] Room '%s': Waiting %v before retry %d/%d",
				roomID, waitTime, attempt+1, conn.Room.ReconnectAttempts)
			time.Sleep(waitTime)
		}

		log.Printf("[RoomManager] Room '%s': Connecting to %s (attempt %d/%d)",
			roomID, conn.Room.Channel, attempt+1, conn.Room.ReconnectAttempts)

		if err := conn.Client.Connect(); err != nil {
			log.Printf("[RoomManager] Room '%s': Connection failed: %v", roomID, err)
			continue
		}

		conn.Connected = true
		conn.ReconnectAttempt = 0
		conn.LastMediaUpdate = time.Now() // Reset timestamp on successful connection
		log.Printf("[RoomManager] Room '%s': Connected successfully", roomID)

		// Start event processing for this room
		go rm.processRoomEvents(roomID)

		// Wait for connection to stabilize
		time.Sleep(3 * time.Second)

		// Login if credentials provided
		if conn.Room.Username != "" && conn.Room.Password != "" {
			log.Printf("[RoomManager] Room '%s': Logging in as %s", roomID, conn.Room.Username)
			if err := conn.Client.Login(conn.Room.Username, conn.Room.Password); err != nil {
				log.Printf("[RoomManager] Room '%s': Login failed: %v", roomID, err)
			} else {
				log.Printf("[RoomManager] Room '%s': Login successful", roomID)

				// Request playlist after login
				if err := conn.Client.RequestPlaylist(); err != nil {
					log.Printf("[RoomManager] Room '%s': Failed to request playlist: %v", roomID, err)
				}
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
			} else {
				// For non-chat events, pass the raw event
				eventData.RawEvent = event
			}

			if err := rm.eventBus.Broadcast(eventType, eventData); err != nil {
				log.Printf("[RoomManager] Room '%s': Failed to broadcast event: %v", roomID, err)
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
			log.Printf("[RoomManager] Room '%s': Error disconnecting: %v", roomID, err)
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

	for _, roomID := range roomIDs {
		if err := rm.StartRoom(roomID); err != nil {
			log.Printf("[RoomManager] Failed to start room '%s': %v", roomID, err)
		}
	}
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
			log.Printf("[RoomManager] Failed to stop room '%s': %v", roomID, err)
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
		Name: toUser,
		Msg:  message,
	}

	// Debug log with actual channel name
	log.Printf("[RoomManager] Sending PM via websocket - Channel: '%s', To: '%s', Message length: %d",
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
			log.Printf("[RoomManager] Room '%s': Not connected (manager: %v, client: %v), will attempt reconnection",
				roomID, connected, clientConnected)
		} else if time.Since(lastMediaUpdate) > 15*time.Second {
			needsReconnect = true
			log.Printf("[RoomManager] Room '%s': No MediaUpdate for %v, assuming disconnected",
				roomID, time.Since(lastMediaUpdate))
		}

		if needsReconnect {
			// Check cooldown period
			conn.mu.RLock()
			timeSinceLastReconnect := time.Since(conn.LastReconnect)
			cooldownMinutes := time.Duration(conn.Room.CooldownMinutes) * time.Minute
			conn.mu.RUnlock()

			if timeSinceLastReconnect < cooldownMinutes {
				log.Printf("[RoomManager] Room '%s': Still in cooldown period (%v remaining)",
					roomID, cooldownMinutes-timeSinceLastReconnect)
				continue
			}

			// Attempt reconnection
			conn.mu.Lock()
			conn.Connected = false
			conn.LastReconnect = time.Now()
			conn.ReconnectAttempt++
			conn.mu.Unlock()

			log.Printf("[RoomManager] Room '%s': Attempting reconnection", roomID)
			go func(id string) {
				if err := rm.StartRoom(id); err != nil {
					log.Printf("[RoomManager] Room '%s': Reconnection failed: %v", id, err)
				}
			}(roomID)
		}
	}
}
