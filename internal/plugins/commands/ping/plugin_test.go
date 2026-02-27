package ping

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

type mockEventBus struct {
	mu         sync.Mutex
	broadcasts []broadcastCall
}

type broadcastCall struct {
	eventType string
	data      *framework.EventData
}

func (m *mockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.broadcasts = append(m.broadcasts, broadcastCall{eventType: eventType, data: data})
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
	_ = correlationID
	_ = response
	_ = err
}

func (m *mockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	_ = eventType
	_ = handler
	return nil
}

func (m *mockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	_ = pattern
	_ = handler
	_ = tags
	return nil
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

// Satisfy any sql-related interfaces in older mocks (not used here).
func (m *mockEventBus) QuerySync(ctx context.Context, sql string, params ...framework.SQLParam) (*sql.Rows, error) {
	_ = ctx
	_ = sql
	_ = params
	return nil, nil
}

func (m *mockEventBus) ExecSync(ctx context.Context, sql string, params ...framework.SQLParam) (sql.Result, error) {
	_ = ctx
	_ = sql
	_ = params
	return nil, nil
}

func TestPing_ChatResponse(t *testing.T) {
	bus := &mockEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	event := &framework.DataEvent{EventType: "command.ping.execute", EventTime: time.Now(), Data: &framework.EventData{PluginRequest: &framework.PluginRequest{Data: &framework.RequestData{Command: &framework.CommandData{Params: map[string]string{
		"username": "alice",
		"channel":  "chan",
		"is_pm":    "false",
	}}}}}}

	if err := p.handleCommand(event); err != nil {
		t.Fatalf("handleCommand error: %v", err)
	}

	bus.mu.Lock()
	defer bus.mu.Unlock()

	var saw bool
	for _, b := range bus.broadcasts {
		if b.eventType == "cytube.send" && b.data != nil && b.data.RawMessage != nil {
			if b.data.RawMessage.Message != "pong" {
				t.Fatalf("got message %q want %q", b.data.RawMessage.Message, "pong")
			}
			saw = true
		}
	}
	if !saw {
		t.Fatalf("expected cytube.send broadcast")
	}
}

func TestPing_PMResponse(t *testing.T) {
	bus := &mockEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	event := &framework.DataEvent{EventType: "command.ping.execute", EventTime: time.Now(), Data: &framework.EventData{PluginRequest: &framework.PluginRequest{Data: &framework.RequestData{Command: &framework.CommandData{Params: map[string]string{
		"username": "alice",
		"channel":  "chan",
		"is_pm":    "true",
	}}}}}}

	if err := p.handleCommand(event); err != nil {
		t.Fatalf("handleCommand error: %v", err)
	}

	bus.mu.Lock()
	defer bus.mu.Unlock()

	var saw bool
	for _, b := range bus.broadcasts {
		if b.eventType == "plugin.response" && b.data != nil && b.data.PluginResponse != nil && b.data.PluginResponse.Data != nil && b.data.PluginResponse.Data.CommandResult != nil {
			if b.data.PluginResponse.Data.CommandResult.Output != "pong" {
				t.Fatalf("got output %q want %q", b.data.PluginResponse.Data.CommandResult.Output, "pong")
			}
			saw = true
		}
	}
	if !saw {
		t.Fatalf("expected plugin.response broadcast")
	}
}
