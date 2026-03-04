package remind

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

func TestParseTimeString(t *testing.T) {
	tests := []struct {
		input string
		ok    bool
	}{
		{"5m", true},
		{"5min", true},
		{"5mins", true},
		{"5minutes", true},
		{"5 minutes", true},
		{"1h30m", true},
		{"1h 30m", true},
		{"1 hour 30 minutes", true},
		{"2hrs", true},
		{"1day", true},
		{"2d", true},
		{"10s", true},
		{"1h0m", true},
		{"15", true},
		{"", false},
		{"5", true},
		{"m5", false},
		{"5x", false},
		{"1h30", false},
	}

	for _, tc := range tests {
		_, ok := parseTimeString(tc.input)
		if ok != tc.ok {
			t.Fatalf("parseTimeString(%q) ok=%v want %v", tc.input, ok, tc.ok)
		}
	}
}

func TestParseTimeStringImplicitMinutes(t *testing.T) {
	d, ok := parseTimeString("15")
	if !ok {
		t.Fatal("expected implicit minute input to parse")
	}
	if d != 15*time.Minute {
		t.Fatalf("expected 15m duration, got %v", d)
	}
}

func TestParseReminderDuration(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantInput    string
		wantDuration time.Duration
		wantOK       bool
	}{
		{
			name:         "single token implicit minutes",
			args:         []string{"15"},
			wantInput:    "15",
			wantDuration: 15 * time.Minute,
			wantOK:       true,
		},
		{
			name:         "multi token compact duration",
			args:         []string{"1h", "30m"},
			wantInput:    "1h 30m",
			wantDuration: 90 * time.Minute,
			wantOK:       true,
		},
		{
			name:         "multi token natural language duration",
			args:         []string{"1", "hour", "30", "minutes"},
			wantInput:    "1 hour 30 minutes",
			wantDuration: 90 * time.Minute,
			wantOK:       true,
		},
		{
			name:      "empty args",
			args:      []string{},
			wantInput: "",
			wantOK:    false,
		},
		{
			name:      "invalid duration",
			args:      []string{"soon", "ish"},
			wantInput: "soon ish",
			wantOK:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input, duration, ok := parseReminderDuration(tc.args)
			if ok != tc.wantOK {
				t.Fatalf("parseReminderDuration(%v) ok=%v want %v", tc.args, ok, tc.wantOK)
			}
			if input != tc.wantInput {
				t.Fatalf("parseReminderDuration(%v) input=%q want %q", tc.args, input, tc.wantInput)
			}
			if tc.wantOK && duration != tc.wantDuration {
				t.Fatalf("parseReminderDuration(%v) duration=%v want %v", tc.args, duration, tc.wantDuration)
			}
		})
	}
}

type remindBroadcast struct {
	eventType string
	data      *framework.EventData
}

type remindMockBus struct {
	mu            sync.Mutex
	subscriptions map[string][]framework.EventHandler
	broadcasts    []remindBroadcast
}

func newRemindMockBus() *remindMockBus {
	return &remindMockBus{
		subscriptions: make(map[string][]framework.EventHandler),
		broadcasts:    make([]remindBroadcast, 0),
	}
}

func (m *remindMockBus) Broadcast(eventType string, data *framework.EventData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.broadcasts = append(m.broadcasts, remindBroadcast{eventType: eventType, data: data})
	return nil
}

func (m *remindMockBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	_ = metadata
	return m.Broadcast(eventType, data)
}

func (m *remindMockBus) Send(target string, eventType string, data *framework.EventData) error {
	_ = target
	return m.Broadcast(eventType, data)
}

func (m *remindMockBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	_ = metadata
	return m.Send(target, eventType, data)
}

func (m *remindMockBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	_ = ctx
	_ = target
	_ = eventType
	_ = data
	_ = metadata
	return nil, context.DeadlineExceeded
}

func (m *remindMockBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
	_ = correlationID
	_ = response
	_ = err
}

func (m *remindMockBus) Subscribe(eventType string, handler framework.EventHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscriptions[eventType] = append(m.subscriptions[eventType], handler)
	return nil
}

