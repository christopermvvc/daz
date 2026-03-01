package signspinning

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

type signspinningBroadcast struct {
	eventType string
	data      *framework.EventData
}

type signspinningEventBus struct {
	broadcasts     []signspinningBroadcast
	requestHandler func(eventType string, data *framework.EventData) (*framework.EventData, error)
}

func (b *signspinningEventBus) Broadcast(eventType string, data *framework.EventData) error {
	b.broadcasts = append(b.broadcasts, signspinningBroadcast{eventType: eventType, data: data})
	return nil
}

func (b *signspinningEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return b.Broadcast(eventType, data)
}

func (b *signspinningEventBus) Send(target string, eventType string, data *framework.EventData) error {
	return nil
}

func (b *signspinningEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return nil
}

func (b *signspinningEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	if b.requestHandler != nil {
		return b.requestHandler(eventType, data)
	}
	return nil, nil
}

func (b *signspinningEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
}

func (b *signspinningEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	return nil
}

func (b *signspinningEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	return nil
}

func (b *signspinningEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	return nil
}

func (b *signspinningEventBus) UnregisterPlugin(name string) error {
	return nil
}

func (b *signspinningEventBus) GetDroppedEventCounts() map[string]int64 {
	return map[string]int64{}
}

func (b *signspinningEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

func TestSignSpinningCooldown(t *testing.T) {
	now := time.Now().UTC()
	when, _ := json.Marshal(now)

	bus := &signspinningEventBus{}
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
	p.cooldown = 36 * time.Hour

	req := &framework.PluginRequest{Data: &framework.RequestData{Command: &framework.CommandData{Params: map[string]string{"channel": "chan", "username": "dazza"}}}}
	event := &framework.DataEvent{Data: &framework.EventData{PluginRequest: req}}
	if err := p.handleCommand(event); err != nil {
		t.Fatalf("handleCommand failed: %v", err)
	}

	if len(bus.broadcasts) == 0 {
		t.Fatalf("expected cooldown response")
	}
	message := strings.ToLower(bus.broadcasts[0].data.RawMessage.Message)
	if !strings.Contains(message, "dazza") {
		t.Fatalf("unexpected cooldown message (missing username): %s", bus.broadcasts[0].data.RawMessage.Message)
	}
	if !regexp.MustCompile(`\d+h\s+\d+m`).MatchString(message) {
		t.Fatalf("unexpected cooldown message: %s", bus.broadcasts[0].data.RawMessage.Message)
	}
}

func TestSignSpinningPMRouting(t *testing.T) {
	bus := &signspinningEventBus{}
	bus.requestHandler = func(eventType string, data *framework.EventData) (*framework.EventData, error) {
		switch eventType {
		case "sql.query.request":
			return &framework.EventData{SQLQueryResponse: &framework.SQLQueryResponse{Success: true, Columns: []string{"last_played_at"}, Rows: [][]json.RawMessage{}}}, nil
		case "sql.exec.request":
			return &framework.EventData{SQLExecResponse: &framework.SQLExecResponse{Success: true}}, nil
		case "plugin.request":
			payload, _ := json.Marshal(framework.CreditResponse{Channel: "chan", Username: "dazza", Amount: 10})
			return &framework.EventData{PluginResponse: &framework.PluginResponse{Success: true, Data: &framework.ResponseData{RawJSON: payload}}}, nil
		default:
			return nil, nil
		}
	}

	p := New().(*Plugin)
	if err := p.Init(json.RawMessage("{}"), bus); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	p.cooldown = 0

	req := &framework.PluginRequest{Data: &framework.RequestData{Command: &framework.CommandData{Params: map[string]string{"channel": "chan", "username": "dazza", "is_pm": "true"}}}}
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

func TestSignSpinningCreditFailure(t *testing.T) {
	bus := &signspinningEventBus{}
	bus.requestHandler = func(eventType string, data *framework.EventData) (*framework.EventData, error) {
		switch eventType {
		case "sql.query.request":
			return &framework.EventData{SQLQueryResponse: &framework.SQLQueryResponse{Success: true, Columns: []string{"last_played_at"}, Rows: [][]json.RawMessage{}}}, nil
		case "plugin.request":
			return &framework.EventData{PluginResponse: &framework.PluginResponse{Success: false, Error: "credit failed"}}, nil
		default:
			return nil, nil
		}
	}

	p := New().(*Plugin)
	if err := p.Init(json.RawMessage("{}"), bus); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	p.cooldown = 0

	req := &framework.PluginRequest{Data: &framework.RequestData{Command: &framework.CommandData{Params: map[string]string{"channel": "chan", "username": "dazza"}}}}
	event := &framework.DataEvent{Data: &framework.EventData{PluginRequest: req}}
	if err := p.handleCommand(event); err != nil {
		t.Fatalf("handleCommand failed: %v", err)
	}

	if len(bus.broadcasts) == 0 {
		t.Fatalf("expected a response")
	}
	last := bus.broadcasts[len(bus.broadcasts)-1]
	if !strings.Contains(last.data.RawMessage.Message, "agency system crashed") {
		t.Fatalf("unexpected message: %s", last.data.RawMessage.Message)
	}
}
