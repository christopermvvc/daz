package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

type mockDelivery struct {
	correlationID string
	response      *framework.EventData
	err           error
}

// MockEventBus implements framework.EventBus for testing
type MockEventBus struct {
	mu            sync.RWMutex
	subscriptions map[string][]framework.EventHandler
	deliveries    []mockDelivery
}

func NewMockEventBus() *MockEventBus {
	return &MockEventBus{
		subscriptions: make(map[string][]framework.EventHandler),
		deliveries:    make([]mockDelivery, 0),
	}
}

func (m *MockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscriptions[eventType] = append(m.subscriptions[eventType], handler)
	return nil
}

func (m *MockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	return m.Subscribe(pattern, handler)
}

func (m *MockEventBus) Unsubscribe(eventType string, handler framework.EventHandler) error {
	return nil
}

func (m *MockEventBus) UnsubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	return nil
}

func (m *MockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	return nil
}

func (m *MockEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return nil
}

func (m *MockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	return nil
}

func (m *MockEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return nil
}

func (m *MockEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	return nil, nil
}

func (m *MockEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deliveries = append(m.deliveries, mockDelivery{
		correlationID: correlationID,
		response:      response,
		err:           err,
	})
}

func (m *MockEventBus) GetDroppedEventCounts() map[string]int64 {
	return make(map[string]int64)
}

