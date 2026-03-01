package oddjob

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

type oddjobBroadcast struct {
	eventType string
	data      *framework.EventData
}

type oddjobEventBus struct {
	broadcasts     []oddjobBroadcast
	requestHandler func(eventType string, data *framework.EventData) (*framework.EventData, error)
}

func (b *oddjobEventBus) Broadcast(eventType string, data *framework.EventData) error {
	b.broadcasts = append(b.broadcasts, oddjobBroadcast{eventType: eventType, data: data})
	return nil
}

func (b *oddjobEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return b.Broadcast(eventType, data)
}

func (b *oddjobEventBus) Send(target string, eventType string, data *framework.EventData) error {
	return nil
}

func (b *oddjobEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return nil
}

func (b *oddjobEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	if b.requestHandler != nil {
		return b.requestHandler(eventType, data)
	}
	return nil, nil
}

func (b *oddjobEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
}

func (b *oddjobEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	return nil
}

func (b *oddjobEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	return nil
}

func (b *oddjobEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	return nil
}

func (b *oddjobEventBus) UnregisterPlugin(name string) error {
	return nil
}

func (b *oddjobEventBus) GetDroppedEventCounts() map[string]int64 {
	return map[string]int64{}
}

func (b *oddjobEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

func TestOddjobCooldown(t *testing.T) {
	now := time.Now().UTC()
	when, _ := json.Marshal(now)

	bus := &oddjobEventBus{}
	bus.requestHandler = func(eventType string, data *framework.EventData) (*framework.EventData, error) {
		if eventType != "sql.query.request" || data.SQLQueryRequest == nil {
			return nil, nil
		}
		return &framework.EventData{
			SQLQueryResponse: &framework.SQLQueryResponse{
				Success: true,
				Columns: []string{"last_played_at"},
				Rows:    [][]json.RawMessage{{when}},
			},
		}, nil
	}

	p := New().(*Plugin)
	if err := p.Init(json.RawMessage("{}"), bus); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	p.cooldown = 8 * time.Hour

	req := &framework.PluginRequest{
		Data: &framework.RequestData{Command: &framework.CommandData{Params: map[string]string{"channel": "chan", "username": "dazza"}}},
	}
	event := &framework.DataEvent{Data: &framework.EventData{PluginRequest: req}}
	if err := p.handleCommand(event); err != nil {
		t.Fatalf("handleCommand failed: %v", err)
	}

	if len(bus.broadcasts) == 0 {
		t.Fatalf("expected cooldown response")
	}
	message := strings.ToLower(bus.broadcasts[0].data.RawMessage.Message)
	if !strings.Contains(message, "cooldown") && !strings.Contains(message, "wait") {
		t.Fatalf("unexpected cooldown message: %s", bus.broadcasts[0].data.RawMessage.Message)
	}
}

func TestOddjobPMRouting(t *testing.T) {
	bus := &oddjobEventBus{}
	bus.requestHandler = func(eventType string, data *framework.EventData) (*framework.EventData, error) {
		if eventType == "sql.query.request" {
			return &framework.EventData{SQLQueryResponse: &framework.SQLQueryResponse{Success: true, Columns: []string{"last_played_at"}, Rows: [][]json.RawMessage{}}}, nil
		}
		if eventType == "sql.exec.request" {
			return &framework.EventData{SQLExecResponse: &framework.SQLExecResponse{Success: true}}, nil
		}
		if eventType == "plugin.request" {
			payload, _ := json.Marshal(framework.CreditResponse{Channel: "chan", Username: "dazza", Amount: 10})
			return &framework.EventData{PluginResponse: &framework.PluginResponse{Success: true, Data: &framework.ResponseData{RawJSON: payload}}}, nil
		}
		return nil, nil
	}

	oldRoll := oddjobFailureRoll
	oddjobFailureRoll = func() bool { return false }
	defer func() { oddjobFailureRoll = oldRoll }()

	p := New().(*Plugin)
	if err := p.Init(json.RawMessage("{}"), bus); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	p.cooldown = 0

	req := &framework.PluginRequest{
		Data: &framework.RequestData{Command: &framework.CommandData{Params: map[string]string{"channel": "chan", "username": "dazza", "is_pm": "true"}}},
	}
	event := &framework.DataEvent{Data: &framework.EventData{PluginRequest: req}}
	if err := p.handleCommand(event); err != nil {
		t.Fatalf("handleCommand failed: %v", err)
	}

	if len(bus.broadcasts) == 0 {
		t.Fatalf("expected a response")
	}
	if bus.broadcasts[0].eventType != "cytube.send.pm" {
		t.Fatalf("expected pm response, got %s", bus.broadcasts[0].eventType)
	}
}

func TestOddjobCreditFailure(t *testing.T) {
	bus := &oddjobEventBus{}
	bus.requestHandler = func(eventType string, data *framework.EventData) (*framework.EventData, error) {
		if eventType == "sql.query.request" {
			return &framework.EventData{SQLQueryResponse: &framework.SQLQueryResponse{Success: true, Columns: []string{"last_played_at"}, Rows: [][]json.RawMessage{}}}, nil
		}
		if eventType == "plugin.request" {
			return &framework.EventData{PluginResponse: &framework.PluginResponse{Success: false, Error: "credit failed"}}, nil
		}
		return nil, nil
	}

	oldRoll := oddjobFailureRoll
	oddjobFailureRoll = func() bool { return false }
	defer func() { oddjobFailureRoll = oldRoll }()

	p := New().(*Plugin)
	if err := p.Init(json.RawMessage("{}"), bus); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	p.cooldown = 0

	req := &framework.PluginRequest{
		Data: &framework.RequestData{Command: &framework.CommandData{Params: map[string]string{"channel": "chan", "username": "dazza"}}},
	}
	event := &framework.DataEvent{Data: &framework.EventData{PluginRequest: req}}
	if err := p.handleCommand(event); err != nil {
		t.Fatalf("handleCommand failed: %v", err)
	}

	if len(bus.broadcasts) == 0 {
		t.Fatalf("expected a response")
	}
	last := bus.broadcasts[len(bus.broadcasts)-1]
	if !strings.Contains(last.data.RawMessage.Message, "boss stiffed") {
		t.Fatalf("unexpected message: %s", last.data.RawMessage.Message)
	}
}
