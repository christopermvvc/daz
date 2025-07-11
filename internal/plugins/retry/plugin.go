package retry

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hildolfr/daz/internal/framework"
	"github.com/hildolfr/daz/internal/metrics"
	"github.com/prometheus/client_golang/prometheus"
)

// Plugin implements the retry manager for failed operations
type Plugin struct {
	name      string
	eventBus  framework.EventBus
	sqlClient *framework.SQLClient
	config    *Config

	// Lifecycle management
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Processing state
	processingMu sync.Mutex
	processing   map[string]bool // correlation_id -> processing
}

// Config holds the retry plugin configuration
type Config struct {
	Enabled             bool              `json:"enabled"`
	PollIntervalSeconds int               `json:"poll_interval_seconds"`
	BatchSize           int               `json:"batch_size"`
	WorkerCount         int               `json:"worker_count"`
	DefaultMaxRetries   int               `json:"default_max_retries"`
	DefaultTimeout      int               `json:"default_timeout_seconds"`
	RetryPolicies       map[string]Policy `json:"retry_policies"`
}

// Policy defines retry behavior for specific operation types
type Policy struct {
	MaxRetries          int     `json:"max_retries"`
	InitialDelaySeconds int     `json:"initial_delay_seconds"`
	MaxDelaySeconds     int     `json:"max_delay_seconds"`
	BackoffMultiplier   float64 `json:"backoff_multiplier"`
	TimeoutSeconds      int     `json:"timeout_seconds"`
}

// NewPlugin creates a new retry manager plugin
func NewPlugin() *Plugin {
	return &Plugin{
		name:       "retry",
		processing: make(map[string]bool),
	}
}

// Init initializes the plugin with configuration
func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
	p.eventBus = bus
	p.sqlClient = framework.NewSQLClient(bus, p.name)

	// Parse configuration
	p.config = &Config{
		Enabled:             true,
		PollIntervalSeconds: 10,
		BatchSize:           10,
		WorkerCount:         3,
		DefaultMaxRetries:   3,
		DefaultTimeout:      30,
		RetryPolicies:       make(map[string]Policy),
	}

	if len(config) > 0 {
		if err := json.Unmarshal(config, p.config); err != nil {
			return fmt.Errorf("failed to parse retry config: %w", err)
		}
	}

	// Set default policies if not configured
	if len(p.config.RetryPolicies) == 0 {
		p.config.RetryPolicies = map[string]Policy{
			"cytube_command": {
				MaxRetries:          5,
				InitialDelaySeconds: 5,
				MaxDelaySeconds:     300,
				BackoffMultiplier:   2.0,
				TimeoutSeconds:      30,
			},
			"sql_operation": {
				MaxRetries:          3,
				InitialDelaySeconds: 2,
				MaxDelaySeconds:     60,
				BackoffMultiplier:   1.5,
				TimeoutSeconds:      15,
			},
			"plugin_request": {
				MaxRetries:          3,
				InitialDelaySeconds: 1,
				MaxDelaySeconds:     30,
				BackoffMultiplier:   2.0,
				TimeoutSeconds:      20,
			},
		}
	}

	log.Printf("[Retry] Initialized with %d retry policies", len(p.config.RetryPolicies))
	return nil
}

// Start starts the retry manager
func (p *Plugin) Start() error {
	if !p.config.Enabled {
		log.Printf("[Retry] Plugin disabled by configuration")
		return nil
	}

	p.ctx, p.cancel = context.WithCancel(context.Background())

	// Subscribe to retry schedule requests
	if err := p.eventBus.Subscribe("retry.schedule", p.handleScheduleRequest); err != nil {
		return fmt.Errorf("failed to subscribe to retry.schedule: %w", err)
	}

	// Subscribe to failure events
	patterns := []string{
		"*.failed",
		"*.error",
		"*.timeout",
	}

	for _, pattern := range patterns {
		if err := p.eventBus.Subscribe(pattern, p.handleFailureEvent); err != nil {
			return fmt.Errorf("failed to subscribe to %s: %w", pattern, err)
		}
	}

	// Start retry workers
	for i := 0; i < p.config.WorkerCount; i++ {
		p.wg.Add(1)
		go p.retryWorker(i)
	}

	// Start queue processor
	p.wg.Add(1)
	go p.queueProcessor()

	log.Printf("[Retry] Started with %d workers", p.config.WorkerCount)
	return nil
}

