package framework

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// RequestConfig defines configuration for request operations
type RequestConfig struct {
	Timeout     time.Duration
	MaxRetries  int
	RetryDelay  time.Duration
	BackoffRate float64
	MetricName  string
}

// DefaultRequestConfigs provides common request configurations
var DefaultRequestConfigs = map[string]RequestConfig{
	"fast": {
		Timeout:     5 * time.Second,
		MaxRetries:  1,
		RetryDelay:  100 * time.Millisecond,
		BackoffRate: 1.0,
		MetricName:  "fast_request",
	},
	"normal": {
		Timeout:     15 * time.Second,
		MaxRetries:  3,
		RetryDelay:  500 * time.Millisecond,
		BackoffRate: 1.5,
		MetricName:  "normal_request",
	},
	"slow": {
		Timeout:     30 * time.Second,
		MaxRetries:  2,
		RetryDelay:  1 * time.Second,
		BackoffRate: 2.0,
		MetricName:  "slow_request",
	},
	"critical": {
		Timeout:     45 * time.Second,
		MaxRetries:  5,
		RetryDelay:  1 * time.Second,
		BackoffRate: 2.0,
		MetricName:  "critical_request",
	},
}

// RequestMetrics holds metrics for request operations
type RequestMetrics struct {
	RequestsTotal     *prometheus.CounterVec
	RequestDuration   *prometheus.HistogramVec
	RequestsRetried   *prometheus.CounterVec
	RequestsSucceeded *prometheus.CounterVec
	RequestsFailed    *prometheus.CounterVec
}

// newRequestMetrics creates metrics for request operations
func newRequestMetrics() *RequestMetrics {
	return &RequestMetrics{
		RequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "daz_requests_total",
				Help: "Total number of requests made",
			},
			[]string{"target", "event_type", "config"},
		),
		RequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "daz_request_duration_seconds",
				Help:    "Duration of requests in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"target", "event_type", "config"},
		),
		RequestsRetried: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "daz_requests_retried_total",
				Help: "Total number of requests that were retried",
			},
			[]string{"target", "event_type", "config"},
		),
		RequestsSucceeded: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "daz_requests_succeeded_total",
				Help: "Total number of successful requests",
			},
			[]string{"target", "event_type", "config"},
		),
		RequestsFailed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "daz_requests_failed_total",
				Help: "Total number of failed requests",
			},
			[]string{"target", "event_type", "config"},
		),
	}
}

// requestMetrics is the global metrics instance
var requestMetrics = newRequestMetrics()

// init registers all request metrics
func init() {
	prometheus.MustRegister(
		requestMetrics.RequestsTotal,
		requestMetrics.RequestDuration,
		requestMetrics.RequestsRetried,
		requestMetrics.RequestsSucceeded,
		requestMetrics.RequestsFailed,
	)
}

// RequestHelper provides enhanced request capabilities with timeout, retry, and metrics
type RequestHelper struct {
	eventBus EventBus
	source   string
}

// NewRequestHelper creates a new request helper
func NewRequestHelper(eventBus EventBus, source string) *RequestHelper {
	return &RequestHelper{
		eventBus: eventBus,
		source:   source,
	}
}

// RequestWithConfig makes a request with the specified configuration
func (h *RequestHelper) RequestWithConfig(
	ctx context.Context,
	target string,
	eventType string,
	data *EventData,
	configName string,
) (*EventData, error) {
	config, exists := DefaultRequestConfigs[configName]
	if !exists {
		return nil, fmt.Errorf("unknown request config: %s", configName)
	}

	return h.requestWithRetry(ctx, target, eventType, data, config)
}

// FastRequest makes a fast request (5s timeout, 1 retry)
func (h *RequestHelper) FastRequest(
	ctx context.Context,
	target string,
	eventType string,
	data *EventData,
) (*EventData, error) {
	return h.RequestWithConfig(ctx, target, eventType, data, "fast")
}

// NormalRequest makes a normal request (15s timeout, 3 retries)
func (h *RequestHelper) NormalRequest(
	ctx context.Context,
	target string,
	eventType string,
	data *EventData,
) (*EventData, error) {
	return h.RequestWithConfig(ctx, target, eventType, data, "normal")
}

// SlowRequest makes a slow request (30s timeout, 2 retries)
func (h *RequestHelper) SlowRequest(
	ctx context.Context,
	target string,
	eventType string,
	data *EventData,
) (*EventData, error) {
	return h.RequestWithConfig(ctx, target, eventType, data, "slow")
}