func (m *remindMockBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	_ = tags
	return m.Subscribe(pattern, handler)
}

func (m *remindMockBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	_ = name
	_ = plugin
	return nil
}

func (m *remindMockBus) UnregisterPlugin(name string) error {
	_ = name
	return nil
}

func (m *remindMockBus) GetDroppedEventCounts() map[string]int64 {
	return map[string]int64{}
}

func (m *remindMockBus) GetDroppedEventCount(eventType string) int64 {
	_ = eventType
	return 0
}

func (m *remindMockBus) lastChannelMessage() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.broadcasts) - 1; i >= 0; i-- {
		call := m.broadcasts[i]
		if call.eventType != "cytube.send" || call.data == nil || call.data.RawMessage == nil {
			continue
		}
		return call.data.RawMessage.Message, true
	}
	return "", false
}

func TestSendChannelMessageUsesSpeechFlavorTemplate(t *testing.T) {
	bus := newRemindMockBus()
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	p.rewriteMessage = func(ctx context.Context, req framework.SpeechFlavorRewriteRequest) (framework.SpeechFlavorRewriteResponse, error) {
		_ = ctx
		if req.Text != "righto {username}, timer set for {duration}" {
			t.Fatalf("unexpected rewrite text %q", req.Text)
		}
		return framework.SpeechFlavorRewriteResponse{
			Text: "sweet as {username}, reminder locked for {duration}",
		}, nil
	}

	p.sendChannelMessage("always_always_sunny", "alice", "righto {username}, timer set for {duration}", map[string]string{
		"{username}": "alice",
		"{duration}": "5m",
	})

	got, ok := bus.lastChannelMessage()
	if !ok {
		t.Fatal("expected channel message broadcast")
	}
	if got != "sweet as alice, reminder locked for 5m" {
		t.Fatalf("unexpected flavored output %q", got)
	}
}

func TestSendChannelMessageFlavorFallbackOnRewriteError(t *testing.T) {
	bus := newRemindMockBus()
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	p.rewriteMessage = func(ctx context.Context, req framework.SpeechFlavorRewriteRequest) (framework.SpeechFlavorRewriteResponse, error) {
		_ = ctx
		_ = req
		return framework.SpeechFlavorRewriteResponse{}, context.DeadlineExceeded
	}

	p.sendChannelMessage("always_always_sunny", "alice", "righto {username}, timer set for {duration}", map[string]string{
		"{username}": "alice",
		"{duration}": "5m",
	})

	got, ok := bus.lastChannelMessage()
	if !ok {
		t.Fatal("expected channel message broadcast")
	}
	if got != "righto alice, timer set for 5m" {
		t.Fatalf("unexpected fallback output %q", got)
	}
}

func TestHandleCommandUsesSpeechFlavorOnValidationResponse(t *testing.T) {
	bus := newRemindMockBus()
	p := New().(*Plugin)
	if err := p.Init(json.RawMessage(`{"cooldown_seconds": 1}`), bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	p.rewriteMessage = func(ctx context.Context, req framework.SpeechFlavorRewriteRequest) (framework.SpeechFlavorRewriteResponse, error) {
		_ = ctx
		if !strings.Contains(req.Text, "dunno what time that is mate") {
			return framework.SpeechFlavorRewriteResponse{Text: req.Text}, nil
		}
		return framework.SpeechFlavorRewriteResponse{
			Text: "nah mate that time format's cooked, try 5m or 1h30m",
		}, nil
	}

	event := &framework.DataEvent{
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				Data: &framework.RequestData{
					Command: &framework.CommandData{
						Name: "remind",
						Args: []string{"soon"},
						Params: map[string]string{
							"username": "alice",
							"channel":  "always_always_sunny",
						},
					},
				},
			},
		},
	}

	if err := p.handleCommand(event); err != nil {
		t.Fatalf("handleCommand error: %v", err)
	}

	got, ok := bus.lastChannelMessage()
	if !ok {
		t.Fatal("expected channel message broadcast")
	}
	if got != "nah mate that time format's cooked, try 5m or 1h30m" {
		t.Fatalf("unexpected command response %q", got)
	}
}