// Stop stops the retry manager
func (p *Plugin) Stop() error {
	if p.cancel != nil {
		p.cancel()
	}

	// Wait for workers to finish
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Printf("[Retry] All workers stopped")
	case <-time.After(30 * time.Second):
		log.Printf("[Retry] Warning: workers did not stop within 30 seconds")
	}

	return nil
}

// HandleEvent handles incoming events (not used with specific subscriptions)
func (p *Plugin) HandleEvent(event framework.Event) error {
	return nil
}

// Status returns the plugin status
func (p *Plugin) Status() framework.PluginStatus {
	return framework.PluginStatus{
		Name:  p.name,
		State: "running",
	}
}

// Name returns the plugin name
func (p *Plugin) Name() string {
	return p.name
}

// Dependencies returns plugin dependencies
func (p *Plugin) Dependencies() []string {
	return []string{"sql"}
}

// handleScheduleRequest handles explicit retry schedule requests
func (p *Plugin) handleScheduleRequest(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil {
		return nil
	}

	// Extract retry request from event data
	if dataEvent.Data.RetryRequest != nil {
		// Create RetryRequest from framework.RetryRequest
		request := &RetryRequest{
			OperationID:   dataEvent.Data.RetryRequest.OperationID,
			OperationType: dataEvent.Data.RetryRequest.OperationType,
			TargetPlugin:  dataEvent.Data.RetryRequest.TargetPlugin,
			EventType:     dataEvent.Data.RetryRequest.EventType,
			Payload:       dataEvent.Data.RetryRequest.Payload,
			Metadata:      dataEvent.Data.RetryRequest.Metadata,
			Priority:      dataEvent.Data.RetryRequest.Priority,
		}
		return p.scheduleRetry(request)
	}

	return nil
}

// handleFailureEvent handles generic failure events
func (p *Plugin) handleFailureEvent(event framework.Event) error {
	dataEvent, ok := event.(*framework.DataEvent)
	if !ok || dataEvent.Data == nil {
		return nil
	}

	// Check if this failure is retryable
	if !p.isRetryable(dataEvent) {
		return nil
	}

	// Generate a unique operation ID
	operationID := fmt.Sprintf("retry-%d", time.Now().UnixNano())

	// Marshal event data to JSON
	eventDataJSON, err := json.Marshal(dataEvent.Data)
	if err != nil {
		log.Printf("[Retry] Failed to marshal event data: %v", err)
		return nil
	}

	// Extract source from event data
	source := "unknown"
	if dataEvent.Data.KeyValue != nil {
		if s, ok := dataEvent.Data.KeyValue["source"]; ok {
			source = s
		}
	}

	// Create retry request from failure event
	request := &RetryRequest{
		OperationID:   operationID,
		OperationType: p.determineOperationType(dataEvent),
		TargetPlugin:  source,
		EventType:     dataEvent.Type(),
		Payload:       eventDataJSON,
		Priority:      0, // Normal priority
	}

	return p.scheduleRetry(request)
}

