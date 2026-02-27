package quote

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
)

type quoteMockEventBus struct {
	broadcasts []quoteBroadcastCall
	subs       []string
}

type quoteBroadcastCall struct {
	eventType string
	data      *framework.EventData
}

func (m *quoteMockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	m.broadcasts = append(m.broadcasts, quoteBroadcastCall{eventType: eventType, data: data})
	return nil
}

func (m *quoteMockEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	_ = metadata
	return m.Broadcast(eventType, data)
}

func (m *quoteMockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	_ = target
	_ = eventType
	_ = data
	return nil
}

func (m *quoteMockEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	_ = metadata
	return m.Send(target, eventType, data)
}

func (m *quoteMockEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	_ = ctx
	_ = target
	_ = eventType
	_ = data
	_ = metadata
	return nil, nil
}

func (m *quoteMockEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
	_ = correlationID
	_ = response
	_ = err
}

func (m *quoteMockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	_ = handler
	m.subs = append(m.subs, eventType)
	return nil
}

func (m *quoteMockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	_ = pattern
	_ = handler
	_ = tags
	return nil
}

func (m *quoteMockEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	_ = name
	_ = plugin
	return nil
}

func (m *quoteMockEventBus) UnregisterPlugin(name string) error {
	_ = name
	return nil
}

func (m *quoteMockEventBus) GetDroppedEventCounts() map[string]int64 {
	return map[string]int64{}
}

func (m *quoteMockEventBus) GetDroppedEventCount(eventType string) int64 {
	_ = eventType
	return 0
}

func TestStartRegistersAndSubscribes(t *testing.T) {
	bus := &quoteMockEventBus{}
	p := New().(*Plugin)

	if err := p.Init(json.RawMessage("{}"), bus); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("start failed: %v", err)
	}

	foundSub := false
	for _, sub := range bus.subs {
		if sub == "command.quote.execute" {
			foundSub = true
			break
		}
	}
	if !foundSub {
		t.Fatalf("expected subscription to command.quote.execute")
	}

	foundReg := false
	for _, call := range bus.broadcasts {
		if call.eventType != "command.register" || call.data == nil || call.data.PluginRequest == nil || call.data.PluginRequest.Data == nil {
			continue
		}
		if call.data.PluginRequest.Data.KeyValue["commands"] == "quote,q,rq" {
			foundReg = true
			break
		}
	}
	if !foundReg {
		t.Fatalf("expected command.register broadcast with quote commands")
	}
}

func TestHandleCommandUsageWhenUsernameMissing(t *testing.T) {
	bus := &quoteMockEventBus{}
	p := New().(*Plugin)
	p.eventBus = bus
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	err := p.handleCommand(&framework.DataEvent{Data: &framework.EventData{PluginRequest: &framework.PluginRequest{
		ID: "req-quote-usage",
		Data: &framework.RequestData{Command: &framework.CommandData{
			Name: "quote",
			Args: []string{},
			Params: map[string]string{
				"username": "tester",
				"channel":  "always_always_sunny",
			},
		}},
	}}})
	if err != nil {
		t.Fatalf("handleCommand failed: %v", err)
	}

	foundUsage := false
	for _, call := range bus.broadcasts {
		if call.eventType != "cytube.send" || call.data == nil || call.data.RawMessage == nil {
			continue
		}
		if call.data.RawMessage.Message == "Usage: !quote <username> (or !rq for random quote)" {
			foundUsage = true
			break
		}
	}

	if !foundUsage {
		t.Fatalf("expected usage response for missing quote username")
	}
}
