# Multi-Room Plugin Refactoring Guide

This guide explains how to refactor plugins to support multi-room functionality.

## Overview

With the multi-room architecture, plugins should no longer have channel-specific configuration. Instead, they should:
1. Get room/channel information from incoming events
2. Process events in the context of the room they came from
3. Store data with room context when needed

## Key Changes

### 1. Remove Channel from Plugin Config

**Before:**
```go
type Config struct {
    Channel string `json:"channel"`
    // other fields...
}
```

**After:**
```go
type Config struct {
    // channel field removed
    // other fields...
}
```

### 2. Get Channel/Room from Events

**Before:**
```go
channel := userJoin.Channel
if channel == "" {
    channel = p.config.Channel  // fallback to configured channel
}
```

**After:**
```go
// Option 1: Use channel from event (must be present)
channel := userJoin.Channel
if channel == "" {
    log.Printf("[Plugin] Skipping event without channel information")
    return nil
}

// Option 2: Use room ID from event (preferred)
roomID := event.(*framework.ChatMessageEvent).RoomID
if roomID == "" {
    log.Printf("[Plugin] Skipping event without room ID")
    return nil
}
```

### 3. Database Schema Updates

For plugins that store data, add room context to database tables:

```sql
-- Add room_id column to existing tables
ALTER TABLE user_activity ADD COLUMN room_id VARCHAR(255);
ALTER TABLE media_stats ADD COLUMN room_id VARCHAR(255);
ALTER TABLE analytics_hourly ADD COLUMN room_id VARCHAR(255);

-- Update indexes to include room_id
CREATE INDEX idx_user_activity_room ON user_activity(room_id, username);
```

### 4. Query Updates

**Before:**
```go
query := `SELECT * FROM user_activity WHERE channel = $1`
rows, err := db.Query(query, p.config.Channel)
```

**After:**
```go
query := `SELECT * FROM user_activity WHERE room_id = $1`
rows, err := db.Query(query, roomID)
```

## Plugin-Specific Refactoring

### UserTracker Plugin

1. Remove `Channel` field from config
2. Update `handleUserJoin` and `handleUserLeave` to use event's room/channel
3. Update `handleInfoCommand` to query based on the room the command came from
4. Update database schema to include room context

### MediaTracker Plugin

1. Remove `Channel` field from config
2. Update all database operations to use room ID from events
3. Modify stats aggregation to be per-room
4. Update playlist tracking to be room-specific

### Analytics Plugin

1. Remove `Channel` field from config
2. Update analytics collection to aggregate per room
3. Modify reporting to support room-specific or cross-room analytics

## Testing Multi-Room Plugins

1. Create test configuration with multiple rooms
2. Send events with different room IDs
3. Verify data is properly segregated by room
4. Test commands in different room contexts

## Migration Strategy

1. **Phase 1**: Update plugins to accept room context from events while maintaining backward compatibility
2. **Phase 2**: Update database schema to support room context
3. **Phase 3**: Remove channel configuration and fully switch to event-based room context
4. **Phase 4**: Clean up and optimize for multi-room operation