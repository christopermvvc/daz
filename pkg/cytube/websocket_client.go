package cytube

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/hildolfr/daz/internal/logger"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hildolfr/daz/internal/framework"
)

// WebSocketClient implements the Client interface using WebSocket transport
type WebSocketClient struct {
	serverURL       string
	channel         string
	roomID          string // ID of the room this client belongs to
	conn            *websocket.Conn
	eventChan       chan<- framework.Event
	parser          *Parser
	connected       bool
	reconnectConfig ReconnectConfig
	mu              sync.RWMutex
	writeMu         sync.Mutex // Protects all writes to the WebSocket connection
	ctx             context.Context
	cancel          context.CancelFunc
	writeChan       chan []byte
	sid             string        // Socket.IO session ID
	socketIOReady   chan struct{} // Signals when Socket.IO connection is ready
	shutdownDone    chan struct{} // Signals when shutdown is complete
	closeOnce       sync.Once     // Ensures channels are closed only once
	readyOnce       sync.Once     // Ensures socketIOReady is closed only once
}

// NewWebSocketClient creates a new WebSocket client for cytube
func NewWebSocketClient(channel string, roomID string, eventChan chan<- framework.Event) (*WebSocketClient, error) {
	// Discover the server URL for this channel
	serverURL, err := DiscoverServer(channel)
	if err != nil {
		return nil, fmt.Errorf("failed to discover server: %w", err)
	}

	logger.Debug("WebSocketClient", "Discovered server URL for channel %s: %s", channel, serverURL)
	return newWebSocketClient(serverURL, channel, roomID, eventChan)
}

// NewWebSocketClientWithServerURL creates a new client using a known server URL.
func NewWebSocketClientWithServerURL(serverURL string, channel string, roomID string, eventChan chan<- framework.Event) (*WebSocketClient, error) {
	if serverURL == "" {
		return nil, fmt.Errorf("server URL is required")
	}

	return newWebSocketClient(serverURL, channel, roomID, eventChan)
}

func newWebSocketClient(serverURL string, channel string, roomID string, eventChan chan<- framework.Event) (*WebSocketClient, error) {
	ctx, cancel := context.WithCancel(context.Background())

	client := &WebSocketClient{
		serverURL: serverURL,
		channel:   channel,
		roomID:    roomID,
		eventChan: eventChan,
		parser:    NewParser(channel, roomID),
		reconnectConfig: ReconnectConfig{
			MaxAttempts:    10,
			RetryDelay:     5 * time.Second,
			CooldownPeriod: 30 * time.Second,
		},
		ctx:           ctx,
		cancel:        cancel,
		writeChan:     make(chan []byte, 100),
		socketIOReady: make(chan struct{}),
		shutdownDone:  make(chan struct{}),
	}

	return client, nil
}

// Connect establishes a WebSocket connection to the cytube server
func (c *WebSocketClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return fmt.Errorf("already connected")
	}

	// Reset channels for new connection
	c.socketIOReady = make(chan struct{})
	c.shutdownDone = make(chan struct{})

	// Parse the server URL to construct WebSocket URL
	u, err := url.Parse(c.serverURL)
	if err != nil {
		return fmt.Errorf("invalid server URL: %w", err)
	}

	// Convert to WebSocket scheme
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		u.Scheme = "wss"
	}

	// Add Socket.IO path and parameters
	u.Path = "/socket.io/"
	q := u.Query()
	q.Set("EIO", "4")
	q.Set("transport", "websocket")
	u.RawQuery = q.Encode()

	wsURL := u.String()
	logger.Info("WebSocketClient", "Connecting to WebSocket URL: %s", wsURL)

	// Create WebSocket connection
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	header := http.Header{}
	header.Add("Origin", c.serverURL)

	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	c.conn = conn
	c.connected = true

	// Start read and write loops
	go c.readLoop()
	go c.writeLoop()

	// Connection established
	return nil
}