// CriticalRequest makes a critical request (45s timeout, 5 retries)
func (h *RequestHelper) CriticalRequest(
	ctx context.Context,
	target string,
	eventType string,
	data *EventData,
) (*EventData, error) {
	return h.RequestWithConfig(ctx, target, eventType, data, "critical")
}

// requestWithRetry implements the core request logic with retry and metrics
func (h *RequestHelper) requestWithRetry(
	ctx context.Context,
	target string,
	eventType string,
	data *EventData,
	config RequestConfig,
) (*EventData, error) {
	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, config.Timeout)
	defer cancel()

	// Start metrics timer
	timer := prometheus.NewTimer(requestMetrics.RequestDuration.WithLabelValues(target, eventType, config.MetricName))
	defer timer.ObserveDuration()

	// Track total requests
	requestMetrics.RequestsTotal.WithLabelValues(target, eventType, config.MetricName).Inc()

	// Generate correlation ID
	correlationID := fmt.Sprintf("%s-%d", h.source, time.Now().UnixNano())

	// Create metadata
	metadata := &EventMetadata{
		CorrelationID: correlationID,
		Timestamp:     time.Now(),
		Source:        h.source,
		Target:        target,
		Timeout:       config.Timeout,
	}

	var lastErr error
	retryDelay := config.RetryDelay

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		// If this is a retry, wait and update metadata
		if attempt > 0 {
			select {
			case <-timeoutCtx.Done():
				requestMetrics.RequestsFailed.WithLabelValues(target, eventType, config.MetricName).Inc()
				return nil, fmt.Errorf("request timeout after %d attempts: %w", attempt, timeoutCtx.Err())
			case <-time.After(retryDelay):
				// Continue with retry
			}

			// Track retry
			requestMetrics.RequestsRetried.WithLabelValues(target, eventType, config.MetricName).Inc()
			log.Printf("[RequestHelper] Retrying request to %s (attempt %d/%d)", target, attempt+1, config.MaxRetries+1)

			// Apply backoff
			retryDelay = time.Duration(float64(retryDelay) * config.BackoffRate)
		}

		// Make the request
		response, err := h.eventBus.Request(timeoutCtx, target, eventType, data, metadata)
		if err != nil {
			lastErr = err

			// Check if error is retryable
			if !h.isRetryableError(err) {
				log.Printf("[RequestHelper] Non-retryable error for %s: %v", target, err)
				requestMetrics.RequestsFailed.WithLabelValues(target, eventType, config.MetricName).Inc()
				return nil, fmt.Errorf("request failed: %w", err)
			}

			log.Printf("[RequestHelper] Retryable error for %s: %v", target, err)
			continue
		}

		// Success
		requestMetrics.RequestsSucceeded.WithLabelValues(target, eventType, config.MetricName).Inc()
		if attempt > 0 {
			log.Printf("[RequestHelper] Request to %s succeeded after %d retries", target, attempt)
		}
		return response, nil
	}

	// All attempts failed
	requestMetrics.RequestsFailed.WithLabelValues(target, eventType, config.MetricName).Inc()
	return nil, fmt.Errorf("request failed after %d attempts: %w", config.MaxRetries+1, lastErr)
}

// isRetryableError determines if an error is retryable
func (h *RequestHelper) isRetryableError(err error) bool {
	// Check for context timeout/cancellation (retryable)
	if err == context.DeadlineExceeded || err == context.Canceled {
		return true
	}

	// Check for specific error patterns that indicate temporary issues
	errStr := err.Error()

	// Network/connection errors (retryable)
	retryablePatterns := []string{
		"connection refused",
		"connection reset",
		"connection timed out",
		"request timeout",
		"no response received",
		"temporary failure",
		"service unavailable",
		"database not connected",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(strings.ToLower(errStr), pattern) {
			return true
		}
	}

	// Database errors that might be temporary
	lowerErr := strings.ToLower(errStr)
	if strings.Contains(lowerErr, "database") && (strings.Contains(lowerErr, "timeout") ||
		strings.Contains(lowerErr, "connection") ||
		strings.Contains(lowerErr, "deadlock") ||
		strings.Contains(lowerErr, "lock")) {
		return true
	}

	return false
}

