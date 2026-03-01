package economy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
)

type mockDelivery struct {
	correlationID string
	response      *framework.EventData
	err           error
}

type mockEventBus struct {
	mu          sync.Mutex
	subscribers map[string][]framework.EventHandler
	deliveries  []mockDelivery
}

func newMockEventBus() *mockEventBus {
	return &mockEventBus{subscribers: make(map[string][]framework.EventHandler)}
}

func (m *mockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	_ = eventType
	_ = data
	return nil
}

func (m *mockEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	_ = metadata
	return m.Broadcast(eventType, data)
}

func (m *mockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	_ = target
	_ = eventType
	_ = data
	return nil
}

func (m *mockEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	_ = metadata
	return m.Send(target, eventType, data)
}

func (m *mockEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	_ = ctx
	_ = target
	_ = eventType
	_ = data
	_ = metadata
	return nil, nil
}

func (m *mockEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deliveries = append(m.deliveries, mockDelivery{correlationID: correlationID, response: response, err: err})
}

func (m *mockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribers[eventType] = append(m.subscribers[eventType], handler)
	return nil
}

func (m *mockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	_ = tags
	return m.Subscribe(pattern, handler)
}

func (m *mockEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	_ = name
	_ = plugin
	return nil
}

func (m *mockEventBus) UnregisterPlugin(name string) error {
	_ = name
	return nil
}

func (m *mockEventBus) GetDroppedEventCounts() map[string]int64 {
	return map[string]int64{}
}

func (m *mockEventBus) GetDroppedEventCount(eventType string) int64 {
	_ = eventType
	return 0
}

