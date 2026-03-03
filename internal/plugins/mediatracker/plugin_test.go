package mediatracker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/pkg/eventbus"
)

// mockEventBus implements framework.EventBus for testing
type mockEventBus struct {
	subscribers    map[string][]framework.EventHandler
	execCalls      []string
	queryCalls     []string
	sendCalls      []sendCall
	broadcastCalls []sendCall
	mu             sync.Mutex

	// Control query responses
	queryResponses map[string]*mockQueryResult
}

type sendCall struct {
	target    string
	eventType string
	data      interface{}
}

func newMockEventBus() *mockEventBus {
	return &mockEventBus{
		subscribers:    make(map[string][]framework.EventHandler),
		execCalls:      []string{},
		queryCalls:     []string{},
		sendCalls:      []sendCall{},
		broadcastCalls: []sendCall{},
		queryResponses: make(map[string]*mockQueryResult),
	}
}

func (m *mockEventBus) Broadcast(eventType string, data *framework.EventData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.broadcastCalls = append(m.broadcastCalls, sendCall{"", eventType, data})
	return nil
}

func (m *mockEventBus) Send(target string, eventType string, data *framework.EventData) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendCalls = append(m.sendCalls, sendCall{target, eventType, data})
	return nil
}

func (m *mockEventBus) SetSQLHandlers(queryHandler, execHandler framework.EventHandler) {
	// No-op for tests
}

func (m *mockEventBus) GetDroppedEventCounts() map[string]int64 {
	return make(map[string]int64)
}

func (m *mockEventBus) GetDroppedEventCount(eventType string) int64 {
	return 0
}

func (m *mockEventBus) Query(sql string, params ...framework.SQLParam) (framework.QueryResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queryCalls = append(m.queryCalls, sql)
	return &mockQueryResult{}, nil
}

func (m *mockEventBus) Exec(sql string, params ...framework.SQLParam) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.execCalls = append(m.execCalls, sql)
	return nil
}

func (m *mockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribers[eventType] = append(m.subscribers[eventType], handler)
	return nil
}

func (m *mockEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subscribers[pattern] = append(m.subscribers[pattern], handler)
	return nil
}

func (m *mockEventBus) QuerySync(ctx context.Context, sql string, params ...framework.SQLParam) (*sql.Rows, error) {
	return nil, fmt.Errorf("sync queries not supported in mock")
}

func (m *mockEventBus) ExecSync(ctx context.Context, sql string, params ...framework.SQLParam) (sql.Result, error) {
	m.mu.Lock()
	m.execCalls = append(m.execCalls, sql)
	m.mu.Unlock()
	return &mockSQLResult{}, nil
}

func (m *mockEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return m.Broadcast(eventType, data)
}

func (m *mockEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return m.Send(target, eventType, data)
}

