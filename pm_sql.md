# Private Message SQL Storage Analysis

## Current State

The Daz bot has infrastructure to handle private messages (PMs) but implementation is incomplete, resulting in partial or missing PM storage in the SQL database.

## Key Findings

### 1. Infrastructure Exists But Is Incomplete

**What's in place:**
- `PrivateMessageEvent` type defined in framework
- `PrivateMessageData` structure with all necessary fields (FromUser, ToUser, Message, MessageTime, Channel)
- CyTube parser correctly handles incoming PM events (`pkg/cytube/parser.go:42`)
- SQL plugin can extract PM fields (`internal/plugins/sql/plugin.go:546-551`)
- Event type `cytube.event.pm` is recognized by the system

**What's missing:**
- Room manager doesn't populate `PrivateMessageData` when broadcasting PM events
- Outgoing PMs sent via `cytube.send.pm` are never logged to SQL

### 2. Current PM Flow

#### Incoming PMs (Partially Working)
1. User sends PM to bot via CyTube
2. WebSocket client receives PM event
3. Parser creates `PrivateMessageEvent` with all fields populated
4. Room manager receives the event BUT only passes it as generic `RawEvent`
5. Event is broadcast as `cytube.event.pm` 
6. SQL plugin stores it in `daz_core_events` table but PM-specific fields are empty

#### Outgoing PMs (Not Stored)
1. Command plugins broadcast `cytube.send.pm` events
2. Core plugin handles these and sends PMs via WebSocket
3. These events are never logged to SQL

### 3. Database Schema

PMs are stored in the general `daz_core_events` table which has columns for:
- `username` (would be FromUser)
- `message` (PM content)
- `to_user` (recipient)
- `message_time` (timestamp)
- `channel` (channel context)

The schema supports PM storage, but the data isn't being properly populated.

## Required Fixes

### 1. Update Room Manager (`internal/core/room_manager.go`)

Add PM event handling after line 227:
```go
} else if pmEvent, ok := event.(*framework.PrivateMessageEvent); ok && event.Type() == "pm" {
    eventData.PrivateMessage = &framework.PrivateMessageData{
        FromUser:    pmEvent.FromUser,
        ToUser:      pmEvent.ToUser,
        Message:     pmEvent.Message,
        MessageTime: pmEvent.MessageTime,
        Channel:     pmEvent.ChannelName,
    }
}
```

### 2. Log Outgoing PMs

In the core plugin's `handleCytubeSendPM` method, after successfully sending a PM, broadcast a logging event:
```go
// Log the outgoing PM
logEvent := &framework.EventData{
    PrivateMessage: &framework.PrivateMessageData{
        FromUser:    "bot", // or actual bot username
        ToUser:      toUser,
        Message:     msg,
        MessageTime: time.Now().Unix(),
        Channel:     channel,
    },
}
p.eventBus.Broadcast("cytube.event.pm.sent", logEvent)
```

### 3. SQL Plugin Configuration

Add a specific logger rule for outgoing PMs in `config.json`:
```json
{
    "event_pattern": "cytube.event.pm.sent",
    "enabled": true,
    "table": "daz_core_events"
}
```

## Testing

After implementing fixes, verify:
1. Incoming PMs show `from_user`, `to_user`, and `message` in `daz_core_events`
2. Outgoing PMs (from commands) are logged with proper fields
3. Both PM directions can be queried from the database

## SQL Queries for PM Analysis

```sql
-- View all PMs (once properly stored)
SELECT timestamp, username as from_user, to_user, message, channel 
FROM daz_core_events 
WHERE event_type IN ('pm', 'pm.sent')
ORDER BY timestamp DESC;

-- Count PMs by user
SELECT username, COUNT(*) as pm_count 
FROM daz_core_events 
WHERE event_type = 'pm' 
GROUP BY username;
```