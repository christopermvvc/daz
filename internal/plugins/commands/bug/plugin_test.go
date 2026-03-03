package bug

import (
	"context"
	crand "crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
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
	broadcastErr  error
	subscribeErr  error
}

func (b *testEventBus) Broadcast(eventType string, data *framework.EventData) error {
	if b.broadcastErr != nil {
		return b.broadcastErr
	}
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
	if b.subscribeErr != nil {
		return b.subscribeErr
	}
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

type errReader struct{}

func (errReader) Read(_ []byte) (int, error) {
	return 0, errors.New("read failed")
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

func TestStartFailsWhenSubscribeFails(t *testing.T) {
	bus := &testEventBus{subscribeErr: errors.New("boom")}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	if err := p.Start(); err == nil || !strings.Contains(err.Error(), "failed to subscribe") {
		t.Fatalf("expected subscribe failure from Start, got %v", err)
	}
}

func TestStartSucceedsWhenCommandRegistrationBroadcastFails(t *testing.T) {
	bus := &testEventBus{broadcastErr: errors.New("broadcast failed")}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Start should tolerate registerCommand broadcast error, got %v", err)
	}
	defer p.Stop()
}

func TestStartAlreadyRunning(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("first Start error: %v", err)
	}
	defer p.Stop()
	if err := p.Start(); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("expected already running error, got %v", err)
	}
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

func TestHandleCommandIgnoresMissingRoutingFields(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	ev := mkBugCommandEvent("", "chan", false, false, "broken")
	if err := p.handleCommand(ev); err != nil {
		t.Fatalf("handleCommand error: %v", err)
	}
	if got := len(bus.broadcasts); got != 0 {
		t.Fatalf("expected no broadcasts for empty username, got %d", got)
	}

	ev = mkBugCommandEvent("alice", "", false, false, "broken")
	if err := p.handleCommand(ev); err != nil {
		t.Fatalf("handleCommand error: %v", err)
	}
	if got := len(bus.broadcasts); got != 0 {
		t.Fatalf("expected no broadcasts for empty channel, got %d", got)
	}
}

