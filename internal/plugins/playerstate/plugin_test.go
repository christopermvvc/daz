package playerstate

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
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

	m.deliveries = append(m.deliveries, mockDelivery{
		correlationID: correlationID,
		response:      response,
		err:           err,
	})
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
	getFn    func(ctx context.Context, channel, username string) (PlayerState, error)
	setFn    func(ctx context.Context, channel, username string, bladder, alcohol, weed, food, lust *int64) (PlayerState, error)
	adjustFn func(ctx context.Context, channel, username string, bladder, alcohol, weed, food, lust *int64) (PlayerState, error)
}

func (s *stubStore) Get(ctx context.Context, channel, username string) (PlayerState, error) {
	if s.getFn == nil {
		panic("unexpected Get call")
	}
	return s.getFn(ctx, channel, username)
}

func (s *stubStore) Set(ctx context.Context, channel, username string, state PlayerState) (PlayerState, error) {
	return s.SetFields(ctx, channel, username, &state.Bladder, &state.Alcohol, &state.Weed, &state.Food, &state.Lust)
}

func (s *stubStore) Adjust(ctx context.Context, channel, username string, delta PlayerState) (PlayerState, error) {
	return s.AdjustFields(ctx, channel, username, &delta.Bladder, &delta.Alcohol, &delta.Weed, &delta.Food, &delta.Lust)
}

func (s *stubStore) SetFields(ctx context.Context, channel, username string, bladder, alcohol, weed, food, lust *int64) (PlayerState, error) {
	if s.setFn == nil {
		panic("unexpected SetFields call")
	}
	return s.setFn(ctx, channel, username, bladder, alcohol, weed, food, lust)
}

func (s *stubStore) AdjustFields(ctx context.Context, channel, username string, bladder, alcohol, weed, food, lust *int64) (PlayerState, error) {
	if s.adjustFn == nil {
		panic("unexpected AdjustFields call")
	}
	return s.adjustFn(ctx, channel, username, bladder, alcohol, weed, food, lust)
}

func TestPlayerStatePlugin_IgnoresOtherTargets(t *testing.T) {
	plugin, bus := setupStartedPlugin(t, &stubStore{})

	event := framework.NewDataEvent(eventbus.EventPluginRequest, &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			ID:   "req-1",
			From: "tester",
			To:   "not-playerstate",
			Type: operationGetPlayerState,
			Data: &framework.RequestData{RawJSON: []byte(`{"channel":"room-a","username":"alice"}`)},
		},
	})

	handler := getPluginRequestHandler(t, bus)
	if err := handler(event); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	if len(bus.deliveries) != 0 {
		t.Fatalf("expected no DeliverResponse calls, got %d", len(bus.deliveries))
	}

	if plugin.Name() != pluginName {
		t.Fatalf("unexpected plugin name: %s", plugin.Name())
	}
}

