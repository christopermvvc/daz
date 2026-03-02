package playerstate

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
)

type mockPlayerStateStoreBus struct {
	execCalls     int
	queryCalls    int
	lastExecQuery string
	lastQuery     string
	requestErr    error

	execFn  func(req *framework.SQLExecRequest) *framework.EventData
	queryFn func(req *framework.SQLQueryRequest) *framework.EventData
}

func (m *mockPlayerStateStoreBus) Broadcast(eventType string, data *framework.EventData) error {
	_ = eventType
	_ = data
	return nil
}

func (m *mockPlayerStateStoreBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	_ = eventType
	_ = data
	_ = metadata
	return nil
}

func (m *mockPlayerStateStoreBus) Send(target string, eventType string, data *framework.EventData) error {
	_ = target
	_ = eventType
	_ = data
	return nil
}

func (m *mockPlayerStateStoreBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	_ = target
	_ = eventType
	_ = data
	_ = metadata
	return nil
}

func (m *mockPlayerStateStoreBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	_ = ctx
	_ = target
	_ = eventType
	_ = metadata

	if m.requestErr != nil {
		return nil, m.requestErr
	}
	if data == nil {
		return nil, nil
	}

	if data.SQLExecRequest != nil {
		m.execCalls++
		m.lastExecQuery = data.SQLExecRequest.Query
		if m.execFn != nil {
			return m.execFn(data.SQLExecRequest), nil
		}
		return &framework.EventData{
			SQLExecResponse: &framework.SQLExecResponse{
				ID:            data.SQLExecRequest.ID,
				CorrelationID: data.SQLExecRequest.CorrelationID,
				Success:       true,
			},
		}, nil
	}

	if data.SQLQueryRequest != nil {
		m.queryCalls++
		m.lastQuery = data.SQLQueryRequest.Query
		if m.queryFn != nil {
			return m.queryFn(data.SQLQueryRequest), nil
		}
		return &framework.EventData{
			SQLQueryResponse: &framework.SQLQueryResponse{
				ID:            data.SQLQueryRequest.ID,
				CorrelationID: data.SQLQueryRequest.CorrelationID,
				Success:       true,
			},
		}, nil
	}

	return nil, nil
}

func (m *mockPlayerStateStoreBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
	_ = correlationID
	_ = response
	_ = err
}

func (m *mockPlayerStateStoreBus) Subscribe(eventType string, handler framework.EventHandler) error {
	_ = eventType
	_ = handler
	return nil
}

func (m *mockPlayerStateStoreBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	_ = pattern
	_ = handler
	_ = tags
	return nil
}

func (m *mockPlayerStateStoreBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	_ = name
	_ = plugin
	return nil
}

func (m *mockPlayerStateStoreBus) UnregisterPlugin(name string) error {
	_ = name
	return nil
}

func (m *mockPlayerStateStoreBus) GetDroppedEventCounts() map[string]int64 {
	return map[string]int64{}
}

func (m *mockPlayerStateStoreBus) GetDroppedEventCount(eventType string) int64 {
	_ = eventType
	return 0
}

func TestPlayerStateStore_NilClientReturnsDBUnavailable(t *testing.T) {
	store := NewStore(nil)

	if _, err := store.Get(context.Background(), "room", "alice"); !isStoreErrorCode(t, err, errorCodeStoreDBUnavailable) {
		t.Fatalf("expected DB_UNAVAILABLE for Get with nil client, got %v", err)
	}

	if _, err := store.Set(context.Background(), "room", "alice", PlayerState{}); !isStoreErrorCode(t, err, errorCodeStoreDBUnavailable) {
		t.Fatalf("expected DB_UNAVAILABLE for Set with nil client, got %v", err)
	}

	if _, err := store.Adjust(context.Background(), "room", "alice", PlayerState{}); !isStoreErrorCode(t, err, errorCodeStoreDBUnavailable) {
		t.Fatalf("expected DB_UNAVAILABLE for Adjust with nil client, got %v", err)
	}
}

