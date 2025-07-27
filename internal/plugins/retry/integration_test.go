//go:build integration
// +build integration

package retry

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestRetryIntegration performs comprehensive integration tests for the retry mechanism
func TestRetryIntegration(t *testing.T) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup test database connection
	db := setupTestDatabase(t)
	defer db.Close()

	// Create test event bus
	bus := NewTestEventBus(db)

	// Initialize and start retry plugin
	plugin := NewPlugin()
	config := json.RawMessage(`{
		"enabled": true,
		"max_retries": 3,
		"initial_delay_seconds": 1,
		"max_delay_seconds": 10,
		"backoff_multiplier": 2,
		"worker_count": 2,
		"batch_size": 5,
		"persistence_enabled": true
	}`)

	if err := plugin.Init(config, bus); err != nil {
		t.Fatalf("Failed to initialize retry plugin: %v", err)
	}

	if err := plugin.Start(); err != nil {
		t.Fatalf("Failed to start retry plugin: %v", err)
	}
	defer plugin.Stop()

	// Wait for plugin to be ready
	time.Sleep(100 * time.Millisecond)

	// Run test scenarios
	t.Run("BasicRetryFlow", func(t *testing.T) {
		testBasicRetryFlow(t, plugin, bus, db)
	})

	t.Run("ExponentialBackoff", func(t *testing.T) {
		testExponentialBackoff(t, plugin, bus, db)
	})

	t.Run("PriorityOrdering", func(t *testing.T) {
		testPriorityOrdering(t, plugin, bus, db)
	})

	t.Run("ConcurrentWorkers", func(t *testing.T) {
		testConcurrentWorkers(t, plugin, bus, db)
	})

	t.Run("MaxAttemptsLimit", func(t *testing.T) {
		testMaxAttemptsLimit(t, plugin, bus, db)
	})

	t.Run("PersistenceAcrossRestart", func(t *testing.T) {
		testPersistenceAcrossRestart(t, plugin, bus, db)
	})
}

func testBasicRetryFlow(t *testing.T, plugin framework.Plugin, bus *TestEventBus, db *sql.DB) {
	// Track retry attempts
	var attempts int32
	bus.SetHandler("test.operation", func(event framework.Event) error {
		count := atomic.AddInt32(&attempts, 1)
		if count < 3 {
			return fmt.Errorf("simulated failure %d", count)
		}
		return nil // Success on third attempt
	})

	// Schedule a retry operation
	request := &framework.RetryRequest{
		OperationID:   "test-basic-" + time.Now().Format("20060102150405"),
		OperationType: "test",
		TargetPlugin:  "test",
		EventType:     "test.operation",
		Payload:       json.RawMessage(`{"test": "data"}`),
		Priority:      5,
	}

	event := &framework.DataEvent{
		Data: &framework.EventData{
			RetryRequest: request,
		},
	}

	// Schedule retry by broadcasting the event
	if err := bus.Broadcast("retry.schedule", event.Data); err != nil {
		t.Fatalf("Failed to schedule retry: %v", err)
	}

	// Wait for retries to complete
	time.Sleep(5 * time.Second)

	// Verify attempts
	finalAttempts := atomic.LoadInt32(&attempts)
	if finalAttempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", finalAttempts)
	}

	// Verify final status in database
	var status string
	err := db.QueryRow(`
		SELECT status FROM daz_retry_queue 
		WHERE operation_id = $1
	`, request.OperationID).Scan(&status)

	if err != nil {
		t.Fatalf("Failed to query retry status: %v", err)
	}

	if status != "completed" {
		t.Errorf("Expected status 'completed', got '%s'", status)
	}
}

