# Daz API Documentation

## Overview

Daz provides several APIs for plugin development, health monitoring, and system interaction. This document covers all available APIs including the EventBus messaging system, plugin interfaces, and HTTP endpoints.

## Table of Contents

- [EventBus API](#eventbus-api)
- [Plugin Interface](#plugin-interface)
- [HTTP Health Endpoints](#http-health-endpoints)
- [SQL Operations](#sql-operations)
- [Event Types](#event-types)
- [Error Handling](#error-handling)

## EventBus API

The EventBus is the central communication system in Daz. All plugins interact through the EventBus using publish/subscribe patterns and request/response mechanisms.

### Core Methods

#### Broadcasting Events

```go
// Broadcast an event to all subscribers
err := eventBus.Broadcast(eventType string, data *EventData) error

// Broadcast with metadata (priority, tags, etc.)
err := eventBus.BroadcastWithMetadata(eventType string, data *EventData, metadata *EventMetadata) error
```

#### Direct Communication

```go
// Send event directly to a specific plugin
err := eventBus.Send(target string, eventType string, data *EventData) error

// Send with metadata
err := eventBus.SendWithMetadata(target string, eventType string, data *EventData, metadata *EventMetadata) error
```

#### Request/Response Pattern

```go
// Make a synchronous request to another plugin
response, err := eventBus.Request(ctx context.Context, target string, eventType string, data *EventData, metadata *EventMetadata) (*EventData, error)

// Deliver a response (used internally by plugins)
eventBus.DeliverResponse(correlationID string, response *EventData, err error)
```

#### Event Subscription

```go
// Subscribe to events by pattern (supports wildcards)
err := eventBus.Subscribe(eventType string, handler EventHandler) error

// Subscribe with tag filtering
err := eventBus.SubscribeWithTags(pattern string, handler EventHandler, tags []string) error
```

### EventData Structure

EventData is a union-type structure where each field represents a different type of event data. Only one of these fields should be populated for any given event.

```go
type EventData struct {
    // Cytube Events
    ChatMessage    *ChatMessage    `json:"chat_message,omitempty"`
    PrivateMessage *PrivateMessage `json:"private_message,omitempty"`
    UserJoin       *UserJoin       `json:"user_join,omitempty"`
    UserLeave      *UserLeave      `json:"user_leave,omitempty"`
    VideoChange    *VideoChange    `json:"video_change,omitempty"`
    QueueUpdate    *QueueUpdate    `json:"queue_update,omitempty"`
    MediaUpdate    *MediaUpdate    `json:"media_update,omitempty"`
    
    // SQL Operations
    SQLQueryRequest  *SQLQueryRequest  `json:"sql_query_request,omitempty"`
    SQLQueryResponse *SQLQueryResponse `json:"sql_query_response,omitempty"`
    SQLExecRequest   *SQLExecRequest   `json:"sql_exec_request,omitempty"`
    SQLExecResponse  *SQLExecResponse  `json:"sql_exec_response,omitempty"`
    SQLBatchRequest  *SQLBatchRequest  `json:"sql_batch_request,omitempty"`
    SQLBatchResponse *SQLBatchResponse `json:"sql_batch_response,omitempty"`
    
    // Plugin Communication
    PluginRequest  *PluginRequest  `json:"plugin_request,omitempty"`
    PluginResponse *PluginResponse `json:"plugin_response,omitempty"`
    
    // Retry System
    RetryRequest *RetryRequest `json:"retry_request,omitempty"`
    RetryStatus  *RetryStatus  `json:"retry_status,omitempty"`
    
    // Generic Fields
    KeyValue    map[string]string `json:"key_value,omitempty"`
    RawMessage  string            `json:"raw_message,omitempty"`
    
    // Raw event fields (for backward compatibility)
    RawEvent map[string]interface{} `json:"raw_event,omitempty"`
}
```

### EventMetadata Structure

Event metadata is passed separately through EventBus methods like `BroadcastWithMetadata` and `SendWithMetadata`:

```go
type EventMetadata struct {
    Priority      int      `json:"priority"`
    Tags          []string `json:"tags,omitempty"`
    CorrelationID string   `json:"correlation_id,omitempty"`
    ReplyTo       string   `json:"reply_to,omitempty"`
}
```

### Event Patterns

Daz supports wildcard patterns for event subscriptions:

- `cytube.event.*` - All Cytube events
- `plugin.command.*` - All command events
- `*.failed` - All failure events

## Plugin Interface

All plugins must implement the `framework.Plugin` interface:

```go
type Plugin interface {
    // Initialize with configuration and EventBus
    Init(config json.RawMessage, bus EventBus) error
    
    // Start the plugin
    Start() error
    
    // Stop the plugin gracefully
    Stop() error
    
    // Handle incoming events
    HandleEvent(event Event) error
    
    // Get current status
    Status() PluginStatus
    
    // Get plugin name
    Name() string
}
```

### Plugin Status

```go
type PluginStatus struct {
    Name          string
    State         string        // "initialized", "running", "stopped", "failed"
    LastError     error
    RetryCount    int
    EventsHandled int64
    Uptime        time.Duration
}
```

## HTTP Health Endpoints

Daz provides HTTP endpoints for health monitoring:

### GET /health

Returns overall system health status.

**Response:**
```json
{
    "status": "UP",
    "timestamp": "2025-07-19T10:30:00Z",
    "uptime": "2h30m15s",
    "components": {
        "eventbus": {
            "name": "eventbus",
            "status": "UP",
            "details": {
                "dropped_events_total": 0,
                "dropped_by_type": {}
            },
            "check_time": "2025-07-19T10:30:00Z"
        },
        "core": {
            "name": "core",
            "status": "UP",
            "details": {
                "state": "running",
                "events_handled": 15234,
                "uptime": "2h30m15s"
            },
            "check_time": "2025-07-19T10:30:00Z"
        }
    }
}
```

**Status Codes:**
- `200 OK` - System is healthy
- `503 Service Unavailable` - System is unhealthy

### GET /health/live

Kubernetes liveness probe endpoint.

**Response:**
```json
{
    "status": "alive",
    "timestamp": "2025-07-19T10:30:00Z"
}
```

### GET /health/ready

Kubernetes readiness probe endpoint.

**Response (when ready):**
```json
{
    "ready": true,
    "timestamp": "2025-07-19T10:30:00Z"
}
```

**Response (when not ready):**
```json
{
    "ready": false,
    "status": "DOWN",
    "timestamp": "2025-07-19T10:30:00Z"
}
```

### GET /metrics

Prometheus metrics endpoint. Returns metrics in Prometheus text format.

```
# HELP daz_bot_uptime_seconds Bot uptime in seconds
# TYPE daz_bot_uptime_seconds gauge
daz_bot_uptime_seconds 9015

# HELP daz_events_received_total Total number of events received
# TYPE daz_events_received_total counter
daz_events_received_total{event_type="cytube.event.chatMsg"} 1523
```

## SQL Operations

Plugins can perform database operations through the SQL plugin via EventBus requests.

### Query Operations

```go
// Create query request
request := &framework.EventData{
    SQLQueryRequest: &framework.SQLQueryRequest{
        ID:            uuid.New().String(),
        CorrelationID: uuid.New().String(),
        Query:         "SELECT * FROM users WHERE username = $1",
        Params:        []framework.SQLParam{{Value: "john"}},
        Timeout:       30 * time.Second,
        RequestBy:     "myplugin",
    },
}

// Send request
response, err := eventBus.Request(ctx, "sql", "plugin.request", request, nil)

// Parse response
if response != nil && response.SQLQueryResponse != nil {
    for _, row := range response.SQLQueryResponse.Rows {
        // Process row data
    }
}
```

### Execute Operations

```go
// Create exec request
request := &framework.EventData{
    SQLExecRequest: &framework.SQLExecRequest{
        ID:            uuid.New().String(),
        CorrelationID: uuid.New().String(),
        Query:         "INSERT INTO logs (message) VALUES ($1)",
        Params:        []framework.SQLParam{{Value: "test log"}},
        Timeout:       30 * time.Second,
        RequestBy:     "myplugin",
    },
}

// Send request
response, err := eventBus.Request(ctx, "sql", "plugin.request", request, nil)

// Check response
if response != nil && response.SQLExecResponse != nil {
    rowsAffected := response.SQLExecResponse.RowsAffected
    // Handle result
}
```

### Batch Operations

```go
// Create batch request
request := &framework.EventData{
    SQLBatchRequest: &framework.SQLBatchRequest{
        ID:            uuid.New().String(),
        CorrelationID: uuid.New().String(),
        Operations: []framework.BatchOperation{
            {
                ID:            "op1",
                OperationType: "query",
                Query:         "SELECT COUNT(*) FROM users",
            },
            {
                ID:            "op2",
                OperationType: "exec",
                Query:         "UPDATE stats SET last_check = $1",
                Params:        []framework.SQLParam{{Value: time.Now()}},
            },
        },
        Atomic:    true,  // Run in transaction
        Timeout:   60 * time.Second,
        RequestBy: "myplugin",
    },
}

// Send request
response, err := eventBus.Request(ctx, "sql", "plugin.request", request, nil)

// Process batch response
if response != nil && response.SQLBatchResponse != nil {
    for _, result := range response.SQLBatchResponse.Results {
        // Handle each operation result
    }
}
```

## Event Types

### Cytube Events

All Cytube events are broadcast with the prefix `cytube.event.`:

- `cytube.event.chatMsg` - Chat message
- `cytube.event.userJoin` - User joined channel
- `cytube.event.userLeave` - User left channel
- `cytube.event.changeMedia` - Media changed
- `cytube.event.queue` - Queue updated
- `cytube.event.playlist` - Playlist updated
- `cytube.event.pm` - Private message
- `cytube.event.connect` - Connected to Cytube
- `cytube.event.disconnect` - Disconnected from Cytube

### Plugin Events

- `plugin.request` - Inter-plugin requests
- `plugin.response` - Inter-plugin responses
- `*.failed` - Failure events for retry mechanism (e.g., `sql.query.failed`, `command.uptime.failed`)
- `*.error` - Error events caught by retry plugin
- `*.timeout` - Timeout events caught by retry plugin

Note: Commands are handled through the eventfilter plugin which parses chat messages and routes them to appropriate plugins, rather than using a `plugin.command.*` pattern.

### System Events

- `log.request` - Request to log data
- `log.batch` - Batch logging request
- `sql.query` - Database query request
- `sql.exec` - Database execute request

## Error Handling

### Failure Events

When operations fail, plugins should emit failure events for the retry mechanism:

```go
// Emit failure event
failureData := &framework.EventData{
    KeyValue: map[string]string{
        "correlation_id": correlationID,
        "source":         "myplugin",
        "operation_type": "api_call",
        "error":          err.Error(),
        "timestamp":      time.Now().Format(time.RFC3339),
    },
}

eventBus.Broadcast("myplugin.operation.failed", failureData)
```

### Error Response Format

```go
type ErrorResponse struct {
    Success bool   `json:"success"`
    Error   string `json:"error"`
    Code    string `json:"code,omitempty"`
    Details any    `json:"details,omitempty"`
}
```

## Best Practices

### 1. Use Correlation IDs

Always include correlation IDs for request tracking:

```go
metadata := &framework.EventMetadata{
    CorrelationID: uuid.New().String(),
}
```

### 2. Handle Context Cancellation

```go
select {
case <-ctx.Done():
    return nil, ctx.Err()
case response := <-responseChan:
    return response, nil
}
```

### 3. Set Appropriate Timeouts

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
```

### 4. Use Structured Logging

```go
logger.Info("MyPlugin", "Processing event: type=%s, correlation_id=%s", 
    event.Type(), correlationID)
```

### 5. Emit Metrics

```go
metrics.EventsProcessed.WithLabelValues("myplugin", "success").Inc()
```

## Rate Limiting

The EventBus has configurable buffer sizes to prevent memory issues:

```json
{
    "event_bus": {
        "buffer_sizes": {
            "cytube.event": 5000,
            "sql.query": 100,
            "plugin.request": 200
        }
    }
}
```

Events will be dropped if buffers are full. Monitor dropped events via:

```go
droppedCounts := eventBus.GetDroppedEventCounts()
```

---

For more examples and plugin development guidelines, see the [Plugin Development Guide](plugins.md).