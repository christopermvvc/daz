package sql

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
)

func TestNewPlugin(t *testing.T) {
	p := NewPlugin()
	if p == nil {
		t.Fatal("NewPlugin returned nil")
	}
	if p.Name() != "sql" {
		t.Errorf("Expected plugin name 'sql', got '%s'", p.Name())
	}
}

func TestPluginInit(t *testing.T) {
	p := NewPlugin()

	config := Config{
		Database: DatabaseConfig{
			Host:     "localhost",
			Port:     5432,
			Database: "test",
			User:     "test",
			Password: "test",
		},
		LoggerRules: []LoggerRule{
			{
				EventPattern: "cytube.event.chatMsg",
				Enabled:      true,
				Table:        "daz_chat_log",
				Fields:       []string{"username", "message"},
			},
		},
	}

	configData, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	mockBus := &mockEventBus{}
	err = p.Init(configData, mockBus)
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if len(p.loggerRules) != 1 {
		t.Errorf("Expected 1 logger rule, got %d", len(p.loggerRules))
	}
}

func TestFindMatchingEventTypes(t *testing.T) {
	p := NewPlugin()

	tests := []struct {
		pattern  string
		expected int
		contains []string
	}{
		{
			pattern:  "cytube.event.*",
			expected: 9,
			contains: []string{"cytube.event.chatMsg", "cytube.event.userJoin", "cytube.event.pm", "cytube.event.pm.sent", "cytube.event.playlist"},
		},
		{
			pattern:  "cytube.event.chatMsg",
			expected: 1,
			contains: []string{"cytube.event.chatMsg"},
		},
		{
			pattern:  "plugin.analytics.*",
			expected: 1,
			contains: []string{"plugin.analytics."},
		},
	}

	for _, test := range tests {
		matches := p.findMatchingEventTypes(test.pattern)
		if len(matches) != test.expected {
			t.Errorf("Pattern %s: expected %d matches, got %d", test.pattern, test.expected, len(matches))
		}
		for _, expected := range test.contains {
			found := false
			for _, match := range matches {
				if match == expected {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Pattern %s: expected to find %s in matches", test.pattern, expected)
			}
		}
	}
}

func TestExtractFieldsForRule(t *testing.T) {
	p := NewPlugin()

	// Test chat message extraction
	data := &framework.EventData{
		ChatMessage: &framework.ChatMessageData{
			Username: "testuser",
			Message:  "hello world",
			UserRank: 1,
			UserID:   "123",
			Channel:  "test",
		},
	}

	rule := LoggerRule{
		Fields: []string{"username", "message"},
	}

	fields := p.extractFieldsForRule(rule, data)

	if fields.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%v'", fields.Username)
	}
	if fields.Message != "hello world" {
		t.Errorf("Expected message 'hello world', got '%v'", fields.Message)
	}
	// When specific fields are requested, other fields should be empty/zero
	if fields.UserRank != 0 {
		t.Error("Expected user_rank to be filtered out (zero value)")
	}
}

func TestSplitConnectionBudget(t *testing.T) {
	tests := []struct {
		name                 string
		total                int
		backgroundConfigured int
		gomaxprocs           int
		wantCritical         int
		wantBackground       int
	}{
		{
			name:                 "single connection keeps critical lane only",
			total:                1,
			backgroundConfigured: 0,
			gomaxprocs:           1,
			wantCritical:         1,
			wantBackground:       0,
		},
		{
			name:                 "auto background split",
			total:                20,
			backgroundConfigured: 0,
			gomaxprocs:           4,
			wantCritical:         15,
			wantBackground:       5,
		},
		{
			name:                 "auto background split low cpu",
			total:                20,
			backgroundConfigured: 0,
			gomaxprocs:           1,
			wantCritical:         18,
			wantBackground:       2,
		},
		{
			name:                 "configured background capped",
			total:                4,
			backgroundConfigured: 10,
			gomaxprocs:           4,
			wantCritical:         1,
			wantBackground:       3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotCritical, gotBackground := splitConnectionBudgetForCPU(tc.total, tc.backgroundConfigured, tc.gomaxprocs)
			if gotCritical != tc.wantCritical || gotBackground != tc.wantBackground {
				t.Fatalf("splitConnectionBudgetForCPU(%d, %d, %d) = (%d, %d), want (%d, %d)",
					tc.total,
					tc.backgroundConfigured,
					tc.gomaxprocs,
					gotCritical,
					gotBackground,
					tc.wantCritical,
					tc.wantBackground,
				)
			}
		})
	}
}

func TestDefaultBackgroundQueueMax(t *testing.T) {
	tests := []struct {
		name            string
		backgroundConns int
		gomaxprocs      int
		want            int
	}{
		{
			name:            "zero background lanes",
			backgroundConns: 0,
			gomaxprocs:      1,
			want:            0,
		},
		{
			name:            "low cpu profile",
			backgroundConns: 2,
			gomaxprocs:      1,
			want:            8,
		},
		{
			name:            "default profile",
			backgroundConns: 2,
			gomaxprocs:      4,
			want:            16,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := defaultBackgroundQueueMax(tc.backgroundConns, tc.gomaxprocs)
			if got != tc.want {
				t.Fatalf("defaultBackgroundQueueMax(%d, %d) = %d, want %d", tc.backgroundConns, tc.gomaxprocs, got, tc.want)
			}
		})
	}
}