// readLoop continuously reads messages from the WebSocket
func (c *WebSocketClient) readLoop() {
	defer func() {
		c.mu.Lock()
		wasConnected := c.connected
		c.connected = false
		c.mu.Unlock()

		// If we were connected and lost connection unexpectedly, trigger reconnection
		if wasConnected {
			logger.Warn("WebSocketClient", "WebSocket connection lost, triggering reconnection event")
			// Send a disconnect event to notify the system
			baseEvent := framework.CytubeEvent{
				EventType:   "disconnect",
				EventTime:   time.Now(),
				ChannelName: c.channel,
				RoomID:      c.roomID,
				Metadata:    map[string]string{"reason": "connection_lost"},
			}
			select {
			case c.eventChan <- &baseEvent:
			case <-c.ctx.Done():
			}
		}

		c.cancel()
		// Signal shutdown is complete
		c.closeOnce.Do(func() {
			close(c.shutdownDone)
		})
	}()

	for {
		messageType, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Error("WebSocketClient", "WebSocket read error: %v", err)
			}
			// Connection lost - the defer will handle triggering reconnection
			return
		}

		if messageType == websocket.TextMessage {
			c.handleMessage(message)
		}
	}
}

// writeLoop continuously writes messages to the WebSocket
func (c *WebSocketClient) writeLoop() {
	for {
		select {
		case message := <-c.writeChan:
			c.writeMu.Lock()
			if c.conn != nil {
				if err := c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
					logger.Error("WebSocketClient", "Failed to set write deadline: %v", err)
					c.writeMu.Unlock()
					return
				}
				if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
					logger.Error("WebSocketClient", "WebSocket write error: %v", err)
					c.writeMu.Unlock()
					return
				}
			}
			c.writeMu.Unlock()

		case <-c.ctx.Done():
			return
		}
	}
}

// handleMessage processes incoming WebSocket messages
func (c *WebSocketClient) handleMessage(message []byte) {
	msg := string(message)

	// Engine.IO protocol handling
	if len(msg) == 0 {
		return
	}

	// DEBUG: Log raw message to help identify user list format
	if len(msg) > 1 && msg[0] == '4' { // Engine.IO message type
		logger.Debug("WebSocketClient", "=== RAW ENGINE.IO MESSAGE ===")
		logger.Debug("WebSocketClient", "Type: %c, Length: %d", msg[0], len(msg))
		if len(msg) > 100 {
			logger.Debug("WebSocketClient", "Content (first 100 chars): %s...", msg[:100])
		} else {
			logger.Debug("WebSocketClient", "Content: %s", msg)
		}
	}

	switch msg[0] {
	case '0': // Connect
		// Parse session ID from handshake
		var handshake HandshakeResponse
		if err := json.Unmarshal([]byte(msg[1:]), &handshake); err == nil {
			c.sid = handshake.SessionID
			logger.Info("WebSocketClient", "Received handshake, SID: %s", c.sid)
			logger.Info("WebSocketClient", "Server ping interval: %dms, timeout: %dms", handshake.PingInterval, handshake.PingTimeout)

			// Send Socket.IO connect packet first
			c.writeChan <- []byte("40")

			// Note: In Engine.IO, the server sends pings and we respond with pongs
			// We should NOT send unsolicited pings from the client side
		} else {
			logger.Error("WebSocketClient", "Failed to parse handshake: %v", err)
			logger.Debug("WebSocketClient", "Raw handshake message: %s", msg)
		}

	case '2': // Ping
		// Send pong
		c.writeChan <- []byte("3")

	case '3': // Pong
		// Received pong, connection is alive

	case '4': // Message
		// Socket.IO message
		if len(msg) > 1 {
			c.handleSocketIOMessage([]byte(msg[1:]))
		}

	case '6': // Noop
		// No operation
	}
}

