package framework

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// mockSQLEventBus implements EventBus interface for SQL client testing
type mockSQLEventBus struct {
	requests  map[string]*EventData
	responses map[string]*EventData
}

func (m *mockSQLEventBus) Send(target, eventType string, data *EventData) error {
	return nil
}

func (m *mockSQLEventBus) SendWithMetadata(target, eventType string, data *EventData, metadata *EventMetadata) error {
	return nil
}

func (m *mockSQLEventBus) Broadcast(eventType string, data *EventData) error {
	return nil
}

func (m *mockSQLEventBus) BroadcastWithMetadata(eventType string, data *EventData, metadata *EventMetadata) error {
	return nil
}

func (m *mockSQLEventBus) Subscribe(pattern string, handler EventHandler) error {
	return nil
}

func (m *mockSQLEventBus) SubscribeWithTags(pattern string, handler EventHandler, tags []string) error {
	return nil
}

func (m *mockSQLEventBus) Unsubscribe(pattern string, handler EventHandler) error {
	return nil
}

func (m *mockSQLEventBus) Request(ctx context.Context, target, eventType string, data *EventData, metadata *EventMetadata) (*EventData, error) {
	// Capture the request
	m.requests[eventType] = data

	// Return mock response based on event type
	if eventType == "sql.exec.request" && data.SQLExecRequest != nil {
		return &EventData{
			SQLExecResponse: &SQLExecResponse{
				ID:            data.SQLExecRequest.ID,
				CorrelationID: data.SQLExecRequest.CorrelationID,
				Success:       true,
				RowsAffected:  1,
			},
		}, nil
	} else if eventType == "sql.query.request" && data.SQLQueryRequest != nil {
		return &EventData{
			SQLQueryResponse: &SQLQueryResponse{
				ID:            data.SQLQueryRequest.ID,
				CorrelationID: data.SQLQueryRequest.CorrelationID,
				Success:       true,
				Columns:       []string{"id", "name", "age"},
				Rows:          [][]json.RawMessage{},
			},
		}, nil
	}

	// Return specific response if set
	if response, exists := m.responses[metadata.CorrelationID]; exists {
		return response, nil
	}
	return nil, nil
}

func (m *mockSQLEventBus) DeliverResponse(correlationID string, response *EventData, err error) {
	// Do nothing for now
}

func (m *mockSQLEventBus) Stop() error {
	return nil
}

func (m *mockSQLEventBus) RegisterPlugin(name string, plugin Plugin) error {
	return nil
}

func (m *mockSQLEventBus) UnregisterPlugin(name string) error {
	return nil
}

func (m *mockSQLEventBus) GetDroppedEventCounts() map[string]int64 {
	return make(map[string]int64)
}

func (m *mockSQLEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

func TestSQLClient_ExecContext(t *testing.T) {
	mockBus := &mockSQLEventBus{
		requests:  make(map[string]*EventData),
		responses: make(map[string]*EventData),
	}

	client := NewSQLClient(mockBus, "test-client")

	// Test with different parameter types
	testCases := []struct {
		name   string
		query  string
		args   []interface{}
		verify func(t *testing.T, params []SQLParam)
	}{
		{
			name:  "string parameter",
			query: "INSERT INTO users (name) VALUES ($1)",
			args:  []interface{}{"John Doe"},
			verify: func(t *testing.T, params []SQLParam) {
				if len(params) != 1 {
					t.Errorf("Expected 1 param, got %d", len(params))
					return
				}
				if params[0].Value != "John Doe" {
					t.Errorf("Expected param value 'John Doe', got %v", params[0].Value)
				}
			},
		},
		{
			name:  "integer parameter",
			query: "UPDATE users SET age = $1 WHERE id = $2",
			args:  []interface{}{25, 123},
			verify: func(t *testing.T, params []SQLParam) {
				if len(params) != 2 {
					t.Errorf("Expected 2 params, got %d", len(params))
					return
				}
				if params[0].Value != 25 {
					t.Errorf("Expected first param value 25, got %v", params[0].Value)
				}
				if params[1].Value != 123 {
					t.Errorf("Expected second param value 123, got %v", params[1].Value)
				}
			},
		},
		{
			name:  "mixed types",
			query: "INSERT INTO logs (message, user_id, timestamp) VALUES ($1, $2, $3)",
			args:  []interface{}{"Test message", 456, time.Now()},
			verify: func(t *testing.T, params []SQLParam) {
				if len(params) != 3 {
					t.Errorf("Expected 3 params, got %d", len(params))
					return
				}
				if params[0].Value != "Test message" {
					t.Errorf("Expected first param value 'Test message', got %v", params[0].Value)
				}
				if params[1].Value != 456 {
					t.Errorf("Expected second param value 456, got %v", params[1].Value)
				}
				// Just verify third param exists (time comparison is complex)
				if params[2].Value == nil {
					t.Errorf("Expected third param to have a value")
				}
			},
		},
		{
			name:  "nil parameter",
			query: "UPDATE users SET deleted_at = $1 WHERE id = $2",
			args:  []interface{}{nil, 789},
			verify: func(t *testing.T, params []SQLParam) {
				if len(params) != 2 {
					t.Errorf("Expected 2 params, got %d", len(params))
					return
				}
				if params[0].Value != nil {
					t.Errorf("Expected first param value nil, got %v", params[0].Value)
				}
				if params[1].Value != 789 {
					t.Errorf("Expected second param value 789, got %v", params[1].Value)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Clear previous requests
			mockBus.requests = make(map[string]*EventData)

			ctx := context.Background()
			_, err := client.ExecContext(ctx, tc.query, tc.args...)
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Verify the request was sent correctly
			req, exists := mockBus.requests["sql.exec.request"]
			if !exists {
				t.Error("No sql.exec.request was sent")
				return
			}

			if req.SQLExecRequest == nil {
				t.Error("SQLExecRequest is nil")
				return
			}

			if req.SQLExecRequest.Query != tc.query {
				t.Errorf("Expected query '%s', got '%s'", tc.query, req.SQLExecRequest.Query)
			}

			// Verify params are structured correctly
			tc.verify(t, req.SQLExecRequest.Params)
		})
	}
}

func TestSQLClient_QueryContext(t *testing.T) {
	mockBus := &mockSQLEventBus{
		requests:  make(map[string]*EventData),
		responses: make(map[string]*EventData),
	}

	client := NewSQLClient(mockBus, "test-client")

	// Test query with parameters
	ctx := context.Background()
	query := "SELECT * FROM users WHERE name = $1 AND age > $2"
	args := []interface{}{"Alice", 21}

	_, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
		return
	}

	// Verify the request
	req, exists := mockBus.requests["sql.query.request"]
	if !exists {
		t.Error("No sql.query.request was sent")
		return
	}

	if req.SQLQueryRequest == nil {
		t.Error("SQLQueryRequest is nil")
		return
	}

	// Verify params structure
	params := req.SQLQueryRequest.Params
	if len(params) != 2 {
		t.Errorf("Expected 2 params, got %d", len(params))
		return
	}

	// Check that params are SQLParam structs with Value field
	if params[0].Value != "Alice" {
		t.Errorf("Expected first param value 'Alice', got %v", params[0].Value)
	}

	if params[1].Value != 21 {
		t.Errorf("Expected second param value 21, got %v", params[1].Value)
	}
}