func TestClassifySQLRequestLane(t *testing.T) {
	p := &Plugin{}

	tests := []struct {
		name     string
		source   string
		query    string
		expected sqlRequestLane
	}{
		{
			name:     "background source routed to background lane",
			source:   "mediatracker",
			query:    "UPDATE daz_mediatracker_queue SET title = $1 WHERE channel = $2",
			expected: sqlLaneBackground,
		},
		{
			name:     "retry source routed to background lane",
			source:   "retry",
			query:    "SELECT * FROM acquire_retry_batch($1)",
			expected: sqlLaneBackground,
		},
		{
			name:     "core events query routed to background lane",
			source:   "unknown",
			query:    "SELECT * FROM daz_core_events LIMIT 10",
			expected: sqlLaneBackground,
		},
		{
			name:     "command source routed to critical lane",
			source:   "update",
			query:    "SELECT 1",
			expected: sqlLaneCritical,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := p.classifySQLRequestLane(tc.source, tc.query)
			if got != tc.expected {
				t.Fatalf("classifySQLRequestLane(%q, %q) = %v, want %v", tc.source, tc.query, got, tc.expected)
			}
		})
	}
}

func TestAcquireSQLRequestLaneCriticalIgnoresBackgroundPressure(t *testing.T) {
	p := &Plugin{}
	p.configureSQLRequestLanes(2, 1)

	// Saturate background lane.
	p.sqlBackgroundSem <- struct{}{}

	release, err := p.acquireSQLRequestLane(context.Background(), "update", "SELECT 1")
	if err != nil {
		t.Fatalf("expected critical lane acquisition to succeed: %v", err)
	}
	defer release()

	if len(p.sqlCriticalSem) != 1 {
		t.Fatalf("expected one critical slot to be occupied, got %d", len(p.sqlCriticalSem))
	}
}

func TestAcquireSQLRequestLaneBackgroundShedsWhenQueueFull(t *testing.T) {
	p := &Plugin{
		config: Config{
			BackgroundQueueMax:    1,
			BackgroundQueueWaitMS: 5,
		},
	}
	p.configureSQLRequestLanes(1, 1)

	// Saturate background active slot and pending queue.
	p.sqlBackgroundSem <- struct{}{}
	p.sqlBackgroundPend <- struct{}{}
	defer func() {
		<-p.sqlBackgroundSem
		<-p.sqlBackgroundPend
	}()

	_, err := p.acquireSQLRequestLane(context.Background(), "mediatracker", "UPDATE daz_mediatracker_queue SET title = $1")
	if err == nil {
		t.Fatal("expected background lane acquisition to fail when queue is full")
	}
	if got := p.sqlBackgroundDrops.Load(); got != 1 {
		t.Fatalf("expected one dropped background request, got %d", got)
	}
}

func TestEnqueueCoreEventLogDropsNonCriticalWhenQueuePressured(t *testing.T) {
	p := &Plugin{
		coreEventLogQueue: make(chan *coreEventLogEntry, 1),
	}
	p.coreEventLogQueue <- &coreEventLogEntry{EventType: "cytube.event.chatMsg"}

	p.enqueueCoreEventLog(&coreEventLogEntry{
		EventType: "cytube.event.mediaUpdate",
	})

	if got := p.coreEventDrops.Load(); got != 1 {
		t.Fatalf("expected one dropped core event, got %d", got)
	}
	if len(p.coreEventLogQueue) != 1 {
		t.Fatalf("expected queue to remain full, got len=%d", len(p.coreEventLogQueue))
	}
}

func TestBuildCoreEventBatchInsert(t *testing.T) {
	now := time.Now().UTC()
	entries := []*coreEventLogEntry{
		{
			EventType:   "cytube.event.chatMsg",
			Timestamp:   now,
			Channel:     "chan",
			Username:    "alice",
			Message:     "hi",
			MessageTime: 1,
			RawData:     []byte(`{"a":1}`),
		},
		{
			EventType:   "cytube.event.pm",
			Timestamp:   now,
			Channel:     "chan",
			Username:    "bob",
			Message:     "yo",
			MessageTime: 2,
			RawData:     []byte(`{"b":2}`),
		},
	}

	query, args := buildCoreEventBatchInsert(entries)
	if !strings.Contains(query, "INSERT INTO daz_core_events") {
		t.Fatalf("expected core events insert query, got %q", query)
	}
	if !strings.Contains(query, "$13") {
		t.Fatalf("expected second row placeholders in query, got %q", query)
	}
	if strings.Count(query, "($") != 2 {
		t.Fatalf("expected two VALUES tuples, got query %q", query)
	}
	if len(args) != 24 {
		t.Fatalf("expected 24 args for two rows, got %d", len(args))
	}
}