func (m *mockEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Handle SQL exec requests
	if eventType == "sql.exec.request" && data != nil && data.SQLExecRequest != nil {
		m.execCalls = append(m.execCalls, data.SQLExecRequest.Query)
		return &framework.EventData{
			SQLExecResponse: &framework.SQLExecResponse{
				Success:      true,
				RowsAffected: 1,
			},
		}, nil
	}

	// Handle SQL query requests
	if eventType == "sql.query.request" && data != nil && data.SQLQueryRequest != nil {
		m.queryCalls = append(m.queryCalls, data.SQLQueryRequest.Query)

		// Check for predefined responses
		for key, result := range m.queryResponses {
			if len(data.SQLQueryRequest.Query) >= len(key) && data.SQLQueryRequest.Query[:len(key)] == key {
				// Convert mock result to SQL response format
				var rows [][]json.RawMessage
				for _, row := range result.rows {
					var jsonRow []json.RawMessage
					for _, val := range row {
						jsonBytes, _ := json.Marshal(val)
						jsonRow = append(jsonRow, json.RawMessage(jsonBytes))
					}
					rows = append(rows, jsonRow)
				}

				return &framework.EventData{
					SQLQueryResponse: &framework.SQLQueryResponse{
						Success: true,
						Columns: []string{"col1", "col2", "col3"}, // Generic columns
						Rows:    rows,
					},
				}, nil
			}
		}

		// Return empty result if no predefined response
		return &framework.EventData{
			SQLQueryResponse: &framework.SQLQueryResponse{
				Success: true,
				Columns: []string{},
				Rows:    [][]json.RawMessage{},
			},
		}, nil
	}

	return nil, fmt.Errorf("request type %s not supported in mock", eventType)
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

func (m *mockEventBus) HandleEvent(ctx context.Context, event framework.Event) error {
	return nil
}

// mockQueryResult implements framework.QueryResult
type mockQueryResult struct {
	rows     [][]interface{}
	rowIndex int
	closed   bool
}

func (m *mockQueryResult) Next() bool {
	if m.closed || m.rowIndex >= len(m.rows) {
		return false
	}
	m.rowIndex++
	return m.rowIndex <= len(m.rows)
}

func (m *mockQueryResult) Scan(dest ...interface{}) error {
	if m.rowIndex == 0 || m.rowIndex > len(m.rows) {
		return errors.New("no current row")
	}
	row := m.rows[m.rowIndex-1]
	for i, d := range dest {
		if i >= len(row) {
			return errors.New("column index out of range")
		}
		switch v := d.(type) {
		case *string:
			*v = row[i].(string)
		case *int:
			*v = row[i].(int)
		case *int64:
			*v = row[i].(int64)
		case *time.Time:
			*v = row[i].(time.Time)
		}
	}
	return nil
}

func (m *mockQueryResult) Close() error {
	m.closed = true
	return nil
}

// mockSQLResult implements sql.Result
type mockSQLResult struct{}

func (m *mockSQLResult) LastInsertId() (int64, error) {
	return 0, nil
}

func (m *mockSQLResult) RowsAffected() (int64, error) {
	return 1, nil
}

func TestNewPlugin(t *testing.T) {
	// Test with nil config
	p := NewPlugin(nil)
	if p.name != "mediatracker" {
		t.Errorf("Expected plugin name to be 'mediatracker', got '%s'", p.name)
	}
	if p.config.StatsUpdateInterval != 5*time.Minute {
		t.Errorf("Expected default stats interval to be 5 minutes, got %v", p.config.StatsUpdateInterval)
	}

	// Test with custom config
	config := &Config{
		StatsUpdateInterval: 10 * time.Minute,
	}
	p = NewPlugin(config)
	if p.config.StatsUpdateInterval != 10*time.Minute {
		t.Errorf("Expected custom stats interval to be 10 minutes, got %v", p.config.StatsUpdateInterval)
	}
}

func TestPluginInitialize(t *testing.T) {
	p := NewPlugin(nil)
	bus := newMockEventBus()

	err := p.Initialize(bus)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Table creation is now deferred to Start(), not Init()
	// So we should not expect it here

	// Check that context was created
	if p.ctx == nil {
		t.Error("Expected context to be created")
	}
	if p.cancel == nil {
		t.Error("Expected cancel function to be created")
	}
}

func TestPluginStart(t *testing.T) {
	p := NewPlugin(nil)
	bus := newMockEventBus()

	// Initialize first
	err := p.Initialize(bus)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Start the plugin
	err = p.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait for async table creation to complete
	time.Sleep(3 * time.Second)

	// Check that tables were created asynchronously
	expectedTables := 6 // plays, queue, stats, library + ALTER TABLE for uid + CREATE INDEX for uid
	bus.mu.Lock()
	actualTables := len(bus.execCalls)
	bus.mu.Unlock()
	if actualTables != expectedTables {
		t.Errorf("Expected %d table creation calls after async creation, got %d", expectedTables, actualTables)
	}

	// Check that it subscribed to the right events
	expectedSubscriptions := []string{
		eventbus.EventCytubeVideoChange,
		"cytube.event.setCurrent",
		"cytube.event.queue",
		eventbus.EventCytubeMediaUpdate,
		"cytube.event.playlist",
		"command.mediatracker.execute",
	}

	for _, eventType := range expectedSubscriptions {
		if _, ok := bus.subscribers[eventType]; !ok {
			t.Errorf("Expected subscription to '%s' event", eventType)
		}
	}

	// Check that it's marked as running
	if !p.running {
		t.Error("Expected plugin to be marked as running")
	}

	// Try starting again - should fail
	err = p.Start()
	if err == nil {
		t.Error("Expected error when starting already running plugin")
	}

	// Stop the plugin
	err = p.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestHandleMediaChange(t *testing.T) {
	p := NewPlugin(nil)
	bus := newMockEventBus()

	err := p.Initialize(bus)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Create a media change event
	mediaData := &framework.VideoChangeData{
		VideoID:   "abc123",
		VideoType: "youtube",
		Title:     "Test Video",
		Duration:  300,
		Channel:   "test-channel",
	}

	event := &framework.DataEvent{
		EventType: eventbus.EventCytubeVideoChange,
		Data: &framework.EventData{
			VideoChange: mediaData,
		},
	}

	// Handle the event
	err = p.handleMediaChange(event)
	if err != nil {
		t.Fatalf("handleMediaChange failed: %v", err)
	}

	// Check that current media was set
	if p.currentMedia == nil {
		t.Fatal("Expected current media to be set")
	}
	if p.currentMedia.ID != "abc123" {
		t.Errorf("Expected media ID to be 'abc123', got '%s'", p.currentMedia.ID)
	}
	if p.currentMedia.Title != "Test Video" {
		t.Errorf("Expected media title to be 'Test Video', got '%s'", p.currentMedia.Title)
	}

	// Check that SQL was executed
	if len(bus.execCalls) < 2 {
		t.Error("Expected at least 2 SQL exec calls (insert play and update stats)")
	}
}

func TestHandleMediaChange_FromRawVideoChangeEvent(t *testing.T) {
	p := NewPlugin(nil)
	bus := newMockEventBus()

	err := p.Initialize(bus)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Some event flows still send changeMedia as a raw VideoChangeEvent.
	event := &framework.DataEvent{
		EventType: eventbus.EventCytubeVideoChange,
		Data: &framework.EventData{
			RawEvent: &framework.VideoChangeEvent{
				CytubeEvent: framework.CytubeEvent{
					ChannelName: "raw-channel",
				},
				VideoID:   "raw123",
				VideoType: "youtube",
				Duration:  120,
				Title:     "Raw Video",
			},
		},
	}

	err = p.handleMediaChange(event)
	if err != nil {
		t.Fatalf("handleMediaChange failed: %v", err)
	}

	if p.currentMedia == nil {
		t.Fatal("Expected current media to be set from raw event")
	}
	if p.currentMedia.ID != "raw123" {
		t.Errorf("Expected media ID to be 'raw123', got '%s'", p.currentMedia.ID)
	}
}

func TestHandlePlaylistEvent_QueuesStagedIngestion(t *testing.T) {
	p := NewPlugin(nil)
	bus := newMockEventBus()

	if err := p.Initialize(bus); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	first := &framework.PlaylistArrayEvent{
		CytubeEvent: framework.CytubeEvent{
			ChannelName: "test-channel",
		},
		Items: []framework.PlaylistItem{
			{Position: 0, MediaID: "a1", MediaType: "yt", Title: "A1", Duration: 100, QueuedBy: "u1"},
			{Position: 1, MediaID: "a2", MediaType: "yt", Title: "A2", Duration: 120, QueuedBy: "u2"},
		},
	}

	if err := p.handlePlaylistEvent(first); err != nil {
		t.Fatalf("handlePlaylistEvent failed: %v", err)
	}

	p.mu.RLock()
	req, ok := p.pendingPlaylistIngest["test-channel"]
	p.mu.RUnlock()
	if !ok {
		t.Fatal("expected staged playlist ingestion to be queued")
	}
	if req.eventName != "playlist event" {
		t.Fatalf("expected eventName 'playlist event', got %q", req.eventName)
	}
	if len(req.items) != 2 {
		t.Fatalf("expected 2 queued items, got %d", len(req.items))
	}

	// New full playlist for the same channel should coalesce/replace the pending request.
	second := &framework.PlaylistArrayEvent{
		CytubeEvent: framework.CytubeEvent{
			ChannelName: "test-channel",
		},
		Items: []framework.PlaylistItem{
			{Position: 0, MediaID: "b1", MediaType: "yt", Title: "B1", Duration: 101, QueuedBy: "u1"},
			{Position: 1, MediaID: "b2", MediaType: "yt", Title: "B2", Duration: 121, QueuedBy: "u2"},
			{Position: 2, MediaID: "b3", MediaType: "yt", Title: "B3", Duration: 131, QueuedBy: "u3"},
		},
	}

	if err := p.handlePlaylistEvent(second); err != nil {
		t.Fatalf("second handlePlaylistEvent failed: %v", err)
	}

	p.mu.RLock()
	req, ok = p.pendingPlaylistIngest["test-channel"]
	pendingCount := len(p.pendingPlaylistIngest)
	p.mu.RUnlock()
	if !ok {
		t.Fatal("expected staged playlist ingestion to remain queued")
	}
	if pendingCount != 1 {
		t.Fatalf("expected a single coalesced pending channel, got %d", pendingCount)
	}
	if len(req.items) != 3 {
		t.Fatalf("expected pending request to be replaced with 3 items, got %d", len(req.items))
	}
	if req.items[0].MediaID != "b1" {
		t.Fatalf("expected coalesced request to contain latest payload, got %s", req.items[0].MediaID)
	}
}

func TestProcessQueueUpdate_Full_QueuesStagedIngestion(t *testing.T) {
	p := NewPlugin(nil)
	bus := newMockEventBus()

	if err := p.Initialize(bus); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	queueData := &framework.QueueUpdateData{
		Channel: "queue-channel",
		Action:  "full",
		Items: []framework.QueueItem{
			{Position: 0, MediaID: "q1", MediaType: "yt", Title: "Q1", Duration: 90, QueuedBy: "u1"},
			{Position: 1, MediaID: "q2", MediaType: "yt", Title: "Q2", Duration: 95, QueuedBy: "u2"},
		},
	}

	if err := p.processQueueUpdate(queueData); err != nil {
		t.Fatalf("processQueueUpdate(full) failed: %v", err)
	}

	p.mu.RLock()
	req, ok := p.pendingPlaylistIngest["queue-channel"]
	p.mu.RUnlock()
	if !ok {
		t.Fatal("expected full queue update to enqueue staged ingestion")
	}
	if req.eventName != "queue event" {
		t.Fatalf("expected eventName 'queue event', got %q", req.eventName)
	}
	if len(req.items) != 2 {
		t.Fatalf("expected 2 queued items, got %d", len(req.items))
	}
	if req.items[0].MediaID != "q1" {
		t.Fatalf("expected first queued media_id q1, got %s", req.items[0].MediaID)
	}

	bus.mu.Lock()
	execCalls := len(bus.execCalls)
	bus.mu.Unlock()
	if execCalls != 0 {
		t.Fatalf("expected no immediate SQL execs for full queue update, got %d", execCalls)
	}
}

func TestProcessPlaylistIngest_PhasedSQLWrites(t *testing.T) {
	p := NewPlugin(&Config{
		StatsUpdateInterval:     5 * time.Minute,
		PlaylistIngestBatchSize: 2,
		PlaylistIngestPauseMS:   0,
		PlaylistPhase2DelayMS:   0,
	})
	bus := newMockEventBus()

	if err := p.Initialize(bus); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	req := playlistIngestRequest{
		channel: "phase-channel",
		items: []framework.PlaylistItem{
			{Position: 0, MediaID: "p1", MediaType: "yt", Title: "P1", Duration: 100, QueuedBy: "u1"},
			{Position: 1, MediaID: "p2", MediaType: "yt", Title: "P2", Duration: 110, QueuedBy: "u2"},
			{Position: 2, MediaID: "p3", MediaType: "yt", Title: "P3", Duration: 120, QueuedBy: "u3"},
		},
		eventName: "playlist event",
		enqueued:  time.Now(),
	}

	p.processPlaylistIngest(req)

	bus.mu.Lock()
	defer bus.mu.Unlock()

	deleteQueueCount := 0
	queueInsertCount := 0
	libraryInsertCount := 0
	for _, q := range bus.execCalls {
		if strings.Contains(q, "DELETE FROM daz_mediatracker_queue") {
			deleteQueueCount++
		}
		if strings.Contains(q, "INSERT INTO daz_mediatracker_queue") {
			queueInsertCount++
		}
		if strings.Contains(q, "INSERT INTO daz_mediatracker_library") {
			libraryInsertCount++
		}
	}

	if deleteQueueCount != 1 {
		t.Fatalf("expected 1 queue delete in phase 1, got %d", deleteQueueCount)
	}
	// 3 items with batch size 2 => 2 queue inserts, then 2 library inserts.
	if queueInsertCount != 2 {
		t.Fatalf("expected 2 queue batch inserts, got %d", queueInsertCount)
	}
	if libraryInsertCount != 2 {
		t.Fatalf("expected 2 library batch inserts, got %d", libraryInsertCount)
	}
}

func TestPlaylistIngestWorker_ProcessesCoalescedLatestPayload(t *testing.T) {
	p := NewPlugin(&Config{
		StatsUpdateInterval:     5 * time.Minute,
		PlaylistIngestBatchSize: 2,
		PlaylistIngestPauseMS:   0,
		PlaylistPhase2DelayMS:   0,
	})
	bus := newMockEventBus()

	if err := p.Initialize(bus); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Queue two payloads for the same channel before worker starts.
	// The second should replace the first in pendingPlaylistIngest.
	p.enqueuePlaylistIngest("worker-channel", []framework.PlaylistItem{
		{Position: 0, MediaID: "old", MediaType: "yt", Title: "Old", Duration: 90, QueuedBy: "u1"},
	}, "playlist event")
	p.enqueuePlaylistIngest("worker-channel", []framework.PlaylistItem{
		{Position: 0, MediaID: "new1", MediaType: "yt", Title: "New1", Duration: 91, QueuedBy: "u1"},
		{Position: 1, MediaID: "new2", MediaType: "yt", Title: "New2", Duration: 92, QueuedBy: "u2"},
		{Position: 2, MediaID: "new3", MediaType: "yt", Title: "New3", Duration: 93, QueuedBy: "u3"},
	}, "playlist event")

	p.wg.Add(1)
	go p.runPlaylistIngestWorker()

	deadline := time.Now().Add(2 * time.Second)
	for {
		bus.mu.Lock()
		execCount := len(bus.execCalls)
		bus.mu.Unlock()
		// 3 items, batch size 2 => 1 delete + 2 queue inserts + 2 library inserts
		if execCount >= 5 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for worker ingestion, only saw %d SQL exec calls", execCount)
		}
		time.Sleep(10 * time.Millisecond)
	}

	p.cancel()
	p.wg.Wait()

	p.mu.RLock()
	pending := len(p.pendingPlaylistIngest)
	p.mu.RUnlock()
	if pending != 0 {
		t.Fatalf("expected worker to drain pending playlist ingest map, got %d entries", pending)
	}
}

func TestHandleNowPlayingCommand(t *testing.T) {
	p := NewPlugin(nil)
	bus := newMockEventBus()

	err := p.Initialize(bus)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Test with no current media
	req := &framework.PluginRequest{
		From: "eventfilter",
		To:   "mediatracker",
		Type: "execute",
		Data: &framework.RequestData{
			Command: &framework.CommandData{
				Name: "nowplaying",
				Params: map[string]string{
					"username": "testuser",
					"channel":  "testchannel",
				},
			},
		},
	}

	p.handleNowPlayingCommand(req)

	// Check response - should be one broadcast call to cytube.send.pm
	if len(bus.broadcastCalls) != 1 {
		t.Fatal("Expected one broadcast call")
	}
	if bus.broadcastCalls[0].eventType != "cytube.send.pm" {
		t.Errorf("Expected broadcast to 'cytube.send.pm', got '%s'", bus.broadcastCalls[0].eventType)
	}

	// Set current media
	p.currentMedia = &MediaState{
		ID:        "test123",
		Type:      "youtube",
		Title:     "Test Video",
		Duration:  300,
		StartedAt: time.Now().Add(-30 * time.Second),
	}

	// Test with current media
	p.handleNowPlayingCommand(req)

	// Should have 2 broadcast calls now
	if len(bus.broadcastCalls) != 2 {
		t.Fatal("Expected two broadcast calls")
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{30 * time.Second, "0:30"},
		{90 * time.Second, "1:30"},
		{3600 * time.Second, "1:00:00"},
		{3661 * time.Second, "1:01:01"},
		{-5 * time.Second, "0:00"},
	}

	for _, test := range tests {
		result := formatDuration(test.input)
		if result != test.expected {
			t.Errorf("formatDuration(%v) = %s, expected %s", test.input, result, test.expected)
		}
	}
}

func TestPluginStop(t *testing.T) {
	p := NewPlugin(nil)
	bus := newMockEventBus()

	// Initialize and start
	err := p.Initialize(bus)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	err = p.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Stop the plugin
	err = p.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Check that it's no longer running
	if p.running {
		t.Error("Expected plugin to be marked as not running")
	}

	// Stop again - should not error
	err = p.Stop()
	if err != nil {
		t.Error("Expected no error when stopping already stopped plugin")
	}
}

func TestHandleEvent(t *testing.T) {
	p := NewPlugin(nil)
	event := &framework.DataEvent{}

	// HandleEvent should always return nil as this plugin uses specific subscriptions
	err := p.HandleEvent(event)
	if err != nil {
		t.Errorf("Expected HandleEvent to return nil, got %v", err)
	}
}