// SQLRequestHelper provides specialized helpers for SQL operations
type SQLRequestHelper struct {
	*RequestHelper
}

// NewSQLRequestHelper creates a new SQL request helper
func NewSQLRequestHelper(eventBus EventBus, source string) *SQLRequestHelper {
	return &SQLRequestHelper{
		RequestHelper: NewRequestHelper(eventBus, source),
	}
}

// QueryWithTimeout executes a SQL query with the specified timeout
func (h *SQLRequestHelper) QueryWithTimeout(
	ctx context.Context,
	query string,
	timeout time.Duration,
	args ...interface{},
) (*QueryRows, error) {
	// Convert args to SQLParam
	params := make([]SQLParam, len(args))
	for i, arg := range args {
		params[i] = NewSQLParam(arg)
	}

	// Create request data
	request := &EventData{
		SQLQueryRequest: &SQLQueryRequest{
			ID:        fmt.Sprintf("%s-%d", h.source, time.Now().UnixNano()),
			Query:     query,
			Params:    params,
			Timeout:   timeout,
			RequestBy: h.source,
		},
	}

	// Use appropriate config based on timeout
	var configName string
	switch {
	case timeout <= 5*time.Second:
		configName = "fast"
	case timeout <= 15*time.Second:
		configName = "normal"
	case timeout <= 30*time.Second:
		configName = "slow"
	default:
		configName = "critical"
	}

	// Make the request
	response, err := h.RequestWithConfig(ctx, "sql", "sql.query.request", request, configName)
	if err != nil {
		return nil, fmt.Errorf("query request failed: %w", err)
	}

	// Process response
	if response != nil && response.SQLQueryResponse != nil {
		if !response.SQLQueryResponse.Success {
			return nil, fmt.Errorf("query failed: %s", response.SQLQueryResponse.Error)
		}
		return &QueryRows{
			columns: response.SQLQueryResponse.Columns,
			rows:    response.SQLQueryResponse.Rows,
			current: -1,
		}, nil
	}

	return nil, fmt.Errorf("no response received")
}

// ExecWithTimeout executes a SQL statement with the specified timeout
func (h *SQLRequestHelper) ExecWithTimeout(
	ctx context.Context,
	query string,
	timeout time.Duration,
	args ...interface{},
) (int64, error) {
	// Convert args to SQLParam
	params := make([]SQLParam, len(args))
	for i, arg := range args {
		params[i] = NewSQLParam(arg)
	}

	// Create request data
	request := &EventData{
		SQLExecRequest: &SQLExecRequest{
			ID:        fmt.Sprintf("%s-%d", h.source, time.Now().UnixNano()),
			Query:     query,
			Params:    params,
			Timeout:   timeout,
			RequestBy: h.source,
		},
	}

	// Use appropriate config based on timeout
	var configName string
	switch {
	case timeout <= 5*time.Second:
		configName = "fast"
	case timeout <= 15*time.Second:
		configName = "normal"
	case timeout <= 30*time.Second:
		configName = "slow"
	default:
		configName = "critical"
	}

	// Make the request
	response, err := h.RequestWithConfig(ctx, "sql", "sql.exec.request", request, configName)
	if err != nil {
		return 0, fmt.Errorf("exec request failed: %w", err)
	}

	// Process response
	if response != nil && response.SQLExecResponse != nil {
		if !response.SQLExecResponse.Success {
			return 0, fmt.Errorf("exec failed: %s", response.SQLExecResponse.Error)
		}
		return response.SQLExecResponse.RowsAffected, nil
	}

	return 0, fmt.Errorf("no response received")
}

// FastQuery executes a fast SQL query (5s timeout, 1 retry)
func (h *SQLRequestHelper) FastQuery(
	ctx context.Context,
	query string,
	args ...interface{},
) (*QueryRows, error) {
	return h.QueryWithTimeout(ctx, query, 5*time.Second, args...)
}

// NormalQuery executes a normal SQL query (15s timeout, 3 retries)
func (h *SQLRequestHelper) NormalQuery(
	ctx context.Context,
	query string,
	args ...interface{},
) (*QueryRows, error) {
	return h.QueryWithTimeout(ctx, query, 15*time.Second, args...)
}

// SlowQuery executes a slow SQL query (30s timeout, 2 retries)
func (h *SQLRequestHelper) SlowQuery(
	ctx context.Context,
	query string,
	args ...interface{},
) (*QueryRows, error) {
	return h.QueryWithTimeout(ctx, query, 30*time.Second, args...)
}

