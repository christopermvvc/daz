# Event Bus Package

This package implements the Phase 3 Event Bus system for the Daz chat bot.

## Features

### Core Event Bus (`eventbus.go`)
- Go channel-based message passing
- Implements the `framework.EventBus` interface
- Non-blocking event distribution (drops events if buffers are full)
- Configurable buffer sizes per event type
- Support for:
  - Broadcast: Send events to all subscribers
  - Direct Send: Send events to specific plugins
  - Subscribe: Register handlers for event types
  - SQL operations: Delegated to core plugin

### Event Type Constants (`events.go`)
- Predefined event type constants for consistent routing
- Dot notation event types (e.g., `cytube.event.chatMsg`)
- Helper function to extract event type prefixes

### Plugin Registry (`registry.go`)
- Thread-safe plugin registration and management
- Plugin lifecycle support
- Concurrent access support

## Usage

```go
// Create event bus with custom buffer sizes
config := &eventbus.Config{
    BufferSizes: map[string]int{
        "cytube.event": 1000,
        "sql.":         100,
        "plugin.":      50,
    },
}
bus := eventbus.NewEventBus(config)

// Start the event bus
if err := bus.Start(); err != nil {
    log.Fatal(err)
}
defer bus.Stop()

// Subscribe to events
bus.Subscribe("cytube.event.chatMsg", func(event framework.Event) error {
    // Handle chat message
    return nil
})

// Broadcast an event
data := &framework.EventData{
    ChatMessage: &framework.ChatMessageData{
        Username: "user",
        Message:  "Hello!",
    },
}
bus.Broadcast("cytube.event.chatMsg", data)
```

## Buffer Size Configuration

Buffer sizes can be configured per event type or using prefix patterns:
- Exact match: `"cytube.event.chatMsg": 500`
- Prefix match: `"sql.": 200` (matches all SQL events)

The system uses the longest matching prefix when determining buffer size.

## Non-blocking Behavior

The event bus is designed to be non-blocking:
- If a channel buffer is full, new events are dropped with a warning
- This prevents slow consumers from blocking the entire system
- Monitor logs for "WARNING: Dropped event" messages

## Testing

Run tests with:
```bash
go test -v ./pkg/eventbus/
```

Benchmark performance with:
```bash
go test -bench=. ./pkg/eventbus/
```