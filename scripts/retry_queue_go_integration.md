# Retry Queue Go Integration Guide

This guide shows how to integrate the retry queue with the Daz application from Go code.

## Data Structures

```go
// RetryQueueItem represents an item in the retry queue
type RetryQueueItem struct {
    ID            int64           `json:"id"`
    EventID       string          `json:"event_id"`
    EventType     string          `json:"event_type"`
    ChannelName   string          `json:"channel_name"`
    PluginName    string          `json:"plugin_name"`
    EventData     json.RawMessage `json:"event_data"`
    RetryCount    int             `json:"retry_count"`
    MaxRetries    int             `json:"max_retries"`
    Priority      int             `json:"priority"`
    CorrelationID string          `json:"correlation_id"`
}

// RetryableError indicates an error that should be retried
type RetryableError struct {
    Err  error
    Code string
}
```

## SQL Queries

```go
const (
    // Add a failed operation to the retry queue
    insertRetryItemSQL = `
        INSERT INTO daz_retry_queue (
            event_type, channel_name, plugin_name, event_data,
            event_metadata, max_retries, retry_delay_seconds,
            backoff_multiplier, priority, correlation_id, tags
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
        RETURNING id, event_id;
    `

    // Worker acquires items for processing
    acquireRetryItemsSQL = `SELECT * FROM acquire_retry_items($1, $2, $3);`

    // Mark item as completed
    completeRetryItemSQL = `SELECT complete_retry_item($1, $2);`

    // Mark item as failed
    failRetryItemSQL = `SELECT fail_retry_item($1, $2, $3, $4);`
)
```

## Adding Items to Retry Queue

```go
func AddToRetryQueue(ctx context.Context, eventType, channel, plugin string, data interface{}) error {
    // Serialize event data
    eventData, err := json.Marshal(data)
    if err != nil {
        return fmt.Errorf("failed to marshal event data: %w", err)
    }

    // Example metadata
    metadata := map[string]interface{}{
        "timestamp":    time.Now().Unix(),
        "retry_reason": "initial_failure",
    }

    // Use SQL plugin to execute insert
    req := &framework.SQLExecRequest{
        Query: insertRetryItemSQL,
        Params: []framework.SQLParam{
            {Value: eventType},
            {Value: channel},
            {Value: plugin},
            {Value: eventData},
            {Value: metadata},
            {Value: 3},    // max_retries
            {Value: 60},   // retry_delay_seconds
            {Value: 2.0},  // backoff_multiplier
            {Value: 5},    // priority
            {Value: correlationID},
            {Value: []string{plugin, channel}}, // tags
        },
    }
    
    // Send via event bus
    return eventBus.Request("sql", "sql.exec", req, 5*time.Second)
}
```

## Retry Worker Implementation

```go
type RetryWorker struct {
    workerID    string
    batchSize   int
    lockTimeout int
    eventBus    *eventbus.EventBus
    sqlClient   framework.SQLClient
}

func (w *RetryWorker) ProcessRetryQueue(ctx context.Context) error {
    // Acquire items
    req := &framework.SQLQueryRequest{
        Query: acquireRetryItemsSQL,
        Params: []framework.SQLParam{
            {Value: w.workerID},
            {Value: w.batchSize},
            {Value: w.lockTimeout},
        },
    }
    
    resp, err := w.sqlClient.Query(ctx, req)
    if err != nil {
        return fmt.Errorf("failed to acquire items: %w", err)
    }
    
    // Process each item
    for _, row := range resp.Rows {
        item := parseRetryItem(row)
        if err := w.processItem(ctx, item); err != nil {
            w.handleFailure(ctx, item, err)
        } else {
            w.markComplete(ctx, item)
        }
    }
    
    return nil
}

func (w *RetryWorker) processItem(ctx context.Context, item RetryQueueItem) error {
    // Route based on event type
    switch item.EventType {
    case "cytube.event.chatMsg":
        return w.retryChatMessage(ctx, item)
    case "sql.exec.failed":
        return w.retrySQLExec(ctx, item)
    default:
        return fmt.Errorf("unknown event type: %s", item.EventType)
    }
}
```

## Integration Points in SQL Plugin

### In handleSQLExec

```go
func (p *Plugin) handleSQLExec(event framework.Event) error {
    // ... existing code ...
    
    result, err := p.db.ExecContext(ctx, req.Query, params...)
    if err != nil {
        // Check if error is retryable
        if isRetryableError(err) {
            // Add to retry queue
            retryData := map[string]interface{}{
                "query":          req.Query,
                "params":         req.Params,
                "correlation_id": req.CorrelationID,
                "error":          err.Error(),
            }
            
            AddToRetryQueue(ctx, "sql.exec.failed", "", p.name, retryData)
        }
        
        // Continue with normal error handling
        metrics.DatabaseErrors.Inc()
        // ...
    }
}
```

### Background Worker in Plugin

```go
func (p *Plugin) Start() error {
    // ... existing start code ...
    
    // Start retry worker
    if p.config.RetryEnabled {
        go p.runRetryWorker()
    }
    
    return nil
}

func (p *Plugin) runRetryWorker() {
    worker := &RetryWorker{
        workerID:    fmt.Sprintf("sql-plugin-%s", hostname),
        batchSize:   10,
        lockTimeout: 300,
        eventBus:    p.eventBus,
        sqlClient:   p.sqlClient,
    }
    
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            if err := worker.ProcessRetryQueue(context.Background()); err != nil {
                log.Printf("[SQL Plugin] Retry worker error: %v", err)
            }
        case <-p.stopCh:
            return
        }
    }
}
```

## Monitoring Integration

```go
func (p *Plugin) GetRetryQueueStats() (map[string]interface{}, error) {
    query := `
        SELECT status, COUNT(*) as count, AVG(retry_count) as avg_retries
        FROM daz_retry_queue
        WHERE created_at > CURRENT_TIMESTAMP - INTERVAL '24 hours'
        GROUP BY status
    `
    
    // Execute query and return stats
    // ...
}

// Expose via HTTP endpoint or metrics
func (p *Plugin) RegisterMetrics() {
    // Prometheus metrics
    retryQueueSize := prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "daz_retry_queue_size",
            Help: "Current size of retry queue by status",
        },
        []string{"status"},
    )
    
    // Update metrics periodically
    go func() {
        ticker := time.NewTicker(1 * time.Minute)
        for range ticker.C {
            stats, _ := p.GetRetryQueueStats()
            // Update Prometheus metrics
        }
    }()
}
```

## Best Practices

1. **Error Classification**: Distinguish between retryable and non-retryable errors
2. **Correlation IDs**: Always include correlation IDs for tracing
3. **Monitoring**: Set up alerts for dead letter queue items
4. **Cleanup**: Run cleanup job regularly to prevent table bloat
5. **Circuit Breaker**: Implement circuit breaker for frequently failing operations
6. **Idempotency**: Ensure retried operations are idempotent

## Configuration Example

```json
{
  "sql": {
    "retry": {
      "enabled": true,
      "worker_count": 2,
      "batch_size": 10,
      "poll_interval": "30s",
      "max_retries": 3,
      "initial_delay": "60s",
      "backoff_multiplier": 2.0,
      "retention_days": 7
    }
  }
}
```