func TestPlayerStateStore_GetSuccess(t *testing.T) {
	bus := &mockPlayerStateStoreBus{
		execFn: func(req *framework.SQLExecRequest) *framework.EventData {
			if req.Query != queryEnsurePlayerState {
				t.Fatalf("unexpected exec query: %q", req.Query)
			}
			if len(req.Params) != 2 {
				t.Fatalf("unexpected exec param count: %d", len(req.Params))
			}
			if req.Params[0].Value != "room" {
				t.Fatalf("expected channel param room, got %#v", req.Params[0].Value)
			}
			if req.Params[1].Value != "alice" {
				t.Fatalf("expected username param alice, got %#v", req.Params[1].Value)
			}

			return &framework.EventData{
				SQLExecResponse: &framework.SQLExecResponse{
					ID:            req.ID,
					CorrelationID: req.CorrelationID,
					Success:       true,
				},
			}
		},
		queryFn: func(req *framework.SQLQueryRequest) *framework.EventData {
			if req.Query != queryGetPlayerState {
				t.Fatalf("unexpected get query: %q", req.Query)
			}
			if len(req.Params) != 2 {
				t.Fatalf("unexpected query param count: %d", len(req.Params))
			}
			return &framework.EventData{
				SQLQueryResponse: &framework.SQLQueryResponse{
					ID:            req.ID,
					CorrelationID: req.CorrelationID,
					Success:       true,
					Rows: [][]json.RawMessage{
						{
							jsonInt(4),
							jsonInt(3),
							jsonInt(2),
							jsonInt(1),
							jsonInt(9),
						},
					},
				},
			}
		},
	}

	store := NewStore(framework.NewSQLClient(bus, "playerstate"))
	state, err := store.Get(context.Background(), "room", "alice")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if state.Bladder != 4 || state.Alcohol != 3 || state.Weed != 2 || state.Food != 1 || state.Lust != 9 {
		t.Fatalf("unexpected state: %#v", state)
	}
	if bus.execCalls != 1 || bus.queryCalls != 1 {
		t.Fatalf("expected one ensure and one get query, got exec=%d query=%d", bus.execCalls, bus.queryCalls)
	}
}

func TestPlayerStateStore_GetMissingRowReturnsStoreError(t *testing.T) {
	bus := &mockPlayerStateStoreBus{
		queryFn: func(req *framework.SQLQueryRequest) *framework.EventData {
			return &framework.EventData{
				SQLQueryResponse: &framework.SQLQueryResponse{
					ID:            req.ID,
					CorrelationID: req.CorrelationID,
					Success:       true,
					Rows:          [][]json.RawMessage{},
				},
			}
		},
		execFn: func(req *framework.SQLExecRequest) *framework.EventData {
			return &framework.EventData{
				SQLExecResponse: &framework.SQLExecResponse{
					ID:            req.ID,
					CorrelationID: req.CorrelationID,
					Success:       true,
				},
			}
		},
	}

	store := NewStore(framework.NewSQLClient(bus, "playerstate"))
	_, err := store.Get(context.Background(), "room", "alice")
	if !isStoreErrorCode(t, err, errorCodeStoreDBError) {
		t.Fatalf("expected DB_ERROR, got %v", err)
	}
}