func (m *MockEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

func (m *MockEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	return nil
}

func (m *MockEventBus) UnregisterPlugin(name string) error {
	return nil
}

func TestNewPlugin(t *testing.T) {
	plugin := New()
	if plugin == nil {
		t.Fatal("New() returned nil")
	}

	if plugin.Name() != "ollama" {
		t.Errorf("Expected plugin name 'ollama', got '%s'", plugin.Name())
	}
}

func TestPluginInit(t *testing.T) {
	plugin := New()
	bus := NewMockEventBus()

	// Test with empty config
	err := plugin.Init(nil, bus)
	if err != nil {
		t.Fatalf("Init failed with empty config: %v", err)
	}

	// Test with custom config
	config := Config{
		OllamaURL:        "http://localhost:11434",
		Model:            "test-model",
		RateLimitSeconds: 30,
		Enabled:          true,
		BotName:          "TestBot",
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	plugin2 := New()
	err = plugin2.Init(configJSON, bus)
	if err != nil {
		t.Fatalf("Init failed with custom config: %v", err)
	}
}

func TestIsBotMentioned(t *testing.T) {
	plugin := &Plugin{
		botName: "Dazza",
	}

	tests := []struct {
		message  string
		expected bool
		name     string
	}{
		{"Hey Dazza, how are you?", true, "Direct mention"},
		{"hey dazza", true, "Lowercase mention"},
		{"@Dazza what's up", true, "@mention"},
		{"@dazza test", true, "Lowercase @mention"},
		{"Hello world", false, "No mention"},
		{"The word dazzle contains dazz", false, "Partial match in another word"},
		{"DAZZA", true, "Uppercase mention"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := plugin.isBotMentioned(tt.message)
			if result != tt.expected {
				t.Errorf("isBotMentioned(%q) = %v, expected %v", tt.message, result, tt.expected)
			}
		})
	}
}

func TestCalculateMessageHash(t *testing.T) {
	plugin := &Plugin{}

	hash1 := plugin.calculateMessageHash("channel1", "user1", "message1", 12345)
	hash2 := plugin.calculateMessageHash("channel1", "user1", "message1", 12345)
	hash3 := plugin.calculateMessageHash("channel1", "user1", "message2", 12345)

	if hash1 != hash2 {
		t.Error("Same input should produce same hash")
	}

	if hash1 == hash3 {
		t.Error("Different messages should produce different hashes")
	}

	if len(hash1) != 64 {
		t.Errorf("SHA256 hash should be 64 characters, got %d", len(hash1))
	}
}

func TestUserListManagement(t *testing.T) {
	plugin := &Plugin{
		userLists: make(map[string]map[string]bool),
	}

	// Test adding user to channel
	event := &framework.DataEvent{
		Data: &framework.EventData{
			UserJoin: &framework.UserJoinData{
				Username: "testuser",
				UserRank: 1,
				Channel:  "testchannel",
			},
		},
	}

	err := plugin.handleUserJoin(event)
	if err != nil {
		t.Fatalf("handleUserJoin failed: %v", err)
	}

	// Check if user is in channel
	if !plugin.isUserInChannel("testchannel", "testuser") {
		t.Error("User should be in channel after joining")
	}

	// Test removing user from channel
	leaveEvent := &framework.DataEvent{
		Data: &framework.EventData{
			UserLeave: &framework.UserLeaveData{
				Username: "testuser",
				Channel:  "testchannel",
			},
		},
	}

	err = plugin.handleUserLeave(leaveEvent)
	if err != nil {
		t.Fatalf("handleUserLeave failed: %v", err)
	}

	// Check if user is removed from channel
	if plugin.isUserInChannel("testchannel", "testuser") {
		t.Error("User should not be in channel after leaving")
	}
}

func TestMessageFreshness(t *testing.T) {
	plugin := &Plugin{
		config: &Config{
			Enabled: true,
		},
		userLists: map[string]map[string]bool{
			"testchannel": {"testuser": true},
		},
		botName: "Dazza",
	}

	// Create a message that's too old
	oldTime := time.Now().Add(-60 * time.Second).UnixMilli()

	event := &framework.DataEvent{
		Data: &framework.EventData{
			ChatMessage: &framework.ChatMessageData{
				Username:    "testuser",
				Message:     "Hey Dazza",
				Channel:     "testchannel",
				MessageTime: oldTime,
			},
		},
	}

	// This should not trigger a response due to message being too old
	err := plugin.handleChatMessage(event)
	if err != nil {
		t.Errorf("handleChatMessage returned error: %v", err)
	}
}

func TestSystemMessageFiltering(t *testing.T) {
	plugin := &Plugin{
		config: &Config{
			Enabled: true,
		},
		userLists: map[string]map[string]bool{
			"testchannel": {"realuser": true},
		},
		botName: "Dazza",
	}

	// Test system message
	systemEvent := &framework.DataEvent{
		Data: &framework.EventData{
			ChatMessage: &framework.ChatMessageData{
				Username:    "System",
				Message:     "Dazza joined",
				Channel:     "testchannel",
				MessageTime: time.Now().UnixMilli(),
			},
		},
	}

	err := plugin.handleChatMessage(systemEvent)
	if err != nil {
		t.Errorf("handleChatMessage returned error for system message: %v", err)
	}

	// Test empty username
	emptyEvent := &framework.DataEvent{
		Data: &framework.EventData{
			ChatMessage: &framework.ChatMessageData{
				Username:    "",
				Message:     "Dazza test",
				Channel:     "testchannel",
				MessageTime: time.Now().UnixMilli(),
			},
		},
	}

	err = plugin.handleChatMessage(emptyEvent)
	if err != nil {
		t.Errorf("handleChatMessage returned error for empty username: %v", err)
	}
}

func TestHandleEvent(t *testing.T) {
	plugin := &Plugin{}

	// HandleEvent should just return nil as this plugin uses subscriptions
	err := plugin.HandleEvent(nil)
	if err != nil {
		t.Errorf("HandleEvent should return nil, got: %v", err)
	}
}

func TestPluginStatus(t *testing.T) {
	plugin := &Plugin{
		name:    "ollama",
		running: true,
	}

	status := plugin.Status()
	if status.Name != "ollama" {
		t.Errorf("Expected status name 'ollama', got '%s'", status.Name)
	}

	if status.State != "running" {
		t.Errorf("Expected state 'running', got '%v'", status.State)
	}

	plugin.running = false
	status = plugin.Status()
	if status.State != "stopped" {
		t.Errorf("Expected state 'stopped', got '%v'", status.State)
	}
}

func TestDependencies(t *testing.T) {
	plugin := &Plugin{}
	deps := plugin.Dependencies()

	if len(deps) != 1 {
		t.Fatalf("Expected 1 dependency, got %d", len(deps))
	}

	if deps[0] != "sql" {
		t.Errorf("Expected 'sql' dependency, got '%s'", deps[0])
	}
}

func TestHandlePluginRequestGenerateSuccess(t *testing.T) {
	plugin := New()
	ollamaPlugin, ok := plugin.(*Plugin)
	if !ok {
		t.Fatalf("New() returned %T", plugin)
	}
	mockBus := NewMockEventBus()

	serverCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls++

		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/api/chat" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var req OllamaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if req.Model != "test-model" {
			t.Errorf("expected model 'test-model', got %q", req.Model)
		}

		if len(req.Messages) != 2 {
			t.Errorf("expected 2 messages, got %d", len(req.Messages))
		}

		if req.Options.NumPredict <= 0 {
			t.Errorf("expected num_predict > 0")
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"role":"assistant","content":"G'day mate"}}`))
	}))
	defer server.Close()

	err := ollamaPlugin.Init(nil, mockBus)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	ollamaPlugin.config.OllamaURL = server.URL
	ollamaPlugin.config.Model = "test-model"

	payload := framework.OllamaGenerateRequest{
		Message:  "Hey",
		Channel:  "test-channel",
		Username: "tester",
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}

	event := &framework.DataEvent{
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				ID:   "generate-1",
				To:   "ollama",
				Type: "generate",
				Data: &framework.RequestData{
					RawJSON: rawPayload,
				},
			},
		},
	}

	if err := ollamaPlugin.handlePluginRequest(event); err != nil {
		t.Fatalf("handlePluginRequest failed: %v", err)
	}

	mockBus.mu.Lock()
	defer mockBus.mu.Unlock()
	if len(mockBus.deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(mockBus.deliveries))
	}

	delivery := mockBus.deliveries[0]
	if delivery.correlationID != "generate-1" {
		t.Errorf("expected correlation ID 'generate-1', got %q", delivery.correlationID)
	}
	if delivery.err != nil {
		t.Errorf("unexpected deliver error: %v", delivery.err)
	}

	resp := delivery.response
	if resp == nil || resp.PluginResponse == nil {
		t.Fatal("expected plugin response")
	}

	if !resp.PluginResponse.Success {
		t.Fatalf("expected successful plugin response, got %s", resp.PluginResponse.Error)
	}
	if resp.PluginResponse.From != "ollama" {
		t.Errorf("expected response from 'ollama', got '%s'", resp.PluginResponse.From)
	}

	var genResp framework.OllamaGenerateResponse
	if err := json.Unmarshal(resp.PluginResponse.Data.RawJSON, &genResp); err != nil {
		t.Fatalf("unmarshal response payload failed: %v", err)
	}
	if genResp.Text != "G'day mate" {
		t.Errorf("expected text 'G'day mate', got %q", genResp.Text)
	}
	if genResp.Model != "test-model" {
		t.Errorf("expected model 'test-model', got %q", genResp.Model)
	}

	if serverCalls != 1 {
		t.Fatalf("expected 1 call to ollama endpoint, got %d", serverCalls)
	}
}

func TestHandlePluginRequestUnsupportedOperation(t *testing.T) {
	plugin := New()
	ollamaPlugin, ok := plugin.(*Plugin)
	if !ok {
		t.Fatalf("New() returned %T", plugin)
	}
	mockBus := NewMockEventBus()

	if err := ollamaPlugin.Init(nil, mockBus); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	event := &framework.DataEvent{
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				ID:   "unsupported-1",
				To:   "ollama",
				Type: "invalid-op",
				Data: &framework.RequestData{
					KeyValue: map[string]string{
						"message": "ignored",
					},
				},
			},
		},
	}

	if err := ollamaPlugin.handlePluginRequest(event); err != nil {
		t.Fatalf("handlePluginRequest failed: %v", err)
	}

	mockBus.mu.Lock()
	defer mockBus.mu.Unlock()
	if len(mockBus.deliveries) != 1 {
		t.Fatalf("expected 1 response, got %d", len(mockBus.deliveries))
	}

	response := mockBus.deliveries[0].response
	if response == nil || response.PluginResponse == nil {
		t.Fatal("expected plugin response")
	}
	if response.PluginResponse.Success {
		t.Fatal("expected failure for unsupported operation")
	}

	var errPayload map[string]interface{}
	if err := json.Unmarshal(response.PluginResponse.Data.RawJSON, &errPayload); err != nil {
		t.Fatalf("failed to unmarshal error payload: %v", err)
	}
	if errPayload["error_code"] != errorCodeUnsupportedOp {
		t.Fatalf("expected error_code %s, got %v", errorCodeUnsupportedOp, errPayload["error_code"])
	}
}

func TestHandlePluginRequestGenerateMissingMessage(t *testing.T) {
	plugin := New()
	ollamaPlugin, ok := plugin.(*Plugin)
	if !ok {
		t.Fatalf("New() returned %T", plugin)
	}
	mockBus := NewMockEventBus()

	if err := ollamaPlugin.Init(nil, mockBus); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	payload := framework.OllamaGenerateRequest{
		Channel:  "test-channel",
		Username: "tester",
		// Message intentionally omitted/empty to trigger validation error
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}

	event := &framework.DataEvent{
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				ID:   "missing-message",
				To:   "ollama",
				Type: "generate",
				Data: &framework.RequestData{
					RawJSON: rawPayload,
				},
			},
		},
	}

	if err := ollamaPlugin.handlePluginRequest(event); err != nil {
		t.Fatalf("handlePluginRequest failed: %v", err)
	}

	mockBus.mu.Lock()
	defer mockBus.mu.Unlock()
	if len(mockBus.deliveries) != 1 {
		t.Fatalf("expected 1 response, got %d", len(mockBus.deliveries))
	}

	response := mockBus.deliveries[0].response
	if response == nil || response.PluginResponse == nil {
		t.Fatal("expected plugin response")
	}
	if response.PluginResponse.Success {
		t.Fatal("expected failure for missing message")
	}

	var errPayload map[string]interface{}
	if err := json.Unmarshal(response.PluginResponse.Data.RawJSON, &errPayload); err != nil {
		t.Fatalf("failed to unmarshal error payload: %v", err)
	}
	if errPayload["error_code"] != errorCodeInvalidRequest {
		t.Fatalf("expected error_code %s, got %v", errorCodeInvalidRequest, errPayload["error_code"])
	}
	if errPayload["message"] != "missing field message" {
		t.Fatalf("expected error message 'missing field message', got %v", errPayload["message"])
	}
}
