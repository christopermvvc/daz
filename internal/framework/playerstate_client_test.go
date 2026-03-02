package framework

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

type mockPlayerStateEventBus struct {
	lastRequest  *EventData
	lastMetadata *EventMetadata
	lastResponse *EventData
	mockErr      error
	requestCount int
}

func (m *mockPlayerStateEventBus) Broadcast(eventType string, data *EventData) error { return nil }
func (m *mockPlayerStateEventBus) BroadcastWithMetadata(eventType string, data *EventData, metadata *EventMetadata) error {
	return nil
}
func (m *mockPlayerStateEventBus) Send(target string, eventType string, data *EventData) error {
	return nil
}
func (m *mockPlayerStateEventBus) SendWithMetadata(target string, eventType string, data *EventData, metadata *EventMetadata) error {
	return nil
}
func (m *mockPlayerStateEventBus) Request(ctx context.Context, target string, eventType string, data *EventData, metadata *EventMetadata) (*EventData, error) {
	m.requestCount++
	m.lastRequest = data
	m.lastMetadata = metadata
	return m.lastResponse, m.mockErr
}
func (m *mockPlayerStateEventBus) DeliverResponse(correlationID string, response *EventData, err error) {
}
func (m *mockPlayerStateEventBus) Subscribe(eventType string, handler EventHandler) error { return nil }
func (m *mockPlayerStateEventBus) SubscribeWithTags(pattern string, handler EventHandler, tags []string) error {
	return nil
}
func (m *mockPlayerStateEventBus) RegisterPlugin(name string, plugin Plugin) error { return nil }
func (m *mockPlayerStateEventBus) UnregisterPlugin(name string) error              { return nil }
func (m *mockPlayerStateEventBus) GetDroppedEventCounts() map[string]int64         { return nil }
func (m *mockPlayerStateEventBus) GetDroppedEventCount(eventType string) int64     { return 0 }

func TestPlayerStateClient_CorrelationMirrored(t *testing.T) {
	bus := &mockPlayerStateEventBus{
		lastResponse: &EventData{
			PluginResponse: &PluginResponse{
				Success: true,
				Data: &ResponseData{
					RawJSON: mustJSON(t, PlayerStateResponse{
						Channel:  "chan",
						Username: "alice",
						PlayerState: PlayerState{
							Bladder: 1,
						},
					}),
				},
			},
		},
	}
	client := NewPlayerStateClient(bus, "test-plugin")

	_, err := client.Get(context.Background(), "chan", "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if bus.lastMetadata.CorrelationID == "" {
		t.Fatal("expected non-empty correlation ID in metadata")
	}
	if bus.lastRequest == nil || bus.lastRequest.PluginRequest == nil {
		t.Fatal("expected plugin request")
	}
	if bus.lastMetadata.CorrelationID != bus.lastRequest.PluginRequest.ID {
		t.Fatalf("correlation ID mismatch: metadata=%s request=%s",
			bus.lastMetadata.CorrelationID, bus.lastRequest.PluginRequest.ID)
	}
}

func TestPlayerStateClient_GetRequestShape(t *testing.T) {
	bus := &mockPlayerStateEventBus{
		lastResponse: &EventData{
			PluginResponse: &PluginResponse{
				Success: true,
				Data: &ResponseData{
					RawJSON: mustJSON(t, PlayerStateResponse{
						Channel:  "chan",
						Username: "alice",
						PlayerState: PlayerState{
							Bladder: 2,
							Alcohol: 3,
							Weed:    4,
							Food:    5,
							Lust:    6,
						},
					}),
				},
			},
		},
	}
	client := NewPlayerStateClient(bus, "state-plugin")

	state, err := client.Get(context.Background(), "chan", "alice")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if bus.lastRequest == nil || bus.lastRequest.PluginRequest == nil {
		t.Fatal("expected plugin request")
	}
	if bus.lastRequest.PluginRequest.Type != "playerstate.get" {
		t.Fatalf("expected op playerstate.get, got %s", bus.lastRequest.PluginRequest.Type)
	}

	if state.Bladder != 2 || state.Alcohol != 3 || state.Weed != 4 || state.Food != 5 || state.Lust != 6 {
		t.Fatalf("unexpected decoded state: %#v", state)
	}
}

