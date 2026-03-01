package couchcoins

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

type couchBroadcast struct {
	eventType string
	data      *framework.EventData
}

type couchEventBus struct {
	broadcasts     []couchBroadcast
	requestHandler func(eventType string, data *framework.EventData) (*framework.EventData, error)
}

func (b *couchEventBus) Broadcast(eventType string, data *framework.EventData) error {
	b.broadcasts = append(b.broadcasts, couchBroadcast{eventType: eventType, data: data})
	return nil
}

func (b *couchEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return b.Broadcast(eventType, data)
}

func (b *couchEventBus) Send(target string, eventType string, data *framework.EventData) error {
	return nil
}

func (b *couchEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return nil
}

func (b *couchEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	if b.requestHandler != nil {
		return b.requestHandler(eventType, data)
	}
	return nil, nil
}

func (b *couchEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
}

func (b *couchEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	return nil
}

func (b *couchEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	return nil
}

func (b *couchEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	return nil
}

func (b *couchEventBus) UnregisterPlugin(name string) error {
	return nil
}

func (b *couchEventBus) GetDroppedEventCounts() map[string]int64 {
	return map[string]int64{}
}

func (b *couchEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

func TestCouchCoinsCooldown(t *testing.T) {
	now := time.Now().UTC()
	when, _ := json.Marshal(now)

	bus := &couchEventBus{}
	bus.requestHandler = func(eventType string, data *framework.EventData) (*framework.EventData, error) {
		if eventType != "sql.query.request" || data.SQLQueryRequest == nil {
			return nil, nil
		}
		return &framework.EventData{SQLQueryResponse: &framework.SQLQueryResponse{Success: true, Columns: []string{"last_played_at"}, Rows: [][]json.RawMessage{{when}}}}, nil
	}

	p := New().(*Plugin)
	if err := p.Init(json.RawMessage("{}"), bus); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	p.cooldown = 12 * time.Hour

	req := &framework.PluginRequest{Data: &framework.RequestData{Command: &framework.CommandData{Params: map[string]string{"channel": "chan", "username": "dazza"}}}}
	event := &framework.DataEvent{Data: &framework.EventData{PluginRequest: req}}
	if err := p.handleCommand(event); err != nil {
		t.Fatalf("handleCommand failed: %v", err)
	}

	if len(bus.broadcasts) == 0 {
		t.Fatalf("expected cooldown response")
	}
	message := strings.ToLower(bus.broadcasts[0].data.RawMessage.Message)
	if !strings.Contains(message, "wait") && !strings.Contains(message, "remaining") && !strings.Contains(message, "patience") {
		t.Fatalf("unexpected cooldown message: %s", bus.broadcasts[0].data.RawMessage.Message)
	}
}

func TestCouchCoinsCreditFailure(t *testing.T) {
	bus := &couchEventBus{}
	bus.requestHandler = func(eventType string, data *framework.EventData) (*framework.EventData, error) {
		switch eventType {
		case "sql.query.request":
			return &framework.EventData{SQLQueryResponse: &framework.SQLQueryResponse{Success: true, Columns: []string{"last_played_at"}, Rows: [][]json.RawMessage{}}}, nil
		case "plugin.request":
			if data.PluginRequest != nil && data.PluginRequest.Type == "economy.get_balance" {
				payload, _ := json.Marshal(framework.GetBalanceResponse{Channel: "chan", Username: "dazza", Balance: 100})
				return &framework.EventData{PluginResponse: &framework.PluginResponse{Success: true, Data: &framework.ResponseData{RawJSON: payload}}}, nil
			}
			return &framework.EventData{PluginResponse: &framework.PluginResponse{Success: false, Error: "credit failed"}}, nil
		default:
			return nil, nil
		}
	}

	oldRoll := rollFindAmountFunc
	oldBad := couchBadEventRoll
	rollFindAmountFunc = func(bool) int { return 10 }
	couchBadEventRoll = func() float64 { return 1 }
	defer func() {
		rollFindAmountFunc = oldRoll
		couchBadEventRoll = oldBad
	}()

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
	if !strings.Contains(last.data.RawMessage.Message, "couch search system fucked itself") {
		t.Fatalf("unexpected message: %s", last.data.RawMessage.Message)
	}
}

func TestCouchCoinsAnnouncementThreshold(t *testing.T) {
	bus := &couchEventBus{}
	bus.requestHandler = func(eventType string, data *framework.EventData) (*framework.EventData, error) {
		switch eventType {
		case "sql.query.request":
			return &framework.EventData{SQLQueryResponse: &framework.SQLQueryResponse{Success: true, Columns: []string{"last_played_at"}, Rows: [][]json.RawMessage{}}}, nil
		case "sql.exec.request":
			return &framework.EventData{SQLExecResponse: &framework.SQLExecResponse{Success: true}}, nil
		case "plugin.request":
			if data.PluginRequest != nil && data.PluginRequest.Type == "economy.get_balance" {
				payload, _ := json.Marshal(framework.GetBalanceResponse{Channel: "chan", Username: "dazza", Balance: 100})
				return &framework.EventData{PluginResponse: &framework.PluginResponse{Success: true, Data: &framework.ResponseData{RawJSON: payload}}}, nil
			}
			payload, _ := json.Marshal(framework.CreditResponse{Channel: "chan", Username: "dazza", Amount: 35})
			return &framework.EventData{PluginResponse: &framework.PluginResponse{Success: true, Data: &framework.ResponseData{RawJSON: payload}}}, nil
		default:
			return nil, nil
		}
	}

	oldRoll := rollFindAmountFunc
	oldBad := couchBadEventRoll
	oldAfter := couchAfterFunc
	oldDelay := couchAnnouncementDelay
	rollFindAmountFunc = func(bool) int { return 35 }
	couchBadEventRoll = func() float64 { return 1 }
	couchAnnouncementDelay = 0
	couchAfterFunc = func(_ time.Duration, f func()) *time.Timer {
		f()
		return time.NewTimer(0)
	}
	defer func() {
		rollFindAmountFunc = oldRoll
		couchBadEventRoll = oldBad
		couchAfterFunc = oldAfter
		couchAnnouncementDelay = oldDelay
	}()

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

	found := false
	for _, broadcast := range bus.broadcasts {
		if broadcast.data.RawMessage != nil && strings.Contains(broadcast.data.RawMessage.Message, "COUCH JACKPOT") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected couch jackpot announcement")
	}
}