// scheduleRetry adds an operation to the retry queue
func (p *Plugin) scheduleRetry(request *RetryRequest) error {
	// Get retry policy
	policy := p.getPolicy(request.OperationType)

	// Calculate initial retry time
	retryAfter := time.Now().Add(time.Duration(policy.InitialDelaySeconds) * time.Second)

	// Store in retry queue
	query := `
		INSERT INTO daz_retry_queue 
		(correlation_id, plugin_name, operation_type, event_type, event_data, 
		 max_retries, retry_after, priority, timeout_seconds)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (correlation_id) DO UPDATE
		SET retry_count = daz_retry_queue.retry_count + 1,
		    retry_after = $7,
		    updated_at = NOW()
		WHERE daz_retry_queue.status != 'completed'
	`

	err := p.sqlClient.Exec(query,
		request.OperationID,
		request.TargetPlugin,
		request.OperationType,
		request.EventType,
		request.Payload,
		policy.MaxRetries,
		retryAfter,
		request.Priority,
		policy.TimeoutSeconds,
	)

	if err != nil {
		metrics.DatabaseErrors.Inc()
		return fmt.Errorf("failed to schedule retry: %w", err)
	}

	log.Printf("[Retry] Scheduled retry for %s (type: %s) after %v",
		request.OperationID, request.OperationType, retryAfter.Sub(time.Now()))

	return nil
}

// queueProcessor polls the retry queue and dispatches work
func (p *Plugin) queueProcessor() {
	defer p.wg.Done()

	ticker := time.NewTicker(time.Duration(p.config.PollIntervalSeconds) * time.Second)
	defer ticker.Stop()

	// Initial poll
	p.pollQueue()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.pollQueue()
		}
	}
}

// pollQueue fetches and processes pending retries
func (p *Plugin) pollQueue() {
	ctx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
	defer cancel()

	// Use the acquire_retry_batch function
	query := `SELECT * FROM acquire_retry_batch($1)`

	rows, err := p.sqlClient.QueryContext(ctx, query, p.config.BatchSize)
	if err != nil {
		log.Printf("[Retry] Failed to poll queue: %v", err)
		metrics.DatabaseErrors.Inc()
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var item RetryQueueItem
		var eventDataJSON []byte

		err := rows.Scan(
			&item.ID,
			&item.CorrelationID,
			&item.PluginName,
			&item.OperationType,
			&item.EventType,
			&eventDataJSON,
			&item.MaxRetries,
			&item.RetryCount,
			&item.RetryAfter,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.LastError,
			&item.Status,
			&item.Priority,
			&item.TimeoutSeconds,
		)

		if err != nil {
			log.Printf("[Retry] Failed to scan retry item: %v", err)
			continue
		}

		// Unmarshal event data
		if err := json.Unmarshal(eventDataJSON, &item.EventData); err != nil {
			log.Printf("[Retry] Failed to unmarshal event data: %v", err)
			p.markFailed(item.ID, fmt.Sprintf("invalid event data: %v", err))
			continue
		}

		// Check if already processing
		p.processingMu.Lock()
		if p.processing[item.CorrelationID] {
			p.processingMu.Unlock()
			continue
		}
		p.processing[item.CorrelationID] = true
		p.processingMu.Unlock()

		// Send to retry channel
		select {
		case retryQueue <- item:
			count++
		case <-p.ctx.Done():
			return
		}
	}

	if count > 0 {
		log.Printf("[Retry] Dispatched %d items for retry", count)
	}
}

// retryWorker processes retry items
func (p *Plugin) retryWorker(id int) {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case item := <-retryQueue:
			p.processRetry(item)
		}
	}
}

// processRetry executes a single retry operation
func (p *Plugin) processRetry(item RetryQueueItem) {
	defer func() {
		p.processingMu.Lock()
		delete(p.processing, item.CorrelationID)
		p.processingMu.Unlock()
	}()

	log.Printf("[Retry] Processing retry %s (attempt %d/%d)",
		item.CorrelationID, item.RetryCount+1, item.MaxRetries)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(p.ctx, time.Duration(item.TimeoutSeconds)*time.Second)
	defer cancel()

	// Create metadata for the retry request
	metadata := &framework.EventMetadata{
		Priority:      item.Priority,
		CorrelationID: item.CorrelationID,
		Source:        p.name,
		Target:        item.PluginName,
	}

	// Execute the retry
	start := time.Now()
	response, err := p.eventBus.Request(ctx, item.PluginName, item.EventType, item.EventData, metadata)
	duration := time.Since(start)

	if err != nil {
		p.handleRetryFailure(item, err, duration)
	} else {
		p.handleRetrySuccess(item, response, duration)
	}
}