func TestPlayerStatePlugin_RejectsMissingCorrelation(t *testing.T) {
	_, bus := setupStartedPlugin(t, &stubStore{})

	event := framework.NewDataEvent(eventbus.EventPluginRequest, &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			ID:   "",
			From: "tester",
			To:   pluginName,
			Type: operationGetPlayerState,
			Data: &framework.RequestData{RawJSON: []byte(`{"channel":"room-a","username":"alice"}`)},
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

func TestPlayerStatePlugin_UnknownOperation(t *testing.T) {
	_, bus := setupStartedPlugin(t, &stubStore{})
	delivery := performRequest(t, bus, "req-unknown-op", "playerstate.nope", map[string]interface{}{})
	if getErrorCode(t, delivery) != errorCodeInvalidArgument {
		t.Fatalf("expected INVALID_ARGUMENT, got %s", getErrorCode(t, delivery))
	}
}

func TestPlayerStatePlugin_GetSuccess(t *testing.T) {
	store := &stubStore{
		getFn: func(_ context.Context, channel, username string) (PlayerState, error) {
			if channel != "room-a" {
				t.Fatalf("expected normalized channel room-a, got %q", channel)
			}
			if username != "alice" {
				t.Fatalf("expected normalized username alice, got %q", username)
			}
			return PlayerState{Bladder: 5, Alcohol: 3, Weed: 2, Food: 1, Lust: 7}, nil
		},
	}

	_, bus := setupStartedPlugin(t, store)
	delivery := performRequest(t, bus, "state-get", operationGetPlayerState, map[string]interface{}{
		"channel":  " room-a ",
		"username": " Alice ",
	})
	payload := getRawPayload(t, delivery)

	if payload["channel"].(string) != "room-a" || payload["username"].(string) != "alice" {
		t.Fatalf("unexpected identity payload: %#v", payload)
	}
	if payload["bladder"].(float64) != 5 || payload["alcohol"].(float64) != 3 ||
		payload["weed"].(float64) != 2 || payload["food"].(float64) != 1 ||
		payload["lust"].(float64) != 7 {
		t.Fatalf("unexpected state payload: %#v", payload)
	}
}

func TestPlayerStatePlugin_SetSuccess(t *testing.T) {
	store := &stubStore{
		setFn: func(_ context.Context, channel, username string, bladder, alcohol, weed, food, lust *int64) (PlayerState, error) {
			if channel != "room-a" {
				t.Fatalf("expected channel room-a, got %q", channel)
			}
			if username != "alice" {
				t.Fatalf("expected username alice, got %q", username)
			}
			if bladder == nil || *bladder != 9 {
				t.Fatalf("expected bladder 9, got %#v", bladder)
			}
			if alcohol == nil || *alcohol != 8 {
				t.Fatalf("expected alcohol 8, got %#v", alcohol)
			}
			return PlayerState{Bladder: 9, Alcohol: 8, Weed: 0, Food: 0, Lust: 1}, nil
		},
	}

	_, bus := setupStartedPlugin(t, store)
	bladder := int64(9)
	alcohol := int64(8)
	delivery := performRequest(t, bus, "state-set", operationSetPlayerState, map[string]interface{}{
		"channel":  "room-a",
		"username": "alice",
		"bladder":  bladder,
		"alcohol":  alcohol,
	})
	payload := getRawPayload(t, delivery)

	if payload["bladder"].(float64) != 9 || payload["alcohol"].(float64) != 8 || payload["lust"].(float64) != 1 {
		t.Fatalf("unexpected payload after set: %#v", payload)
	}
}

func TestPlayerStatePlugin_AdjustSuccess(t *testing.T) {
	store := &stubStore{
		adjustFn: func(_ context.Context, channel, username string, bladder, alcohol, weed, food, lust *int64) (PlayerState, error) {
			if bladder == nil || *bladder != -5 {
				t.Fatalf("expected delta -5, got %#v", bladder)
			}
			return PlayerState{Bladder: 0, Alcohol: 4, Weed: 2, Food: 0, Lust: 0}, nil
		},
	}

	_, bus := setupStartedPlugin(t, store)
	bladder := int64(-5)
	delivery := performRequest(t, bus, "state-adjust", operationAdjustPlayerState, map[string]interface{}{
		"channel":  "room-a",
		"username": "alice",
		"bladder":  bladder,
	})
	payload := getRawPayload(t, delivery)

	if payload["bladder"].(float64) != 0 || payload["lust"].(float64) != 0 {
		t.Fatalf("expected clamped bladder 0, got %#v", payload["bladder"])
	}
}

func TestPlayerStateHelpers(t *testing.T) {
	if got := normalizeChannel(" TestRoom "); got != "TestRoom" {
		t.Fatalf("normalizeChannel() = %q, want %q", got, "TestRoom")
	}
	if got := normalizeUsername(" Alice "); got != "alice" {
		t.Fatalf("normalizeUsername() = %q, want %q", got, "alice")
	}

	payload := playerStateToPayload("room", "alice", PlayerState{
		Bladder: 1,
		Alcohol: 2,
		Weed:    3,
		Food:    4,
		Lust:    5,
	})
	expected := map[string]interface{}{
		"channel":  "room",
		"username": "alice",
		"bladder":  int64(1),
		"alcohol":  int64(2),
		"weed":     int64(3),
		"food":     int64(4),
		"lust":     int64(5),
	}
	if !reflect.DeepEqual(payload, expected) {
		t.Fatalf("playerStateToPayload = %#v, want %#v", payload, expected)
	}

	for _, code := range []string{
		errorCodeInvalidArgument,
		errorCodeDBUnavailable,
		errorCodeDBError,
		errorCodeInternal,
		"UNKNOWN",
	} {
		if got := operationErrorMessage(code, operationGetPlayerState); got == "" {
			t.Fatalf("expected message for error code %s", code)
		}
	}

	if got := mapStoreErrorCode(&StoreError{}); got != errorCodeDBUnavailable {
		t.Fatalf("expected DB_UNAVAILABLE for empty store error, got %s", got)
	}
	if got := mapStoreErrorCode(fmt.Errorf("boom")); got != errorCodeDBUnavailable {
		t.Fatalf("expected DB_UNAVAILABLE for generic error, got %s", got)
	}
}

func TestPlayerStatePlugin_MissingFields(t *testing.T) {
	_, bus := setupStartedPlugin(t, &stubStore{
		setFn: func(_ context.Context, channel, username string, bladder, alcohol, weed, food, lust *int64) (PlayerState, error) {
			t.Fatalf("unexpected set call")
			return PlayerState{}, nil
		},
		adjustFn: func(_ context.Context, channel, username string, bladder, alcohol, weed, food, lust *int64) (PlayerState, error) {
			t.Fatalf("unexpected adjust call")
			return PlayerState{}, nil
		},
	})

	cases := []struct {
		name    string
		op      string
		payload map[string]interface{}
	}{
		{name: "set_missing_channel", op: operationSetPlayerState, payload: map[string]interface{}{"username": "alice", "bladder": 1}},
		{name: "set_missing_username", op: operationSetPlayerState, payload: map[string]interface{}{"channel": "room-a", "bladder": 1}},
		{name: "adjust_missing_identity", op: operationAdjustPlayerState, payload: map[string]interface{}{"channel": "room-a", "bladder": 1}},
		{name: "set_no_fields", op: operationSetPlayerState, payload: map[string]interface{}{"channel": "room-a", "username": "alice"}},
		{name: "adjust_no_fields", op: operationAdjustPlayerState, payload: map[string]interface{}{"channel": "room-a", "username": "alice"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			delivery := performRequest(t, bus, tc.name, tc.op, tc.payload)
			if delivery.err != nil {
				t.Fatalf("expected plugin response error nil, got %v", delivery.err)
			}
			payload := getRawPayload(t, delivery)
			if payload["error_code"].(string) != errorCodeInvalidArgument {
				t.Fatalf("expected INVALID_ARGUMENT, got %#v", payload["error_code"])
			}
			if msg, ok := payload["message"].(string); !ok || msg == "" {
				t.Fatalf("expected message in response, got %#v", payload["message"])
			}
		})
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
		t.Fatal("missing response payload")
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

func TestPlayerStatePlugin_InvalidJSONReturnsInvalidArgument(t *testing.T) {
	_, bus := setupStartedPlugin(t, &stubStore{})

	for _, operation := range []string{operationGetPlayerState, operationSetPlayerState, operationAdjustPlayerState} {
		t.Run(operation, func(t *testing.T) {
			delivery := performRequestRaw(t, bus, fmt.Sprintf("bad-json-%s", operation), operation, []byte("{"))
			if getErrorCode(t, delivery) != errorCodeInvalidArgument {
				t.Fatalf("expected INVALID_ARGUMENT, got %s", getErrorCode(t, delivery))
			}
		})
	}
}

func TestPlayerStatePlugin_StoreErrorsMapToErrorCodes(t *testing.T) {
	t.Run("get_db_error", func(t *testing.T) {
		store := &stubStore{
			getFn: func(context.Context, string, string) (PlayerState, error) {
				return PlayerState{}, &StoreError{Code: errorCodeStoreDBError, Err: fmt.Errorf("db error")}
			},
		}
		_, bus := setupStartedPlugin(t, store)

		delivery := performRequest(t, bus, "get-db-error", operationGetPlayerState, map[string]interface{}{
			"channel":  "room",
			"username": "alice",
		})
		if got := getErrorCode(t, delivery); got != errorCodeDBError {
			t.Fatalf("expected %s, got %s", errorCodeDBError, got)
		}
	})

	t.Run("set_store_error", func(t *testing.T) {
		store := &stubStore{
			setFn: func(_ context.Context, channel, username string, _, _, _, _, _ *int64) (PlayerState, error) {
				if username != "alice" {
					t.Fatalf("expected username alice, got %s", username)
				}
				if channel != "room" {
					t.Fatalf("expected channel room, got %s", channel)
				}
				return PlayerState{}, &StoreError{Code: errorCodeStoreDBUnavailable, Err: fmt.Errorf("db unavailable")}
			},
		}
		_, bus := setupStartedPlugin(t, store)

		delivery := performRequest(t, bus, "set-db-unavailable", operationSetPlayerState, map[string]interface{}{
			"channel":  "room",
			"username": "alice",
			"bladder":  12,
		})
		if got := getErrorCode(t, delivery); got != errorCodeDBUnavailable {
			t.Fatalf("expected %s, got %s", errorCodeDBUnavailable, got)
		}
	})

	t.Run("adjust_generic_error", func(t *testing.T) {
		store := &stubStore{
			adjustFn: func(_ context.Context, channel, username string, _, _, _, _, _ *int64) (PlayerState, error) {
				if username != "alice" {
					t.Fatalf("expected username alice, got %s", username)
				}
				if channel != "room" {
					t.Fatalf("expected channel room, got %s", channel)
				}
				return PlayerState{}, fmt.Errorf("some db problem")
			},
		}
		_, bus := setupStartedPlugin(t, store)

		delivery := performRequest(t, bus, "adjust-generic-error", operationAdjustPlayerState, map[string]interface{}{
			"channel":  "room",
			"username": "alice",
			"bladder":  12,
		})
		if got := getErrorCode(t, delivery); got != errorCodeDBUnavailable {
			t.Fatalf("expected %s, got %s", errorCodeDBUnavailable, got)
		}
	})
}
