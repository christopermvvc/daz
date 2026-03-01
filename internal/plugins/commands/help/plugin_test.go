package help

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

type mockEventBus struct {
	mu            sync.Mutex
	subscriptions map[string][]framework.EventHandler
	broadcasts    []broadcastCall
	sends         []sendCall
	queryResult   *mockQueryResult
	sendNotify    chan struct{} // Notify when Send is called
}

type broadcastCall struct {
	eventType string
	data      *framework.EventData
}

type sendCall struct {
	target    string
	eventType string
	data      *framework.EventData
}

func newMockEventBus() *mockEventBus {
	return &mockEventBus{
		subscriptions: make(map[string][]framework.EventHandler),
		queryResult:   &mockQueryResult{},
		sendNotify:    make(chan struct{}, 10),
	}
}

func (m *mockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.broadcasts = append(m.broadcasts, broadcastCall{eventType: eventType, data: data})
	// Notify that Broadcast was called (for tests expecting response)
	select {
	case m.sendNotify <- struct{}{}:
	default:
	}
	return nil
}

func (m *mockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends = append(m.sends, sendCall{target: target, eventType: eventType, data: data})
	// Notify that Send was called
	select {
	case m.sendNotify <- struct{}{}:
	default:
	}
	return nil
}

func (m *mockEventBus) Query(sql string, params ...framework.SQLParam) (framework.QueryResult, error) {
	return m.queryResult, nil
}

func (m *mockEventBus) Exec(sql string, params ...framework.SQLParam) error {
	return nil
}

func (m *mockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscriptions[eventType] = append(m.subscriptions[eventType], handler)
	return nil
}

func (m *mockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscriptions[pattern] = append(m.subscriptions[pattern], handler)
	return nil
}

func (m *mockEventBus) SetSQLHandlers(queryHandler, execHandler framework.EventHandler) {
	// Mock implementation - can be empty
}

func (m *mockEventBus) GetDroppedEventCounts() map[string]int64 {
	return make(map[string]int64)
}