// handleRetrySuccess processes successful retry
func (p *Plugin) handleRetrySuccess(item RetryQueueItem, response *framework.EventData, duration time.Duration) {
	// Mark as completed
	if err := p.markCompleted(item.ID); err != nil {
		log.Printf("[Retry] Failed to mark completed: %v", err)
	}

	// Update metrics
	retrySuccessTotal.WithLabelValues(item.OperationType).Inc()
	retryDuration.WithLabelValues(item.OperationType, "success").Observe(duration.Seconds())

	// Emit success event
	statusData := &framework.RetryStatus{
		OperationID:   item.CorrelationID,
		OperationType: item.OperationType,
		RetryCount:    item.RetryCount + 1,
		MaxRetries:    item.MaxRetries,
		Status:        "completed",
		UpdatedAt:     time.Now(),
	}

	p.eventBus.Broadcast("retry.success", &framework.EventData{
		RetryStatus: statusData,
	})

	log.Printf("[Retry] Successfully completed retry for %s after %d attempts",
		item.CorrelationID, item.RetryCount+1)
}

// handleRetryFailure processes failed retry
func (p *Plugin) handleRetryFailure(item RetryQueueItem, err error, duration time.Duration) {
	// Update metrics
	retryFailureTotal.WithLabelValues(item.OperationType).Inc()
	retryDuration.WithLabelValues(item.OperationType, "failure").Observe(duration.Seconds())

	// Check if we've exceeded max retries
	if item.RetryCount+1 >= item.MaxRetries {
		// Mark as permanently failed
		if err := p.markFailed(item.ID, err.Error()); err != nil {
			log.Printf("[Retry] Failed to mark as failed: %v", err)
		}

		// Emit final failure event
		statusData := &framework.RetryStatus{
			OperationID:   item.CorrelationID,
			OperationType: item.OperationType,
			RetryCount:    item.RetryCount + 1,
			MaxRetries:    item.MaxRetries,
			Status:        "failed",
			LastError:     err.Error(),
			UpdatedAt:     time.Now(),
		}

		p.eventBus.Broadcast("retry.failed", &framework.EventData{
			RetryStatus: statusData,
		})

		log.Printf("[Retry] Permanently failed %s after %d attempts: %v",
			item.CorrelationID, item.RetryCount+1, err)
	} else {
		// Calculate next retry time
		policy := p.getPolicy(item.OperationType)
		nextRetry := p.calculateNextRetry(item.RetryCount+1, policy)

		// Update for next retry
		if err := p.updateRetryTime(item.ID, nextRetry, err.Error()); err != nil {
			log.Printf("[Retry] Failed to update retry time: %v", err)
		}

		log.Printf("[Retry] Retry %s failed (attempt %d/%d), next retry at %v: %v",
			item.CorrelationID, item.RetryCount+1, item.MaxRetries, nextRetry, err)
	}
}

// Helper methods

func (p *Plugin) isRetryable(event *framework.DataEvent) bool {
	// Check if event has retry metadata indicating it should not be retried in KeyValue
	if event.Data != nil && event.Data.KeyValue != nil {
		if val, ok := event.Data.KeyValue["no-retry"]; ok && val == "true" {
			return false
		}

		// Check error type
		if errorMsg, ok := event.Data.KeyValue["error"]; ok {
			// Don't retry validation errors, auth failures, etc.
			nonRetryableErrors := []string{
				"validation", "unauthorized", "forbidden", "not found",
			}
			for _, errType := range nonRetryableErrors {
				if containsIgnoreCase(errorMsg, errType) {
					return false
				}
			}
		}
	}

	return true
}

func (p *Plugin) determineOperationType(event *framework.DataEvent) string {
	// Determine operation type from event
	eventType := event.Type()

	switch {
	case containsString(eventType, "cytube"):
		return "cytube_command"
	case containsString(eventType, "sql"):
		return "sql_operation"
	case containsString(eventType, "plugin"):
		return "plugin_request"
	default:
		return "unknown"
	}
}