func TestPlayerStateClient_SetAndAdjust(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		bus := &mockPlayerStateEventBus{
			lastResponse: &EventData{
				PluginResponse: &PluginResponse{
					Success: true,
					Data: &ResponseData{
						RawJSON: mustJSON(t, PlayerStateResponse{
							Channel:  "chan",
							Username: "alice",
							PlayerState: PlayerState{
								Bladder: 4,
								Alcohol: 3,
								Weed:    2,
								Food:    1,
								Lust:    0,
							},
						}),
					},
				},
			},
		}
		client := NewPlayerStateClient(bus, "state-plugin")

		state, err := client.Set(context.Background(), PlayerStateMutationRequest{
			Channel:  "chan",
			Username: "alice",
			Bladder:  int64Ptr(4),
			Alcohol:  int64Ptr(3),
			Weed:     int64Ptr(2),
			Food:     int64Ptr(1),
			Lust:     int64Ptr(0),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if state == nil {
			t.Fatal("expected state response")
		}
		if bus.lastRequest == nil || bus.lastRequest.PluginRequest == nil {
			t.Fatal("expected plugin request")
		}
		if bus.lastRequest.PluginRequest.Type != "playerstate.set" {
			t.Fatalf("expected op playerstate.set, got %s", bus.lastRequest.PluginRequest.Type)
		}

		var req PlayerStateMutationRequest
		if err := json.Unmarshal(bus.lastRequest.PluginRequest.Data.RawJSON, &req); err != nil {
			t.Fatalf("failed to unmarshal request: %v", err)
		}
		if req.Channel != "chan" || req.Username != "alice" {
			t.Fatalf("unexpected identity: %+v", req)
		}
		if req.Bladder == nil || *req.Bladder != 4 ||
			req.Alcohol == nil || *req.Alcohol != 3 ||
			req.Weed == nil || *req.Weed != 2 ||
			req.Food == nil || *req.Food != 1 ||
			req.Lust == nil || *req.Lust != 0 {
			t.Fatalf("unexpected request payload: %#v", req)
		}
	})

	t.Run("adjust", func(t *testing.T) {
		bus := &mockPlayerStateEventBus{
			lastResponse: &EventData{
				PluginResponse: &PluginResponse{
					Success: true,
					Data: &ResponseData{
						RawJSON: mustJSON(t, PlayerStateResponse{
							Channel:  "chan",
							Username: "alice",
							PlayerState: PlayerState{
								Bladder: 1,
								Alcohol: 2,
								Weed:    3,
								Food:    4,
								Lust:    5,
							},
						}),
					},
				},
			},
		}
		client := NewPlayerStateClient(bus, "state-plugin")

		state, err := client.Adjust(context.Background(), PlayerStateMutationRequest{
			Channel:  "chan",
			Username: "alice",
			Bladder:  int64Ptr(-1),
			Lust:     int64Ptr(6),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if state == nil {
			t.Fatal("expected state response")
		}
		if bus.lastRequest == nil || bus.lastRequest.PluginRequest == nil {
			t.Fatal("expected plugin request")
		}
		if bus.lastRequest.PluginRequest.Type != "playerstate.adjust" {
			t.Fatalf("expected op playerstate.adjust, got %s", bus.lastRequest.PluginRequest.Type)
		}

		var req PlayerStateMutationRequest
		if err := json.Unmarshal(bus.lastRequest.PluginRequest.Data.RawJSON, &req); err != nil {
			t.Fatalf("failed to unmarshal request: %v", err)
		}
		if req.Bladder == nil || *req.Bladder != -1 {
			t.Fatalf("expected bladder delta -1, got %#v", req.Bladder)
		}
		if req.Lust == nil || *req.Lust != 6 {
			t.Fatalf("expected lust delta 6, got %#v", req.Lust)
		}
	})
}

func TestPlayerStateClient_AdjustFieldHelpers(t *testing.T) {
	cases := []struct {
		name      string
		opType    string
		call      func(context.Context, *PlayerStateClient, int64) error
		fieldName string
		fieldVal  int64
	}{
		{
			name:   "adjust bladder",
			opType: "playerstate.adjust",
			call: func(ctx context.Context, c *PlayerStateClient, val int64) error {
				_, err := c.AdjustBladder(ctx, "chan", "alice", val)
				return err
			},
			fieldName: "bladder",
			fieldVal:  -3,
		},
		{
			name:   "adjust alcohol",
			opType: "playerstate.adjust",
			call: func(ctx context.Context, c *PlayerStateClient, val int64) error {
				_, err := c.AdjustAlcohol(ctx, "chan", "alice", val)
				return err
			},
			fieldName: "alcohol",
			fieldVal:  4,
		},
		{
			name:   "adjust weed",
			opType: "playerstate.adjust",
			call: func(ctx context.Context, c *PlayerStateClient, val int64) error {
				_, err := c.AdjustWeed(ctx, "chan", "alice", val)
				return err
			},
			fieldName: "weed",
			fieldVal:  1,
		},
		{
			name:   "adjust food",
			opType: "playerstate.adjust",
			call: func(ctx context.Context, c *PlayerStateClient, val int64) error {
				_, err := c.AdjustFood(ctx, "chan", "alice", val)
				return err
			},
			fieldName: "food",
			fieldVal:  2,
		},
		{
			name:   "adjust lust",
			opType: "playerstate.adjust",
			call: func(ctx context.Context, c *PlayerStateClient, val int64) error {
				_, err := c.AdjustLust(ctx, "chan", "alice", val)
				return err
			},
			fieldName: "lust",
			fieldVal:  7,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus := &mockPlayerStateEventBus{
				lastResponse: &EventData{
					PluginResponse: &PluginResponse{
						Success: true,
						Data: &ResponseData{
							RawJSON: mustJSON(t, PlayerStateResponse{
								Channel:  "chan",
								Username: "alice",
								PlayerState: PlayerState{
									Bladder: 10,
								},
							}),
						},
					},
				},
			}
			client := NewPlayerStateClient(bus, "state-plugin")

			err := tc.call(context.Background(), client, tc.fieldVal)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if bus.lastRequest == nil || bus.lastRequest.PluginRequest == nil {
				t.Fatal("expected plugin request")
			}
			if bus.lastRequest.PluginRequest.Type != tc.opType {
				t.Fatalf("expected op %q, got %q", tc.opType, bus.lastRequest.PluginRequest.Type)
			}

			var req PlayerStateMutationRequest
			if err := json.Unmarshal(bus.lastRequest.PluginRequest.Data.RawJSON, &req); err != nil {
				t.Fatalf("failed to unmarshal request: %v", err)
			}
			if req.Channel != "chan" || req.Username != "alice" {
				t.Fatalf("unexpected identity: %+v", req)
			}
			if value, ok := getMutationField(req, tc.fieldName); !ok || value != tc.fieldVal {
				t.Fatalf("expected field %s=%d, got %d (ok=%v)", tc.fieldName, tc.fieldVal, value, ok)
			}
		})
	}
}

func TestPlayerStateClient_SetFieldHelpers(t *testing.T) {
	cases := []struct {
		name      string
		opType    string
		call      func(context.Context, *PlayerStateClient, int64) error
		fieldName string
		fieldVal  int64
	}{
		{
			name:   "set bladder",
			opType: "playerstate.set",
			call: func(ctx context.Context, c *PlayerStateClient, val int64) error {
				_, err := c.SetBladder(ctx, "chan", "alice", val)
				return err
			},
			fieldName: "bladder",
			fieldVal:  42,
		},
		{
			name:   "set alcohol",
			opType: "playerstate.set",
			call: func(ctx context.Context, c *PlayerStateClient, val int64) error {
				_, err := c.SetAlcohol(ctx, "chan", "alice", val)
				return err
			},
			fieldName: "alcohol",
			fieldVal:  13,
		},
		{
			name:   "set weed",
			opType: "playerstate.set",
			call: func(ctx context.Context, c *PlayerStateClient, val int64) error {
				_, err := c.SetWeed(ctx, "chan", "alice", val)
				return err
			},
			fieldName: "weed",
			fieldVal:  9,
		},
		{
			name:   "set food",
			opType: "playerstate.set",
			call: func(ctx context.Context, c *PlayerStateClient, val int64) error {
				_, err := c.SetFood(ctx, "chan", "alice", val)
				return err
			},
			fieldName: "food",
			fieldVal:  6,
		},
		{
			name:   "set lust",
			opType: "playerstate.set",
			call: func(ctx context.Context, c *PlayerStateClient, val int64) error {
				_, err := c.SetLust(ctx, "chan", "alice", val)
				return err
			},
			fieldName: "lust",
			fieldVal:  11,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus := &mockPlayerStateEventBus{
				lastResponse: &EventData{
					PluginResponse: &PluginResponse{
						Success: true,
						Data: &ResponseData{
							RawJSON: mustJSON(t, PlayerStateResponse{
								Channel:  "chan",
								Username: "alice",
								PlayerState: PlayerState{
									Bladder: 10,
								},
							}),
						},
					},
				},
			}
			client := NewPlayerStateClient(bus, "state-plugin")

			err := tc.call(context.Background(), client, tc.fieldVal)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if bus.lastRequest == nil || bus.lastRequest.PluginRequest == nil {
				t.Fatal("expected plugin request")
			}
			if bus.lastRequest.PluginRequest.Type != tc.opType {
				t.Fatalf("expected op %q, got %q", tc.opType, bus.lastRequest.PluginRequest.Type)
			}

			var req PlayerStateMutationRequest
			if err := json.Unmarshal(bus.lastRequest.PluginRequest.Data.RawJSON, &req); err != nil {
				t.Fatalf("failed to unmarshal request: %v", err)
			}
			if req.Channel != "chan" || req.Username != "alice" {
				t.Fatalf("unexpected identity: %+v", req)
			}
			if value, ok := getMutationField(req, tc.fieldName); !ok || value != tc.fieldVal {
				t.Fatalf("expected field %s=%d, got %d (ok=%v)", tc.fieldName, tc.fieldVal, value, ok)
			}
		})
	}
}