func TestHandleCommandRejectsLongComment(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init([]byte(`{"max_comment_length":8}`), bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	if err := p.handleCommand(mkBugCommandEvent("alice", "chan", false, false, "this", "is", "too", "long")); err != nil {
		t.Fatalf("handleCommand error: %v", err)
	}
	if got := bus.lastChatMessage(); !strings.Contains(got, "comment is too long") {
		t.Fatalf("expected too long response, got %q", got)
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

func TestHandleCommandCooldownExpiresAfterWindow(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init([]byte(`{"cooldown_minutes":60}`), bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	now := time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC)
	p.now = func() time.Time { return now }
	createCount := 0
	p.createPRFunc = func(ctx context.Context, report bugReport) (*createdPR, error) {
		_ = ctx
		_ = report
		createCount++
		return &createdPR{Number: 50 + createCount, HTMLURL: fmt.Sprintf("https://example.com/pr/%d", 50+createCount)}, nil
	}

	if err := p.handleCommand(mkBugCommandEvent("alice", "chan", false, false, "first")); err != nil {
		t.Fatalf("first call error: %v", err)
	}
	now = now.Add(61 * time.Minute)
	if err := p.handleCommand(mkBugCommandEvent("alice", "chan", false, false, "second")); err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if createCount != 2 {
		t.Fatalf("expected cooldown expiry to allow second run, createCount=%d", createCount)
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

func TestHandleCommandFailureDoesNotConsumeCooldown(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	calls := 0
	p.createPRFunc = func(ctx context.Context, report bugReport) (*createdPR, error) {
		_ = ctx
		_ = report
		calls++
		if calls == 1 {
			return nil, errors.New(strings.Repeat("x", 300))
		}
		return &createdPR{Number: 99, HTMLURL: "https://example.com/pr/99"}, nil
	}

	if err := p.handleCommand(mkBugCommandEvent("alice", "chan", false, false, "first")); err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if got := bus.lastChatMessage(); !strings.Contains(got, "failed to file bug PR:") {
		t.Fatalf("expected failure response, got %q", got)
	}
	if !strings.Contains(bus.lastChatMessage(), "...") {
		t.Fatalf("expected truncated safe error message")
	}

	if err := p.handleCommand(mkBugCommandEvent("alice", "chan", false, false, "second")); err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected second call not blocked by cooldown after failure, calls=%d", calls)
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

func TestSendResponseIgnoresEmptyMessage(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.sendResponse("alice", "chan", false, "   ")
	if len(bus.broadcasts) != 0 {
		t.Fatalf("expected no broadcast for empty response message")
	}
}

func TestHandleCommandHandlesNilOrNonDataEvent(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	if err := p.handleCommand(nil); err != nil {
		t.Fatalf("expected nil event to be ignored, got %v", err)
	}

	ev := &framework.DataEvent{}
	if err := p.handleCommand(ev); err != nil {
		t.Fatalf("expected empty DataEvent to be ignored, got %v", err)
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

func TestCreatePRFromReportFailsOnBaseBranchLookup(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"missing ref"}`))
	}))
	defer server.Close()

	bus := &testEventBus{}
	p := New().(*Plugin)
	cfg := []byte(fmt.Sprintf(`{"api_base_url":"%s"}`, server.URL))
	if err := p.Init(cfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.httpClient = server.Client()

	_, err := p.createPRFromReport(context.Background(), bugReport{
		Channel:   "chan",
		Username:  "alice",
		Comment:   "broken",
		CreatedAt: time.Now().UTC(),
	})
	if err == nil || !strings.Contains(err.Error(), "read base branch ref") {
		t.Fatalf("expected base-branch lookup error, got %v", err)
	}
}

func TestCreatePRFromReportFailsOnCommitStep(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/hildolfr/daz/git/ref/heads/master":
			_, _ = w.Write([]byte(`{"object":{"sha":"abc123"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/repos/hildolfr/daz/git/refs":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/repos/hildolfr/daz/contents/"):
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"commit denied"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	bus := &testEventBus{}
	p := New().(*Plugin)
	cfg := []byte(fmt.Sprintf(`{"api_base_url":"%s","repo_owner":"hildolfr","repo_name":"daz","base_branch":"master"}`, server.URL))
	if err := p.Init(cfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.httpClient = server.Client()
	p.now = func() time.Time { return time.Date(2026, 3, 3, 10, 30, 0, 0, time.UTC) }

	_, err := p.createPRFromReport(context.Background(), bugReport{
		Channel:   "chan",
		Username:  "alice",
		Comment:   "broken",
		CreatedAt: p.now(),
	})
	if err == nil || !strings.Contains(err.Error(), "commit report file") {
		t.Fatalf("expected commit-step error, got %v", err)
	}
}

func TestCreateUniqueBranchRetriesOnConflict(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "token")
	refCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/hildolfr/daz/git/refs" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		refCalls++
		if refCalls == 1 {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"Reference already exists"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	bus := &testEventBus{}
	p := New().(*Plugin)
	cfg := []byte(fmt.Sprintf(`{"api_base_url":"%s"}`, server.URL))
	if err := p.Init(cfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.httpClient = server.Client()

	branch, err := p.createUniqueBranch(context.Background(), "token", bugReport{
		Comment:   "collision test",
		CreatedAt: time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC),
	}, "abc123")
	if err != nil {
		t.Fatalf("createUniqueBranch error: %v", err)
	}
	if !strings.HasPrefix(branch, "daz/bug/20260303-100000-collision-test-") {
		t.Fatalf("unexpected branch name: %q", branch)
	}
	if refCalls != 2 {
		t.Fatalf("expected 2 attempts, got %d", refCalls)
	}
}

func TestCreateUniqueBranchFailsAfterRetries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"Reference already exists"}`))
	}))
	defer server.Close()

	bus := &testEventBus{}
	p := New().(*Plugin)
	cfg := []byte(fmt.Sprintf(`{"api_base_url":"%s"}`, server.URL))
	if err := p.Init(cfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.httpClient = server.Client()

	_, err := p.createUniqueBranch(context.Background(), "token", bugReport{
		Comment:   "collision test",
		CreatedAt: time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC),
	}, "abc123")
	if err == nil || !strings.Contains(err.Error(), "failed to create unique branch after retries") {
		t.Fatalf("expected retry exhaustion error, got %v", err)
	}
}

func TestFetchBaseBranchSHARequiresValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"object":{"sha":""}}`))
	}))
	defer server.Close()

	bus := &testEventBus{}
	p := New().(*Plugin)
	cfg := []byte(fmt.Sprintf(`{"api_base_url":"%s"}`, server.URL))
	if err := p.Init(cfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.httpClient = server.Client()

	_, err := p.fetchBaseBranchSHA(context.Background(), "token")
	if err == nil || !strings.Contains(err.Error(), "empty base branch sha") {
		t.Fatalf("expected empty-sha error, got %v", err)
	}
}

func TestCommitReportFilePropagatesGitHubError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"write failed"}`))
	}))
	defer server.Close()

	bus := &testEventBus{}
	p := New().(*Plugin)
	cfg := []byte(fmt.Sprintf(`{"api_base_url":"%s"}`, server.URL))
	if err := p.Init(cfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.httpClient = server.Client()

	err := p.commitReportFile(
		context.Background(),
		"token",
		"daz/bug/a",
		"data/bugreports/test.md",
		"report",
		bugReport{Username: "alice", Channel: "chan"},
	)
	if err == nil || !strings.Contains(err.Error(), "commit report file") {
		t.Fatalf("expected commit report error, got %v", err)
	}
}

func TestOpenPullRequestRequiresMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"number":0,"html_url":""}`))
	}))
	defer server.Close()

	bus := &testEventBus{}
	p := New().(*Plugin)
	cfg := []byte(fmt.Sprintf(`{"api_base_url":"%s"}`, server.URL))
	if err := p.Init(cfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.httpClient = server.Client()

	_, err := p.openPullRequest(context.Background(), "token", "daz/bug/a", "title", "body")
	if err == nil || !strings.Contains(err.Error(), "missing PR metadata") {
		t.Fatalf("expected missing metadata error, got %v", err)
	}
}

func TestGitHubRequestHandlesStatusErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"bad payload"}`))
	}))
	defer server.Close()

	bus := &testEventBus{}
	p := New().(*Plugin)
	cfg := []byte(fmt.Sprintf(`{"api_base_url":"%s"}`, server.URL))
	if err := p.Init(cfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.httpClient = server.Client()

	err := p.githubRequest(context.Background(), "token", http.MethodGet, "/x", nil, nil)
	if err == nil {
		t.Fatalf("expected status error")
	}
	var statusErr *githubStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected githubStatusError, got %T", err)
	}
	if statusErr.StatusCode != http.StatusBadRequest || statusErr.Message != "bad payload" {
		t.Fatalf("unexpected status error: %+v", statusErr)
	}
}

func TestGitHubRequestStatusErrorFallsBackToStatusText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer server.Close()

	bus := &testEventBus{}
	p := New().(*Plugin)
	cfg := []byte(fmt.Sprintf(`{"api_base_url":"%s"}`, server.URL))
	if err := p.Init(cfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.httpClient = server.Client()

	err := p.githubRequest(context.Background(), "token", http.MethodGet, "/x", nil, nil)
	var statusErr *githubStatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected githubStatusError, got %v", err)
	}
	if statusErr.StatusCode != http.StatusTeapot {
		t.Fatalf("expected status 418, got %d", statusErr.StatusCode)
	}
	if !strings.Contains(strings.ToLower(statusErr.Message), "teapot") {
		t.Fatalf("expected status-text fallback message, got %q", statusErr.Message)
	}
}

func TestGitHubRequestHandlesDecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not-json`))
	}))
	defer server.Close()

	bus := &testEventBus{}
	p := New().(*Plugin)
	cfg := []byte(fmt.Sprintf(`{"api_base_url":"%s"}`, server.URL))
	if err := p.Init(cfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.httpClient = server.Client()

	var out map[string]any
	err := p.githubRequest(context.Background(), "token", http.MethodGet, "/x", nil, &out)
	if err == nil || !strings.Contains(err.Error(), "decode github response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestGitHubRequestHandlesMarshalError(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	err := p.githubRequest(context.Background(), "token", http.MethodPost, "/x", make(chan int), nil)
	if err == nil || !strings.Contains(err.Error(), "marshal github request payload") {
		t.Fatalf("expected marshal error, got %v", err)
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

func TestInitInvalidConfigFails(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init([]byte(`{`), bus); err == nil {
		t.Fatalf("expected Init to fail on invalid JSON")
	}
}

func TestInitDefaultsWhenConfigBlankOrInvalidValues(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	raw := []byte(`{"repo_owner":"  ","repo_name":" ","base_branch":"","api_base_url":" ","title_prefix":" ","cooldown_minutes":0,"request_timeout_seconds":0,"max_comment_length":0}`)
	if err := p.Init(raw, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	if p.config.RepoOwner != defaultRepoOwner || p.config.RepoName != defaultRepoName || p.config.BaseBranch != defaultBaseBranch {
		t.Fatalf("expected default owner/repo/base, got %+v", p.config)
	}
	if p.config.CooldownMinutes != defaultCooldownMinutes || p.config.RequestTimeoutSecs != defaultRequestTimeoutSecs || p.config.MaxCommentLength != defaultMaxCommentLength {
		t.Fatalf("expected default limits/timeouts, got %+v", p.config)
	}
}

func TestLifecycleNameStatusHandleEventStop(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	if p.Name() != "bug" {
		t.Fatalf("unexpected plugin name: %q", p.Name())
	}
	if got := p.Status().State; got != "stopped" {
		t.Fatalf("unexpected initial status: %q", got)
	}
	if err := p.HandleEvent(nil); err != nil {
		t.Fatalf("HandleEvent should ignore events: %v", err)
	}
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop should be idempotent while stopped: %v", err)
	}
	if err := p.Start(); err != nil {
		t.Fatalf("Start error: %v", err)
	}
	if got := p.Status().State; got != "running" {
		t.Fatalf("expected running status, got %q", got)
	}
	if err := p.Stop(); err != nil {
		t.Fatalf("Stop error: %v", err)
	}
	if got := p.Status().State; got != "stopped" {
		t.Fatalf("expected stopped status, got %q", got)
	}
}

func TestGithubStatusErrorString(t *testing.T) {
	err := (&githubStatusError{StatusCode: 409, Message: "conflict"}).Error()
	if !strings.Contains(err, "409") || !strings.Contains(err, "conflict") {
		t.Fatalf("unexpected error string: %q", err)
	}
}

func TestFormatCooldownVariants(t *testing.T) {
	if got := formatCooldown(0); got != "0m" {
		t.Fatalf("formatCooldown zero = %q", got)
	}
	if got := formatCooldown(30 * time.Second); got != "1m" {
		t.Fatalf("formatCooldown seconds = %q", got)
	}
	if got := formatCooldown(95 * time.Minute); got != "1h 35m" {
		t.Fatalf("formatCooldown hour/minute = %q", got)
	}
}

func TestSafeErrMessageVariants(t *testing.T) {
	if got := safeErrMessage(nil); got != "unknown error" {
		t.Fatalf("safeErrMessage(nil) = %q", got)
	}
	if got := safeErrMessage(errors.New("   ")); got != "unknown error" {
		t.Fatalf("safeErrMessage(blank) = %q", got)
	}
	long := strings.Repeat("a", 240)
	got := safeErrMessage(errors.New(long))
	if !strings.HasSuffix(got, "...") || len(got) != 223 {
		t.Fatalf("safeErrMessage truncation failed, got len=%d value=%q", len(got), got)
	}
}

func TestBuildBugReportAssetsFallbackAndFormatting(t *testing.T) {
	p := New().(*Plugin)
	p.config.TitlePrefix = "bug report"
	path, title, content, body := p.buildBugReportAssets(bugReport{
		Channel:   "chan",
		Username:  "!!!",
		Comment:   "???",
		CreatedAt: time.Date(2026, 3, 3, 10, 30, 0, 0, time.UTC),
	})
	if !strings.Contains(path, "unknown_report.md") {
		t.Fatalf("expected unknown/report fallback path, got %q", path)
	}
	if !strings.Contains(title, "bug report (chan): ???") {
		t.Fatalf("unexpected title: %q", title)
	}
	if !strings.Contains(content, "## Triage Notes") || !strings.Contains(body, "Original Comment:") {
		t.Fatalf("missing expected sections in generated assets")
	}
}

func TestSlugFromTextAndQuoteBlock(t *testing.T) {
	if got := slugFromText("  Hello, World!  ", 64); got != "hello-world" {
		t.Fatalf("unexpected slug: %q", got)
	}
	if got := slugFromText("abcde", 3); got != "abc" {
		t.Fatalf("unexpected max-limited slug: %q", got)
	}
	block := quoteBlock("line1\n\n line2 ")
	want := "> line1\n>\n> line2"
	if block != want {
		t.Fatalf("unexpected quote block:\n%s\nwant:\n%s", block, want)
	}
}

func TestRandomSuffixReadError(t *testing.T) {
	orig := crand.Reader
	crand.Reader = errReader{}
	t.Cleanup(func() {
		crand.Reader = orig
	})

	if _, err := randomSuffix(3); err == nil {
		t.Fatalf("expected randomSuffix to fail when rand.Reader fails")
	}
}

func TestRandomSuffixDefaultsLengthWhenZero(t *testing.T) {
	value, err := randomSuffix(0)
	if err != nil {
		t.Fatalf("randomSuffix(0) error: %v", err)
	}
	if len(value) != 4 {
		t.Fatalf("expected default 2-byte suffix -> 4 hex chars, got %q", value)
	}
}
