# Fix Chat Duplication in SQL Storage

## Problem Description

When the Daz bot reconnects to CyTube channels after a restart, it re-collects and stores chat messages that were already saved in the database. This happens because CyTube sends recent chat history when a client joins a channel, and the bot treats these historical messages as new events.

### Symptoms
- Chat messages are stored multiple times with identical `message_time` values
- Each bot restart adds another copy of recent messages
- Example: A message sent once appears 3 times in the database after 2 bot restarts

### Impact
- Inflated storage usage
- Incorrect statistics (message counts, user activity)
- Duplicate results in queries
- Potential performance degradation over time

## Root Cause

1. **CyTube Behavior**: When a client joins a channel, CyTube sends recent chat history (last ~25 messages)
2. **Bot Implementation**: The bot processes all incoming events without checking if they already exist
3. **No Deduplication**: The SQL plugin stores every event it receives without checking for duplicates

## Evidence

```sql
-- Query showing duplicate messages
SELECT channel_name, username, message, message_time, COUNT(*) as count 
FROM daz_core_events 
WHERE event_type = 'cytube.event.chatMsg' 
GROUP BY channel_name, username, message, message_time 
HAVING COUNT(*) > 1;
```

Results show messages with 2-3 copies, correlating with the number of bot restarts.

## Proposed Solutions

### Solution 1: Database-Level Deduplication (Recommended)

Add a unique constraint or check at the database level to prevent duplicate insertions.

**Implementation**:
1. Add a unique index on (channel_name, message_time, username) for chat messages
2. Use INSERT ... ON CONFLICT DO NOTHING to skip duplicates
3. Or check existence before insert

**Pros**:
- Guaranteed deduplication
- Works even if multiple bot instances run
- No message loss

**Cons**:
- Requires database schema change
- Slightly slower inserts

### Solution 2: Application-Level Filtering

Track the latest message time and ignore older messages on reconnect.

**Implementation**:
1. On startup, query the latest message_time for each channel
2. Ignore incoming messages with message_time <= last known time
3. Update the threshold as new messages arrive

**Pros**:
- No database changes needed
- Fast filtering

**Cons**:
- Could miss messages if bot was down when they were sent
- Requires careful state management

### Solution 3: Message Cache

Maintain a recent message cache to detect duplicates.

**Implementation**:
1. Keep last N message_times in memory per channel
2. Check cache before storing new messages
3. Use LRU eviction for the cache

**Pros**:
- Fast lookup
- No database changes

**Cons**:
- Memory overhead
- Cache lost on restart

## Recommended Approach

Implement **Solution 1** (Database-Level Deduplication) as the primary fix:

```sql
-- Add unique constraint
ALTER TABLE daz_core_events 
ADD CONSTRAINT unique_chat_message 
UNIQUE (channel_name, message_time, username) 
WHERE event_type = 'cytube.event.chatMsg';

-- Or create unique index
CREATE UNIQUE INDEX idx_unique_chat_message 
ON daz_core_events (channel_name, message_time, username) 
WHERE event_type = 'cytube.event.chatMsg';
```

Then modify the SQL plugin to use ON CONFLICT:

```go
query := `
    INSERT INTO daz_core_events 
    (event_type, channel_name, username, message, message_time, ...) 
    VALUES ($1, $2, $3, $4, $5, ...) 
    ON CONFLICT (channel_name, message_time, username) 
    WHERE event_type = 'cytube.event.chatMsg'
    DO NOTHING
`
```

## Additional Considerations

1. **Other Event Types**: Similar deduplication might be needed for:
   - User join/leave events
   - Media change events
   - PM events

2. **Performance**: Monitor insert performance after adding constraints

3. **Migration**: Clean up existing duplicates before adding constraints:
   ```sql
   -- Remove duplicates keeping only the earliest entry
   DELETE FROM daz_core_events a
   USING daz_core_events b
   WHERE a.id > b.id
   AND a.event_type = b.event_type
   AND a.channel_name = b.channel_name
   AND a.message_time = b.message_time
   AND a.username = b.username
   AND a.event_type = 'cytube.event.chatMsg';
   ```

4. **Monitoring**: Add metrics to track:
   - Number of duplicates rejected
   - Insert performance
   - Table size growth

## Testing Plan

1. Start bot and let it collect messages
2. Send test messages in both channels
3. Restart bot
4. Verify no duplicate messages are stored
5. Confirm new messages are still collected properly