// CriticalQuery executes a critical SQL query (45s timeout, 5 retries)
func (h *SQLRequestHelper) CriticalQuery(
	ctx context.Context,
	query string,
	args ...interface{},
) (*QueryRows, error) {
	return h.QueryWithTimeout(ctx, query, 45*time.Second, args...)
}

// FastExec executes a fast SQL statement (5s timeout, 1 retry)
func (h *SQLRequestHelper) FastExec(
	ctx context.Context,
	query string,
	args ...interface{},
) (int64, error) {
	return h.ExecWithTimeout(ctx, query, 5*time.Second, args...)
}

// NormalExec executes a normal SQL statement (15s timeout, 3 retries)
func (h *SQLRequestHelper) NormalExec(
	ctx context.Context,
	query string,
	args ...interface{},
) (int64, error) {
	return h.ExecWithTimeout(ctx, query, 15*time.Second, args...)
}

// SlowExec executes a slow SQL statement (30s timeout, 2 retries)
func (h *SQLRequestHelper) SlowExec(
	ctx context.Context,
	query string,
	args ...interface{},
) (int64, error) {
	return h.ExecWithTimeout(ctx, query, 30*time.Second, args...)
}

// CriticalExec executes a critical SQL statement (45s timeout, 5 retries)
func (h *SQLRequestHelper) CriticalExec(
	ctx context.Context,
	query string,
	args ...interface{},
) (int64, error) {
	return h.ExecWithTimeout(ctx, query, 45*time.Second, args...)
}

// BatchWithTimeout executes a batch of SQL operations with the specified timeout
func (h *SQLRequestHelper) BatchWithTimeout(
	ctx context.Context,
	operations []BatchOperation,
	timeout time.Duration,
	atomic bool,
) (*SQLBatchResponse, error) {
	// Create request data
	request := &EventData{
		SQLBatchRequest: &SQLBatchRequest{
			ID:            fmt.Sprintf("%s-%d", h.source, time.Now().UnixNano()),
			CorrelationID: fmt.Sprintf("%s-batch-%d", h.source, time.Now().UnixNano()),
			Operations:    operations,
			Atomic:        atomic,
			Timeout:       timeout,
			RequestBy:     h.source,
		},
	}

	// Use appropriate config based on timeout
	var configName string
	switch {
	case timeout <= 5*time.Second:
		configName = "fast"
	case timeout <= 15*time.Second:
		configName = "normal"
	case timeout <= 30*time.Second:
		configName = "slow"
	default:
		configName = "critical"
	}

	// Make the request
	response, err := h.RequestWithConfig(ctx, "sql", "sql.batch.request", request, configName)
	if err != nil {
		return nil, fmt.Errorf("batch request failed: %w", err)
	}

	// Process response
	if response != nil && response.SQLBatchResponse != nil {
		return response.SQLBatchResponse, nil
	}

	return nil, fmt.Errorf("no response received")
}

// FastBatch executes a fast batch of SQL operations (5s timeout, 1 retry)
func (h *SQLRequestHelper) FastBatch(
	ctx context.Context,
	operations []BatchOperation,
	atomic bool,
) (*SQLBatchResponse, error) {
	return h.BatchWithTimeout(ctx, operations, 5*time.Second, atomic)
}

// NormalBatch executes a normal batch of SQL operations (15s timeout, 3 retries)
func (h *SQLRequestHelper) NormalBatch(
	ctx context.Context,
	operations []BatchOperation,
	atomic bool,
) (*SQLBatchResponse, error) {
	return h.BatchWithTimeout(ctx, operations, 15*time.Second, atomic)
}

// SlowBatch executes a slow batch of SQL operations (30s timeout, 2 retries)
func (h *SQLRequestHelper) SlowBatch(
	ctx context.Context,
	operations []BatchOperation,
	atomic bool,
) (*SQLBatchResponse, error) {
	return h.BatchWithTimeout(ctx, operations, 30*time.Second, atomic)
}

// CriticalBatch executes a critical batch of SQL operations (45s timeout, 5 retries)
func (h *SQLRequestHelper) CriticalBatch(
	ctx context.Context,
	operations []BatchOperation,
	atomic bool,
) (*SQLBatchResponse, error) {
	return h.BatchWithTimeout(ctx, operations, 45*time.Second, atomic)
}
