# Retry Mechanism Testing Scripts

This directory contains scripts to verify that the retry mechanism in daz is working correctly.

## Overview

The retry mechanism in daz automatically retries failed operations (SQL queries, plugin requests, etc.) with configurable backoff strategies. When an operation fails, it's added to the `daz_retry_queue` table and processed by the retry plugin.

## Test Scripts

### 1. test-retry-simple.sh

A simple test script that doesn't require sudo privileges. It tests the retry mechanism by:
- Creating test retry entries directly in the database
- Monitoring the retry queue processing
- Verifying that retries are attempted and completed

**Usage:**
```bash
./scripts/test-retry-simple.sh
```

**What it does:**
1. Starts daz with a test configuration (aggressive retry settings)
2. Creates test retry entries with different scenarios:
   - Valid SQL operations that should succeed
   - Invalid operations that should fail permanently
   - High-priority operations for immediate retry
3. Monitors the retry queue for 60 seconds
4. Reports on successful retries, failures, and dead letters

### 2. test-retry-mechanism.sh

A more comprehensive test that simulates real failure scenarios. Requires sudo privileges for network manipulation.

**Usage:**
```bash
sudo ./scripts/test-retry-mechanism.sh
```

**What it does:**
1. Temporarily blocks database connections using iptables
2. Monitors for failed operations being added to retry queue
3. Restores connectivity and watches retry attempts
4. Provides detailed retry statistics

## Monitoring Retry Activity

### Check Retry Queue Status
```sql
-- View pending retries
SELECT * FROM daz_retry_queue WHERE status = 'pending';

-- View retry statistics
SELECT * FROM daz_retry_queue_stats;

-- View items requiring attention
SELECT * FROM daz_retry_queue_alerts;
```

### Monitor Logs
```bash
# Watch for retry activity in real-time
tail -f /tmp/daz-retry-test.log | grep -i retry

# Check for retry errors
grep -i "retry.*error" /tmp/daz-retry-test.log
```

## Retry Configuration

The retry mechanism is configured in the `retry` plugin section of config.json:

```json
{
  "retry": {
    "enabled": true,
    "poll_interval_seconds": 10,
    "batch_size": 10,
    "worker_count": 3,
    "default_max_retries": 3,
    "default_timeout_seconds": 30,
    "retry_policies": {
      "sql_operation": {
        "max_retries": 3,
        "initial_delay_seconds": 2,
        "max_delay_seconds": 60,
        "backoff_multiplier": 1.5,
        "timeout_seconds": 15
      }
    }
  }
}
```

## How the Retry Mechanism Works

1. **Failure Detection**: When an operation fails (e.g., SQL query timeout), the plugin emits a failure event
2. **Retry Scheduling**: The retry plugin subscribes to failure events and creates entries in `daz_retry_queue`
3. **Queue Processing**: Workers poll the queue every `poll_interval_seconds` and attempt to retry operations
4. **Backoff Strategy**: Failed retries use exponential backoff (delay * multiplier^attempt)
5. **Dead Letter**: Operations that exceed `max_retries` are moved to dead letter status

## Troubleshooting

### Retries Not Working

1. **Check if retry plugin is enabled:**
   ```bash
   grep -i "retry.*started" /path/to/daz.log
   ```

2. **Verify retry queue table exists:**
   ```sql
   \dt daz_retry_queue
   ```

3. **Check for processing errors:**
   ```sql
   SELECT * FROM daz_retry_queue WHERE last_error IS NOT NULL;
   ```

### Performance Issues

1. **Monitor queue size:**
   ```sql
   SELECT status, COUNT(*) FROM daz_retry_queue GROUP BY status;
   ```

2. **Check worker utilization:**
   ```bash
   grep "Processing retry" /path/to/daz.log | tail -20
   ```

3. **Adjust configuration:**
   - Increase `worker_count` for more parallelism
   - Reduce `poll_interval_seconds` for faster processing
   - Increase `batch_size` to process more items per poll

## Manual Retry Management

### Retry a specific operation
```sql
UPDATE daz_retry_queue 
SET status = 'pending', retry_count = 0, retry_after = NOW()
WHERE correlation_id = 'your-operation-id';
```

### Move item to dead letter
```sql
UPDATE daz_retry_queue 
SET status = 'dead_letter'
WHERE id = 123;
```

### Clean up old entries
```sql
DELETE FROM daz_retry_queue 
WHERE status IN ('completed', 'dead_letter') 
AND created_at < NOW() - INTERVAL '7 days';
```