func testExponentialBackoff(t *testing.T, plugin framework.Plugin, bus *TestEventBus, db *sql.DB) {
	// Track attempt times
	var attemptTimes []time.Time
	var mu sync.Mutex

	bus.SetHandler("test.backoff", func(event framework.Event) error {
		mu.Lock()
		attemptTimes = append(attemptTimes, time.Now())
		mu.Unlock()
		return fmt.Errorf("always fail")
	})

	// Schedule operation
	request := &framework.RetryRequest{
		OperationID:   "test-backoff-" + time.Now().Format("20060102150405"),
		OperationType: "test",
		TargetPlugin:  "test",
		EventType:     "test.backoff",
		Payload:       json.RawMessage(`{}`),
	}

	event := &framework.DataEvent{
		Data: &framework.EventData{
			RetryRequest: request,
		},
	}

	// Schedule retry by broadcasting the event
	if err := bus.Broadcast("retry.schedule", event.Data); err != nil {
		t.Fatalf("Failed to schedule retry: %v", err)
	}

	// Wait for multiple attempts
	time.Sleep(8 * time.Second)

	// Verify exponential delays
	mu.Lock()
	times := attemptTimes
	mu.Unlock()

	if len(times) < 3 {
		t.Fatalf("Expected at least 3 attempts, got %d", len(times))
	}

	// Check delays (should be approximately 1s, 2s, 4s)
	for i := 1; i < len(times); i++ {
		delay := times[i].Sub(times[i-1])
		expectedMin := time.Duration(1<<(i-1)) * time.Second * 8 / 10  // 80% of expected
		expectedMax := time.Duration(1<<(i-1)) * time.Second * 12 / 10 // 120% of expected

		if delay < expectedMin || delay > expectedMax {
			t.Errorf("Attempt %d: delay %v not in expected range [%v, %v]",
				i, delay, expectedMin, expectedMax)
		}
	}
}

func testPriorityOrdering(t *testing.T, plugin framework.Plugin, bus *TestEventBus, db *sql.DB) {
	// Track order of execution
	var executionOrder []string
	var mu sync.Mutex

	bus.SetHandler("test.priority", func(event framework.Event) error {
		dataEvent := event.(*framework.DataEvent)
		if dataEvent.Data != nil && dataEvent.Data.RetryRequest != nil {
			mu.Lock()
			executionOrder = append(executionOrder, dataEvent.Data.RetryRequest.OperationID)
			mu.Unlock()
		}
		return nil
	})

	// Clear retry queue
	db.Exec("TRUNCATE daz_retry_queue")

	// Schedule operations with different priorities
	priorities := []struct {
		id       string
		priority int
	}{
		{"low-priority", 1},
		{"high-priority", 10},
		{"medium-priority", 5},
		{"urgent-priority", 15},
	}

	for _, p := range priorities {
		request := &framework.RetryRequest{
			OperationID:   p.id,
			OperationType: "test",
			TargetPlugin:  "test",
			EventType:     "test.priority",
			Payload:       json.RawMessage(`{}`),
			Priority:      p.priority,
		}

		// Insert directly to ensure they're all pending at once
		_, err := db.Exec(`
			INSERT INTO daz_retry_queue 
			(operation_id, operation_type, target_plugin, event_type, payload, priority, retry_after)
			VALUES ($1, $2, $3, $4, $5, $6, NOW())
		`, request.OperationID, request.OperationType, request.TargetPlugin,
			request.EventType, request.Payload, request.Priority)

		if err != nil {
			t.Fatalf("Failed to insert retry operation: %v", err)
		}
	}

	// Wait for processing
	time.Sleep(3 * time.Second)

	// Verify execution order (should be highest priority first)
	mu.Lock()
	order := executionOrder
	mu.Unlock()

	expectedOrder := []string{"urgent-priority", "high-priority", "medium-priority", "low-priority"}
	if len(order) != len(expectedOrder) {
		t.Fatalf("Expected %d operations, got %d", len(expectedOrder), len(order))
	}

	for i, expected := range expectedOrder {
		if i < len(order) && order[i] != expected {
			t.Errorf("Position %d: expected '%s', got '%s'", i, expected, order[i])
		}
	}
}

func testConcurrentWorkers(t *testing.T, plugin framework.Plugin, bus *TestEventBus, db *sql.DB) {
	// Track concurrent executions
	var concurrent int32
	var maxConcurrent int32

	bus.SetHandler("test.concurrent", func(event framework.Event) error {
		current := atomic.AddInt32(&concurrent, 1)

		// Update max if needed
		for {
			max := atomic.LoadInt32(&maxConcurrent)
			if current <= max || atomic.CompareAndSwapInt32(&maxConcurrent, max, current) {
				break
			}
		}

		// Simulate work
		time.Sleep(500 * time.Millisecond)

		atomic.AddInt32(&concurrent, -1)
		return nil
	})

	// Clear retry queue
	db.Exec("TRUNCATE daz_retry_queue")

	// Schedule multiple operations
	for i := 0; i < 10; i++ {
		request := &framework.RetryRequest{
			OperationID:   fmt.Sprintf("concurrent-%d", i),
			OperationType: "test",
			TargetPlugin:  "test",
			EventType:     "test.concurrent",
			Payload:       json.RawMessage(`{}`),
		}

		_, err := db.Exec(`
			INSERT INTO daz_retry_queue 
			(operation_id, operation_type, target_plugin, event_type, payload, retry_after)
			VALUES ($1, $2, $3, $4, $5, NOW())
		`, request.OperationID, request.OperationType, request.TargetPlugin,
			request.EventType, request.Payload)

		if err != nil {
			t.Fatalf("Failed to insert retry operation: %v", err)
		}
	}

	// Wait for processing
	time.Sleep(3 * time.Second)

	// Verify concurrent execution (should be 2 based on worker_count)
	max := atomic.LoadInt32(&maxConcurrent)
	if max != 2 {
		t.Errorf("Expected max concurrent workers to be 2, got %d", max)
	}
}

