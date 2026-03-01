package framework

import (
	"context"
	"encoding/json"
	"testing"
)

type mockEconomyEventBus struct {
	lastRequest  *EventData
	lastMetadata *EventMetadata
	mockResponse *EventData
	mockErr      error
}

func (m *mockEconomyEventBus) Broadcast(eventType string, data *EventData) error { return nil }
func (m *mockEconomyEventBus) BroadcastWithMetadata(eventType string, data *EventData, metadata *EventMetadata) error {
	return nil
}
func (m *mockEconomyEventBus) Send(target string, eventType string, data *EventData) error {
	return nil
}
func (m *mockEconomyEventBus) SendWithMetadata(target string, eventType string, data *EventData, metadata *EventMetadata) error {
	return nil
}
func (m *mockEconomyEventBus) Request(ctx context.Context, target string, eventType string, data *EventData, metadata *EventMetadata) (*EventData, error) {
	m.lastRequest = data
	m.lastMetadata = metadata
	if m.mockResponse != nil && m.mockResponse.PluginResponse != nil && data.PluginRequest != nil {
		m.mockResponse.PluginResponse.ID = data.PluginRequest.ID
	}
	return m.mockResponse, m.mockErr
}
func (m *mockEconomyEventBus) DeliverResponse(correlationID string, response *EventData, err error) {}
func (m *mockEconomyEventBus) Subscribe(eventType string, handler EventHandler) error               { return nil }
func (m *mockEconomyEventBus) SubscribeWithTags(pattern string, handler EventHandler, tags []string) error {
	return nil
}
func (m *mockEconomyEventBus) RegisterPlugin(name string, plugin Plugin) error { return nil }
func (m *mockEconomyEventBus) UnregisterPlugin(name string) error              { return nil }
func (m *mockEconomyEventBus) GetDroppedEventCounts() map[string]int64         { return nil }
func (m *mockEconomyEventBus) GetDroppedEventCount(eventType string) int64     { return 0 }

func TestEconomyClient_CorrelationMirrored(t *testing.T) {
	mockBus := &mockEconomyEventBus{}
	client := NewEconomyClient(mockBus, "test-plugin")

	// Set up mock response
	respData, _ := json.Marshal(GetBalanceResponse{
		Channel:  "test-channel",
		Username: "alice",
		Balance:  1000,
	})
	mockBus.mockResponse = &EventData{
		PluginResponse: &PluginResponse{
			Success: true,
			Data: &ResponseData{
				RawJSON: respData,
			},
		},
	}

	ctx := context.Background()
	_, err := client.GetBalance(ctx, "test-channel", "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify correlation ID mirroring
	if mockBus.lastMetadata.CorrelationID == "" {
		t.Error("expected non-empty correlation ID in metadata")
	}
	if mockBus.lastRequest.PluginRequest.ID == "" {
		t.Error("expected non-empty correlation ID in plugin request")
	}
	if mockBus.lastMetadata.CorrelationID != mockBus.lastRequest.PluginRequest.ID {
		t.Errorf("correlation ID mismatch: metadata=%s, request=%s",
			mockBus.lastMetadata.CorrelationID, mockBus.lastRequest.PluginRequest.ID)
	}
}

func TestEconomyClient_IdempotencyDefault(t *testing.T) {
	mockBus := &mockEconomyEventBus{}
	client := NewEconomyClient(mockBus, "test-plugin")

	// Set up mock response for Credit
	respData, _ := json.Marshal(CreditResponse{
		Channel:        "test-channel",
		Username:       "alice",
		Amount:         100,
		BalanceBefore:  1000,
		BalanceAfter:   1100,
		IdempotencyKey: "auto-gen-key",
		AlreadyApplied: false,
	})
	mockBus.mockResponse = &EventData{
		PluginResponse: &PluginResponse{
			Success: true,
			Data: &ResponseData{
				RawJSON: respData,
			},
		},
	}

	ctx := context.Background()
	_, err := client.Credit(ctx, CreditRequest{
		Channel:  "test-channel",
		Username: "alice",
		Amount:   100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify idempotency key was defaulted to correlation ID
	var req CreditRequest
	if err := json.Unmarshal(mockBus.lastRequest.PluginRequest.Data.RawJSON, &req); err != nil {
		t.Fatalf("failed to unmarshal request: %v", err)
	}

	correlationID := mockBus.lastRequest.PluginRequest.ID
	if req.IdempotencyKey == "" {
		t.Error("expected idempotency key to be populated")
	}
	if req.IdempotencyKey != correlationID {
		t.Errorf("expected idempotency key to match correlation ID, got %s, want %s",
			req.IdempotencyKey, correlationID)
	}
}

func TestEconomyClient_ErrorHandling(t *testing.T) {
	mockBus := &mockEconomyEventBus{}
	client := NewEconomyClient(mockBus, "test-plugin")

	// Set up structured error response
	errData, _ := json.Marshal(EconomyError{
		ErrorCode: "INSUFFICIENT_FUNDS",
		Message:   "not enough money",
	})
	mockBus.mockResponse = &EventData{
		PluginResponse: &PluginResponse{
			Success: false,
			Error:   "insufficient funds",
			Data: &ResponseData{
				RawJSON: errData,
			},
		},
	}

	ctx := context.Background()
	_, err := client.Debit(ctx, DebitRequest{
		Channel:  "test-channel",
		Username: "alice",
		Amount:   5000,
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	econErr, ok := err.(*EconomyError)
	if !ok {
		t.Fatalf("expected *EconomyError, got %T", err)
	}

	if econErr.ErrorCode != "INSUFFICIENT_FUNDS" {
		t.Errorf("expected error code INSUFFICIENT_FUNDS, got %s", econErr.ErrorCode)
	}
}

func TestEconomyClient_TransferHappyPath(t *testing.T) {
	mockBus := &mockEconomyEventBus{}
	client := NewEconomyClient(mockBus, "test-plugin")

	// Set up mock response for Transfer
	respData, _ := json.Marshal(TransferResponse{
		Channel:           "test-channel",
		FromUsername:      "alice",
		ToUsername:        "bob",
		Amount:            50,
		FromBalanceBefore: 1000,
		FromBalanceAfter:  950,
		ToBalanceBefore:   100,
		ToBalanceAfter:    150,
		IdempotencyKey:    "xfer-key",
		AlreadyApplied:    false,
	})
	mockBus.mockResponse = &EventData{
		PluginResponse: &PluginResponse{
			Success: true,
			Data: &ResponseData{
				RawJSON: respData,
			},
		},
	}

	ctx := context.Background()
	resp, err := client.Transfer(ctx, TransferRequest{
		Channel:      "test-channel",
		FromUsername: "alice",
		ToUsername:   "bob",
		Amount:       50,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.FromBalanceAfter != 950 || resp.ToBalanceAfter != 150 {
		t.Errorf("unexpected balances in response: from=%d, to=%d", resp.FromBalanceAfter, resp.ToBalanceAfter)
	}

	// Verify request type
	if mockBus.lastRequest.PluginRequest.Type != "economy.transfer" {
		t.Errorf("expected type economy.transfer, got %s", mockBus.lastRequest.PluginRequest.Type)
	}
}