func TestExecLogReturnsErrorWithoutPool(t *testing.T) {
	p := &Plugin{}
	if err := p.execLog(context.Background(), "SELECT 1"); err == nil {
		t.Fatal("expected error when log pool is unavailable")
	}
}

func TestFlushCoreEventBatchUsesExecFn(t *testing.T) {
	now := time.Now().UTC()
	var called atomic.Bool
	var argCount atomic.Int64

	p := &Plugin{
		ctx: context.Background(),
		coreEventExecFn: func(ctx context.Context, query string, args ...interface{}) error {
			called.Store(true)
			if !strings.Contains(query, "INSERT INTO daz_core_events") {
				return fmt.Errorf("unexpected query: %s", query)
			}
			argCount.Store(int64(len(args)))
			return nil
		},
	}

	err := p.flushCoreEventBatch([]*coreEventLogEntry{
		{
			EventType:   "cytube.event.chatMsg",
			Timestamp:   now,
			Channel:     "chan",
			Username:    "alice",
			Message:     "hi",
			MessageTime: 10,
			RawData:     []byte(`{}`),
		},
	})
	if err != nil {
		t.Fatalf("flushCoreEventBatch failed: %v", err)
	}
	if !called.Load() {
		t.Fatal("expected exec callback to be called")
	}
	if argCount.Load() != 12 {
		t.Fatalf("expected 12 args for one row, got %d", argCount.Load())
	}
}

func TestFlushCoreEventBatchReturnsExecError(t *testing.T) {
	p := &Plugin{
		ctx: context.Background(),
		coreEventExecFn: func(ctx context.Context, query string, args ...interface{}) error {
			return errors.New("boom")
		},
	}

	err := p.flushCoreEventBatch([]*coreEventLogEntry{
		{
			EventType: "cytube.event.chatMsg",
			RawData:   []byte(`{}`),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected propagated exec error, got %v", err)
	}
}

func TestStartCoreEventWriterNoQueueNoWorker(t *testing.T) {
	p := &Plugin{}
	p.startCoreEventWriter()

	done := make(chan struct{})
	go func() {
		p.workerWg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("waitgroup did not complete immediately with no queue")
	}
}

func TestRunCoreEventWriterFlushesAndDrainsOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var flushCalls atomic.Int64
	var flushedRows atomic.Int64
	p := &Plugin{
		ctx:                ctx,
		coreEventLogQueue:  make(chan *coreEventLogEntry, 8),
		coreEventBatchSize: 2,
		// Keep periodic flush out of the way to make batching deterministic in test.
		coreEventFlushEvery: 30 * time.Second,
		coreEventExecFn: func(ctx context.Context, query string, args ...interface{}) error {
			flushCalls.Add(1)
			flushedRows.Add(int64(len(args) / 12))
			return nil
		},
	}

	p.startCoreEventWriter()

	for i := 0; i < 3; i++ {
		p.enqueueCoreEventLog(&coreEventLogEntry{
			EventType:   "cytube.event.chatMsg",
			Timestamp:   time.Now().UTC(),
			Channel:     "chan",
			Username:    "user",
			Message:     "msg",
			MessageTime: int64(i + 1),
			RawData:     []byte(`{}`),
		})
	}

	deadline := time.Now().Add(2 * time.Second)
	for flushCalls.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if flushCalls.Load() < 1 {
		t.Fatal("expected at least one flush while worker running")
	}

	cancel()

	done := make(chan struct{})
	go func() {
		p.workerWg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for writer worker shutdown")
	}

	if flushedRows.Load() != 3 {
		t.Fatalf("expected all 3 rows flushed before shutdown, got %d", flushedRows.Load())
	}
	if flushCalls.Load() < 2 {
		t.Fatalf("expected at least two flush calls (batch + drain), got %d", flushCalls.Load())
	}
}

// mockEventBus implements a minimal EventBus interface for testing
type mockEventBus struct {
	broadcasts []broadcastCall
	responses  map[string]*framework.EventData
}

type broadcastCall struct {
	eventType string
	data      *framework.EventData
}

func (m *mockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	if m.broadcasts == nil {
		m.broadcasts = make([]broadcastCall, 0)
	}
	m.broadcasts = append(m.broadcasts, broadcastCall{eventType: eventType, data: data})
	return nil
}

func (m *mockEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return m.Broadcast(eventType, data)
}

func (m *mockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	return nil
}

func (m *mockEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return nil
}

func (m *mockEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
	if m.responses == nil {
		m.responses = make(map[string]*framework.EventData)
	}
	m.responses[correlationID] = response
}

func (m *mockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	return nil
}

func (m *mockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	return nil
}

func (m *mockEventBus) RegisterPlugin(name string, plugin framework.Plugin) error {
	return nil
}

func (m *mockEventBus) UnregisterPlugin(name string) error {
	return nil
}

func (m *mockEventBus) GetDroppedEventCounts() map[string]int64 {
	return nil
}

func (m *mockEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}
