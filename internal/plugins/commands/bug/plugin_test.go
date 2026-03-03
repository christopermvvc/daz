package bug

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

type broadcastCall struct {
	eventType string
	data      *framework.EventData
}

type testEventBus struct {
	mu            sync.Mutex
	broadcasts    []broadcastCall
	subscriptions []string
}

func (b *testEventBus) Broadcast(eventType string, data *framework.EventData) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.broadcasts = append(b.broadcasts, broadcastCall{eventType: eventType, data: data})
	return nil
}

func (b *testEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	_ = metadata
	return b.Broadcast(eventType, data)
}

func (b *testEventBus) Send(target string, eventType string, data *framework.EventData) error {
	_ = target
	_ = eventType
	_ = data
	return nil
}

func (b *testEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	_ = metadata
	return b.Send(target, eventType, data)
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
	_ = correlationID
	_ = response
	_ = err
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

func (b *testEventBus) QuerySync(ctx context.Context, query string, params ...framework.SQLParam) (*sql.Rows, error) {
	_ = ctx
	_ = query
	_ = params
	return nil, nil
}

func (b *testEventBus) ExecSync(ctx context.Context, query string, params ...framework.SQLParam) (sql.Result, error) {
	_ = ctx
	_ = query
	_ = params
	return nil, nil
}

func (b *testEventBus) lastChatMessage() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.broadcasts) - 1; i >= 0; i-- {
		call := b.broadcasts[i]
		if call.eventType == "cytube.send" && call.data != nil && call.data.RawMessage != nil {
			return call.data.RawMessage.Message
		}
	}
	return ""
}

func (b *testEventBus) lastPMMessage() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i := len(b.broadcasts) - 1; i >= 0; i-- {
		call := b.broadcasts[i]
		if call.eventType == "plugin.response" &&
			call.data != nil &&
			call.data.PluginResponse != nil &&
			call.data.PluginResponse.Data != nil &&
			call.data.PluginResponse.Data.CommandResult != nil {
			return call.data.PluginResponse.Data.CommandResult.Output
		}
	}
	return ""
}

