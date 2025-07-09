package cytube

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hildolfr/daz/internal/framework"
)

func TestNewWebSocketClient(t *testing.T) {
	// Mock server for discovery
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/socketconfig/test-channel.json" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"servers":[{"url":"http://test.example.com","secure":false}]}`))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	eventChan := make(chan framework.Event, 100)

	// Test with valid channel (should succeed since it hits real server)
	client, err := NewWebSocketClient("test-channel", "test-room", eventChan)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if client == nil {
		t.Error("expected client to be non-nil")
		return
	}

	// Verify client properties
	if client.channel != "test-channel" {
		t.Errorf("expected channel = test-channel, got %s", client.channel)
	}

	if client.eventChan == nil {
		t.Error("expected eventChan to be non-nil")
	}

	if client.parser == nil {
		t.Error("expected parser to be non-nil")
	}

	// Clean up
	client.cancel()
}

func TestWebSocketClient_Connect(t *testing.T) {
	// Create a test WebSocket server
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	// Channel to signal when server has completed handshake
	serverReady := make(chan struct{})
	// Channel to keep server connection open until test completes
	serverDone := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/socket.io/" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade error: %v", err)
			return
		}
		defer func() {
			_ = conn.Close()
		}()

		// Send Engine.IO handshake
		handshake := `0{"sid":"test-sid","upgrades":[],"pingInterval":25000,"pingTimeout":60000,"maxPayload":1000000}`
		if err := conn.WriteMessage(websocket.TextMessage, []byte(handshake)); err != nil {
			t.Errorf("failed to send handshake: %v", err)
			return
		}

		// Read Socket.IO connect
		_, msg, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("failed to read connect: %v", err)
			return
		}

		if string(msg) != "40" {
			t.Errorf("expected Socket.IO connect (40), got: %s", msg)
		}

		// Signal that server handshake is complete
		close(serverReady)

		// Keep connection open until test signals completion
		<-serverDone
	}))
	defer server.Close()

	// Create client with test server URL
	eventChan := make(chan framework.Event, 100)
	client := &WebSocketClient{
		serverURL: server.URL,
		channel:   "test-channel",
		roomID:    "test-room",
		eventChan: eventChan,
		parser:    NewParser("test-channel", "test-room"),
		writeChan: make(chan []byte, 100),
	}

	ctx, cancel := context.WithCancel(context.Background())
	client.ctx = ctx
	client.cancel = cancel

	// Connect should succeed now
	err := client.Connect()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	// Wait for server to complete handshake
	select {
	case <-serverReady:
		// Server has completed handshake, connection is established
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server handshake")
	}

	// Verify client is connected
	if !client.IsConnected() {
		t.Error("expected client to be connected after handshake")
	}

	// Clean up
	_ = client.Disconnect()
	close(serverDone)
}

func TestWebSocketClient_IsConnected(t *testing.T) {
	client := &WebSocketClient{
		connected: false,
	}

	if client.IsConnected() {
		t.Error("expected IsConnected to return false")
	}

	client.connected = true
	if !client.IsConnected() {
		t.Error("expected IsConnected to return true")
	}
}

func TestWebSocketClient_SetReconnectConfig(t *testing.T) {
	client := &WebSocketClient{}

	config := ReconnectConfig{
		MaxAttempts:    5,
		RetryDelay:     10 * time.Second,
		CooldownPeriod: 60 * time.Second,
	}

	client.SetReconnectConfig(&config)

	if client.reconnectConfig.MaxAttempts != 5 {
		t.Errorf("expected MaxAttempts = 5, got %d", client.reconnectConfig.MaxAttempts)
	}

	if client.reconnectConfig.RetryDelay != 10*time.Second {
		t.Errorf("expected RetryDelay = 10s, got %v", client.reconnectConfig.RetryDelay)
	}

	if client.reconnectConfig.CooldownPeriod != 60*time.Second {
		t.Errorf("expected CooldownPeriod = 60s, got %v", client.reconnectConfig.CooldownPeriod)
	}
}

