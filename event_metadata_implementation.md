# EventMetadata Implementation for Cytube Events

## Overview
The Core plugin has been updated to add EventMetadata with tags to all Cytube events. This enhancement provides better event categorization, filtering capabilities, and logging control.

## Key Changes

### 1. Added `createEventMetadata` Helper Function
Location: `/internal/core/room_manager.go`

This function creates appropriate metadata for each Cytube event type with:
- Source identification ("core" plugin)
- Event categorization tags
- Logging preferences
- Priority levels

### 2. Updated Event Broadcasting
Changed from:
```go
rm.eventBus.Broadcast(eventType, eventData)
```

To:
```go
metadata := rm.createEventMetadata(event.Type(), roomID)
rm.eventBus.BroadcastWithMetadata(eventType, eventData, metadata)
```

## Event Categorization and Tags

### Chat Events
- **chatMsg**: `["chat", "public", "user-content"]` - Logged at info level
- **pm**: `["chat", "private", "user-content"]` - Logged at info level, priority 1

### User Events
- **userJoin**: `["user", "presence", "join"]` - Logged at info level
- **userLeave**: `["user", "presence", "leave"]` - Logged at info level
- **setAFK**: `["user", "status"]`
- **setUserRank**: `["user", "permission"]` - Logged at info level
- **setUserMeta**: `["user", "metadata"]`

### Media Events
- **changeMedia**: `["media", "playlist", "change"]` - Logged at info level
- **mediaUpdate**: `["media", "sync", "playback"]` - Not logged by default (high frequency)
- **playlist**: `["media", "playlist", "update"]`
- **queue**: `["media", "playlist", "queue"]` - Logged at info level
- **queueFail**: `["media", "playlist", "error"]` - Logged at warn level, priority 2
- **moveVideo**: `["media", "playlist", "reorder"]`
- **setCurrent**: `["media", "playlist", "current"]`
- **setPlaylistLocked**: `["media", "playlist", "permission"]` - Logged at info level
- **setPlaylistMeta**: `["media", "playlist", "metadata"]`

### Channel Events
- **channelOpts**: `["channel", "config"]` - Logged at info level
- **channelCSSJS**: `["channel", "style"]`
- **setMotd**: `["channel", "announcement"]` - Logged at info level
- **setPermissions**: `["channel", "permission"]` - Logged at warn level, priority 2
- **meta**: `["channel", "metadata"]`

### System Events
- **login**: `["system", "auth", "login"]` - Logged at info level, priority 2
- **addUser**: `["system", "user", "registration"]` - Logged at info level, priority 2
- **delete**: `["system", "moderation"]` - Logged at warn level, priority 3
- **clearVoteskipVote**: `["system", "voting"]`
- **disconnect**: `["system", "connection", "disconnect"]` - Logged at warn level, priority 2

### Additional Tags
All events also include:
- Room-specific tag: `"room:{roomID}"` for filtering by room

## Benefits

1. **Better Event Filtering**: Plugins can now subscribe to specific event categories using tags
2. **Controlled Logging**: Important events are marked for logging, while high-frequency events (like mediaUpdate) are not
3. **Priority System**: Critical events (errors, moderation) have higher priority
4. **Extensibility**: New event types automatically get basic metadata with "unknown" tag

## Example Usage in Plugins

Plugins can now filter events based on metadata:
```go
// Example: Only process chat events
if metadata.Tags contains "chat" {
    // Process chat event
}

// Example: Only log events marked as loggable
if metadata.Loggable {
    log.Printf("[%s] %s", metadata.LogLevel, eventData)
}

// Example: Priority handling
if metadata.Priority > 2 {
    // Handle high-priority event immediately
}
```

## Testing
All tests pass successfully, and the binary builds without errors.