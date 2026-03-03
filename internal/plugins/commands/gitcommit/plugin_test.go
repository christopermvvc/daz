package gitcommit

import (
	"context"
	"database/sql"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/buildinfo"
	"github.com/hildolfr/daz/internal/framework"
)

type mockEventBus struct {
	mu         sync.Mutex
	broadcasts []broadcastCall
}

type broadcastCall struct {
	eventType string
	data      *framework.EventData
}

func (m *mockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.broadcasts = append(m.broadcasts, broadcastCall{eventType: eventType, data: data})
	return nil
}

func (m *mockEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	_ = metadata
	return m.Broadcast(eventType, data)
}

func (m *mockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	_ = target
	_ = eventType
	_ = data
	return nil
}

func (m *mockEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	_ = metadata
	return m.Send(target, eventType, data)
}

func (m *mockEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	_ = ctx
	_ = target
	_ = eventType
	_ = data
	_ = metadata
	return nil, nil
}

func (m *mockEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
	_ = correlationID
	_ = response
	_ = err
}

func (m *mockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	_ = eventType
	_ = handler
	return nil
}

func (m *mockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	_ = pattern
	_ = handler
	_ = tags
	return nil
}

func (m *mockEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	_ = name
	_ = plugin
	return nil
}

func (m *mockEventBus) UnregisterPlugin(name string) error {
	_ = name
	return nil
}

func (m *mockEventBus) GetDroppedEventCounts() map[string]int64 {
	return map[string]int64{}
}

func (m *mockEventBus) GetDroppedEventCount(eventType string) int64 {
	_ = eventType
	return 0
}

func (m *mockEventBus) QuerySync(ctx context.Context, query string, params ...framework.SQLParam) (*sql.Rows, error) {
	_ = ctx
	_ = query
	_ = params
	return nil, nil
}

func (m *mockEventBus) ExecSync(ctx context.Context, query string, params ...framework.SQLParam) (sql.Result, error) {
	_ = ctx
	_ = query
	_ = params
	return nil, nil
}

func mkCommandEvent(username, channel string, isPM, isAdmin bool) *framework.DataEvent {
	return &framework.DataEvent{
		EventType: "command.gitcommit.execute",
		EventTime: time.Now(),
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				Data: &framework.RequestData{
					Command: &framework.CommandData{
						Params: map[string]string{
							"username": username,
							"channel":  channel,
							"is_pm":    boolToString(isPM),
							"is_admin": boolToString(isAdmin),
						},
					},
				},
			},
		},
	}
}

func boolToString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func TestStartRegistersCommandAdminOnly(t *testing.T) {
	bus := &mockEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer p.Stop()

	bus.mu.Lock()
	defer bus.mu.Unlock()

	for _, b := range bus.broadcasts {
		if b.eventType != "command.register" || b.data == nil || b.data.PluginRequest == nil || b.data.PluginRequest.Data == nil {
			continue
		}
		fields := b.data.PluginRequest.Data.KeyValue
		if !hasCommandAliases(fields["commands"], "gitcommit", "gitrev", "gitver") {
			continue
		}
		if fields["admin_only"] != "true" {
			t.Fatalf("expected admin_only=true, got %q", fields["admin_only"])
		}
		return
	}

	t.Fatalf("expected command.register for gitcommit")
}

func TestStartRegistersGitverAlias(t *testing.T) {
	bus := &mockEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	if err := p.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	defer p.Stop()

	bus.mu.Lock()
	defer bus.mu.Unlock()

	for _, b := range bus.broadcasts {
		if b.eventType != "command.register" || b.data == nil || b.data.PluginRequest == nil || b.data.PluginRequest.Data == nil {
			continue
		}
		commands := b.data.PluginRequest.Data.KeyValue["commands"]
		if hasCommandAliases(commands, "gitcommit", "gitver") {
			return
		}
	}

	t.Fatalf("expected gitver alias to be registered")
}

func TestHandleCommandAdminChat(t *testing.T) {
	bus := &mockEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.resolveCommit = func() string { return "Current git commit: deadbeefcaf0" }
	p.rewriteMessage = nil

	if err := p.handleCommand(mkCommandEvent("alice", "chan", false, true)); err != nil {
		t.Fatalf("handleCommand error: %v", err)
	}

	bus.mu.Lock()
	defer bus.mu.Unlock()

	for _, b := range bus.broadcasts {
		if b.eventType == "cytube.send" && b.data != nil && b.data.RawMessage != nil {
			if b.data.RawMessage.Message != "Current git commit: deadbeefcaf0" {
				t.Fatalf("got %q, want commit message", b.data.RawMessage.Message)
			}
			return
		}
	}
	t.Fatalf("expected cytube.send response")
}

func TestHandleCommandNonAdminDenied(t *testing.T) {
	bus := &mockEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.resolveCommit = func() string { return "Current git commit: deadbeefcaf0" }

	if err := p.handleCommand(mkCommandEvent("alice", "chan", false, false)); err != nil {
		t.Fatalf("handleCommand error: %v", err)
	}

	bus.mu.Lock()
	defer bus.mu.Unlock()

	for _, b := range bus.broadcasts {
		if b.eventType == "cytube.send" && b.data != nil && b.data.RawMessage != nil {
			if b.data.RawMessage.Message != "This command is admin-only." {
				t.Fatalf("got %q, want admin-only message", b.data.RawMessage.Message)
			}
			return
		}
	}
	t.Fatalf("expected cytube.send response")
}