func TestWebSocketClient_SendNotConnected(t *testing.T) {
	client := &WebSocketClient{
		connected: false,
	}

	err := client.Send("test", nil)
	if err == nil {
		t.Error("expected error when sending while not connected")
	}

	if err.Error() != "not connected" {
		t.Errorf("expected 'not connected' error, got: %v", err)
	}
}

func TestWebSocketClient_LoginNotConnected(t *testing.T) {
	client := &WebSocketClient{
		connected: false,
	}

	// Start timer to ensure we don't wait 2 seconds when not connected
	start := time.Now()
	err := client.Login("test", "pass")
	duration := time.Since(start)

	// Should fail immediately, not after 2 seconds
	if duration > 100*time.Millisecond {
		t.Errorf("Login took too long when not connected: %v", duration)
	}

	if err == nil {
		t.Error("expected error when logging in while not connected")
	}
}

func TestWebSocketClient_DisconnectNotConnected(t *testing.T) {
	client := &WebSocketClient{
		connected: false,
	}

	err := client.Disconnect()
	if err != nil {
		t.Errorf("expected no error when disconnecting while not connected, got: %v", err)
	}
}

func TestWebSocketClient_handleMessage(t *testing.T) {
	eventChan := make(chan framework.Event, 100)
	writeChan := make(chan []byte, 100)

	client := &WebSocketClient{
		channel:   "test-channel",
		eventChan: eventChan,
		parser:    NewParser("test-channel", "test-room"),
		writeChan: writeChan,
	}

	ctx, cancel := context.WithCancel(context.Background())
	client.ctx = ctx
	client.cancel = cancel
	defer cancel()

	// Test Engine.IO handshake
	handshakeMsg := `0{"sid":"test-sid","upgrades":[],"pingInterval":25000,"pingTimeout":60000}`
	client.handleMessage([]byte(handshakeMsg))

	if client.sid != "test-sid" {
		t.Errorf("expected sid = test-sid, got %s", client.sid)
	}

	// Should send Socket.IO connect
	select {
	case msg := <-writeChan:
		if string(msg) != "40" {
			t.Errorf("expected Socket.IO connect (40), got: %s", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected Socket.IO connect message")
	}

	// Test ping
	client.handleMessage([]byte("2"))

	// Should send pong
	select {
	case msg := <-writeChan:
		if string(msg) != "3" {
			t.Errorf("expected pong (3), got: %s", msg)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected pong message")
	}

	// Test empty message
	client.handleMessage([]byte(""))
	// Should not panic

	// Test noop
	client.handleMessage([]byte("6"))
	// Should not do anything
}

func TestWebSocketClient_handleSocketIOMessage(t *testing.T) {
	eventChan := make(chan framework.Event, 100)

	client := &WebSocketClient{
		channel:   "test-channel",
		eventChan: eventChan,
		parser:    NewParser("test-channel", "test-room"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	client.ctx = ctx
	client.cancel = cancel
	defer cancel()

	// Test Socket.IO connect
	client.handleSocketIOMessage([]byte("0"))
	// Should log but not send event

	// Test Socket.IO disconnect
	client.handleSocketIOMessage([]byte("1"))

	// Should send disconnect event
	select {
	case event := <-eventChan:
		cytubeEvent, ok := event.(*framework.CytubeEvent)
		if !ok {
			t.Errorf("expected CytubeEvent, got %T", event)
		}
		if cytubeEvent.EventType != "disconnect" {
			t.Errorf("expected disconnect event, got %s", cytubeEvent.EventType)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected disconnect event")
	}

	// Test Socket.IO error
	client.handleSocketIOMessage([]byte("4error message"))
	// Should log error

	// Test empty message
	client.handleSocketIOMessage([]byte(""))
	// Should not panic
}
