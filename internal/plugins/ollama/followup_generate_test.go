package ollama

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/stretchr/testify/assert"
)

func TestHandleGenerateRequestStartsFollowUpSession(t *testing.T) {
	bus := NewMockEventBus()

	plugin := New()
	ollamaPlugin, ok := plugin.(*Plugin)
	if !ok {
		t.Fatalf("New returned %T", plugin)
	}

	if err := ollamaPlugin.Init(nil, bus); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	ollamaPlugin.config.FollowUpEnabled = true
	ollamaPlugin.config.FollowUpWindowSeconds = 120

	serverCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls++
		_, err := w.Write([]byte(`{"message":{"content":"gday mate"}}`))
		if err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
	defer server.Close()
	ollamaPlugin.config.OllamaURL = server.URL

	payload := map[string]interface{}{
		"channel":          "testchannel",
		"username":         "alice",
		"message":          "hello",
		"enable_follow_up": true,
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	err = ollamaPlugin.handlePluginRequest(&framework.DataEvent{
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				ID:   "followup-session-1",
				To:   "ollama",
				From: "test",
				Type: "generate",
				Data: &framework.RequestData{
					RawJSON: rawPayload,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("handlePluginRequest returned error: %v", err)
	}

	key := ollamaPlugin.followUpSessionKey("testchannel", "alice")
	ollamaPlugin.followUpMu.RLock()
	expiresAt := ollamaPlugin.followUpSessions[key]
	ollamaPlugin.followUpMu.RUnlock()
	ollamaPlugin.followUpMu.RLock()
	_, hasSession := ollamaPlugin.followUpSessions[key]
	ollamaPlugin.followUpMu.RUnlock()
	if !hasSession {
		t.Fatalf("expected follow-up session for key %s", key)
	}
	if !expiresAt.After(time.Now()) {
		t.Fatalf("expected follow-up session expiry in future, got %v", expiresAt)
	}
	if serverCalls != 1 {
		t.Fatalf("expected 1 ollama API call, got %d", serverCalls)
	}

	deliveries := bus.deliveries
	assert.Equal(t, "followup-session-1", deliveries[0].correlationID)
}

func TestHandleGenerateRequestNoFollowUpWhenDisabled(t *testing.T) {
	bus := NewMockEventBus()

	plugin := New()
	ollamaPlugin, ok := plugin.(*Plugin)
	if !ok {
		t.Fatalf("New returned %T", plugin)
	}

	if err := ollamaPlugin.Init(nil, bus); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	ollamaPlugin.config.FollowUpEnabled = false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte(`{"message":{"content":"hey"}}`))
		if err != nil {
			t.Fatalf("failed to write response: %v", err)
		}
	}))
	defer server.Close()
	ollamaPlugin.config.OllamaURL = server.URL

	payload := map[string]interface{}{
		"channel":          "testchannel",
		"username":         "alice",
		"message":          "hello",
		"enable_follow_up": false,
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	err = ollamaPlugin.handlePluginRequest(&framework.DataEvent{
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				ID:   "followup-session-2",
				To:   "ollama",
				From: "test",
				Type: "generate",
				Data: &framework.RequestData{
					RawJSON: rawPayload,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("handlePluginRequest returned error: %v", err)
	}

	key := ollamaPlugin.followUpSessionKey("testchannel", "alice")
	ollamaPlugin.followUpMu.RLock()
	_, hasSession := ollamaPlugin.followUpSessions[key]
	ollamaPlugin.followUpMu.RUnlock()
	if hasSession {
		t.Fatalf("did not expect follow-up session for key %s", key)
	}
}
