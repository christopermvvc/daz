package update

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	mu         sync.Mutex
	broadcasts []broadcastCall
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
	_ = correlationID
	_ = response
	_ = err
}

func (b *testEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	_ = eventType
	_ = handler
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

func (b *testEventBus) hasMessage(eventType, channel, prefix string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, bcast := range b.broadcasts {
		if bcast.eventType != eventType || bcast.data == nil || bcast.data.RawMessage == nil {
			continue
		}
		if (channel == "" || bcast.data.RawMessage.Channel == channel) && strings.HasPrefix(bcast.data.RawMessage.Message, prefix) {
			return true
		}
	}
	return false
}

func (b *testEventBus) hasPrivateMessage(eventType, username, prefix string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, bcast := range b.broadcasts {
		if bcast.eventType != eventType || bcast.data == nil || bcast.data.PrivateMessage == nil {
			continue
		}
		if bcast.data.PrivateMessage.ToUser == username && strings.HasPrefix(bcast.data.PrivateMessage.Message, prefix) {
			return true
		}
	}
	return false
}

func (b *testEventBus) hasMessageContaining(eventType, channel, fragment string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, bcast := range b.broadcasts {
		if bcast.eventType != eventType || bcast.data == nil || bcast.data.RawMessage == nil {
			continue
		}
		if channel != "" && bcast.data.RawMessage.Channel != channel {
			continue
		}
		if strings.Contains(bcast.data.RawMessage.Message, fragment) {
			return true
		}
	}
	return false
}

func mkUpdateCommandEvent(username, channel, isPM string) *framework.DataEvent {
	return &framework.DataEvent{
		Data: &framework.EventData{
			PluginRequest: &framework.PluginRequest{
				Data: &framework.RequestData{
					Command: &framework.CommandData{
						Params: map[string]string{
							"username": username,
							"channel":  channel,
							"is_pm":    isPM,
						},
					},
				},
			},
		},
	}
}

func TestInitDefaultsToProjectLogPath(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	if p.logFile == "" {
		t.Fatal("expected log file path to be set")
	}

	if !strings.HasSuffix(p.logFile, defaultLogFileName) {
		t.Fatalf("expected log file to end with %q, got %q", defaultLogFileName, p.logFile)
	}

	if p.config.LogFilePath != p.logFile {
		t.Fatalf("expected config log file path %q, got %q", p.logFile, p.config.LogFilePath)
	}
}

func TestInitCanUseConfiguredLogPath(t *testing.T) {
	bus := &testEventBus{}
	tmp := t.TempDir()
	explicit := filepath.Join(tmp, "custom-update.log")
	rawCfg := []byte(fmt.Sprintf(`{"log_file":"%s","operation_timeout_seconds":10}`, explicit))

	p := New().(*Plugin)
	if err := p.Init(rawCfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	if p.logFile != explicit {
		t.Fatalf("expected log file %q, got %q", explicit, p.logFile)
	}
}

func TestHandleCommandRejectsPMWithNotice(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	if err := p.handleCommand(mkUpdateCommandEvent("alice", "always_always_sunny", "true")); err != nil {
		t.Fatalf("handleCommand error: %v", err)
	}

	if !bus.hasPrivateMessage("cytube.send.pm", "alice", "(!update is chat-only") {
		t.Fatalf("expected pm response for PM command")
	}

	if bus.hasMessage("cytube.send", "always_always_sunny", "Updating bot") {
		t.Fatalf("did not expect chat response for PM command")
	}
}

func TestHandleCommandTakesUpdateFlowPath(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	completed := make(chan struct{})
	p.runUpdateFlow = func(channel, username string) {
		p.sendChat(channel, "test-run")
		close(completed)
	}

	if err := p.handleCommand(mkUpdateCommandEvent("alice", "always_always_sunny", "false")); err != nil {
		t.Fatalf("handleCommand error: %v", err)
	}

	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("expected update flow to run")
	}

	if !bus.hasMessage("cytube.send", "always_always_sunny", "alice: Updating bot") {
		t.Fatalf("expected acknowledged start message")
	}

	if !bus.hasMessage("cytube.send", "always_always_sunny", "test-run") {
		t.Fatalf("expected injected update flow to send message")
	}
}

func TestHandleCommandSkipsWhenAlreadyUpdating(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	p.mu.Lock()
	p.updating = true
	p.mu.Unlock()

	called := false
	p.runUpdateFlow = func(channel, username string) {
		called = true
	}

	if err := p.handleCommand(mkUpdateCommandEvent("alice", "always_always_sunny", "false")); err != nil {
		t.Fatalf("handleCommand error: %v", err)
	}

	if called {
		t.Fatal("expected runUpdateFlow to be skipped when already updating")
	}

	if !bus.hasMessage("cytube.send", "always_always_sunny", "Update already in progress.") {
		t.Fatalf("expected in-progress guard response")
	}
}

func TestRunUpdateCycleNoopStillSendsAndSkipsRestart(t *testing.T) {
	bus := &testEventBus{}
	rawCfg := []byte(`{"operation_timeout_seconds":10}`)
	p := New().(*Plugin)
	if err := p.Init(rawCfg, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}

	commitCall := 0
	p.commandOutputFunc = func(_ context.Context, _ string, command ...string) (string, error) {
		commitCall++
		if strings.Contains(strings.Join(command, " "), "git log -1") {
			return "feedface\x00123", nil
		}
		if strings.Contains(strings.Join(command, " "), "git rev-parse") {
			return "feedface", nil
		}
		return "", fmt.Errorf("unexpected command: %v", command)
	}

	p.runGitCommandFunc = func(_ context.Context, _ string, args ...string) error {
		if len(args) == 0 {
			return fmt.Errorf("empty git command")
		}
		return nil
	}

	buildCalled := false
	p.runCommandFunc = func(_ context.Context, _ string, command ...string) error {
		buildCalled = true
		return nil
	}

	p.runUpdateCycle("always_always_sunny", "alice")

	if buildCalled {
		t.Fatal("expected build command to be skipped on no-op update")
	}
	if commitCall < 4 {
		t.Fatalf("expected both commit probes to run, got %d calls", commitCall)
	}
	if !bus.hasMessage("cytube.send", "always_always_sunny", "alice: already up to date") {
		t.Fatalf("expected no-op update message")
	}
}

func TestRunUpdateCycleRestartInterruptionIsNotReportedAsFailure(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init([]byte(`{"operation_timeout_seconds":10}`), bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.stateFile = filepath.Join(t.TempDir(), "pending_restart.json")

	logCall := 0
	shortCall := 0
	p.commandOutputFunc = func(_ context.Context, _ string, command ...string) (string, error) {
		cmd := strings.Join(command, " ")
		switch {
		case strings.Contains(cmd, "git log -1"):
			logCall++
			if logCall == 1 {
				return "origfull\x00old subject", nil
			}
			return "newfull\x00new subject", nil
		case strings.Contains(cmd, "git rev-parse"):
			shortCall++
			if shortCall == 1 {
				return "orig123", nil
			}
			return "new456", nil
		default:
			return "", fmt.Errorf("unexpected command: %v", command)
		}
	}

	p.runGitCommandFunc = func(_ context.Context, _ string, args ...string) error {
		if len(args) == 0 {
			return fmt.Errorf("empty git command")
		}
		return nil
	}

	buildCmd := strings.Join(p.config.BuildCommand, " ")
	restartCmd := strings.Join(p.config.RestartCommand, " ")
	buildCalled := false
	restartCalled := false
	p.runCommandFunc = func(_ context.Context, _ string, command ...string) error {
		cmd := strings.Join(command, " ")
		switch cmd {
		case buildCmd:
			buildCalled = true
			return nil
		case restartCmd:
			restartCalled = true
			return fmt.Errorf("signal: killed:")
		default:
			return fmt.Errorf("unexpected command: %v", command)
		}
	}

	p.runUpdateCycle("always_always_sunny", "alice")

	if !buildCalled || !restartCalled {
		t.Fatalf("expected build and restart commands to run (build=%v restart=%v)", buildCalled, restartCalled)
	}
	if !bus.hasMessage("cytube.send", "always_always_sunny", "alice: pulled commit new456") {
		t.Fatalf("expected pull/restart status message")
	}
	if bus.hasMessageContaining("cytube.send", "always_always_sunny", "(build ") {
		t.Fatalf("did not expect pre-restart status message to include build duration")
	}
	if bus.hasMessage("cytube.send", "always_always_sunny", "alice: Build succeeded for new456, but restart failed:") {
		t.Fatalf("did not expect false restart failure message for self-restart interruption")
	}

	p.mu.RLock()
	pending := p.pending
	p.mu.RUnlock()
	if pending == nil {
		t.Fatalf("expected pending restart notice to be persisted for post-restart announcement")
	}
	if strings.TrimSpace(pending.BuildDuration) == "" {
		t.Fatalf("expected pending restart notice to include build duration")
	}

	raw, err := os.ReadFile(p.stateFile)
	if err != nil {
		t.Fatalf("expected pending restart notice file to be written: %v", err)
	}
	var persisted pendingRestartNotice
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("unmarshal pending restart notice: %v", err)
	}
	if persisted.CommitShort != "new456" {
		t.Fatalf("expected commit short to be persisted, got %q", persisted.CommitShort)
	}
}

func TestRunUpdateCycleRestartFailureStillReported(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init([]byte(`{"operation_timeout_seconds":10}`), bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.stateFile = filepath.Join(t.TempDir(), "pending_restart.json")

	logCall := 0
	shortCall := 0
	p.commandOutputFunc = func(_ context.Context, _ string, command ...string) (string, error) {
		cmd := strings.Join(command, " ")
		switch {
		case strings.Contains(cmd, "git log -1"):
			logCall++
			if logCall == 1 {
				return "origfull\x00old subject", nil
			}
			return "newfull\x00new subject", nil
		case strings.Contains(cmd, "git rev-parse"):
			shortCall++
			if shortCall == 1 {
				return "orig123", nil
			}
			return "new456", nil
		default:
			return "", fmt.Errorf("unexpected command: %v", command)
		}
	}

	p.runGitCommandFunc = func(_ context.Context, _ string, args ...string) error {
		if len(args) == 0 {
			return fmt.Errorf("empty git command")
		}
		return nil
	}

	buildCmd := strings.Join(p.config.BuildCommand, " ")
	restartCmd := strings.Join(p.config.RestartCommand, " ")
	p.runCommandFunc = func(_ context.Context, _ string, command ...string) error {
		cmd := strings.Join(command, " ")
		switch cmd {
		case buildCmd:
			return nil
		case restartCmd:
			return fmt.Errorf("exit status 4: access denied")
		default:
			return fmt.Errorf("unexpected command: %v", command)
		}
	}

	p.runUpdateCycle("always_always_sunny", "alice")

	if !bus.hasMessage("cytube.send", "always_always_sunny", "alice: Build (") {
		t.Fatalf("expected restart failure output to include build duration")
	}
	if !bus.hasMessageContaining("cytube.send", "always_always_sunny", "succeeded for new456, but restart failed: exit status 4: access denied") {
		t.Fatalf("expected concrete restart failure to be reported")
	}
	p.mu.RLock()
	pending := p.pending
	p.mu.RUnlock()
	if pending != nil {
		t.Fatalf("expected pending restart notice to be cleared when restart fails")
	}
	if _, err := os.Stat(p.stateFile); !os.IsNotExist(err) {
		t.Fatalf("expected pending restart file to be removed after failed restart, err=%v", err)
	}
}

func TestHandleUserlistStartPublishesPendingRestartMessage(t *testing.T) {
	bus := &testEventBus{}
	p := New().(*Plugin)
	if err := p.Init(nil, bus); err != nil {
		t.Fatalf("Init error: %v", err)
	}
	p.stateFile = filepath.Join(t.TempDir(), "pending_restart.json")

	notice := &pendingRestartNotice{
		Channel:       "always_always_sunny",
		Username:      "alice",
		CommitShort:   "new456",
		CommitSubject: "new subject",
		BuildDuration: "1.2s",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	if err := p.persistPendingRestartNotice(notice); err != nil {
		t.Fatalf("persistPendingRestartNotice error: %v", err)
	}

	if err := p.handleUserlistStart(&framework.DataEvent{
		Data: &framework.EventData{
			RawMessage: &framework.RawMessageData{Channel: "some_other_channel"},
		},
	}); err != nil {
		t.Fatalf("handleUserlistStart mismatch error: %v", err)
	}
	if bus.hasMessageContaining("cytube.send", "always_always_sunny", "I'm back.") {
		t.Fatalf("did not expect post-restart message for non-matching channel")
	}

	if err := p.handleUserlistStart(&framework.DataEvent{
		Data: &framework.EventData{
			RawMessage: &framework.RawMessageData{Channel: "always_always_sunny"},
		},
	}); err != nil {
		t.Fatalf("handleUserlistStart error: %v", err)
	}

	if !bus.hasMessageContaining("cytube.send", "always_always_sunny", "alice: I'm back. Update complete in 1.2s build time. Running commit new456 — new subject") {
		t.Fatalf("expected post-restart status message with build duration")
	}

	p.mu.RLock()
	pending := p.pending
	p.mu.RUnlock()
	if pending != nil {
		t.Fatalf("expected pending restart notice to be cleared after publish")
	}
	if _, err := os.Stat(p.stateFile); !os.IsNotExist(err) {
		t.Fatalf("expected pending restart file to be removed after publish, err=%v", err)
	}
}

func TestFormatBuildDuration(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want string
	}{
		{name: "zero", in: 0, want: "0s"},
		{name: "milliseconds", in: 345 * time.Millisecond, want: "350ms"},
		{name: "seconds", in: 1349 * time.Millisecond, want: "1.3s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBuildDuration(tt.in)
			if got != tt.want {
				t.Fatalf("formatBuildDuration(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestUpdateLogWritesToFile(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "update.log")
	p := &Plugin{logFile: logPath}

	p.appendLogLine("INFO", "hello from test")

	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	contents := string(b)
	if !strings.Contains(contents, "[INFO] hello from test") {
		t.Fatalf("log contents missing message: %s", contents)
	}
}

func TestRunCommandOutputFailsWhenCommandEmpty(t *testing.T) {
	var p Plugin
	if _, err := p.runCommandOutput(context.Background(), "repo"); err == nil {
		t.Fatal("expected error for empty command")
	}
}
