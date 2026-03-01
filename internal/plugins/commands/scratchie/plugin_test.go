package scratchie

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

type scratchieBroadcast struct {
	eventType string
	data      *framework.EventData
}

type scratchieEventBus struct {
	broadcasts     []scratchieBroadcast
	requestHandler func(eventType string, data *framework.EventData) (*framework.EventData, error)
}

func (b *scratchieEventBus) Broadcast(eventType string, data *framework.EventData) error {
	b.broadcasts = append(b.broadcasts, scratchieBroadcast{eventType: eventType, data: data})
	return nil
}

func (b *scratchieEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return b.Broadcast(eventType, data)
}

func (b *scratchieEventBus) Send(target string, eventType string, data *framework.EventData) error {
	return nil
}

func (b *scratchieEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return nil
}

func (b *scratchieEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	if b.requestHandler != nil {
		return b.requestHandler(eventType, data)
	}
	return nil, nil
}

func (b *scratchieEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
}

func (b *scratchieEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	return nil
}

func (b *scratchieEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	return nil
}

func (b *scratchieEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	return nil
}

func (b *scratchieEventBus) UnregisterPlugin(name string) error {
	return nil
}

func (b *scratchieEventBus) GetDroppedEventCounts() map[string]int64 {
	return map[string]int64{}
}

func (b *scratchieEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

func TestScratchieCooldown(t *testing.T) {
	p := &Plugin{
		name:     "scratchie",
		cooldown: 20 * time.Millisecond,
		lastUse:  make(map[string]time.Time),
	}

	if _, ok := p.checkCooldown("channel", "user"); !ok {
		t.Fatalf("expected first cooldown check to pass")
	}
	if _, ok := p.checkCooldown("channel", "user"); ok {
		t.Fatalf("expected cooldown to block immediate reuse")
	}
	time.Sleep(25 * time.Millisecond)
	if _, ok := p.checkCooldown("channel", "user"); !ok {
		t.Fatalf("expected cooldown to expire")
	}
}

func TestScratchieDebitFailure(t *testing.T) {
	bus := &scratchieEventBus{}
	bus.requestHandler = func(eventType string, data *framework.EventData) (*framework.EventData, error) {
		if eventType != "plugin.request" || data.PluginRequest == nil {
			return nil, nil
		}
		switch data.PluginRequest.Type {
		case "economy.get_balance":
			payload, _ := json.Marshal(framework.GetBalanceResponse{Channel: "chan", Username: "dazza", Balance: 100})
			return &framework.EventData{
				PluginResponse: &framework.PluginResponse{Success: true, Data: &framework.ResponseData{RawJSON: payload}},
			}, nil
		case "economy.debit":
			return &framework.EventData{
				PluginResponse: &framework.PluginResponse{Success: false, Error: "debit failed"},
			}, nil
		default:
			return nil, nil
		}
	}

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
		t.Fatalf("expected a response broadcast")
	}
	got := bus.broadcasts[len(bus.broadcasts)-1].data.RawMessage.Message
	if got != "scratchie machine broke mate" {
		t.Fatalf("unexpected message: %s", got)
	}
}

func TestScratchiePMRouting(t *testing.T) {
	bus := &scratchieEventBus{}
	bus.requestHandler = func(eventType string, data *framework.EventData) (*framework.EventData, error) {
		if eventType != "plugin.request" || data.PluginRequest == nil {
			return nil, nil
		}
		switch data.PluginRequest.Type {
		case "economy.get_balance":
			payload, _ := json.Marshal(framework.GetBalanceResponse{Channel: "chan", Username: "dazza", Balance: 100})
			return &framework.EventData{
				PluginResponse: &framework.PluginResponse{Success: true, Data: &framework.ResponseData{RawJSON: payload}},
			}, nil
		case "economy.debit":
			payload, _ := json.Marshal(framework.DebitResponse{Channel: "chan", Username: "dazza", Amount: 5})
			return &framework.EventData{
				PluginResponse: &framework.PluginResponse{Success: true, Data: &framework.ResponseData{RawJSON: payload}},
			}, nil
		default:
			return nil, nil
		}
	}

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
		t.Fatalf("expected a response broadcast")
	}
	if bus.broadcasts[0].eventType != "cytube.send.pm" {
		t.Fatalf("expected pm response, got %s", bus.broadcasts[0].eventType)
	}
}