func mkBugCommandEvent(username, channel string, isPM, isAdmin bool, args ...string) *framework.DataEvent {
	return &framework.DataEvent{
		EventType: "command.bug.execute",
		EventTime: time.Now(),
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				Data: &framework.RequestData{
					Command: &framework.CommandData{
						Args: args,
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

func TestStartRegistersBugCommand(t *testing.T) {
	bus := &testEventBus{}
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

	for _, call := range bus.broadcasts {
		if call.eventType != "command.register" || call.data == nil || call.data.PluginRequest == nil || call.data.PluginRequest.Data == nil {
			continue
		}
		fields := call.data.PluginRequest.Data.KeyValue
		if fields["commands"] != "bug" {
			continue
		}
		if fields["admin_only"] == "true" {
			t.Fatalf("expected !bug command to not be admin-only")
		}
		return
	}

	t.Fatalf("expected command.register for bug")
}

func TestHandleCommandRequiresComment(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	if err := p.handleCommand(mkBugCommandEvent("alice", "chan", false, false)); err != nil {
		t.Fatalf("handleCommand error: %v", err)
	}
	if got := bus.lastChatMessage(); !strings.Contains(got, "usage: !bug <comment>") {
		t.Fatalf("unexpected chat message: %q", got)
	}
}

func TestHandleCommandNonAdminCooldown(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	now := time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return now }
	createCount := 0
	p.createPRFunc = func(ctx context.Context, report bugReport) (*createdPR, error) {
		_ = ctx
		_ = report
		createCount++
		return &createdPR{Number: 41, HTMLURL: "https://example.com/pr/41"}, nil
	}

	if err := p.handleCommand(mkBugCommandEvent("alice", "chan", false, false, "first", "bug")); err != nil {
		t.Fatalf("handleCommand first call error: %v", err)
	}
	if createCount != 1 {
		t.Fatalf("expected first call to create PR once, got %d", createCount)
	}

	now = now.Add(10 * time.Minute)
	if err := p.handleCommand(mkBugCommandEvent("alice", "chan", false, false, "second", "bug")); err != nil {
		t.Fatalf("handleCommand second call error: %v", err)
	}
	if createCount != 1 {
		t.Fatalf("expected cooldown to block second PR creation, got %d creations", createCount)
	}
	if got := bus.lastChatMessage(); !strings.Contains(got, "cooldown active") {
		t.Fatalf("expected cooldown message, got %q", got)
	}
}

func TestHandleCommandAdminBypassesCooldown(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	now := time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return now }
	createCount := 0
	p.createPRFunc = func(ctx context.Context, report bugReport) (*createdPR, error) {
		_ = ctx
		_ = report
		createCount++
		return &createdPR{Number: 77, HTMLURL: "https://example.com/pr/77"}, nil
	}

	if err := p.handleCommand(mkBugCommandEvent("admin", "chan", false, true, "bug", "one")); err != nil {
		t.Fatalf("first admin call error: %v", err)
	}
	if err := p.handleCommand(mkBugCommandEvent("admin", "chan", false, true, "bug", "two")); err != nil {
		t.Fatalf("second admin call error: %v", err)
	}
	if createCount != 2 {
		t.Fatalf("expected admin to bypass cooldown, createCount=%d", createCount)
	}
}

func TestHandleCommandPMUsesPluginResponse(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.createPRFunc = func(ctx context.Context, report bugReport) (*createdPR, error) {
		_ = ctx
		_ = report
		return &createdPR{Number: 11, HTMLURL: "https://example.com/pr/11"}, nil
	}

	if err := p.handleCommand(mkBugCommandEvent("alice", "chan", true, false, "pm", "bug")); err != nil {
		t.Fatalf("pm call error: %v", err)
	}
	if got := bus.lastPMMessage(); !strings.Contains(got, "PR #11") {
		t.Fatalf("expected PM plugin response with PR id, got %q", got)
	}
}

func TestCreatePRFromReportUsesGitHubAPI(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	var mu sync.Mutex
	var branch string
	var contentPathSeen bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			http.Error(w, fmt.Sprintf("bad auth header: %q", got), http.StatusUnauthorized)
			return
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/hildolfr/daz/git/ref/heads/master":
			_, _ = w.Write([]byte(`{"object":{"sha":"abc123"}}`))
			return

		case r.Method == http.MethodPost && r.URL.Path == "/repos/hildolfr/daz/git/refs":
			var req map[string]string
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			branch = strings.TrimPrefix(req["ref"], "refs/heads/")
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
			return

		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/repos/hildolfr/daz/contents/"):
			var req map[string]string
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			wantBranch := branch
			mu.Unlock()
			if req["branch"] != wantBranch {
				http.Error(w, "unexpected branch in commit payload", http.StatusBadRequest)
				return
			}
			contentPathSeen = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
			return

		case r.Method == http.MethodPost && r.URL.Path == "/repos/hildolfr/daz/pulls":
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			wantBranch := branch
			mu.Unlock()
			if req["head"] != wantBranch {
				http.Error(w, "unexpected head branch", http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"number":123,"html_url":"https://github.com/hildolfr/daz/pull/123"}`))
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	bus := &testEventBus{}
	p := New().(*Plugin)
	cfg := []byte(fmt.Sprintf(`{"api_base_url":"%s","repo_owner":"hildolfr","repo_name":"daz","base_branch":"master"}`, server.URL))
	if err := p.Init(cfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.httpClient = server.Client()
	p.now = func() time.Time {
		return time.Date(2026, 3, 3, 10, 30, 0, 0, time.UTC)
	}

	pr, err := p.createPRFromReport(context.Background(), bugReport{
		Channel:   "always_always_sunny",
		Username:  "alice",
		Comment:   "it broke after queue shuffle",
		CreatedAt: p.now(),
	})
	if err != nil {
		t.Fatalf("createPRFromReport error: %v", err)
	}
	if pr.Number != 123 {
		t.Fatalf("expected pr number 123, got %d", pr.Number)
	}
	if pr.HTMLURL != "https://github.com/hildolfr/daz/pull/123" {
		t.Fatalf("unexpected pr url: %q", pr.HTMLURL)
	}
	if !contentPathSeen {
		t.Fatalf("expected contents API call to commit bug report file")
	}
}

func TestCreatePRFromReportMissingToken(t *testing.T) {
	_ = os.Unsetenv("GITHUB_TOKEN")
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	_, err := p.createPRFromReport(context.Background(), bugReport{
		Channel:   "chan",
		Username:  "alice",
		Comment:   "something broke",
		CreatedAt: time.Now().UTC(),
	})
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("expected missing token error, got %v", err)
	}
}