func TestPlayerStateClient_ErrorMapping(t *testing.T) {
	bus := &mockPlayerStateEventBus{
		lastResponse: &EventData{
			PluginResponse: &PluginResponse{
				Success: false,
				Error:   "state failed",
				Data: &ResponseData{
					RawJSON: mustJSON(t, PlayerStateError{
						ErrorCode: "INVALID_ARGUMENT",
						Message:   "missing channel",
					}),
				},
			},
		},
	}
	client := NewPlayerStateClient(bus, "state-plugin")

	_, err := client.SetLust(context.Background(), "chan", "alice", 5)
	if err == nil {
		t.Fatal("expected error")
	}

	stateErr, ok := err.(*PlayerStateError)
	if !ok {
		t.Fatalf("expected *PlayerStateError, got %T", err)
	}
	if stateErr.ErrorCode != "INVALID_ARGUMENT" {
		t.Fatalf("expected INVALID_ARGUMENT, got %s", stateErr.ErrorCode)
	}
}

func TestPlayerStateClient_UnstructuredFailureUsesPluginError(t *testing.T) {
	bus := &mockPlayerStateEventBus{
		lastResponse: &EventData{
			PluginResponse: &PluginResponse{
				Success: false,
				Error:   "plugin failure",
				Data: &ResponseData{
					RawJSON: []byte(`not json`),
				},
			},
		},
	}
	client := NewPlayerStateClient(bus, "state-plugin")

	_, err := client.AdjustFood(context.Background(), "chan", "alice", 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "plugin request failed: plugin failure" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPlayerStateClient_NoResponseIsError(t *testing.T) {
	bus := &mockPlayerStateEventBus{
		lastResponse: nil,
	}
	client := NewPlayerStateClient(bus, "state-plugin")

	_, err := client.AdjustLust(context.Background(), "chan", "alice", 1)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestPlayerStateClient_ConfigForTimeout(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		if got := configForTimeout(0); got != "normal" {
			t.Fatalf("expected normal, got %s", got)
		}
		if got := configForTimeout(-time.Second); got != "normal" {
			t.Fatalf("expected normal, got %s", got)
		}
	})
	t.Run("fast", func(t *testing.T) {
		if got := configForTimeout(2 * time.Second); got != "fast" {
			t.Fatalf("expected fast, got %s", got)
		}
		if got := configForTimeout(5 * time.Second); got != "fast" {
			t.Fatalf("expected fast, got %s", got)
		}
	})
	t.Run("normal_range", func(t *testing.T) {
		if got := configForTimeout(10 * time.Second); got != "normal" {
			t.Fatalf("expected normal, got %s", got)
		}
	})
	t.Run("slow", func(t *testing.T) {
		if got := configForTimeout(20 * time.Second); got != "slow" {
			t.Fatalf("expected slow, got %s", got)
		}
	})
	t.Run("critical", func(t *testing.T) {
		if got := configForTimeout(90 * time.Second); got != "critical" {
			t.Fatalf("expected critical, got %s", got)
		}
	})
}

func TestPlayerStateClient_Int64Ptr(t *testing.T) {
	value := int64(99)
	ptr := int64Ptr(value)
	if ptr == nil || *ptr != 99 {
		t.Fatalf("int64Ptr() = %v", ptr)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}
	return data
}

func getMutationField(req PlayerStateMutationRequest, field string) (int64, bool) {
	switch field {
	case "bladder":
		if req.Bladder == nil {
			return 0, false
		}
		return *req.Bladder, true
	case "alcohol":
		if req.Alcohol == nil {
			return 0, false
		}
		return *req.Alcohol, true
	case "weed":
		if req.Weed == nil {
			return 0, false
		}
		return *req.Weed, true
	case "food":
		if req.Food == nil {
			return 0, false
		}
		return *req.Food, true
	case "lust":
		if req.Lust == nil {
			return 0, false
		}
		return *req.Lust, true
	default:
		return 0, false
	}
}
