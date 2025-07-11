# Retry Queue Schema Documentation

## Overview

The retry queue schema provides a robust, persistent retry mechanism for failed operations in the Daz system. It supports exponential backoff, priority-based processing, and automatic dead letter queue management.

## Schema Components

### Main Table: `daz_retry_queue`

The core table that stores all retry queue items with the following key features:

- **Event Identification**: Unique event IDs, event types, channel names, and plugin names
- **Retry Configuration**: Configurable max retries, delay, and backoff multiplier per item
- **Status Tracking**: Enum-based status (pending, processing, completed, failed, dead_letter)
- **Priority Support**: 1-10 priority scale for processing order
- **Locking Mechanism**: Prevents duplicate processing with worker-based locking
- **Expiration**: Automatic cleanup of old items

### Supporting Table: `daz_retry_history`

Optional table for detailed retry attempt tracking and audit trail.

### Key Functions

1. **`acquire_retry_items(worker_id, batch_size, lock_timeout)`**
   - Atomically acquires items for processing
   - Handles lock cleanup for failed workers
   - Returns items in priority order

2. **`complete_retry_item(item_id, worker_id)`**
   - Marks an item as successfully processed
   - Validates worker ownership

3. **`fail_retry_item(item_id, worker_id, error_msg, error_code)`**
   - Records failure and schedules retry
   - Automatically moves to dead letter after max retries
   - Implements exponential backoff

4. **`calculate_next_retry(retry_count, delay, multiplier)`**
   - Calculates next retry time using exponential backoff
   - Example: 60s, 120s, 240s, 480s with 2x multiplier

5. **`cleanup_retry_queue(retention_days)`**
   - Removes old completed and dead letter items
   - Maintains database performance

### Monitoring Views

1. **`daz_retry_queue_stats`**: Aggregated statistics by status, event type, and plugin
2. **`daz_retry_queue_alerts`**: Items requiring immediate attention

## Usage Examples

### Adding an Item to Retry Queue

```sql
INSERT INTO daz_retry_queue (
    event_type,
    channel_name,
    plugin_name,
    event_data,
    max_retries,
    retry_delay_seconds,
    priority
) VALUES (
    'cytube.event.chatMsg',
    'RIFFTRAX_MST3K',
    'sql',
    '{"message": "test", "user": "***REMOVED***"}'::jsonb,
    5,
    30,
    7
);
```

### Processing Items (Worker Pattern)

```sql
-- Worker acquires items
SELECT * FROM acquire_retry_items('worker-001', 10, 300);

-- Process each item...
-- On success:
SELECT complete_retry_item(123, 'worker-001');

-- On failure:
SELECT fail_retry_item(123, 'worker-001', 'Connection timeout', 'TIMEOUT');
```

### Monitoring

```sql
-- View current queue status
SELECT * FROM daz_retry_queue_stats;

-- Check for items needing attention
SELECT * FROM daz_retry_queue_alerts;

-- Find stuck items
SELECT * FROM daz_retry_queue 
WHERE status = 'processing' 
  AND locked_at < CURRENT_TIMESTAMP - INTERVAL '1 hour';
```

## Installation

1. Ensure you're connected to the database as a user with CREATE privileges
2. Run the schema creation script:
   ```bash
   psql -h localhost -U ***REMOVED*** -d daz < scripts/create_retry_queue_schema.sql
   ```

## Configuration Recommendations

### Retry Strategy

- **Default**: 3 retries with 60s initial delay and 2x backoff
- **Critical Operations**: 5-7 retries with 30s initial delay
- **Non-Critical**: 2-3 retries with 120s initial delay

### Performance Tuning

1. **Indexes**: All necessary indexes are created by default
2. **Partitioning**: Consider monthly partitioning for > 1M items/month
3. **Cleanup**: Run cleanup job daily to remove old items

### Worker Configuration

- Use unique worker IDs (hostname + process ID recommended)
- Set appropriate batch sizes (10-50 items typical)
- Implement health checks to release stale locks

## Security Considerations

1. **Permissions**: Only `***REMOVED***` has access by default
2. **Data Retention**: 7-day default retention for completed items
3. **PII Handling**: Consider encryption for sensitive event_data

## Maintenance

### Regular Tasks

1. **Daily**: Run `cleanup_retry_queue()` to remove old items
2. **Weekly**: Check `daz_retry_queue_alerts` for dead letter items
3. **Monthly**: Review statistics and adjust retry configurations

### Monitoring Queries

```sql
-- Items per hour
SELECT date_trunc('hour', created_at) as hour,
       COUNT(*) as items_created
FROM daz_retry_queue
WHERE created_at > CURRENT_TIMESTAMP - INTERVAL '24 hours'
GROUP BY hour
ORDER BY hour;

-- Success rate by event type
SELECT event_type,
       COUNT(CASE WHEN status = 'completed' THEN 1 END)::float / 
       COUNT(*)::float as success_rate
FROM daz_retry_queue
GROUP BY event_type;
```

## Rollback

If you need to remove the retry queue schema:

```bash
psql -h localhost -U ***REMOVED*** -d daz < scripts/rollback_retry_queue_schema.sql
```

**Warning**: This will permanently delete all retry queue data!

## Future Enhancements

1. **Partitioning**: Uncomment partitioning section for high-volume scenarios
2. **Circuit Breaker**: Add circuit breaker pattern for frequently failing operations
3. **Metrics Integration**: Export stats to Prometheus/Grafana
4. **Event Replay**: Add capability to replay successful events