func (m *mockEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

func (m *mockEventBus) QuerySync(ctx context.Context, sql string, params ...framework.SQLParam) (*sql.Rows, error) {
	return nil, fmt.Errorf("sync queries not supported in mock")
}

func (m *mockEventBus) ExecSync(ctx context.Context, sql string, params ...framework.SQLParam) (sql.Result, error) {
	return nil, fmt.Errorf("sync exec not supported in mock")
}

func (m *mockEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return m.Broadcast(eventType, data)
}

func (m *mockEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return m.Send(target, eventType, data)
}

func (m *mockEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	return nil, fmt.Errorf("request not supported in mock")
}

func (m *mockEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
	// Mock implementation - can be empty
}

func (m *mockEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	return nil
}

func (m *mockEventBus) UnregisterPlugin(name string) error {
	return nil
}

type mockQueryResult struct {
	rows []mockRow
	idx  int
}

type mockRow struct {
	command        string
	pluginName     string
	isAlias        bool
	primaryCommand *string
}

func (m *mockQueryResult) Next() bool {
	return m.idx < len(m.rows)
}

func (m *mockQueryResult) Scan(dest ...interface{}) error {
	if m.idx >= len(m.rows) {
		return nil
	}

	row := m.rows[m.idx]
	if len(dest) >= 4 {
		*dest[0].(*string) = row.command
		*dest[1].(*string) = row.pluginName
		*dest[2].(*bool) = row.isAlias
		*dest[3].(**string) = row.primaryCommand
	}

	m.idx++
	return nil
}

func (m *mockQueryResult) Close() error {
	return nil
}

func TestNew(t *testing.T) {
	plugin := New()
	if plugin == nil {
		t.Fatal("New() returned nil")
	}

	p, ok := plugin.(*Plugin)
	if !ok {
		t.Fatal("New() did not return *Plugin")
	}

	if p.name != "help" {
		t.Errorf("Expected name 'help', got '%s'", p.name)
	}
}

func TestInit(t *testing.T) {
	tests := []struct {
		name   string
		config json.RawMessage
		want   *Config
	}{
		{
			name:   "default config",
			config: nil,
			want: &Config{
				ShowAliases: true,
			},
		},
		{
			name:   "custom config",
			config: json.RawMessage(`{"show_aliases": false}`),
			want: &Config{
				ShowAliases: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := New().(*Plugin)
			bus := newMockEventBus()

			err := plugin.Init(tt.config, bus)
			if err != nil {
				t.Fatalf("Init() error = %v", err)
			}

			if plugin.eventBus == nil {
				t.Error("EventBus not set")
			}

			if tt.want != nil {
				if plugin.config.ShowAliases != tt.want.ShowAliases {
					t.Errorf("ShowAliases = %v, want %v", plugin.config.ShowAliases, tt.want.ShowAliases)
				}
			}
		})
	}
}

func TestStartStop(t *testing.T) {
	plugin := New().(*Plugin)
	bus := newMockEventBus()

	err := plugin.Init(nil, bus)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Test Start
	err = plugin.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if !plugin.running {
		t.Error("Plugin should be running after Start()")
	}

	// Check subscription
	bus.mu.Lock()
	subCount := len(bus.subscriptions)
	bus.mu.Unlock()
	if subCount != 2 {
		t.Errorf("Expected 2 subscriptions, got %d", subCount)
	}

	// Check command registration
	bus.mu.Lock()
	bcastCount := len(bus.broadcasts)
	bus.mu.Unlock()
	if bcastCount != 1 {
		t.Errorf("Expected 1 broadcast (command registration), got %d", bcastCount)
	}

	// Test double start
	err = plugin.Start()
	if err == nil {
		t.Error("Expected error on double Start()")
	}

	// Test Stop
	err = plugin.Stop()
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if plugin.running {
		t.Error("Plugin should not be running after Stop()")
	}
}

func TestHandleEvent(t *testing.T) {
	plugin := New().(*Plugin)
	bus := newMockEventBus()

	err := plugin.Init(nil, bus)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// HandleEvent should always return nil
	event := &framework.CytubeEvent{
		EventType: "test",
		EventTime: time.Now(),
	}

	err = plugin.HandleEvent(event)
	if err != nil {
		t.Errorf("HandleEvent() error = %v", err)
	}
}

func TestStatus(t *testing.T) {
	plugin := New().(*Plugin)
	bus := newMockEventBus()

	err := plugin.Init(nil, bus)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Test stopped status
	status := plugin.Status()
	if status.Name != "help" {
		t.Errorf("Status.Name = %s, want help", status.Name)
	}
	if status.State != "stopped" {
		t.Errorf("Status.State = %s, want stopped", status.State)
	}

	// Test running status
	err = plugin.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	status = plugin.Status()
	if status.State != "running" {
		t.Errorf("Status.State = %s, want running", status.State)
	}
}

func TestHandleCommand(t *testing.T) {
	plugin := New().(*Plugin)
	bus := newMockEventBus()

	// Set up mock query results
	helpAlias := "help"
	bus.queryResult.rows = []mockRow{
		{command: "about", pluginName: "about", isAlias: false, primaryCommand: nil},
		{command: "h", pluginName: "help", isAlias: true, primaryCommand: &helpAlias},
		{command: "help", pluginName: "help", isAlias: false, primaryCommand: nil},
	}

	err := plugin.Init(nil, bus)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	err = plugin.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Create a command event
	eventData := &framework.EventData{
		PluginRequest: &framework.PluginRequest{
			From: "commandrouter",
			To:   "help",
			Data: &framework.RequestData{
				Command: &framework.CommandData{
					Name: "help",
					Params: map[string]string{
						"channel":  "test-channel",
						"username": "testuser",
					},
				},
			},
		},
	}

	event := framework.NewDataEvent("command.help.execute", eventData)

	// Handle the command
	bus.mu.Lock()
	handler := bus.subscriptions["command.help.execute"][0]
	bus.mu.Unlock()
	err = handler(event)
	if err != nil {
		t.Errorf("handleCommand() error = %v", err)
	}

	// Wait for async handler to complete
	select {
	case <-bus.sendNotify:
		// Broadcast was called
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for handler to send response")
	}

	// Give a bit more time for the handler to finish
	handlerFinish := make(chan struct{})
	go func() {
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		<-timer.C
		close(handlerFinish)
	}()
	<-handlerFinish

	// Check that a PM was broadcast
	bus.mu.Lock()
	broadcastCount := len(bus.broadcasts)
	// Find the cytube.send.pm broadcast
	var broadcast broadcastCall
	found := false
	for _, b := range bus.broadcasts {
		if b.eventType == "cytube.send.pm" {
			broadcast = b
			found = true
			break
		}
	}
	bus.mu.Unlock()
	if !found {
		t.Errorf("Expected cytube.send.pm broadcast not found in %d broadcasts", broadcastCount)
		return
	}
	if broadcast.data.PrivateMessage == nil {
		t.Fatal("Expected PrivateMessage data")
	}
	if broadcast.data.PrivateMessage.ToUser != "testuser" {
		t.Errorf("Expected ToUser 'testuser', got '%s'", broadcast.data.PrivateMessage.ToUser)
	}
}

func TestHelpVisibilityParityListAndDetail(t *testing.T) {
	plugin := New().(*Plugin)
	bus := newMockEventBus()

	if err := plugin.Init(nil, bus); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	plugin.cacheMu.Lock()
	plugin.commandCache = map[string]*commandEntry{
		"ping": {
			Primary:    "ping",
			PluginName: "ping",
			MinRank:    0,
			AdminOnly:  false,
		},
		"uptime": {
			Primary:    "uptime",
			PluginName: "uptime",
			MinRank:    0,
			Aliases:    []string{"up"},
			AdminOnly:  true,
		},
		"modcmd": {
			Primary:    "modcmd",
			PluginName: "mod",
			MinRank:    2,
			AdminOnly:  false,
		},
	}
	plugin.aliasIndex = map[string]string{"up": "uptime"}
	plugin.cacheMu.Unlock()

	nonAdminReq := makeHelpReq("bob", "test", 0, false)
	nonAdminList, err := plugin.buildCommandList(nonAdminReq)
	if err != nil {
		t.Fatalf("buildCommandList() error = %v", err)
	}
	joinedNonAdmin := strings.Join(nonAdminList, "\n")
	if !strings.Contains(joinedNonAdmin, "!ping") {
		t.Fatalf("expected !ping in non-admin list: %s", joinedNonAdmin)
	}
	if strings.Contains(joinedNonAdmin, "!uptime") {
		t.Fatalf("did not expect !uptime in non-admin list: %s", joinedNonAdmin)
	}
	if strings.Contains(joinedNonAdmin, "!modcmd") {
		t.Fatalf("did not expect !modcmd in non-admin list: %s", joinedNonAdmin)
	}

	nonAdminDetail, _, err := plugin.lookupCommand(nonAdminReq, "uptime")
	if err != nil {
		t.Fatalf("lookupCommand(non-admin) error = %v", err)
	}
	if nonAdminDetail != nil {
		t.Fatal("expected non-admin detail lookup for uptime to be hidden")
	}

	adminReq := makeHelpReq("alice", "test", 0, true)
	adminList, err := plugin.buildCommandList(adminReq)
	if err != nil {
		t.Fatalf("buildCommandList(admin) error = %v", err)
	}
	joinedAdmin := strings.Join(adminList, "\n")
	if !strings.Contains(joinedAdmin, "!uptime") {
		t.Fatalf("expected !uptime in admin list: %s", joinedAdmin)
	}

	adminDetail, _, err := plugin.lookupCommand(adminReq, "up")
	if err != nil {
		t.Fatalf("lookupCommand(admin alias) error = %v", err)
	}
	if adminDetail == nil || adminDetail.Primary != "uptime" {
		t.Fatal("expected admin alias lookup for up to resolve to uptime")
	}
}

func TestCheckHelpCooldown(t *testing.T) {
	plugin := New().(*Plugin)
	bus := newMockEventBus()

	if err := plugin.Init(nil, bus); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if wait, ok := plugin.checkHelpCooldown("test", "user"); !ok || wait != 0 {
		t.Fatalf("first cooldown check should allow request, got ok=%v wait=%v", ok, wait)
	}

	if wait, ok := plugin.checkHelpCooldown("test", "user"); ok || wait <= 0 {
		t.Fatalf("second cooldown check should block request, got ok=%v wait=%v", ok, wait)
	}
}

func TestIsMissingDescriptionError(t *testing.T) {
	if !isMissingDescriptionError(fmt.Errorf("column \"description\" does not exist")) {
		t.Fatalf("expected missing description column error")
	}
	if !isMissingDescriptionError(fmt.Errorf("pq: column description does not exist")) {
		t.Fatalf("expected missing description column error")
	}
	if isMissingDescriptionError(fmt.Errorf("some other error")) {
		t.Fatalf("did not expect unrelated error to match")
	}
}

func TestSplitMessagesSinglePMWhenWithinLimit(t *testing.T) {
	lines := []string{"!about - bot info", "!uptime - show uptime"}
	messages := splitMessages("Commands:", lines, 200)

	if len(messages) != 1 {
		t.Fatalf("expected one message, got %d", len(messages))
	}

	if !strings.Contains(messages[0], "Commands:") {
		t.Fatalf("expected header in message, got %q", messages[0])
	}
	if !strings.Contains(messages[0], "!about - bot info") || !strings.Contains(messages[0], "!uptime - show uptime") {
		t.Fatalf("expected both lines in single message, got %q", messages[0])
	}
}

func TestSplitMessagesSplitsWhenOverLimit(t *testing.T) {
	lines := []string{
		"!about - bot info",
		"!uptime - show uptime",
		"!quote - random quote",
		"!weather - weather report",
	}
	maxLen := 40
	messages := splitMessages("Commands:", lines, maxLen)

	if len(messages) < 2 {
		t.Fatalf("expected multiple messages, got %d", len(messages))
	}

	for i, message := range messages {
		if got := len([]rune(message)); got > maxLen {
			t.Fatalf("message %d exceeded max length: got %d want <= %d", i, got, maxLen)
		}
	}
}

func TestSplitMessagesExactBoundaryStaysSingle(t *testing.T) {
	lines := []string{"!about - bot info", "!uptime - show uptime"}
	full := "Commands:\n" + strings.Join(lines, "\n")
	maxLen := len([]rune(full))

	messages := splitMessages("Commands:", lines, maxLen)
	if len(messages) != 1 {
		t.Fatalf("expected one message at exact boundary, got %d", len(messages))
	}
	if messages[0] != full {
		t.Fatalf("unexpected message at boundary: got %q want %q", messages[0], full)
	}
}

func makeHelpReq(username, channel string, rank int, isAdmin bool) *framework.PluginRequest {
	return &framework.PluginRequest{
		Data: &framework.RequestData{
			Command: &framework.CommandData{
				Params: map[string]string{
					"username": username,
					"channel":  channel,
					"rank":     fmt.Sprintf("%d", rank),
					"is_admin": fmt.Sprintf("%t", isAdmin),
				},
			},
		},
	}
}
