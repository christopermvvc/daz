package mysterybox

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

type mysteryboxBroadcast struct {
	eventType string
	data      *framework.EventData
}

type mysteryboxEventBus struct {
	broadcasts     []mysteryboxBroadcast
	requestHandler func(eventType string, data *framework.EventData) (*framework.EventData, error)
}

func (b *mysteryboxEventBus) Broadcast(eventType string, data *framework.EventData) error {
	b.broadcasts = append(b.broadcasts, mysteryboxBroadcast{eventType: eventType, data: data})
	return nil
}

func (b *mysteryboxEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return b.Broadcast(eventType, data)
}

func (b *mysteryboxEventBus) Send(target string, eventType string, data *framework.EventData) error {
	return nil
}

func (b *mysteryboxEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return nil
}

func (b *mysteryboxEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	if b.requestHandler != nil {
		return b.requestHandler(eventType, data)
	}
	return nil, nil
}

func (b *mysteryboxEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
}

func (b *mysteryboxEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	return nil
}

func (b *mysteryboxEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	return nil
}

func (b *mysteryboxEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	return nil
}

func (b *mysteryboxEventBus) UnregisterPlugin(name string) error {
	return nil
}

func (b *mysteryboxEventBus) GetDroppedEventCounts() map[string]int64 {
	return map[string]int64{}
}

func (b *mysteryboxEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

func TestMysteryBoxCooldown(t *testing.T) {
	now := time.Now().UTC()
	when, _ := json.Marshal(now)

	bus := &mysteryboxEventBus{}
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
	p.cooldown = 24 * time.Hour

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
	if !strings.Contains(bus.broadcasts[0].data.RawMessage.Message, "already cracked a box") {
		t.Fatalf("unexpected cooldown message: %s", bus.broadcasts[0].data.RawMessage.Message)
	}
}

func TestMysteryBoxPMRouting(t *testing.T) {
	bus := &mysteryboxEventBus{}
	bus.requestHandler = func(eventType string, data *framework.EventData) (*framework.EventData, error) {
		if eventType == "sql.query.request" {
			return &framework.EventData{SQLQueryResponse: &framework.SQLQueryResponse{Success: true, Columns: []string{"last_played_at"}, Rows: [][]json.RawMessage{}}}, nil
		}
		if eventType == "sql.exec.request" {
			return &framework.EventData{SQLExecResponse: &framework.SQLExecResponse{Success: true}}, nil
		}
		return nil, nil
	}

	oldRoll := rollMysteryBoxFunc
	rollMysteryBoxFunc = func() mysteryResult {
		return mysteryResult{amount: 0, message: "empty", jackpot: false, bigWin: false}
	}
	defer func() { rollMysteryBoxFunc = oldRoll }()

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

func TestMysteryBoxCreditFailure(t *testing.T) {
	bus := &mysteryboxEventBus{}
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

	oldRoll := rollMysteryBoxFunc
	rollMysteryBoxFunc = func() mysteryResult {
		return mysteryResult{amount: 25, message: "nice!", jackpot: false, bigWin: false}
	}
	defer func() { rollMysteryBoxFunc = oldRoll }()

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
	got := bus.broadcasts[len(bus.broadcasts)-1].data.PrivateMessage.Message
	if got != "mystery box jammed. try again later" {
		t.Fatalf("unexpected message: %s", got)
	}
}
