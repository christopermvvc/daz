# Daz Plugin Development Guide

## Introduction

Daz's plugin architecture allows you to extend the bot's functionality by creating custom plugins. This guide will walk you through creating, testing, and deploying your own plugins.

## Table of Contents

- [Plugin Architecture Overview](#plugin-architecture-overview)
- [Creating Your First Plugin](#creating-your-first-plugin)
- [Plugin Lifecycle](#plugin-lifecycle)
- [Event Handling](#event-handling)
- [Database Operations](#database-operations)
- [Inter-Plugin Communication](#inter-plugin-communication)
- [Command Plugins](#command-plugins)
- [Testing Plugins](#testing-plugins)
- [Best Practices](#best-practices)
- [Example Plugins](#example-plugins)

## Plugin Architecture Overview

Daz plugins are self-contained modules that:
- Implement the `framework.Plugin` interface
- Communicate via the EventBus
- Can persist data through the SQL plugin
- Handle specific types of events
- Provide isolated functionality

### Key Concepts

1. **Event-Driven**: Plugins react to events, not direct method calls
2. **Isolated**: Plugins don't directly reference each other
3. **Configurable**: Each plugin has its own configuration section
4. **Resilient**: Plugin failures don't crash the system

## Creating Your First Plugin

Let's create a simple greeting plugin that responds to "hello" messages.

### Step 1: Create Plugin Structure

Create a new file `internal/plugins/greeting/plugin.go`:

```go
package greeting

import (
    "context"
    "encoding/json"
    "strings"
    "time"
    
    "github.com/hildolfr/daz/internal/framework"
    "github.com/hildolfr/daz/internal/logger"
)

type Plugin struct {
    eventBus      framework.EventBus
    config        Config
    startTime     time.Time
    eventsHandled int64
}

type Config struct {
    Enabled     bool     `json:"enabled"`
    Greeting    string   `json:"greeting"`
    Triggers    []string `json:"triggers"`
}

func New() framework.Plugin {
    return &Plugin{}
}
```

### Step 2: Implement Plugin Interface

```go
func (p *Plugin) Name() string {
    return "greeting"
}

func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
    // Set defaults
    p.config = Config{
        Enabled:  true,
        Greeting: "Hello there, {username}!",
        Triggers: []string{"hello", "hi", "hey"},
    }
    
    // Parse configuration
    if len(config) > 0 {
        if err := json.Unmarshal(config, &p.config); err != nil {
            return err
        }
    }
    
    p.eventBus = bus
    logger.Info("Greeting", "Initialized with %d triggers", len(p.config.Triggers))
    return nil
}

func (p *Plugin) Start() error {
    p.startTime = time.Now()
    
    // Subscribe to chat messages
    if err := p.eventBus.Subscribe("cytube.event.chatMsg", p.handleChatMessage); err != nil {
        return err
    }
    
    logger.Info("Greeting", "Started successfully")
    return nil
}

func (p *Plugin) Stop() error {
    logger.Info("Greeting", "Stopped")
    return nil
}

func (p *Plugin) HandleEvent(event framework.Event) error {
    p.eventsHandled++
    return nil
}

func (p *Plugin) Status() framework.PluginStatus {
    return framework.PluginStatus{
        Name:          p.Name(),
        State:         "running",
        EventsHandled: p.eventsHandled,
        Uptime:        time.Since(p.startTime),
    }
}
```

### Step 3: Implement Event Handler

```go
func (p *Plugin) handleChatMessage(event framework.Event) error {
    dataEvent, ok := event.(*framework.DataEvent)
    if !ok || dataEvent.Data == nil || dataEvent.Data.ChatMessage == nil {
        return nil
    }
    
    chatMessage := dataEvent.Data.ChatMessage
    p.eventsHandled++
    
    // Check if message contains a trigger word
    messageLower := strings.ToLower(chatMessage.Message)
    for _, trigger := range p.config.Triggers {
        if strings.Contains(messageLower, trigger) {
            return p.sendGreeting(chatMessage)
        }
    }
    
    return nil
}

func (p *Plugin) sendGreeting(chatMessage *framework.ChatMessage) error {
    // Format greeting message
    greeting := strings.ReplaceAll(p.config.Greeting, "{username}", chatMessage.Username)
    
    // Create response event
    responseData := &framework.EventData{
        KeyValue: map[string]string{
            "channel": chatMessage.ChannelName,
            "message": greeting,
        },
    }
    
    // Send to core plugin - core will handle the actual chat send
    return p.eventBus.Send("core", "chat.send", responseData)
}
```

### Step 4: Register Plugin

Add to `cmd/daz/main.go`:

```go
import "github.com/hildolfr/daz/internal/plugins/greeting"

// In the plugins array:
{"greeting", greeting.New()},
```

### Step 5: Configure Plugin

Add to `config.json`:

```json
{
    "plugins": {
        "greeting": {
            "enabled": true,
            "greeting": "Hey {username}! Welcome to the channel!",
            "triggers": ["hello", "hi", "hey", "greetings"]
        }
    }
}
```

## Plugin Lifecycle

### Initialization Order

1. **Plugin Creation**: `New()` creates plugin instance
2. **Registration**: Plugin registered with PluginManager
3. **Initialization**: `Init()` called with config and EventBus
4. **Start**: `Start()` called to begin operation
5. **Running**: Plugin processes events
6. **Stop**: `Stop()` called for graceful shutdown

### State Management

```go
type PluginState string

const (
    StateInitialized PluginState = "initialized"
    StateRunning     PluginState = "running"
    StateStopped     PluginState = "stopped"
    StateFailed      PluginState = "failed"
)
```

## Event Handling

### Subscribing to Events

```go
// Subscribe to specific event type
p.eventBus.Subscribe("cytube.event.userJoin", p.handleUserJoin)

// Subscribe with wildcard pattern
p.eventBus.Subscribe("cytube.event.*", p.handleAllCytubeEvents)

// Subscribe with tags
p.eventBus.SubscribeWithTags("cytube.event.chatMsg", p.handleTaggedChat, []string{"important"})
```

### Event Type Assertion

```go
func (p *Plugin) handleEvent(event framework.Event) error {
    dataEvent, ok := event.(*framework.DataEvent)
    if !ok || dataEvent.Data == nil {
        return nil
    }
    
    // Check different event types
    switch {
    case dataEvent.Data.ChatMessage != nil:
        return p.handleChat(dataEvent.Data.ChatMessage)
    case dataEvent.Data.UserJoin != nil:
        return p.handleUserJoin(dataEvent.Data.UserJoin)
    default:
        logger.Debug("MyPlugin", "Unhandled event type: %s", dataEvent.Type())
    }
    return nil
}
```

### Broadcasting Events

```go
// Simple broadcast
p.eventBus.Broadcast("myplugin.data.updated", &framework.EventData{
    KeyValue: map[string]string{
        "source": p.Name(),
        "key": "value",
    },
})

// Broadcast with metadata
data := &framework.EventData{
    KeyValue: map[string]string{
        "action": "user_logged_in",
        "username": "john",
    },
}
metadata := &framework.EventMetadata{
    Priority: 5,
    Tags:     []string{"notification", "user-action"},
}
p.eventBus.BroadcastWithMetadata("myplugin.action", data, metadata)
```

## Database Operations

### Setting Up Schema

Create a migration in your plugin's Init:

```go
func (p *Plugin) createSchema() error {
    ctx := context.Background()
    query := `
        CREATE TABLE IF NOT EXISTS daz_greeting_stats (
            id SERIAL PRIMARY KEY,
            username VARCHAR(255) NOT NULL,
            greeting_count INT DEFAULT 1,
            last_greeting TIMESTAMP DEFAULT NOW(),
            UNIQUE(username)
        )
    `
    
    request := &framework.SQLExecRequest{
        Query:     query,
        RequestBy: p.Name(),
    }
    
    _, err := p.executeSQLRequest(ctx, request)
    return err
}
```

### Executing Queries

```go
func (p *Plugin) getGreetingCount(username string) (int, error) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    response, err := p.querySQLRequest(ctx, 
        "SELECT greeting_count FROM daz_greeting_stats WHERE username = $1",
        []framework.SQLParam{{Value: username}})
    if err != nil {
        return 0, err
    }
    
    if len(response.Rows) == 0 {
        return 0, nil
    }
    
    var count int
    if err := json.Unmarshal(response.Rows[0][0], &count); err != nil {
        return 0, err
    }
    
    return count, nil
}
```

### Helper Methods

```go
func (p *Plugin) executeSQLRequest(ctx context.Context, query string, params []framework.SQLParam) (*framework.SQLExecResponse, error) {
    request := &framework.EventData{
        SQLExecRequest: &framework.SQLExecRequest{
            ID:            uuid.New().String(),
            CorrelationID: uuid.New().String(),
            Query:         query,
            Params:        params,
            Timeout:       30 * time.Second,
            RequestBy:     p.Name(),
        },
    }
    
    response, err := p.eventBus.Request(ctx, "sql", "plugin.request", request, nil)
    if err != nil {
        return nil, err
    }
    
    if response == nil || response.SQLExecResponse == nil {
        return nil, fmt.Errorf("invalid response from SQL plugin")
    }
    
    return response.SQLExecResponse, nil
}

func (p *Plugin) querySQLRequest(ctx context.Context, query string, params []framework.SQLParam) (*framework.SQLQueryResponse, error) {
    request := &framework.EventData{
        SQLQueryRequest: &framework.SQLQueryRequest{
            ID:            uuid.New().String(),
            CorrelationID: uuid.New().String(),
            Query:         query,
            Params:        params,
            Timeout:       30 * time.Second,
            RequestBy:     p.Name(),
        },
    }
    
    response, err := p.eventBus.Request(ctx, "sql", "plugin.request", request, nil)
    if err != nil {
        return nil, err
    }
    
    if response == nil || response.SQLQueryResponse == nil {
        return nil, fmt.Errorf("invalid response from SQL plugin")
    }
    
    return response.SQLQueryResponse, nil
}
```

## Inter-Plugin Communication

### Direct Messaging

```go
// Send message to specific plugin
err := p.eventBus.Send("analytics", "greeting.recorded", &framework.EventData{
    Source: p.Name(),
    KeyValue: map[string]string{
        "username": username,
        "count":    strconv.Itoa(count),
    },
})
```

### Request/Response

```go
// Make request to another plugin
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

response, err := p.eventBus.Request(ctx, "usertracker", "user.info", 
    &framework.EventData{
        Source: p.Name(),
        KeyValue: map[string]string{
            "username": username,
        },
    }, nil)

if err != nil {
    return nil, err
}

// Parse response
var userInfo UserInfo
if err := json.Unmarshal(response.RawData, &userInfo); err != nil {
    return nil, err
}
```

## Command Plugins

### Creating a Command Plugin

```go
package mycommand

type Plugin struct {
    eventBus framework.EventBus
}

func (p *Plugin) Start() error {
    // Register command with EventFilter
    cmdData := &framework.EventData{
        PluginRequest: &framework.PluginRequest{
            Action: "register_command",
            Data: map[string]interface{}{
                "command":     "mycommand",
                "description": "Does something cool",
                "usage":       "!mycommand [args]",
                "min_rank":    0,
                "plugin_name": p.Name(),
            },
        },
    }
    
    return p.eventBus.Send("eventfilter", "plugin.request", cmdData)
}

func (p *Plugin) HandleEvent(event framework.Event) error {
    dataEvent, ok := event.(*framework.DataEvent)
    if !ok || dataEvent.Data == nil {
        return nil
    }
    
    // Check if this is a command for us
    if dataEvent.Data.PluginRequest != nil && 
       dataEvent.Data.PluginRequest.Action == "command" &&
       dataEvent.Data.PluginRequest.Data["command"] == "mycommand" {
        return p.handleCommand(dataEvent)
    }
    
    return nil
}

func (p *Plugin) handleCommand(dataEvent *framework.DataEvent) error {
    req := dataEvent.Data.PluginRequest
    
    // Extract command data
    username, _ := req.Data["username"].(string)
    channel, _ := req.Data["channel"].(string)
    args, _ := req.Data["args"].([]interface{})
    
    // Process command
    argStrings := make([]string, len(args))
    for i, arg := range args {
        argStrings[i] = fmt.Sprint(arg)
    }
    response := fmt.Sprintf("Hello %s! You said: %s", 
        username, 
        strings.Join(argStrings, " "))
    
    // Send response
    return p.eventBus.Send("core", "chat.send", &framework.EventData{
        KeyValue: map[string]string{
            "channel": channel,
            "message": response,
        },
    })
}
```

## Testing Plugins

### Unit Testing

```go
package greeting

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/mock"
)

type MockEventBus struct {
    mock.Mock
}

func (m *MockEventBus) Subscribe(eventType string, handler framework.EventHandler) error {
    args := m.Called(eventType, handler)
    return args.Error(0)
}

func TestPluginInit(t *testing.T) {
    plugin := New()
    mockBus := new(MockEventBus)
    
    config := []byte(`{"greeting": "Test greeting"}`)
    
    err := plugin.Init(config, mockBus)
    assert.NoError(t, err)
    
    greetingPlugin := plugin.(*Plugin)
    assert.Equal(t, "Test greeting", greetingPlugin.config.Greeting)
}
```

### Integration Testing

```go
func TestPluginIntegration(t *testing.T) {
    // Create real EventBus
    bus := eventbus.NewEventBus(&eventbus.Config{
        BufferSizes: map[string]int{
            "cytube.event": 100,
        },
    })
    bus.Start()
    defer bus.Stop()
    
    // Create and start plugin
    plugin := New()
    err := plugin.Init([]byte(`{}`), bus)
    assert.NoError(t, err)
    
    err = plugin.Start()
    assert.NoError(t, err)
    defer plugin.Stop()
    
    // Simulate chat event
    chatEvent := &framework.ChatMessageEvent{
        CytubeEvent: framework.CytubeEvent{
            EventType: "chatMsg",
            EventTime: time.Now(),
        },
        Username: "testuser",
        Message:  "hello everyone",
    }
    
    // Should trigger greeting
    err = bus.Broadcast("cytube.event.chatMsg", chatEvent)
    assert.NoError(t, err)
}
```

## Best Practices

### 1. Error Handling

Always emit failure events for retry:

```go
func (p *Plugin) processData() error {
    if err := p.doSomething(); err != nil {
        // Emit failure event
        p.eventBus.Broadcast("greeting.process.failed", &framework.EventData{
            Source: p.Name(),
            KeyValue: map[string]string{
                "error":     err.Error(),
                "operation": "process_data",
            },
        })
        return err
    }
    return nil
}
```

### 2. Configuration Validation

```go
func (p *Plugin) Init(config json.RawMessage, bus framework.EventBus) error {
    // ... parse config ...
    
    // Validate
    if p.config.Greeting == "" {
        return fmt.Errorf("greeting message cannot be empty")
    }
    
    if len(p.config.Triggers) == 0 {
        return fmt.Errorf("at least one trigger word required")
    }
    
    return nil
}
```

### 3. Resource Cleanup

```go
func (p *Plugin) Stop() error {
    // Cancel any ongoing operations
    if p.cancel != nil {
        p.cancel()
    }
    
    // Close any connections
    if p.client != nil {
        p.client.Close()
    }
    
    // Wait for goroutines
    p.wg.Wait()
    
    logger.Info(p.Name(), "Stopped gracefully")
    return nil
}
```

### 4. Metrics and Monitoring

```go
func (p *Plugin) recordMetric(action string, success bool) {
    status := "success"
    if !success {
        status = "failure"
    }
    
    metrics.PluginOperations.WithLabelValues(p.Name(), action, status).Inc()
}
```

### 5. Logging

```go
// Use structured logging
logger.Info("Greeting", "Processing message: user=%s, trigger=%s", username, trigger)
logger.Debug("Greeting", "Config loaded: %+v", p.config)
logger.Error("Greeting", "Failed to query database: %v", err)
```

## Example Plugins

### Analytics Plugin

Tracks user activity and generates reports:

```go
// internal/plugins/analytics/plugin.go
type Plugin struct {
    eventBus      framework.EventBus
    userActivity  map[string]*UserStats
    mu            sync.RWMutex
}

type UserStats struct {
    MessageCount int
    LastSeen     time.Time
    JoinTime     time.Time
}
```

### Media Tracker Plugin

Monitors media plays:

```go
// internal/plugins/mediatracker/plugin.go
type Plugin struct {
    eventBus     framework.EventBus
    currentMedia *MediaInfo
    playHistory  []PlayRecord
}

type MediaInfo struct {
    ID       string
    Title    string
    Duration int
    StartTime time.Time
}
```

### User Tracker Plugin

Maintains user session information:

```go
// internal/plugins/usertracker/plugin.go
type Plugin struct {
    eventBus      framework.EventBus
    activeSessions map[string]*Session
    mu            sync.RWMutex
}

type Session struct {
    Username  string
    JoinTime  time.Time
    LastActivity time.Time
    Rank      int
}
```

## Advanced Topics

### Plugin Dependencies

Some plugins may depend on others:

```go
func (p *Plugin) Start() error {
    // Wait for SQL plugin to be ready
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    if err := p.waitForPlugin(ctx, "sql"); err != nil {
        return fmt.Errorf("sql plugin not available: %w", err)
    }
    
    // Continue startup
    return nil
}
```

### Dynamic Configuration

React to configuration changes:

```go
func (p *Plugin) Start() error {
    // Subscribe to config updates
    p.eventBus.Subscribe("config.updated.greeting", p.handleConfigUpdate)
    return nil
}

func (p *Plugin) handleConfigUpdate(event framework.Event) error {
    // Reload configuration
    // Update internal state
    // No restart required
    return nil
}
```

### Performance Optimization

```go
// Use buffered channels for high-volume events
type Plugin struct {
    eventQueue chan framework.Event
}

func (p *Plugin) Start() error {
    p.eventQueue = make(chan framework.Event, 1000)
    
    // Process events in separate goroutine
    go p.eventProcessor()
    
    return p.eventBus.Subscribe("cytube.event.*", p.queueEvent)
}

func (p *Plugin) queueEvent(event framework.Event) error {
    select {
    case p.eventQueue <- event:
        return nil
    default:
        // Queue full, drop event
        metrics.DroppedEvents.WithLabelValues(p.Name()).Inc()
        return nil
    }
}
```

## Troubleshooting

### Common Issues

1. **Plugin not receiving events**: Check subscription patterns and event types
2. **Database errors**: Ensure SQL plugin is running and schema exists
3. **High memory usage**: Check for unbounded data structures
4. **Events being dropped**: Increase buffer sizes in config

### Debug Mode

Enable debug logging for your plugin:

```go
if p.config.Debug {
    logger.SetLevel(logger.LevelDebug)
}
```

### Health Checks

Implement detailed health reporting:

```go
func (p *Plugin) Status() framework.PluginStatus {
    status := framework.PluginStatus{
        Name:          p.Name(),
        State:         "running",
        EventsHandled: p.eventsHandled,
        Uptime:        time.Since(p.startTime),
    }
    
    // Add custom health info
    if p.lastError != nil {
        status.State = "degraded"
        status.LastError = p.lastError
    }
    
    return status
}
```

---

For API reference and event types, see the [API Documentation](api.md).