func testMaxAttemptsLimit(t *testing.T, plugin framework.Plugin, bus *TestEventBus, db *sql.DB) {
	// Always fail handler
	var attempts int32
	bus.SetHandler("test.maxattempts", func(event framework.Event) error {
		atomic.AddInt32(&attempts, 1)
		return fmt.Errorf("always fail")
	})

	// Schedule operation with max_attempts = 3
	request := &framework.RetryRequest{
		OperationID:   "test-maxattempts-" + time.Now().Format("20060102150405"),
		OperationType: "test",
		TargetPlugin:  "test",
		EventType:     "test.maxattempts",
		Payload:       json.RawMessage(`{}`),
	}

	// Insert with specific max_attempts
	_, err := db.Exec(`
		INSERT INTO daz_retry_queue 
		(operation_id, operation_type, target_plugin, event_type, payload, max_attempts, retry_after)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
	`, request.OperationID, request.OperationType, request.TargetPlugin,
		request.EventType, request.Payload, 3)

	if err != nil {
		t.Fatalf("Failed to insert retry operation: %v", err)
	}

	// Wait for retries
	time.Sleep(10 * time.Second)

	// Verify attempts
	finalAttempts := atomic.LoadInt32(&attempts)
	if finalAttempts != 3 {
		t.Errorf("Expected exactly 3 attempts, got %d", finalAttempts)
	}

	// Verify final status
	var status string
	var attemptCount int
	err = db.QueryRow(`
		SELECT status, attempt_count FROM daz_retry_queue 
		WHERE operation_id = $1
	`, request.OperationID).Scan(&status, &attemptCount)

	if err != nil {
		t.Fatalf("Failed to query retry status: %v", err)
	}

	if status != "failed" {
		t.Errorf("Expected status 'failed', got '%s'", status)
	}

	if attemptCount != 3 {
		t.Errorf("Expected attempt_count 3, got %d", attemptCount)
	}
}

func testPersistenceAcrossRestart(t *testing.T, plugin framework.Plugin, bus *TestEventBus, db *sql.DB) {
	// Clear retry queue
	db.Exec("TRUNCATE daz_retry_queue")

	// Insert pending operations
	for i := 0; i < 5; i++ {
		_, err := db.Exec(`
			INSERT INTO daz_retry_queue 
			(operation_id, operation_type, target_plugin, event_type, payload, retry_after)
			VALUES ($1, $2, $3, $4, $5, NOW())
		`, fmt.Sprintf("persist-%d", i), "test", "test", "test.persist", "{}")

		if err != nil {
			t.Fatalf("Failed to insert retry operation: %v", err)
		}
	}

	// Track executions
	var executed int32
	bus.SetHandler("test.persist", func(event framework.Event) error {
		atomic.AddInt32(&executed, 1)
		return nil
	})

	// Create and start new plugin instance (simulating restart)
	newPlugin := NewPlugin()
	config := json.RawMessage(`{
		"enabled": true,
		"worker_count": 2,
		"batch_size": 5,
		"persistence_enabled": true
	}`)

	if err := newPlugin.Init(config, bus); err != nil {
		t.Fatalf("Failed to initialize new retry plugin: %v", err)
	}

	if err := newPlugin.Start(); err != nil {
		t.Fatalf("Failed to start new retry plugin: %v", err)
	}
	defer newPlugin.Stop()

	// Wait for processing
	time.Sleep(3 * time.Second)

	// Verify all operations were executed
	finalExecuted := atomic.LoadInt32(&executed)
	if finalExecuted != 5 {
		t.Errorf("Expected 5 operations to be executed after restart, got %d", finalExecuted)
	}
}

