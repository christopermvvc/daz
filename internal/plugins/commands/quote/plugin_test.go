package quote

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hildolfr/daz/internal/framework"
)

type quoteMockEventBus struct {
	broadcasts  []quoteBroadcastCall
	subs        []string
	onBroadcast func(eventType string, data *framework.EventData)
}

type quoteBroadcastCall struct {
	eventType string
	data      *framework.EventData
}

func (m *quoteMockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	m.broadcasts = append(m.broadcasts, quoteBroadcastCall{eventType: eventType, data: data})
	if m.onBroadcast != nil {
		m.onBroadcast(eventType, data)
	}
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

func TestIsSelfTarget(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		botUsername string
		want        bool
	}{
		{name: "no configured bot username", target: "dazza", botUsername: "", want: false},
		{name: "configured username", target: "myBot", botUsername: "myBot", want: true},
		{name: "normal user", target: "hildolfr", botUsername: "dazza", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSelfTarget(tt.target, tt.botUsername)
			if got != tt.want {
				t.Fatalf("isSelfTarget(%q, %q) = %v, want %v", tt.target, tt.botUsername, got, tt.want)
			}
		})
	}
}

func TestSanitizeQuoteMessage(t *testing.T) {
	raw := `<span class="quote">&gt;dip my nuts in ink </span>`
	got := sanitizeQuoteMessage(raw)

	if strings.Contains(got, "<span") || strings.Contains(got, "</span>") {
		t.Fatalf("expected html tags stripped, got %q", got)
	}
	if !strings.Contains(got, "&gt;dip my nuts in ink") {
		t.Fatalf("expected quote text preserved and escaped, got %q", got)
	}
}

func TestInitUsesEnvBotNameWhenConfigEmpty(t *testing.T) {
	t.Setenv("DAZ_BOT_NAME", "EnvBot")
	t.Setenv("DAZ_CYTUBE_USERNAME", "CytubeBot")

	p := New().(*Plugin)
	if err := p.Init(json.RawMessage("{}"), &quoteMockEventBus{}); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	if p.config.BotUsername != "EnvBot" {
		t.Fatalf("expected BotUsername from env, got %q", p.config.BotUsername)
	}
}

func TestResolveBotUsernameFallsBackToCytubeUsername(t *testing.T) {
	t.Setenv("DAZ_BOT_NAME", "")
	t.Setenv("DAZ_CYTUBE_USERNAME", "CytubeBot")

	got := resolveBotUsername("")
	if got != "CytubeBot" {
		t.Fatalf("expected DAZ_CYTUBE_USERNAME fallback, got %q", got)
	}
}

func TestResolveBotUsernamePrefersConfig(t *testing.T) {
	t.Setenv("DAZ_BOT_NAME", "EnvBot")
	t.Setenv("DAZ_CYTUBE_USERNAME", "CytubeBot")

	got := resolveBotUsername("ConfiguredBot")
	if got != "ConfiguredBot" {
		t.Fatalf("expected configured bot username, got %q", got)
	}
}

func TestResolveBotUsernameForChannelFromCoreResponse(t *testing.T) {
	bus := &quoteMockEventBus{}

	t.Setenv("DAZ_BOT_NAME", "")
	t.Setenv("DAZ_CYTUBE_USERNAME", "")

	p := New().(*Plugin)
	if err := p.Init(json.RawMessage("{}"), bus); err != nil {
		t.Fatalf("init failed: %v", err)
	}

	bus.onBroadcast = func(eventType string, data *framework.EventData) {
		if eventType != "plugin.request" || data == nil || data.PluginRequest == nil {
			return
		}
		requestID := data.PluginRequest.ID
		if requestID == "" {
			return
		}

		respEvent := &framework.DataEvent{Data: &framework.EventData{PluginResponse: &framework.PluginResponse{
			ID:      requestID,
			Success: true,
			Data: &framework.ResponseData{
				RawJSON: json.RawMessage(`{"channels":[{"channel":"always_always_sunny","username":"dazza","enabled":true,"connected":true}]}`),
			},
		}}}

		go func() {
			_ = p.handlePluginResponse(respEvent)
		}()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	botUsername, err := p.resolveBotUsernameForChannel(ctx, "always_always_sunny")
	if err != nil {
		t.Fatalf("resolveBotUsernameForChannel failed: %v", err)
	}
	if botUsername != "dazza" {
		t.Fatalf("expected bot username from core response, got %q", botUsername)
	}
}

func TestExtractBotUsernameFromChannels(t *testing.T) {
	username, err := extractBotUsernameFromChannels(
		json.RawMessage(`{"channels":[{"channel":"always_always_sunny","username":"dazza"}]}`),
		"always_always_sunny",
	)
	if err != nil {
		t.Fatalf("extractBotUsernameFromChannels failed: %v", err)
	}
	if username != "dazza" {
		t.Fatalf("expected dazza, got %q", username)
	}
}

func TestHandleCommandRejectsSelfTargetResolvedFromCore(t *testing.T) {
	bus := &quoteMockEventBus{}
	p := New().(*Plugin)

	t.Setenv("DAZ_BOT_NAME", "")
	t.Setenv("DAZ_CYTUBE_USERNAME", "")

	if err := p.Init(json.RawMessage("{}"), bus); err != nil {
		t.Fatalf("init failed: %v", err)
	}
	p.ctx, p.cancel = context.WithCancel(context.Background())
	defer p.cancel()

	bus.onBroadcast = func(eventType string, data *framework.EventData) {
		if eventType != "plugin.request" || data == nil || data.PluginRequest == nil {
			return
		}
		if data.PluginRequest.Type != "get_configured_channels" {
			return
		}

		requestID := data.PluginRequest.ID
		if requestID == "" {
			return
		}

		respEvent := &framework.DataEvent{Data: &framework.EventData{PluginResponse: &framework.PluginResponse{
			ID:      requestID,
			Success: true,
			Data: &framework.ResponseData{
				RawJSON: json.RawMessage(`{"channels":[{"channel":"always_always_sunny","username":"dazza","enabled":true,"connected":true}]}`),
			},
		}}}

		go func() {
			_ = p.handlePluginResponse(respEvent)
		}()
	}

	err := p.handleCommand(&framework.DataEvent{Data: &framework.EventData{PluginRequest: &framework.PluginRequest{
		ID: "req-quote-self",
		Data: &framework.RequestData{Command: &framework.CommandData{
			Name: "q",
			Args: []string{"dazza"},
			Params: map[string]string{
				"username": "hildolfr",
				"channel":  "always_always_sunny",
			},
		}},
	}}})
	if err != nil {
		t.Fatalf("handleCommand failed: %v", err)
	}

	foundBlock := false
	for _, call := range bus.broadcasts {
		if call.eventType != "cytube.send" || call.data == nil || call.data.RawMessage == nil {
			continue
		}
		if call.data.RawMessage.Message == "Nope. I won't quote myself." {
			foundBlock = true
			break
		}
	}

	if !foundBlock {
		t.Fatalf("expected self-quote block response")
	}
}
