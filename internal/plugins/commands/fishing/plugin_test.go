package fishing

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

type fishingBroadcast struct {
	eventType string
	data      *framework.EventData
}

type fishingEventBus struct {
	broadcasts     []fishingBroadcast
	requestHandler func(eventType string, data *framework.EventData) (*framework.EventData, error)
}

func (b *fishingEventBus) Broadcast(eventType string, data *framework.EventData) error {
	b.broadcasts = append(b.broadcasts, fishingBroadcast{eventType: eventType, data: data})
	return nil
}

func (b *fishingEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return b.Broadcast(eventType, data)
}

func (b *fishingEventBus) Send(target string, eventType string, data *framework.EventData) error {
	return nil
}

func (b *fishingEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return nil
}

func (b *fishingEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	if b.requestHandler != nil {
		return b.requestHandler(eventType, data)
	}
	return nil, nil
}

func (b *fishingEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
}

func (b *fishingEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	return nil
}

func (b *fishingEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	return nil
}

func (b *fishingEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	return nil
}

func (b *fishingEventBus) UnregisterPlugin(name string) error {
	return nil
}

func (b *fishingEventBus) GetDroppedEventCounts() map[string]int64 {
	return map[string]int64{}
}

func (b *fishingEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

func TestFishingCooldown(t *testing.T) {
	now := time.Now().UTC()
	when, _ := json.Marshal(now)

	bus := &fishingEventBus{}
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
	p.cooldown = 2 * time.Hour

	req := &framework.PluginRequest{Data: &framework.RequestData{Command: &framework.CommandData{Params: map[string]string{"channel": "chan", "username": "dazza"}}}}
	event := &framework.DataEvent{Data: &framework.EventData{PluginRequest: req}}
	if err := p.handleCommand(event); err != nil {
		t.Fatalf("handleCommand failed: %v", err)
	}

	if len(bus.broadcasts) == 0 {
		t.Fatalf("expected cooldown response")
	}
	if bus.broadcasts[0].eventType != "cytube.send.pm" {
		t.Fatalf("expected pm response")
	}
	message := strings.ToLower(bus.broadcasts[0].data.PrivateMessage.Message)
	if !strings.Contains(message, "wait") && !strings.Contains(message, "tide") && !strings.Contains(message, "rest") && !strings.Contains(message, "left") && !strings.Contains(message, "ciggie") && !strings.Contains(message, "smoke") {
		t.Fatalf("unexpected cooldown message: %s", bus.broadcasts[0].data.PrivateMessage.Message)
	}
}

func TestFishingPMAndAnnouncement(t *testing.T) {
	bus := &fishingEventBus{}
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
			if data.PluginRequest != nil && data.PluginRequest.Type == "economy.debit" {
				payload, _ := json.Marshal(framework.DebitResponse{Channel: "chan", Username: "dazza", Amount: 2})
				return &framework.EventData{PluginResponse: &framework.PluginResponse{Success: true, Data: &framework.ResponseData{RawJSON: payload}}}, nil
			}
			payload, _ := json.Marshal(framework.CreditResponse{Channel: "chan", Username: "dazza", Amount: 150})
			return &framework.EventData{PluginResponse: &framework.PluginResponse{Success: true, Data: &framework.ResponseData{RawJSON: payload}}}, nil
		default:
			return nil, nil
		}
	}

	oldRoll := rollFishingResultFunc
	rollFishingResultFunc = func(bait baitInfo, username string) fishingResult {
		return fishingResult{
			message:            "🎣 You caught a 5.0kg barra worth $150!",
			publicAnnouncement: "🎣💥 Rare catch! dazza just hauled in a 5.0kg barra worth $150",
			payout:             150,
			bestCatch:          150,
			bigWin:             true,
			caught:             true,
			rare:               true,
		}
	}
	defer func() { rollFishingResultFunc = oldRoll }()

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

	var sawPM bool
	var sawAnnouncement bool
	for _, broadcast := range bus.broadcasts {
		if broadcast.eventType == "cytube.send.pm" {
			sawPM = true
		}
		if broadcast.eventType == "cytube.send" && strings.Contains(broadcast.data.RawMessage.Message, "Rare catch") {
			sawAnnouncement = true
		}
	}
	if !sawPM {
		t.Fatalf("expected pm result")
	}
	if !sawAnnouncement {
		t.Fatalf("expected public announcement")
	}
}

func TestFishingInsufficientFunds(t *testing.T) {
	bus := &fishingEventBus{}
	bus.requestHandler = func(eventType string, data *framework.EventData) (*framework.EventData, error) {
		switch eventType {
		case "sql.query.request":
			return &framework.EventData{SQLQueryResponse: &framework.SQLQueryResponse{Success: true, Columns: []string{"last_played_at"}, Rows: [][]json.RawMessage{}}}, nil
		case "plugin.request":
			if data.PluginRequest != nil && data.PluginRequest.Type == "economy.get_balance" {
				payload, _ := json.Marshal(framework.GetBalanceResponse{Channel: "chan", Username: "dazza", Balance: 10})
				return &framework.EventData{PluginResponse: &framework.PluginResponse{Success: true, Data: &framework.ResponseData{RawJSON: payload}}}, nil
			}
			return nil, nil
		default:
			return nil, nil
		}
	}

	p := New().(*Plugin)
	if err := p.Init(json.RawMessage("{}"), bus); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	p.cooldown = 0

	req := &framework.PluginRequest{Data: &framework.RequestData{Command: &framework.CommandData{Params: map[string]string{"channel": "chan", "username": "dazza"}, Args: []string{"squid"}}}}
	event := &framework.DataEvent{Data: &framework.EventData{PluginRequest: req}}
	if err := p.handleCommand(event); err != nil {
		t.Fatalf("handleCommand failed: %v", err)
	}

	if len(bus.broadcasts) == 0 {
		t.Fatalf("expected a response")
	}
	message := bus.broadcasts[len(bus.broadcasts)-1].data.PrivateMessage.Message
	if !strings.Contains(message, "$40") {
		t.Fatalf("unexpected message: %s", message)
	}
}