// TestEventBus is a test implementation of EventBus for integration testing
type TestEventBus struct {
	db       *sql.DB
	handlers map[string]func(framework.Event) error
	mu       sync.RWMutex
}

func NewTestEventBus(db *sql.DB) *TestEventBus {
	return &TestEventBus{
		db:       db,
		handlers: make(map[string]func(framework.Event) error),
	}
}

func (b *TestEventBus) SetHandler(eventType string, handler func(framework.Event) error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = handler
}

func (b *TestEventBus) Broadcast(eventType string, data *framework.EventData) error {
	b.mu.RLock()
	handler, exists := b.handlers[eventType]
	b.mu.RUnlock()

	if exists {
		event := &framework.DataEvent{
			Data: data,
		}
		return handler(event)
	}
	return nil
}

func (b *TestEventBus) BroadcastWithMetadata(eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return b.Broadcast(eventType, data)
}

func (b *TestEventBus) Send(target string, eventType string, data *framework.EventData) error {
	return b.Broadcast(eventType, data)
}

func (b *TestEventBus) SendWithMetadata(target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) error {
	return b.Send(target, eventType, data)
}

func (b *TestEventBus) Request(ctx context.Context, target string, eventType string, data *framework.EventData, metadata *framework.EventMetadata) (*framework.EventData, error) {
	// For SQL operations, return success
	if eventType == "sql.exec.request" || eventType == "sql.query.request" {
		return &framework.EventData{
			SQLExecResponse: &framework.SQLExecResponse{
				Success: true,
			},
		}, nil
	}
	return nil, nil
}

func (b *TestEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
	return nil
}

func (b *TestEventBus) SubscribeWithTags(pattern string, handler framework.EventHandler, tags []string) error {
	return nil
}

// Implement remaining EventBus methods...
func (b *TestEventBus) DeliverResponse(correlationID string, response *framework.EventData, err error) {
}
func (b *TestEventBus) SetSQLHandlers(queryHandler, execHandler framework.EventHandler) {}
func (b *TestEventBus) RegisterPlugin(name string, plugin framework.Plugin) error       { return nil }
func (b *TestEventBus) UnregisterPlugin(name string) error                              { return nil }
func (b *TestEventBus) GetDroppedEventCounts() map[string]int64                         { return nil }
func (b *TestEventBus) GetDroppedEventCount(eventType string) int64                     { return 0 }

func setupTestDatabase(t *testing.T) *sql.DB {
	// Get database configuration from environment
	host := os.Getenv("DAZ_TEST_DB_HOST")
	if host == "" {
		host = "localhost"
	}

	port := os.Getenv("DAZ_TEST_DB_PORT")
	if port == "" {
		port = "5432"
	}

	user := os.Getenv("DAZ_TEST_DB_USER")
	if user == "" {
		user = "***REMOVED***"
	}

	password := os.Getenv("DAZ_TEST_DB_PASSWORD")
	if password == "" {
		t.Skip("DAZ_TEST_DB_PASSWORD not set, skipping integration test")
	}

	dbname := os.Getenv("DAZ_TEST_DB_NAME")
	if dbname == "" {
		dbname = "daz_test_db"
	}

	// Connect to database
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	db, err := sql.Open("pgx", connStr)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	// Create retry queue table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS daz_retry_queue (
			id SERIAL PRIMARY KEY,
			operation_id VARCHAR(255) UNIQUE NOT NULL,
			operation_type VARCHAR(100) NOT NULL,
			target_plugin VARCHAR(100) NOT NULL,
			event_type VARCHAR(255) NOT NULL,
			payload JSONB NOT NULL,
			metadata JSONB,
			attempt_count INT DEFAULT 0,
			max_attempts INT DEFAULT 5,
			status VARCHAR(50) DEFAULT 'pending',
			priority INT DEFAULT 5,
			created_at TIMESTAMP DEFAULT NOW(),
			updated_at TIMESTAMP DEFAULT NOW(),
			retry_after TIMESTAMP DEFAULT NOW(),
			last_error TEXT,
			correlation_id VARCHAR(255),
			CONSTRAINT unique_operation_id UNIQUE (operation_id)
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create retry queue table: %v", err)
	}

	return db
}