type stubStore struct {
	getBalanceFn func(ctx context.Context, channel, username string) (GetBalanceResult, error)
	creditFn     func(ctx context.Context, channel, username string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (CreditResult, error)
	debitFn      func(ctx context.Context, channel, username string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (DebitResult, error)
	transferFn   func(ctx context.Context, channel, fromUsername, toUsername string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (TransferResult, error)
}

func (s *stubStore) GetBalance(ctx context.Context, channel, username string) (GetBalanceResult, error) {
	if s.getBalanceFn == nil {
		panic("unexpected GetBalance call")
	}
	return s.getBalanceFn(ctx, channel, username)
}

func (s *stubStore) Credit(ctx context.Context, channel, username string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (CreditResult, error) {
	if s.creditFn == nil {
		panic("unexpected Credit call")
	}
	return s.creditFn(ctx, channel, username, amount, idempotencyKey, actor, reason, metadata)
}

func (s *stubStore) Debit(ctx context.Context, channel, username string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (DebitResult, error) {
	if s.debitFn == nil {
		panic("unexpected Debit call")
	}
	return s.debitFn(ctx, channel, username, amount, idempotencyKey, actor, reason, metadata)
}

func (s *stubStore) Transfer(ctx context.Context, channel, fromUsername, toUsername string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (TransferResult, error) {
	if s.transferFn == nil {
		panic("unexpected Transfer call")
	}
	return s.transferFn(ctx, channel, fromUsername, toUsername, amount, idempotencyKey, actor, reason, metadata)
}

func TestEconomyPlugin_IgnoresOtherTargets(t *testing.T) {
	pl, bus := setupStartedPlugin(t, &stubStore{})

	event := framework.NewDataEvent(eventbus.EventPluginRequest, &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			ID:   "req-1",
			From: "tester",
			To:   "not-economy",
			Type: operationGetBalance,
		},
	})

	handler := getPluginRequestHandler(t, bus)
	if err := handler(event); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(bus.deliveries) != 0 {
		t.Fatalf("expected no DeliverResponse calls, got %d", len(bus.deliveries))
	}

	if pl.Name() != pluginName {
		t.Fatalf("unexpected plugin name: %s", pl.Name())
	}
}

func TestEconomyPlugin_RejectsMissingCorrelation(t *testing.T) {
	_, bus := setupStartedPlugin(t, &stubStore{})

	event := framework.NewDataEvent(eventbus.EventPluginRequest, &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			ID:   "",
			From: "tester",
			To:   pluginName,
			Type: operationGetBalance,
		},
	})

	handler := getPluginRequestHandler(t, bus)
	if err := handler(event); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(bus.deliveries) != 0 {
		t.Fatalf("expected no DeliverResponse calls for missing correlation ID, got %d", len(bus.deliveries))
	}
}

func TestEconomyPlugin_UnknownOperationDeliversError(t *testing.T) {
	_, bus := setupStartedPlugin(t, &stubStore{})

	requestID := "req-unknown-op"
	delivery := performRequest(t, bus, requestID, "economy.does_not_exist", map[string]interface{}{})

	if delivery.correlationID != requestID {
		t.Fatalf("expected correlation ID %q, got %q", requestID, delivery.correlationID)
	}

	if getErrorCode(t, delivery) != errorCodeInvalidArg {
		t.Fatalf("expected INVALID_ARGUMENT, got %s", getErrorCode(t, delivery))
	}
}

func TestEconomyPlugin_GetBalanceSuccess(t *testing.T) {
	store := &stubStore{
		getBalanceFn: func(ctx context.Context, channel, username string) (GetBalanceResult, error) {
			if channel != "room-a" {
				t.Fatalf("expected normalized channel room-a, got %q", channel)
			}
			if username != "alice" {
				t.Fatalf("expected normalized username alice, got %q", username)
			}
			return GetBalanceResult{OK: true, Balance: 1250}, nil
		},
	}

	_, bus := setupStartedPlugin(t, store)
	delivery := performRequest(t, bus, "bal-1", operationGetBalance, map[string]interface{}{
		"channel":  " room-a ",
		"username": " Alice ",
	})

	payload := getRawPayload(t, delivery)
	if payload["balance"].(float64) != 1250 {
		t.Fatalf("expected balance 1250, got %#v", payload["balance"])
	}
	if payload["channel"].(string) != "room-a" {
		t.Fatalf("expected channel room-a, got %#v", payload["channel"])
	}
	if payload["username"].(string) != "alice" {
		t.Fatalf("expected username alice, got %#v", payload["username"])
	}
}

func TestEconomyPlugin_CreditSuccess(t *testing.T) {
	store := &stubStore{
		creditFn: func(ctx context.Context, channel, username string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (CreditResult, error) {
			if channel != "room-a" || username != "alice" || amount != 250 {
				t.Fatalf("unexpected credit args: %q %q %d", channel, username, amount)
			}
			if idempotencyKey != "cred-1" {
				t.Fatalf("expected fallback idempotency key cred-1, got %q", idempotencyKey)
			}
			if actor != "tester" {
				t.Fatalf("expected actor tester, got %q", actor)
			}
			if reason != "daily" {
				t.Fatalf("expected reason daily, got %q", reason)
			}
			if metadata["source"].(string) != "rewards" {
				t.Fatalf("unexpected metadata: %#v", metadata)
			}
			return CreditResult{OK: true, Balance: 1500, AlreadyApplied: false}, nil
		},
	}

	_, bus := setupStartedPlugin(t, store)
	delivery := performRequest(t, bus, "cred-1", operationCredit, map[string]interface{}{
		"channel":  "room-a",
		"username": "alice",
		"amount":   250,
		"reason":   "daily",
		"metadata": map[string]interface{}{"source": "rewards"},
	})

	payload := getRawPayload(t, delivery)
	if payload["balance_before"].(float64) != 1250 || payload["balance_after"].(float64) != 1500 {
		t.Fatalf("unexpected credit balances: %#v", payload)
	}
	if payload["idempotency_key"].(string) != "cred-1" {
		t.Fatalf("expected idempotency key cred-1, got %#v", payload["idempotency_key"])
	}
}

func TestEconomyPlugin_DebitSuccess(t *testing.T) {
	store := &stubStore{
		debitFn: func(ctx context.Context, channel, username string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (DebitResult, error) {
			if idempotencyKey != "debit-key" {
				t.Fatalf("expected request idempotency key, got %q", idempotencyKey)
			}
			return DebitResult{OK: true, Balance: 700, AlreadyApplied: false}, nil
		},
	}

	_, bus := setupStartedPlugin(t, store)
	delivery := performRequest(t, bus, "deb-1", operationDebit, map[string]interface{}{
		"channel":         "room-a",
		"username":        "alice",
		"amount":          300,
		"idempotency_key": "debit-key",
	})

	payload := getRawPayload(t, delivery)
	if payload["balance_before"].(float64) != 1000 || payload["balance_after"].(float64) != 700 {
		t.Fatalf("unexpected debit balances: %#v", payload)
	}
}

func TestEconomyPlugin_TransferSuccess(t *testing.T) {
	store := &stubStore{
		transferFn: func(ctx context.Context, channel, fromUsername, toUsername string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (TransferResult, error) {
			if fromUsername != "alice" || toUsername != "bob" || amount != 50 {
				t.Fatalf("unexpected transfer args: %q %q %d", fromUsername, toUsername, amount)
			}
			return TransferResult{OK: true, FromBalance: 1450, ToBalance: 70, AlreadyApplied: false}, nil
		},
	}

	_, bus := setupStartedPlugin(t, store)
	delivery := performRequest(t, bus, "xfer-1", operationTransfer, map[string]interface{}{
		"channel":       "room-a",
		"from_username": "Alice",
		"to_username":   "Bob",
		"amount":        50,
	})

	payload := getRawPayload(t, delivery)
	if payload["from_balance_before"].(float64) != 1500 || payload["from_balance_after"].(float64) != 1450 {
		t.Fatalf("unexpected from balances: %#v", payload)
	}
	if payload["to_balance_before"].(float64) != 20 || payload["to_balance_after"].(float64) != 70 {
		t.Fatalf("unexpected to balances: %#v", payload)
	}
}

func TestEconomyPlugin_InvalidJSONReturnsInvalidArgument(t *testing.T) {
	_, bus := setupStartedPlugin(t, &stubStore{})

	operations := []string{operationGetBalance, operationCredit, operationDebit, operationTransfer}
	for _, operation := range operations {
		t.Run(operation, func(t *testing.T) {
			delivery := performRequestRaw(t, bus, fmt.Sprintf("bad-json-%s", operation), operation, []byte("{"))
			if getErrorCode(t, delivery) != errorCodeInvalidArg {
				t.Fatalf("expected INVALID_ARGUMENT, got %s", getErrorCode(t, delivery))
			}
		})
	}
}

func TestEconomyPlugin_MissingFieldsReturnInvalidArgument(t *testing.T) {
	tests := []struct {
		name      string
		op        string
		payload   map[string]interface{}
		errorCode string
	}{
		{name: "get_balance_missing_username", op: operationGetBalance, payload: map[string]interface{}{"channel": "room-a"}, errorCode: errorCodeInvalidArg},
		{name: "credit_missing_channel", op: operationCredit, payload: map[string]interface{}{"username": "alice", "amount": 5}, errorCode: errorCodeInvalidArg},
		{name: "debit_missing_username", op: operationDebit, payload: map[string]interface{}{"channel": "room-a", "amount": 5}, errorCode: errorCodeInvalidArg},
		{name: "transfer_missing_to", op: operationTransfer, payload: map[string]interface{}{"channel": "room-a", "from_username": "alice", "amount": 5}, errorCode: errorCodeInvalidArg},
	}

	_, bus := setupStartedPlugin(t, &stubStore{})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delivery := performRequest(t, bus, tt.name, tt.op, tt.payload)
			if getErrorCode(t, delivery) != tt.errorCode {
				t.Fatalf("expected %s, got %s", tt.errorCode, getErrorCode(t, delivery))
			}
		})
	}
}

func TestEconomyPlugin_InvalidAmountReturnsInvalidAmount(t *testing.T) {
	_, bus := setupStartedPlugin(t, &stubStore{})

	invalidAmountCases := []struct {
		name    string
		op      string
		payload map[string]interface{}
	}{
		{name: "credit", op: operationCredit, payload: map[string]interface{}{"channel": "room-a", "username": "alice", "amount": 0}},
		{name: "debit", op: operationDebit, payload: map[string]interface{}{"channel": "room-a", "username": "alice", "amount": -1}},
		{name: "transfer", op: operationTransfer, payload: map[string]interface{}{"channel": "room-a", "from_username": "alice", "to_username": "bob", "amount": 0}},
	}

	for _, tc := range invalidAmountCases {
		t.Run(tc.name, func(t *testing.T) {
			delivery := performRequest(t, bus, tc.name, tc.op, tc.payload)
			if getErrorCode(t, delivery) != errorCodeInvalidAmount {
				t.Fatalf("expected INVALID_AMOUNT, got %s", getErrorCode(t, delivery))
			}
		})
	}
}

func TestEconomyPlugin_InsufficientFundsMapping(t *testing.T) {
	store := &stubStore{
		debitFn: func(ctx context.Context, channel, username string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (DebitResult, error) {
			return DebitResult{OK: false, ErrorCode: errorCodeInsufficient}, nil
		},
		transferFn: func(ctx context.Context, channel, fromUsername, toUsername string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (TransferResult, error) {
			return TransferResult{OK: false, ErrorCode: errorCodeInsufficient}, nil
		},
	}

	_, bus := setupStartedPlugin(t, store)

	debitDelivery := performRequest(t, bus, "deb-ins", operationDebit, map[string]interface{}{"channel": "room-a", "username": "alice", "amount": 10})
	if getErrorCode(t, debitDelivery) != errorCodeInsufficient {
		t.Fatalf("expected INSUFFICIENT_FUNDS, got %s", getErrorCode(t, debitDelivery))
	}

	transferDelivery := performRequest(t, bus, "xfer-ins", operationTransfer, map[string]interface{}{"channel": "room-a", "from_username": "alice", "to_username": "bob", "amount": 10})
	if getErrorCode(t, transferDelivery) != errorCodeInsufficient {
		t.Fatalf("expected INSUFFICIENT_FUNDS, got %s", getErrorCode(t, transferDelivery))
	}
}

func TestEconomyPlugin_IdempotencyConflictMapping(t *testing.T) {
	store := &stubStore{
		creditFn: func(ctx context.Context, channel, username string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (CreditResult, error) {
			return CreditResult{OK: false, ErrorCode: errorCodeIdempotency}, nil
		},
		debitFn: func(ctx context.Context, channel, username string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (DebitResult, error) {
			return DebitResult{OK: false, ErrorCode: errorCodeIdempotency}, nil
		},
		transferFn: func(ctx context.Context, channel, fromUsername, toUsername string, amount int64, idempotencyKey, actor, reason string, metadata map[string]interface{}) (TransferResult, error) {
			return TransferResult{OK: false, ErrorCode: errorCodeIdempotency}, nil
		},
	}

	_, bus := setupStartedPlugin(t, store)

	creditDelivery := performRequest(t, bus, "cred-conflict", operationCredit, map[string]interface{}{"channel": "room-a", "username": "alice", "amount": 1})
	if getErrorCode(t, creditDelivery) != errorCodeIdempotency {
		t.Fatalf("expected IDEMPOTENCY_CONFLICT, got %s", getErrorCode(t, creditDelivery))
	}

	debitDelivery := performRequest(t, bus, "deb-conflict", operationDebit, map[string]interface{}{"channel": "room-a", "username": "alice", "amount": 1})
	if getErrorCode(t, debitDelivery) != errorCodeIdempotency {
		t.Fatalf("expected IDEMPOTENCY_CONFLICT, got %s", getErrorCode(t, debitDelivery))
	}

	transferDelivery := performRequest(t, bus, "xfer-conflict", operationTransfer, map[string]interface{}{"channel": "room-a", "from_username": "alice", "to_username": "bob", "amount": 1})
	if getErrorCode(t, transferDelivery) != errorCodeIdempotency {
		t.Fatalf("expected IDEMPOTENCY_CONFLICT, got %s", getErrorCode(t, transferDelivery))
	}
}

func setupStartedPlugin(t *testing.T, store Store) (*Plugin, *mockEventBus) {
	t.Helper()

	plugin, ok := New().(*Plugin)
	if !ok {
		t.Fatalf("unexpected plugin type: %T", New())
	}

	if store != nil {
		plugin.store = store
	}

	bus := newMockEventBus()
	if err := plugin.Init(nil, bus); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if err := plugin.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	return plugin, bus
}

func getPluginRequestHandler(t *testing.T, bus *mockEventBus) framework.EventHandler {
	t.Helper()

	handlers := bus.subscribers[eventbus.EventPluginRequest]
	if len(handlers) != 1 {
		t.Fatalf("expected exactly one plugin.request handler, got %d", len(handlers))
	}

	return handlers[0]
}

func performRequest(t *testing.T, bus *mockEventBus, id, operation string, payload map[string]interface{}) mockDelivery {
	t.Helper()

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	return performRequestRaw(t, bus, id, operation, rawPayload)
}

func performRequestRaw(t *testing.T, bus *mockEventBus, id, operation string, rawPayload []byte) mockDelivery {
	t.Helper()

	event := framework.NewDataEvent(eventbus.EventPluginRequest, &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			ID:   id,
			From: "tester",
			To:   pluginName,
			Type: operation,
			Data: &framework.RequestData{RawJSON: rawPayload},
		},
	})

	handler := getPluginRequestHandler(t, bus)
	if err := handler(event); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(bus.deliveries) == 0 {
		t.Fatalf("expected one delivery for %s", operation)
	}

	return bus.deliveries[len(bus.deliveries)-1]
}

func getRawPayload(t *testing.T, delivery mockDelivery) map[string]interface{} {
	t.Helper()

	if delivery.err != nil {
		t.Fatalf("expected nil delivery error, got %v", delivery.err)
	}
	if delivery.response == nil || delivery.response.PluginResponse == nil {
		t.Fatal("missing plugin response")
	}

	resp := delivery.response.PluginResponse
	if resp.Data == nil || len(resp.Data.RawJSON) == 0 {
		t.Fatal("missing response data.raw_json")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(resp.Data.RawJSON, &payload); err != nil {
		t.Fatalf("unmarshal response payload: %v", err)
	}

	return payload
}

func getErrorCode(t *testing.T, delivery mockDelivery) string {
	t.Helper()
	payload := getRawPayload(t, delivery)

	errorCode, ok := payload["error_code"].(string)
	if !ok {
		t.Fatalf("error payload missing error_code: %#v", payload)
	}

	return errorCode
}
