package update

import (
	"context"
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
