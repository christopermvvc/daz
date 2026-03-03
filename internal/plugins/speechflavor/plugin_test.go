package speechflavor

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
)

type deliveryRecord struct {
	correlationID string
	response      *framework.EventData
	err           error
}

type testEventBus struct {
	mu            sync.Mutex
	subscriptions []string
	deliveries    []deliveryRecord
}

func (b *testEventBus) Broadcast(eventType string, data *framework.EventData) error {
	_ = eventType
	_ = data
	return nil
}

func (b *testEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	_ = eventType
	_ = data
	_ = metadata
	return nil
}

func (b *testEventBus) Send(target string, eventType string, data *framework.EventData) error {
	_ = target
	_ = eventType
	_ = data
	return nil
}

func (b *testEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	_ = target
	_ = eventType
	_ = data
	_ = metadata
	return nil
}

func (b *testEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	_ = ctx
	_ = target
	_ = eventType
	_ = data
	_ = metadata
	return nil, nil
}

func (b *testEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.deliveries = append(b.deliveries, deliveryRecord{
		correlationID: correlationID,
		response:      response,
		err:           err,
	})
}

func (b *testEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	_ = handler
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscriptions = append(b.subscriptions, eventType)
	return nil
}

func (b *testEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	_ = pattern
	_ = handler
	_ = tags
	return nil
}

func (b *testEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	_ = name
	_ = plugin
	return nil
}

func (b *testEventBus) UnregisterPlugin(name string) error {
	_ = name
	return nil
}

func (b *testEventBus) GetDroppedEventCounts() map[string]int64 {
	return map[string]int64{}
}

func (b *testEventBus) GetDroppedEventCount(eventType string) int64 {
	_ = eventType
	return 0
}

func TestStartSubscribesPluginRequest(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)

	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}

	found := false
	for _, s := range bus.subscriptions {
		if s == "plugin.request" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected plugin.request subscription")
	}
}

func TestRewriteSuccessPreservesTokens(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.generateFunc = func(ctx context.Context, req framework.OllamaGenerateRequest) (framework.OllamaGenerateResponse, error) {
		_ = ctx
		if req.Message != "hey __DAZ_TOKEN_0__ from __DAZ_TOKEN_1__" {
			t.Fatalf("unexpected protected message: %q", req.Message)
		}
		return framework.OllamaGenerateResponse{
			Text:  "oi __DAZ_TOKEN_0__ from __DAZ_TOKEN_1__",
			Model: "unit-model",
		}, nil
	}

	raw := []byte(`{"text":"hey <user> from !update"}`)
	event := &framework.DataEvent{
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				ID:   "rewrite-1",
				To:   pluginName,
				Type: operationRewrite,
				Data: &framework.RequestData{RawJSON: raw},
			},
		},
	}

	if err := p.handlePluginRequest(event); err != nil {
		t.Fatalf("handlePluginRequest error: %v", err)
	}

	if len(bus.deliveries) != 1 {
		t.Fatalf("expected one delivery, got %d", len(bus.deliveries))
	}
	resp := bus.deliveries[0].response.PluginResponse
	if !resp.Success {
		t.Fatalf("expected success response, got error: %s", resp.Error)
	}

	var payload rewriteResponse
	if err := json.Unmarshal(resp.Data.RawJSON, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if payload.Text != "oi <user> from !update" {
		t.Fatalf("unexpected rewritten text: %q", payload.Text)
	}
	if payload.FallbackUsed {
		t.Fatalf("did not expect fallback")
	}
}

func TestRewriteFallsBackOnTokenLoss(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.generateFunc = func(ctx context.Context, req framework.OllamaGenerateRequest) (framework.OllamaGenerateResponse, error) {
		_ = ctx
		_ = req
		return framework.OllamaGenerateResponse{
			Text: "oi mate",
		}, nil
	}

	raw := []byte(`{"text":"hello <user>"}`)
	event := &framework.DataEvent{
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				ID:   "rewrite-2",
				To:   pluginName,
				Type: operationRewrite,
				Data: &framework.RequestData{RawJSON: raw},
			},
		},
	}
	if err := p.handlePluginRequest(event); err != nil {
		t.Fatalf("handlePluginRequest error: %v", err)
	}

	resp := bus.deliveries[0].response.PluginResponse
	var payload rewriteResponse
	if err := json.Unmarshal(resp.Data.RawJSON, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !payload.FallbackUsed {
		t.Fatalf("expected fallback response")
	}
	if payload.Reason != "token_preservation_failed" {
		t.Fatalf("unexpected reason: %q", payload.Reason)
	}
	if payload.Text != "hello <user>" {
		t.Fatalf("expected original text fallback, got %q", payload.Text)
	}
}

func TestRewriteFallsBackOnPolarityMismatch(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.generateFunc = func(ctx context.Context, req framework.OllamaGenerateRequest) (framework.OllamaGenerateResponse, error) {
		_ = ctx
		_ = req
		return framework.OllamaGenerateResponse{Text: "no"}, nil
	}

	raw := []byte(`{"text":"yes"}`)
	event := &framework.DataEvent{
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				ID:   "rewrite-3",
				To:   pluginName,
				Type: operationRewrite,
				Data: &framework.RequestData{RawJSON: raw},
			},
		},
	}
	if err := p.handlePluginRequest(event); err != nil {
		t.Fatalf("handlePluginRequest error: %v", err)
	}

	resp := bus.deliveries[0].response.PluginResponse
	var payload rewriteResponse
	if err := json.Unmarshal(resp.Data.RawJSON, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !payload.FallbackUsed {
		t.Fatalf("expected fallback response")
	}
	if payload.Reason != "polarity_guard_triggered" {
		t.Fatalf("unexpected fallback reason: %q", payload.Reason)
	}
	if payload.Text != "yes" {
		t.Fatalf("expected original text fallback, got %q", payload.Text)
	}
}

func TestRewriteDisabledReturnsOriginal(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init([]byte(`{"enabled":false}`), bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	raw := []byte(`{"text":"Hi, how are you doing?"}`)
	event := &framework.DataEvent{
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				ID:   "rewrite-4",
				To:   pluginName,
				Type: operationRewrite,
				Data: &framework.RequestData{RawJSON: raw},
			},
		},
	}
	if err := p.handlePluginRequest(event); err != nil {
		t.Fatalf("handlePluginRequest error: %v", err)
	}

	resp := bus.deliveries[0].response.PluginResponse
	var payload rewriteResponse
	if err := json.Unmarshal(resp.Data.RawJSON, &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !payload.FallbackUsed || payload.Reason != "disabled" {
		t.Fatalf("expected disabled fallback, got %+v", payload)
	}
	if payload.Text != "Hi, how are you doing?" {
		t.Fatalf("unexpected text %q", payload.Text)
	}
}

func TestProtectAndRestoreTokens(t *testing.T) {
	protected, tokens := protectTokens("run !update for <user> with %s and ${name}")
	if len(tokens) != 4 {
		t.Fatalf("expected 4 tokens, got %d", len(tokens))
	}
	restored, ok := restoreTokens(protected, tokens)
	if !ok {
		t.Fatalf("restoreTokens reported failure")
	}
	if restored != "run !update for <user> with %s and ${name}" {
		t.Fatalf("unexpected restored text: %q", restored)
	}
}
