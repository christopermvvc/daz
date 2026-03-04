package bug

import (
	"context"
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
	"github.com/hildolfr/daz/pkg/eventbus"
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
	subscribeErrs map[string]error
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
	if b.subscribeErrs != nil {
		if err, exists := b.subscribeErrs[eventType]; exists {
			return err
		}
	}
	if b.subscribeErr != nil {
		return b.subscribeErr
	}
	_ = handler
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subscriptions = append(b.subscriptions, eventType)
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
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

func mkChatEvent(username, channel, message string) *framework.DataEvent {
	return &framework.DataEvent{
		EventType: eventbus.EventCytubeChatMsg,
		EventTime: time.Now(),
		Data: &framework.EventData{
			ChatMessage: &framework.ChatMessageData{
				Username: username,
				Channel:  channel,
				Message:  message,
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
		if !strings.Contains(strings.ToLower(fields["description"]), "issue") {
			t.Fatalf("expected issue-based description, got %q", fields["description"])
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

func TestStartFailsWhenChatSubscriptionFails(t *testing.T) {
	bus := &testEventBus{
		subscribeErrs: map[string]error{
			eventbus.EventCytubeChatMsg: errors.New("chat-sub-fail"),
		},
	}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	err := p.Start()
	if err == nil || !strings.Contains(err.Error(), eventbus.EventCytubeChatMsg) {
		t.Fatalf("expected chat subscription failure, got %v", err)
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
	p.createIssueFunc = func(ctx context.Context, report bugReport) (*createdIssue, error) {
		_ = ctx
		_ = report
		createCount++
		return &createdIssue{Number: 41, HTMLURL: "https://example.com/issues/41"}, nil
	}

	if err := p.handleCommand(mkBugCommandEvent("alice", "chan", false, false, "first", "bug")); err != nil {
		t.Fatalf("handleCommand first call error: %v", err)
	}
	if createCount != 1 {
		t.Fatalf("expected first call to create issue once, got %d", createCount)
	}

	now = now.Add(10 * time.Minute)
	if err := p.handleCommand(mkBugCommandEvent("alice", "chan", false, false, "second", "bug")); err != nil {
		t.Fatalf("handleCommand second call error: %v", err)
	}
	if createCount != 1 {
		t.Fatalf("expected cooldown to block second issue creation, got %d creations", createCount)
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
	p.createIssueFunc = func(ctx context.Context, report bugReport) (*createdIssue, error) {
		_ = ctx
		_ = report
		createCount++
		return &createdIssue{
			Number:  50 + createCount,
			HTMLURL: fmt.Sprintf("https://example.com/issues/%d", 50+createCount),
		}, nil
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
	p.createIssueFunc = func(ctx context.Context, report bugReport) (*createdIssue, error) {
		_ = ctx
		_ = report
		createCount++
		return &createdIssue{Number: 77, HTMLURL: "https://example.com/issues/77"}, nil
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
	p.createIssueFunc = func(ctx context.Context, report bugReport) (*createdIssue, error) {
		_ = ctx
		_ = report
		calls++
		if calls == 1 {
			return nil, errors.New(strings.Repeat("x", 300))
		}
		return &createdIssue{Number: 99, HTMLURL: "https://example.com/issues/99"}, nil
	}

	if err := p.handleCommand(mkBugCommandEvent("alice", "chan", false, false, "first")); err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if got := bus.lastChatMessage(); !strings.Contains(got, "failed to file bug issue:") {
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
	p.createIssueFunc = func(ctx context.Context, report bugReport) (*createdIssue, error) {
		_ = ctx
		_ = report
		return &createdIssue{Number: 11, HTMLURL: "https://example.com/issues/11"}, nil
	}

	if err := p.handleCommand(mkBugCommandEvent("alice", "chan", true, false, "pm", "bug")); err != nil {
		t.Fatalf("pm call error: %v", err)
	}
	if got := bus.lastPMMessage(); !strings.Contains(strings.ToLower(got), "issue #11") {
		t.Fatalf("expected PM plugin response with issue id, got %q", got)
	}
}

func TestHandleCommandPublicSuccessUsesPMForIssueLink(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.createIssueFunc = func(ctx context.Context, report bugReport) (*createdIssue, error) {
		_ = ctx
		_ = report
		return &createdIssue{Number: 88, HTMLURL: "https://example.com/issues/88"}, nil
	}

	if err := p.handleCommand(mkBugCommandEvent("alice", "chan", false, false, "public", "bug")); err != nil {
		t.Fatalf("public call error: %v", err)
	}

	pm := bus.lastPMMessage()
	if !strings.Contains(pm, "issue #88") || !strings.Contains(pm, "https://example.com/issues/88") {
		t.Fatalf("expected PM to contain issue link, got %q", pm)
	}

	chat := bus.lastChatMessage()
	if !strings.Contains(strings.ToLower(chat), "sent you the issue link in pm") {
		t.Fatalf("expected public ack to mention PM delivery, got %q", chat)
	}
	if strings.Contains(chat, "https://") {
		t.Fatalf("public ack should not include issue URL, got %q", chat)
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

func TestHandleChatMessageIgnoresInvalidEvents(t *testing.T) {
	p := New().(*Plugin)
	if err := p.Init(nil, &testEventBus{}); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	events := []framework.Event{
		nil,
		&framework.DataEvent{},
		&framework.DataEvent{Data: &framework.EventData{}},
		&framework.DataEvent{Data: &framework.EventData{ChatMessage: &framework.ChatMessageData{Username: "", Channel: "chan", Message: "x"}}},
		&framework.DataEvent{Data: &framework.EventData{ChatMessage: &framework.ChatMessageData{Username: "u", Channel: "", Message: "x"}}},
		&framework.DataEvent{Data: &framework.EventData{ChatMessage: &framework.ChatMessageData{Username: "u", Channel: "chan", Message: ""}}},
	}
	for _, ev := range events {
		if err := p.handleChatMessage(ev); err != nil {
			t.Fatalf("handleChatMessage should ignore invalid event, got %v", err)
		}
	}

	if got := p.getRecentChat("chan", 10); len(got) != 0 {
		t.Fatalf("expected no captured lines for invalid events, got %d", len(got))
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

func TestSendResponseToleratesBroadcastErrors(t *testing.T) {
	bus := &testEventBus{broadcastErr: errors.New("down")}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	p.sendResponse("alice", "chan", false, "hello chat")
	p.sendResponse("alice", "chan", true, "hello pm")
}

func TestCreateIssueFromReportUsesGitHubAPI(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	var payloadTitle string
	var payloadBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			http.Error(w, fmt.Sprintf("bad auth header: %q", got), http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/repos/hildolfr/daz/issues" {
			http.NotFound(w, r)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		payloadTitle, _ = req["title"].(string)
		payloadBody, _ = req["body"].(string)
		_, _ = w.Write([]byte(`{"number":123,"html_url":"https://github.com/hildolfr/daz/issues/123"}`))
	}))
	defer server.Close()

	bus := &testEventBus{}
	p := New().(*Plugin)
	cfg := []byte(fmt.Sprintf(`{"api_base_url":"%s","repo_owner":"hildolfr","repo_name":"daz"}`, server.URL))
	if err := p.Init(cfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.httpClient = server.Client()
	p.now = func() time.Time {
		return time.Date(2026, 3, 3, 10, 30, 0, 0, time.UTC)
	}

	issue, err := p.createIssueFromReport(context.Background(), bugReport{
		Channel:   "always_always_sunny",
		Username:  "alice",
		Comment:   "it broke after queue shuffle",
		CreatedAt: p.now(),
	})
	if err != nil {
		t.Fatalf("createIssueFromReport error: %v", err)
	}
	if issue.Number != 123 {
		t.Fatalf("expected issue number 123, got %d", issue.Number)
	}
	if issue.HTMLURL != "https://github.com/hildolfr/daz/issues/123" {
		t.Fatalf("unexpected issue url: %q", issue.HTMLURL)
	}
	if !strings.Contains(strings.ToLower(payloadTitle), "bug report") {
		t.Fatalf("expected bug report title, got %q", payloadTitle)
	}
	if !strings.Contains(payloadBody, "Reporter: alice") || !strings.Contains(payloadBody, "queue shuffle") {
		t.Fatalf("issue body missing report details: %q", payloadBody)
	}
}

func TestCreateIssueFromReportMissingToken(t *testing.T) {
	_ = os.Unsetenv("GITHUB_TOKEN")
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	_, err := p.createIssueFromReport(context.Background(), bugReport{
		Channel:   "chan",
		Username:  "alice",
		Comment:   "something broke",
		CreatedAt: time.Now().UTC(),
	})
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("expected missing token error, got %v", err)
	}
}

func TestCreateIssueFromReportPropagatesOpenError(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"Resource not accessible by personal access token"}`))
	}))
	defer server.Close()

	bus := &testEventBus{}
	p := New().(*Plugin)
	cfg := []byte(fmt.Sprintf(`{"api_base_url":"%s"}`, server.URL))
	if err := p.Init(cfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.httpClient = server.Client()

	_, err := p.createIssueFromReport(context.Background(), bugReport{
		Channel:   "chan",
		Username:  "alice",
		Comment:   "broken",
		CreatedAt: time.Now().UTC(),
	})
	if err == nil || !strings.Contains(err.Error(), "create issue") {
		t.Fatalf("expected create issue error, got %v", err)
	}
}

func TestOpenIssueRequiresMetadata(t *testing.T) {
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

	_, err := p.openIssue(context.Background(), "token", "title", "body")
	if err == nil || !strings.Contains(err.Error(), "missing issue metadata") {
		t.Fatalf("expected missing metadata error, got %v", err)
	}
}

func TestOpenIssueIncludesLabels(t *testing.T) {
	var gotLabels []any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		labels, _ := req["labels"].([]any)
		gotLabels = labels
		_, _ = w.Write([]byte(`{"number":44,"html_url":"https://example.com/issues/44"}`))
	}))
	defer server.Close()

	bus := &testEventBus{}
	p := New().(*Plugin)
	cfg := []byte(fmt.Sprintf(`{"api_base_url":"%s","labels":["bug","from-chat"]}`, server.URL))
	if err := p.Init(cfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.httpClient = server.Client()

	issue, err := p.openIssue(context.Background(), "token", "title", "body")
	if err != nil {
		t.Fatalf("openIssue error: %v", err)
	}
	if issue.Number != 44 {
		t.Fatalf("unexpected issue number %d", issue.Number)
	}
	if len(gotLabels) != 2 {
		t.Fatalf("expected labels payload, got %#v", gotLabels)
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

func TestGitHubRequestHandlesTransportFailure(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			_ = req
			return nil, errors.New("dial fail")
		}),
	}

	err := p.githubRequest(context.Background(), "token", http.MethodGet, "/x", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "github request failed") {
		t.Fatalf("expected transport failure error, got %v", err)
	}
}

func TestGitHubRequestSuccessWithNilOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	bus := &testEventBus{}
	p := New().(*Plugin)
	cfg := []byte(fmt.Sprintf(`{"api_base_url":"%s"}`, server.URL))
	if err := p.Init(cfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.httpClient = server.Client()

	if err := p.githubRequest(context.Background(), "token", http.MethodGet, "/x", nil, nil); err != nil {
		t.Fatalf("expected nil-out success path, got %v", err)
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
	raw := []byte(`{"repo_owner":"  ","repo_name":" ","api_base_url":" ","title_prefix":" ","cooldown_minutes":0,"request_timeout_seconds":0,"max_comment_length":0,"labels":[" ","BUG","bug","from-chat"]}`)
	if err := p.Init(raw, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	if p.config.RepoOwner != defaultRepoOwner || p.config.RepoName != defaultRepoName {
		t.Fatalf("expected default owner/repo, got %+v", p.config)
	}
	if p.config.APIBaseURL != defaultAPIBaseURL || p.config.TitlePrefix != defaultTitlePrefix {
		t.Fatalf("expected default api/title, got %+v", p.config)
	}
	if p.config.CooldownMinutes != defaultCooldownMinutes || p.config.RequestTimeoutSecs != defaultRequestTimeoutSecs || p.config.MaxCommentLength != defaultMaxCommentLength {
		t.Fatalf("expected default limits/timeouts, got %+v", p.config)
	}
	if len(p.config.Labels) != 2 {
		t.Fatalf("expected deduplicated labels, got %+v", p.config.Labels)
	}
}

func TestNormalizeLabels(t *testing.T) {
	got := normalizeLabels([]string{"", " bug ", "BUG", "from-chat", " From-Chat ", "triage"})
	want := []string{"bug", "from-chat", "triage"}
	if len(got) != len(want) {
		t.Fatalf("normalizeLabels length mismatch: got=%v want=%v", got, want)
	}
	for i := range want {
		if strings.ToLower(got[i]) != strings.ToLower(want[i]) {
			t.Fatalf("normalizeLabels[%d] = %q, want %q (case-insensitive)", i, got[i], want[i])
		}
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

func TestBuildBugIssueAssetsFormatting(t *testing.T) {
	p := New().(*Plugin)
	p.config.TitlePrefix = "bug report"
	title, body := p.buildBugIssueAssets(bugReport{
		Channel:   "chan",
		Username:  "alice",
		Comment:   "line one\nline two",
		CreatedAt: time.Date(2026, 3, 3, 10, 30, 0, 0, time.UTC),
	}, []chatLine{
		{Username: "bob", Message: "seen this too"},
	})
	if !strings.Contains(title, "bug report (chan):") {
		t.Fatalf("unexpected title: %q", title)
	}
	if !strings.Contains(body, "Reporter: alice") || !strings.Contains(body, "> line one") || !strings.Contains(body, "> line two") {
		t.Fatalf("issue body missing expected format: %q", body)
	}
	if !strings.Contains(body, "Recent Chat Context (last 10 lines):") || !strings.Contains(body, "- bob: seen this too") {
		t.Fatalf("issue body missing recent chat context: %q", body)
	}
}

func TestBuildBugIssueAssetsUsesFallbackContextAndTruncatesTitleComment(t *testing.T) {
	p := New().(*Plugin)
	p.config.TitlePrefix = "bug report"
	longComment := strings.Repeat("a", 90)
	title, body := p.buildBugIssueAssets(bugReport{
		Channel:   "chan",
		Username:  "alice",
		Comment:   longComment,
		CreatedAt: time.Date(2026, 3, 3, 10, 30, 0, 0, time.UTC),
	}, nil)
	if !strings.Contains(title, "...") {
		t.Fatalf("expected truncated title comment, got %q", title)
	}
	if !strings.Contains(body, "- (no recent chat captured)") {
		t.Fatalf("expected fallback context marker, got %q", body)
	}
}

func TestQuoteBlock(t *testing.T) {
	block := quoteBlock("line1\n\n line2 ")
	want := "> line1\n>\n> line2"
	if block != want {
		t.Fatalf("unexpected quote block:\n%s\nwant:\n%s", block, want)
	}
}

func TestHandleChatMessageCapturesRecentLines(t *testing.T) {
	p := New().(*Plugin)
	if err := p.Init(nil, &testEventBus{}); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	for i := 1; i <= 12; i++ {
		if err := p.handleChatMessage(mkChatEvent("user", "chan", fmt.Sprintf("line %d", i))); err != nil {
			t.Fatalf("handleChatMessage error: %v", err)
		}
	}

	lines := p.getRecentChat("chan", 10)
	if len(lines) != 10 {
		t.Fatalf("expected 10 recent lines, got %d", len(lines))
	}
	if lines[0].Message != "line 3" || lines[9].Message != "line 12" {
		t.Fatalf("unexpected line window: first=%q last=%q", lines[0].Message, lines[9].Message)
	}
}

func TestAppendRecentChatTrimsToHistoryLimit(t *testing.T) {
	p := New().(*Plugin)
	if err := p.Init(nil, &testEventBus{}); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	for i := 1; i <= 60; i++ {
		p.appendRecentChat("chan", "u", fmt.Sprintf("line %d", i))
	}
	lines := p.getRecentChat("chan", 100)
	if len(lines) != chatHistoryLimit {
		t.Fatalf("expected %d lines, got %d", chatHistoryLimit, len(lines))
	}
	if lines[0].Message != "line 11" || lines[len(lines)-1].Message != "line 60" {
		t.Fatalf("unexpected retained window: first=%q last=%q", lines[0].Message, lines[len(lines)-1].Message)
	}
}

func TestAppendRecentChatIgnoresMissingFields(t *testing.T) {
	p := New().(*Plugin)
	if err := p.Init(nil, &testEventBus{}); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.appendRecentChat("", "u", "msg")
	p.appendRecentChat("chan", "", "msg")
	p.appendRecentChat("chan", "u", "")
	if got := p.getRecentChat("chan", 10); len(got) != 0 {
		t.Fatalf("expected no captured lines, got %d", len(got))
	}
}

func TestCreateIssueFromReportIncludesRecentChatContext(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	var payloadBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/repos/hildolfr/daz/issues" {
			http.NotFound(w, r)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		payloadBody, _ = req["body"].(string)
		_, _ = w.Write([]byte(`{"number":321,"html_url":"https://github.com/hildolfr/daz/issues/321"}`))
	}))
	defer server.Close()

	bus := &testEventBus{}
	p := New().(*Plugin)
	cfg := []byte(fmt.Sprintf(`{"api_base_url":"%s","repo_owner":"hildolfr","repo_name":"daz"}`, server.URL))
	if err := p.Init(cfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.httpClient = server.Client()

	for i := 1; i <= 12; i++ {
		if err := p.handleChatMessage(mkChatEvent("u", "always_always_sunny", fmt.Sprintf("ctx %d", i))); err != nil {
			t.Fatalf("handleChatMessage error: %v", err)
		}
	}

	_, err := p.createIssueFromReport(context.Background(), bugReport{
		Channel:   "always_always_sunny",
		Username:  "alice",
		Comment:   "bug happened",
		CreatedAt: time.Date(2026, 3, 3, 10, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("createIssueFromReport error: %v", err)
	}

	if !strings.Contains(payloadBody, "Recent Chat Context (last 10 lines):") {
		t.Fatalf("missing context section in issue body: %q", payloadBody)
	}
	if !strings.Contains(payloadBody, "- u: ctx 3") || !strings.Contains(payloadBody, "- u: ctx 12") {
		t.Fatalf("issue body missing expected recent chat lines: %q", payloadBody)
	}
	if strings.Contains(payloadBody, "- u: ctx 1\n") || strings.Contains(payloadBody, "- u: ctx 2\n") {
		t.Fatalf("issue body should only include last 10 lines: %q", payloadBody)
	}
}

func TestGetRecentChatEdgeCases(t *testing.T) {
	p := New().(*Plugin)
	if err := p.Init(nil, &testEventBus{}); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.appendRecentChat("chan", "u", "line")

	if got := p.getRecentChat("chan", 0); got != nil {
		t.Fatalf("expected nil for non-positive limit, got %#v", got)
	}
	if got := p.getRecentChat("", 5); got != nil {
		t.Fatalf("expected nil for empty channel, got %#v", got)
	}
	if got := p.getRecentChat("other", 5); got != nil {
		t.Fatalf("expected nil for missing channel history, got %#v", got)
	}
}

func TestFormatChatContextEdgeCases(t *testing.T) {
	if got := formatChatContext(nil); !strings.Contains(got, "no recent chat captured") {
		t.Fatalf("expected empty-context fallback, got %q", got)
	}

	invalidOnly := formatChatContext([]chatLine{
		{Username: "", Message: "msg"},
		{Username: "u", Message: ""},
	})
	if !strings.Contains(invalidOnly, "no recent chat captured") {
		t.Fatalf("expected fallback when all lines invalid, got %q", invalidOnly)
	}

	long := strings.Repeat("x", 240)
	got := formatChatContext([]chatLine{{Username: "u", Message: long}})
	if !strings.Contains(got, "...") {
		t.Fatalf("expected truncated long line context, got %q", got)
	}
}
