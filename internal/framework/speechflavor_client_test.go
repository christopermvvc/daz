package framework

import (
	"context"
	"encoding/json"
	"testing"
)

type mockSpeechFlavorEventBus struct {
	lastRequest  *EventData
	lastMetadata *EventMetadata
	mockResponse *EventData
	mockErr      error
}

func (m *mockSpeechFlavorEventBus) Broadcast(eventType string, data *EventData) error { return nil }
func (m *mockSpeechFlavorEventBus) BroadcastWithMetadata(eventType string, data *EventData, metadata *EventMetadata) error {
	return nil
}
func (m *mockSpeechFlavorEventBus) Send(target string, eventType string, data *EventData) error {
	return nil
}
func (m *mockSpeechFlavorEventBus) SendWithMetadata(target string, eventType string, data *EventData, metadata *EventMetadata) error {
	return nil
}
func (m *mockSpeechFlavorEventBus) Request(ctx context.Context, target string, eventType string, data *EventData, metadata *EventMetadata) (*EventData, error) {
	m.lastRequest = data
	m.lastMetadata = metadata
	if m.mockResponse != nil && m.mockResponse.PluginResponse != nil && data != nil && data.PluginRequest != nil {
		m.mockResponse.PluginResponse.ID = data.PluginRequest.ID
	}
	return m.mockResponse, m.mockErr
}
func (m *mockSpeechFlavorEventBus) DeliverResponse(correlationID string, response *EventData, err error) {
}
func (m *mockSpeechFlavorEventBus) Subscribe(eventType string, handler EventHandler) error {
	return nil
}
func (m *mockSpeechFlavorEventBus) SubscribeWithTags(pattern string, handler EventHandler, tags []string) error {
	return nil
}
func (m *mockSpeechFlavorEventBus) RegisterPlugin(name string, plugin Plugin) error { return nil }
func (m *mockSpeechFlavorEventBus) UnregisterPlugin(name string) error              { return nil }
func (m *mockSpeechFlavorEventBus) GetDroppedEventCounts() map[string]int64         { return nil }
func (m *mockSpeechFlavorEventBus) GetDroppedEventCount(eventType string) int64     { return 0 }

func TestSpeechFlavorClientRewriteSuccess(t *testing.T) {
	mockBus := &mockSpeechFlavorEventBus{}
	client := NewSpeechFlavorClient(mockBus, "test-plugin")

	raw, _ := json.Marshal(SpeechFlavorRewriteResponse{
		Text:         "yeah nah mate",
		FallbackUsed: false,
		Model:        "test-model",
	})
	mockBus.mockResponse = &EventData{
		PluginResponse: &PluginResponse{
			Success: true,
			Data: &ResponseData{
				RawJSON: raw,
			},
		},
	}

	resp, err := client.Rewrite(context.Background(), SpeechFlavorRewriteRequest{
		Text: "yes no",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != "yeah nah mate" {
		t.Fatalf("unexpected text %q", resp.Text)
	}
	if mockBus.lastRequest == nil || mockBus.lastRequest.PluginRequest == nil {
		t.Fatalf("expected request to be captured")
	}
	if mockBus.lastRequest.PluginRequest.To != speechFlavorTarget {
		t.Fatalf("expected target %q, got %q", speechFlavorTarget, mockBus.lastRequest.PluginRequest.To)
	}
	if mockBus.lastRequest.PluginRequest.Type != "rewrite" {
		t.Fatalf("expected rewrite operation, got %q", mockBus.lastRequest.PluginRequest.Type)
	}
}

func TestSpeechFlavorClientRewriteStructuredError(t *testing.T) {
	mockBus := &mockSpeechFlavorEventBus{}
	client := NewSpeechFlavorClient(mockBus, "test-plugin")

	raw, _ := json.Marshal(SpeechFlavorError{
		ErrorCode: "INVALID_REQUEST",
		Message:   "missing field text",
	})
	mockBus.mockResponse = &EventData{
		PluginResponse: &PluginResponse{
			Success: false,
			Error:   "invalid",
			Data: &ResponseData{
				RawJSON: raw,
			},
		},
	}

	_, err := client.Rewrite(context.Background(), SpeechFlavorRewriteRequest{})
	if err == nil {
		t.Fatalf("expected error")
	}
	if _, ok := err.(*SpeechFlavorError); !ok {
		t.Fatalf("expected *SpeechFlavorError, got %T", err)
	}
}