func (p *Plugin) getPolicy(operationType string) Policy {
	if policy, ok := p.config.RetryPolicies[operationType]; ok {
		return policy
	}

	// Return default policy
	return Policy{
		MaxRetries:          p.config.DefaultMaxRetries,
		InitialDelaySeconds: 5,
		MaxDelaySeconds:     300,
		BackoffMultiplier:   2.0,
		TimeoutSeconds:      p.config.DefaultTimeout,
	}
}

func (p *Plugin) calculateNextRetry(attempt int, policy Policy) time.Time {
	// Calculate exponential backoff
	delaySeconds := float64(policy.InitialDelaySeconds)
	for i := 1; i < attempt; i++ {
		delaySeconds *= policy.BackoffMultiplier
	}

	// Cap at max delay
	if delaySeconds > float64(policy.MaxDelaySeconds) {
		delaySeconds = float64(policy.MaxDelaySeconds)
	}

	delay := time.Duration(delaySeconds) * time.Second
	return time.Now().Add(delay)
}

// Database operations

func (p *Plugin) markCompleted(id int64) error {
	query := `SELECT complete_retry($1)`
	return p.sqlClient.Exec(query, id)
}

func (p *Plugin) markFailed(id int64, errorMsg string) error {
	query := `SELECT fail_retry($1, $2)`
	return p.sqlClient.Exec(query, id, errorMsg)
}

func (p *Plugin) updateRetryTime(id int64, retryAfter time.Time, errorMsg string) error {
	query := `
		UPDATE daz_retry_queue 
		SET retry_count = retry_count + 1,
		    retry_after = $2,
		    last_error = $3,
		    status = 'pending',
		    updated_at = NOW()
		WHERE id = $1
	`
	return p.sqlClient.Exec(query, id, retryAfter, errorMsg)
}

// Types

// RetryRequest represents a request to schedule a retry
type RetryRequest struct {
	OperationID    string            `json:"operation_id"`
	OperationType  string            `json:"operation_type"`
	TargetPlugin   string            `json:"target_plugin"`
	EventType      string            `json:"event_type"`
	Payload        json.RawMessage   `json:"payload"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	Priority       int               `json:"priority,omitempty"`
	MaxRetries     int               `json:"max_retries,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
}

// RetryQueueItem represents an item from the retry queue
type RetryQueueItem struct {
	ID             int64                `json:"id"`
	CorrelationID  string               `json:"correlation_id"`
	PluginName     string               `json:"plugin_name"`
	OperationType  string               `json:"operation_type"`
	EventType      string               `json:"event_type"`
	EventData      *framework.EventData `json:"event_data"`
	MaxRetries     int                  `json:"max_retries"`
	RetryCount     int                  `json:"retry_count"`
	RetryAfter     time.Time            `json:"retry_after"`
	CreatedAt      time.Time            `json:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at"`
	LastError      *string              `json:"last_error"`
	Status         string               `json:"status"`
	Priority       int                  `json:"priority"`
	TimeoutSeconds int                  `json:"timeout_seconds"`
}

// Shared retry queue channel
var retryQueue = make(chan RetryQueueItem, 100)

// Metrics
var (
	retrySuccessTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "daz_retry_success_total",
			Help: "Total successful retries",
		},
		[]string{"operation_type"},
	)
	retryFailureTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "daz_retry_failure_total",
			Help: "Total failed retries",
		},
		[]string{"operation_type"},
	)
	retryDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name: "daz_retry_duration_seconds",
			Help: "Retry operation duration",
		},
		[]string{"operation_type", "status"},
	)
)

func init() {
	// Register metrics
	prometheus.MustRegister(retrySuccessTotal)
	prometheus.MustRegister(retryFailureTotal)
	prometheus.MustRegister(retryDuration)
}

// Helper functions
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && s[0:len(substr)] == substr
}

func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s[0:len(substr)] == substr ||
			strings.ToLower(s[0:len(substr)]) == strings.ToLower(substr))
}