func TestHandleCommandAdminChatUsesSpeechFlavor(t *testing.T) {
	bus := &mockEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.resolveCommit = func() string { return "Current git commit: deadbeefcaf0 (dirty)" }
	p.rewriteMessage = func(ctx context.Context, req framework.SpeechFlavorRewriteRequest) (framework.SpeechFlavorRewriteResponse, error) {
		_ = ctx
		_ = req
		return framework.SpeechFlavorRewriteResponse{
			Text: "oi mate deadbeefcaf0 still dirty",
		}, nil
	}

	if err := p.handleCommand(mkCommandEvent("alice", "chan", false, true)); err != nil {
		t.Fatalf("handleCommand error: %v", err)
	}

	bus.mu.Lock()
	defer bus.mu.Unlock()
	for _, b := range bus.broadcasts {
		if b.eventType == "cytube.send" && b.data != nil && b.data.RawMessage != nil {
			if b.data.RawMessage.Message != "oi mate deadbeefcaf0 still dirty" {
				t.Fatalf("got %q, want flavored message", b.data.RawMessage.Message)
			}
			return
		}
	}
	t.Fatalf("expected cytube.send response")
}

func TestHandleCommandAdminChatFlavorFallbackWhenCommitMissing(t *testing.T) {
	bus := &mockEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.resolveCommit = func() string { return "Current git commit: deadbeefcaf0" }
	p.rewriteMessage = func(ctx context.Context, req framework.SpeechFlavorRewriteRequest) (framework.SpeechFlavorRewriteResponse, error) {
		_ = ctx
		_ = req
		return framework.SpeechFlavorRewriteResponse{
			Text: "yeah mate latest build running",
		}, nil
	}

	if err := p.handleCommand(mkCommandEvent("alice", "chan", false, true)); err != nil {
		t.Fatalf("handleCommand error: %v", err)
	}

	bus.mu.Lock()
	defer bus.mu.Unlock()
	for _, b := range bus.broadcasts {
		if b.eventType == "cytube.send" && b.data != nil && b.data.RawMessage != nil {
			if b.data.RawMessage.Message != "Current git commit: deadbeefcaf0" {
				t.Fatalf("got %q, want original message fallback", b.data.RawMessage.Message)
			}
			return
		}
	}
	t.Fatalf("expected cytube.send response")
}

func TestHandleCommandAdminPM(t *testing.T) {
	bus := &mockEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.resolveCommit = func() string { return "Current git commit: deadbeefcaf0" }
	p.rewriteMessage = nil

	if err := p.handleCommand(mkCommandEvent("alice", "chan", true, true)); err != nil {
		t.Fatalf("handleCommand error: %v", err)
	}

	bus.mu.Lock()
	defer bus.mu.Unlock()

	for _, b := range bus.broadcasts {
		if b.eventType != "plugin.response" || b.data == nil || b.data.PluginResponse == nil || b.data.PluginResponse.Data == nil || b.data.PluginResponse.Data.CommandResult == nil {
			continue
		}
		if b.data.PluginResponse.Data.CommandResult.Output != "Current git commit: deadbeefcaf0" {
			t.Fatalf("got %q, want commit message", b.data.PluginResponse.Data.CommandResult.Output)
		}
		return
	}
	t.Fatalf("expected plugin.response response")
}

func TestDefaultCommitMessageUsesInjectedBuildInfo(t *testing.T) {
	prevCommit := buildinfo.GitCommit
	prevDirty := buildinfo.GitDirty
	t.Cleanup(func() {
		buildinfo.GitCommit = prevCommit
		buildinfo.GitDirty = prevDirty
	})

	buildinfo.GitCommit = "deadbeefcafebabe"
	buildinfo.GitDirty = "true"

	got := defaultCommitMessage()
	want := "Current git commit: deadbeefcafe (dirty)"
	if got != want {
		t.Fatalf("defaultCommitMessage() = %q, want %q", got, want)
	}
}

func TestCommitDetailsPreserved(t *testing.T) {
	tests := []struct {
		name      string
		original  string
		rewritten string
		want      bool
	}{
		{
			name:      "preserved hash",
			original:  "Current git commit: deadbeefcaf0",
			rewritten: "oi mate deadbeefcaf0 is running",
			want:      true,
		},
		{
			name:      "missing hash",
			original:  "Current git commit: deadbeefcaf0",
			rewritten: "oi mate latest build",
			want:      false,
		},
		{
			name:      "dirty preserved",
			original:  "Current git commit: deadbeefcaf0 (dirty)",
			rewritten: "deadbeefcaf0 still dirty mate",
			want:      true,
		},
		{
			name:      "dirty dropped",
			original:  "Current git commit: deadbeefcaf0 (dirty)",
			rewritten: "deadbeefcaf0 running",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commitDetailsPreserved(tt.original, tt.rewritten)
			if got != tt.want {
				t.Fatalf("commitDetailsPreserved(%q, %q)=%v want %v", tt.original, tt.rewritten, got, tt.want)
			}
		})
	}
}

func hasCommandAliases(commands string, aliases ...string) bool {
	parts := strings.Split(commands, ",")
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			seen[trimmed] = struct{}{}
		}
	}

	for _, alias := range aliases {
		if _, ok := seen[alias]; !ok {
			return false
		}
	}
	return true
}
