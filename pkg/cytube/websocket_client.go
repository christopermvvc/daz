package cytube

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
}

// NewWebSocketClient creates a new WebSocket client for cytube
func NewWebSocketClient(channel string, eventChan chan<- framework.Event) (*WebSocketClient, error) {
	// Discover the server URL for this channel
	serverURL, err := DiscoverServer(channel)
	if err != nil {
		return nil, fmt.Errorf("failed to discover server: %w", err)
	}

	log.Printf("Discovered server URL for channel %s: %s", channel, serverURL)

	ctx, cancel := context.WithCancel(context.Background())

	client := &WebSocketClient{
		serverURL: serverURL,
		channel:   channel,
		eventChan: eventChan,
		parser:    NewParser(channel),
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
	log.Printf("Connecting to WebSocket URL: %s", wsURL)

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
		c.connected = false
		c.mu.Unlock()
		c.cancel()
		// Signal shutdown is complete
		select {
		case <-c.shutdownDone:
			// Already closed
		default:
			close(c.shutdownDone)
		}
	}()

	for {
		messageType, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
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
					log.Printf("Failed to set write deadline: %v", err)
					c.writeMu.Unlock()
					return
				}
				if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
					log.Printf("WebSocket write error: %v", err)
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

	switch msg[0] {
	case '0': // Connect
		// Parse session ID from handshake
		var handshake HandshakeResponse
		if err := json.Unmarshal([]byte(msg[1:]), &handshake); err == nil {
			c.sid = handshake.SessionID
			log.Printf("Received handshake, SID: %s", c.sid)
			log.Printf("Server ping interval: %dms, timeout: %dms", handshake.PingInterval, handshake.PingTimeout)

			// Send Socket.IO connect packet first
			c.writeChan <- []byte("40")

			// Note: In Engine.IO, the server sends pings and we respond with pongs
			// We should NOT send unsolicited pings from the client side
		} else {
			log.Printf("Failed to parse handshake: %v", err)
			log.Printf("Raw handshake message: %s", msg)
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
		log.Println("Socket.IO connected")
		// Signal that Socket.IO is ready and join channel
		if c.socketIOReady != nil {
			select {
			case <-c.socketIOReady:
				// Already signaled
			default:
				close(c.socketIOReady)
				go func() {
					if err := c.joinChannel(); err != nil {
						log.Printf("Failed to join channel: %v", err)
					}
				}()
			}
		}

	case '1': // Disconnect
		log.Println("Socket.IO disconnected")
		// Send disconnect event
		baseEvent := framework.CytubeEvent{
			EventType:   "disconnect",
			EventTime:   time.Now(),
			ChannelName: c.channel,
			Metadata:    make(map[string]string),
		}
		select {
		case c.eventChan <- &baseEvent:
		case <-c.ctx.Done():
		}

	case '2': // Event
		// Parse Socket.IO event
		eventType, data, err := parseSocketIOMessage(msg)
		if err != nil {
			log.Printf("Failed to parse Socket.IO message: %v", err)
			return
		}

		// Handle the event
		c.handleEvent(eventType, data)

	case '3': // Ack
		// Acknowledgment

	case '4': // Error
		log.Printf("Socket.IO error: %s", msg[1:])
	}
}

// handleEvent processes cytube events
func (c *WebSocketClient) handleEvent(eventType string, data json.RawMessage) {
	// Create Event structure for parser
	event := Event{
		Type: eventType,
		Data: data,
	}

	// Parse the event
	parsedEvent, err := c.parser.ParseEvent(event)
	if err != nil {
		log.Printf("Failed to parse event %s: %v", eventType, err)
		return
	}

	// Send to event channel
	select {
	case c.eventChan <- parsedEvent:
	case <-c.ctx.Done():
		return
	default:
		log.Printf("Event channel full, dropping event: %s", eventType)
	}
}

// joinChannel joins the specified channel
func (c *WebSocketClient) joinChannel() error {
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
		log.Printf("Sent join request for channel: %s", c.channel)
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
			log.Printf("Error setting write deadline during disconnect: %v", err)
		}
		if err := c.conn.WriteMessage(websocket.TextMessage, []byte("41")); err != nil {
			log.Printf("Error sending disconnect message: %v", err)
		}
		if err := c.conn.Close(); err != nil {
			log.Printf("Error closing WebSocket connection: %v", err)
		}
		c.conn = nil
	}

	log.Println("WebSocket disconnected")
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
	if eventType == "requestPlaylist" || eventType == "login" {
		log.Printf("[WebSocket] Sending %s command", eventType)
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
	log.Println("Waiting 2 seconds before login (per cytube protocol)...")
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
		log.Printf("Connection attempt %d failed: %v", attempts, err)

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