func TestPlayerStateStore_SetUsesAllFields(t *testing.T) {
	bus := &mockPlayerStateStoreBus{
		queryFn: func(req *framework.SQLQueryRequest) *framework.EventData {
			if req.Query != querySetPlayerState {
				t.Fatalf("unexpected set query: %q", req.Query)
			}
			if len(req.Params) != 7 {
				t.Fatalf("unexpected param count: %d", len(req.Params))
			}
			expect := []any{"room", "alice", int64(11), int64(22), int64(33), int64(44), int64(55)}
			for i, value := range expect {
				if req.Params[i].Value != value {
					t.Fatalf("set param %d expected %#v, got %#v", i, value, req.Params[i].Value)
				}
			}
			return &framework.EventData{
				SQLQueryResponse: &framework.SQLQueryResponse{
					ID:            req.ID,
					CorrelationID: req.CorrelationID,
					Success:       true,
					Rows: [][]json.RawMessage{
						{
							jsonInt(11),
							jsonInt(22),
							jsonInt(33),
							jsonInt(44),
							jsonInt(55),
						},
					},
				},
			}
		},
	}

	store := NewStore(framework.NewSQLClient(bus, "playerstate"))
	state, err := store.Set(context.Background(), "room", "alice", PlayerState{
		Bladder: 11,
		Alcohol: 22,
		Weed:    33,
		Food:    44,
		Lust:    55,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if state.Bladder != 11 || state.Alcohol != 22 || state.Weed != 33 || state.Food != 44 || state.Lust != 55 {
		t.Fatalf("unexpected state: %#v", state)
	}
	if bus.queryCalls != 1 {
		t.Fatalf("expected one set query, got %d", bus.queryCalls)
	}
}

func TestPlayerStateStore_AdjustSupportsNilAndNegativeValues(t *testing.T) {
	bus := &mockPlayerStateStoreBus{
		queryFn: func(req *framework.SQLQueryRequest) *framework.EventData {
			if req.Query != queryAdjustPlayerState {
				t.Fatalf("unexpected adjust query: %q", req.Query)
			}
			if len(req.Params) != 7 {
				t.Fatalf("unexpected param count: %d", len(req.Params))
			}
			if req.Params[2] != (framework.SQLParam{Value: int64(-7)}) {
				t.Fatalf("expected bladder delta -7, got %#v", req.Params[2])
			}
			if req.Params[3] != (framework.SQLParam{Value: nil}) {
				t.Fatalf("expected nil alcohol delta, got %#v", req.Params[3])
			}
			if req.Params[6] != (framework.SQLParam{Value: int64(9)}) {
				t.Fatalf("expected lust delta 9, got %#v", req.Params[6])
			}
			return &framework.EventData{
				SQLQueryResponse: &framework.SQLQueryResponse{
					ID:            req.ID,
					CorrelationID: req.CorrelationID,
					Success:       true,
					Rows: [][]json.RawMessage{
						{
							jsonInt(3),
							jsonInt(2),
							jsonInt(1),
							jsonInt(0),
							jsonInt(9),
						},
					},
				},
			}
		},
	}

	store := NewStore(framework.NewSQLClient(bus, "playerstate"))
	_, err := store.AdjustFields(context.Background(), "room", "alice", int64Ptr(-7), nil, nil, nil, int64Ptr(9))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if bus.queryCalls != 1 {
		t.Fatalf("expected one adjust query, got %d", bus.queryCalls)
	}
}

func TestPlayerStateStore_MapStoreError(t *testing.T) {
	t.Run("nil_error", func(t *testing.T) {
		if err := mapStoreError(nil); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("query_failure", func(t *testing.T) {
		err := mapStoreError(errors.New("query failed for reason"))
		var se *StoreError
		if !errors.As(err, &se) || se.Code != errorCodeStoreDBError {
			t.Fatalf("expected DB_ERROR, got %v", err)
		}
	})

	t.Run("scan_error", func(t *testing.T) {
		err := mapStoreError(errors.New("scan destination count mismatch"))
		var se *StoreError
		if !errors.As(err, &se) || se.Code != errorCodeStoreDBError {
			t.Fatalf("expected DB_ERROR, got %v", err)
		}
	})

	t.Run("unrecognized_error", func(t *testing.T) {
		err := mapStoreError(errors.New("something is missing"))
		var se *StoreError
		if !errors.As(err, &se) || se.Code != errorCodeStoreDBUnavailable {
			t.Fatalf("expected DB_UNAVAILABLE, got %v", err)
		}
	})
}

func TestPlayerStateStore_SetFieldsSupportsNilValues(t *testing.T) {
	bus := &mockPlayerStateStoreBus{
		queryFn: func(req *framework.SQLQueryRequest) *framework.EventData {
			if req.Query != querySetPlayerState {
				t.Fatalf("unexpected query: %q", req.Query)
			}
			if len(req.Params) != 7 {
				t.Fatalf("unexpected param count: %d", len(req.Params))
			}
			if req.Params[2] != (framework.SQLParam{Value: nil}) {
				t.Fatalf("expected bladder to be nil, got %#v", req.Params[2])
			}
			if req.Params[3] != (framework.SQLParam{Value: int64(10)}) {
				t.Fatalf("expected alcohol 10, got %#v", req.Params[3])
			}

			return &framework.EventData{
				SQLQueryResponse: &framework.SQLQueryResponse{
					ID:            req.ID,
					CorrelationID: req.CorrelationID,
					Success:       true,
					Rows: [][]json.RawMessage{
						{
							jsonInt(1),
							jsonInt(10),
							jsonInt(0),
							jsonInt(0),
							jsonInt(0),
						},
					},
				},
			}
		},
	}

	store := NewStore(framework.NewSQLClient(bus, "playerstate"))
	state, err := store.SetFields(context.Background(), "room", "alice", nil, int64Ptr(10), nil, nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if state.Bladder != 1 || state.Alcohol != 10 {
		t.Fatalf("unexpected state: %#v", state)
	}
}

func int64Ptr(value int64) *int64 {
	return &value
}

func isStoreErrorCode(t *testing.T, err error, expected string) bool {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error")
		return false
	}
	var storeErr *StoreError
	if !errors.As(err, &storeErr) {
		t.Fatalf("expected StoreError, got %T", err)
		return false
	}
	return storeErr.Code == expected
}

func jsonInt(v int64) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}