// handleSocketIOMessage processes Socket.IO messages
func (c *WebSocketClient) handleSocketIOMessage(message []byte) {
	msg := string(message)
	if len(msg) == 0 {
		return
	}

	switch msg[0] {
	case '0': // Connect
		logger.Info("WebSocketClient", "Socket.IO connected")
		// Signal that Socket.IO is ready but don't join channel yet
		// Channel join should happen after login
		if c.socketIOReady != nil {
			c.readyOnce.Do(func() {
				close(c.socketIOReady)
			})
		}

	case '1': // Disconnect
		logger.Info("WebSocketClient", "Socket.IO disconnected")
		// Send disconnect event with connection_lost reason to trigger reconnection
		baseEvent := framework.CytubeEvent{
			EventType:   "disconnect",
			EventTime:   time.Now(),
			ChannelName: c.channel,
			RoomID:      c.roomID,
			Metadata:    map[string]string{"reason": "connection_lost"},
		}
		select {
		case c.eventChan <- &baseEvent:
		case <-c.ctx.Done():
		}
		// Close the connection to trigger readLoop exit and reconnection
		c.mu.Lock()
		if c.conn != nil {
			_ = c.conn.Close() // Ignore error on close
		}
		c.mu.Unlock()

	case '2': // Event
		// Parse Socket.IO event
		eventType, data, err := parseSocketIOMessage(msg)
		if err != nil {
			logger.Error("WebSocketClient", "Failed to parse Socket.IO message: %v", err)
			return
		}

		// DEBUG: Log ALL events received to identify user list
		logger.Debug("WebSocketClient", "=== RECEIVED EVENT: %s ===", eventType)
		logger.Debug("WebSocketClient", "Raw data: %s", string(data))

		// Handle the event
		c.handleEvent(eventType, data)

	case '3': // Ack
		// Acknowledgment

	case '4': // Error
		logger.Error("WebSocketClient", "Socket.IO error: %s", msg[1:])
	}
}

// handleEvent processes cytube events
func (c *WebSocketClient) handleEvent(eventType string, data json.RawMessage) {
	// Log specific events we're interested in
	if eventType == "userJoin" || eventType == "userLeave" || eventType == "addUser" {
		logger.Debug("WebSocketClient", "Received %s event: %s", eventType, string(data))
	}

	// Create Event structure for parser
	event := Event{
		Type: eventType,
		Data: data,
	}

	// Parse the event
	parsedEvent, err := c.parser.ParseEvent(event)
	if err != nil {
		logger.Error("WebSocketClient", "Failed to parse event %s: %v", eventType, err)
		return
	}

	// Send to event channel
	select {
	case c.eventChan <- parsedEvent:
		if eventType == "userJoin" || eventType == "userLeave" {
			logger.Debug("WebSocketClient", "Sent %s event to event channel", eventType)
		}
	case <-c.ctx.Done():
		return
	default:
		logger.Warn("WebSocketClient", "Event channel full, dropping event: %s", eventType)
	}
}

// JoinChannel joins the specified channel
func (c *WebSocketClient) JoinChannel() error {
	joinData := ChannelJoinData{
		Name: c.channel,
	}

	joinDataJSON, err := json.Marshal(joinData)
	if err != nil {
		return fmt.Errorf("failed to marshal join data: %w", err)
	}

	message, err := formatSocketIOMessage(MessageTypeEvent, "joinChannel", joinDataJSON)
	if err != nil {
		return fmt.Errorf("failed to format join message: %w", err)
	}

	// Send the join message
	select {
	case c.writeChan <- []byte(message):
		logger.Info("WebSocketClient", "Sent join request for channel: %s", c.channel)
		return nil
	case <-c.ctx.Done():
		return fmt.Errorf("context cancelled")
	}
}

// Disconnect closes the WebSocket connection
func (c *WebSocketClient) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil
	}

	// Mark as disconnected first
	c.connected = false

	// Cancel context to stop goroutines
	c.cancel()

	// Wait for goroutines to exit with timeout
	if c.shutdownDone != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer shutdownCancel()

		select {
		case <-c.shutdownDone:
			// Graceful shutdown complete
		case <-shutdownCtx.Done():
			// Timeout reached
		}
	}

	// Lock for writing the disconnect message
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.conn != nil {
		// Send disconnect message
		if err := c.conn.SetWriteDeadline(time.Now().Add(1 * time.Second)); err != nil {
			logger.Error("WebSocketClient", "Error setting write deadline during disconnect: %v", err)
		}
		if err := c.conn.WriteMessage(websocket.TextMessage, []byte("41")); err != nil {
			logger.Error("WebSocketClient", "Error sending disconnect message: %v", err)
		}
		if err := c.conn.Close(); err != nil {
			logger.Error("WebSocketClient", "Error closing WebSocket connection: %v", err)
		}
		c.conn = nil
	}

	logger.Info("WebSocketClient", "WebSocket disconnected")
	return nil
}

// Send sends a message to the server
func (c *WebSocketClient) Send(eventType string, data EventPayload) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected || c.conn == nil {
		return fmt.Errorf("not connected")
	}

	// Marshal data to JSON
	var dataJSON json.RawMessage
	if data != nil {
		marshaled, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to marshal data: %w", err)
		}
		dataJSON = marshaled
	}

	message, err := formatSocketIOMessage(MessageTypeEvent, eventType, dataJSON)
	if err != nil {
		return fmt.Errorf("failed to format message: %w", err)
	}

	// Only log important messages, not all traffic
	if eventType == "login" {
		logger.Debug("WebSocketClient", "Sending login command with redacted payload")
	} else if eventType == "requestPlaylist" || eventType == "pm" || eventType == "queue" || eventType == "delete" {
		logger.Debug("WebSocketClient", "Sending %s command with data: %s", eventType, string(dataJSON))
	}

	select {
	case c.writeChan <- []byte(message):
		return nil
	case <-c.ctx.Done():
		return fmt.Errorf("context cancelled")
	default:
		return fmt.Errorf("write channel full")
	}
}

// Login authenticates with the server
func (c *WebSocketClient) Login(username, password string) error {
	// Check if connected first
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	// Per ***REMOVED***-connect.md, we need a 2-second delay between join and login
	logger.Info("WebSocketClient", "Waiting 2 seconds before login (per cytube protocol)...")
	loginTimer := time.NewTimer(2 * time.Second)
	select {
	case <-loginTimer.C:
		// Timer expired, continue with login
	case <-c.ctx.Done():
		loginTimer.Stop()
		return fmt.Errorf("context cancelled during login delay")
	}

	loginData := LoginData{
		Name:     username,
		Password: password,
	}

	return c.Send("login", loginData)
}

// IsConnected returns true if the client is connected
func (c *WebSocketClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// RequestPlaylist sends a request to get the full playlist
func (c *WebSocketClient) RequestPlaylist() error {
	// Check if connected first
	if !c.IsConnected() {
		return fmt.Errorf("not connected")
	}

	// Send an empty object for playlist request
	return c.Send("requestPlaylist", nil)
}

// SetReconnectConfig updates the reconnection configuration
func (c *WebSocketClient) SetReconnectConfig(config *ReconnectConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if config != nil {
		c.reconnectConfig = *config
	}
}

// ConnectWithRetry attempts to connect with retry logic
func (c *WebSocketClient) ConnectWithRetry() error {
	var lastErr error
	attempts := 0

	for attempts < c.reconnectConfig.MaxAttempts {
		attempts++

		if c.reconnectConfig.OnReconnecting != nil && attempts > 1 {
			c.reconnectConfig.OnReconnecting(attempts)
		}

		err := c.Connect()
		if err == nil {
			return nil
		}

		lastErr = err
		logger.Warn("WebSocketClient", "Connection attempt %d failed: %v", attempts, err)

		if attempts < c.reconnectConfig.MaxAttempts {
			retryTimer := time.NewTimer(c.reconnectConfig.RetryDelay)
			select {
			case <-retryTimer.C:
				// Timer expired, continue with next attempt
			case <-c.ctx.Done():
				retryTimer.Stop()
				return fmt.Errorf("context cancelled during retry: %w", c.ctx.Err())
			}
		}
	}

	// Apply cooldown if configured
	if c.reconnectConfig.CooldownPeriod > 0 && c.reconnectConfig.OnCooldown != nil {
		cooldownUntil := time.Now().Add(c.reconnectConfig.CooldownPeriod)
		c.reconnectConfig.OnCooldown(cooldownUntil)
	}

	return fmt.Errorf("failed after %d attempts: %w", attempts, lastErr)
}
