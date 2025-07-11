-- Retry Queue Schema for Daz
-- This schema provides persistent retry capabilities for failed operations
-- Compatible with PostgreSQL

-- Create enum for retry status
CREATE TYPE retry_status AS ENUM ('pending', 'processing', 'completed', 'failed', 'dead_letter');

-- Main retry queue table
CREATE TABLE IF NOT EXISTS daz_retry_queue (
    -- Primary key
    id BIGSERIAL PRIMARY KEY,
    
    -- Event identification
    event_id UUID NOT NULL DEFAULT gen_random_uuid(),
    event_type VARCHAR(255) NOT NULL,
    channel_name VARCHAR(255),
    plugin_name VARCHAR(255),
    
    -- Event data
    event_data JSONB NOT NULL,
    event_metadata JSONB,
    
    -- Retry configuration
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 3,
    retry_delay_seconds INTEGER NOT NULL DEFAULT 60,
    backoff_multiplier NUMERIC(3,2) NOT NULL DEFAULT 2.0,
    
    -- Status tracking
    status retry_status NOT NULL DEFAULT 'pending',
    error_message TEXT,
    last_error_code VARCHAR(50),
    
    -- Timing
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    next_retry_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_attempted_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT (CURRENT_TIMESTAMP + INTERVAL '7 days'),
    
    -- Processing control
    locked_by VARCHAR(255),
    locked_at TIMESTAMP WITH TIME ZONE,
    
    -- Additional metadata
    priority INTEGER NOT NULL DEFAULT 5 CHECK (priority >= 1 AND priority <= 10),
    correlation_id VARCHAR(255),
    tags TEXT[]
);

-- Indexes for efficient querying
CREATE INDEX idx_retry_queue_status_next_retry 
    ON daz_retry_queue(status, next_retry_at) 
    WHERE status IN ('pending', 'processing');

CREATE INDEX idx_retry_queue_channel_status 
    ON daz_retry_queue(channel_name, status);

CREATE INDEX idx_retry_queue_plugin_status 
    ON daz_retry_queue(plugin_name, status);

CREATE INDEX idx_retry_queue_correlation_id 
    ON daz_retry_queue(correlation_id) 
    WHERE correlation_id IS NOT NULL;

CREATE INDEX idx_retry_queue_event_type 
    ON daz_retry_queue(event_type);

CREATE INDEX idx_retry_queue_created_at 
    ON daz_retry_queue(created_at);

CREATE INDEX idx_retry_queue_priority_status 
    ON daz_retry_queue(priority DESC, status, next_retry_at) 
    WHERE status = 'pending';

CREATE INDEX idx_retry_queue_locked_by 
    ON daz_retry_queue(locked_by, locked_at) 
    WHERE locked_by IS NOT NULL;

CREATE INDEX idx_retry_queue_tags 
    ON daz_retry_queue USING GIN(tags) 
    WHERE tags IS NOT NULL;

-- Partial index for finding expired items
CREATE INDEX idx_retry_queue_expired 
    ON daz_retry_queue(expires_at) 
    WHERE status NOT IN ('completed', 'dead_letter');

-- Function to calculate next retry time with exponential backoff
CREATE OR REPLACE FUNCTION calculate_next_retry(
    retry_count INTEGER,
    retry_delay_seconds INTEGER,
    backoff_multiplier NUMERIC
) RETURNS TIMESTAMP WITH TIME ZONE AS $$
BEGIN
    RETURN CURRENT_TIMESTAMP + 
        (retry_delay_seconds * POWER(backoff_multiplier, retry_count) || ' seconds')::INTERVAL;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- Function to acquire a lock on pending items for processing
CREATE OR REPLACE FUNCTION acquire_retry_items(
    worker_id VARCHAR(255),
    batch_size INTEGER DEFAULT 10,
    lock_timeout_seconds INTEGER DEFAULT 300
) RETURNS TABLE(
    id BIGINT,
    event_id UUID,
    event_type VARCHAR(255),
    event_data JSONB,
    retry_count INTEGER
) AS $$
BEGIN
    -- Release any expired locks from this worker
    UPDATE daz_retry_queue
    SET locked_by = NULL,
        locked_at = NULL,
        status = 'pending'
    WHERE locked_by = worker_id
      AND locked_at < CURRENT_TIMESTAMP - (lock_timeout_seconds || ' seconds')::INTERVAL
      AND status = 'processing';
    
    -- Acquire new items
    RETURN QUERY
    UPDATE daz_retry_queue rq
    SET locked_by = worker_id,
        locked_at = CURRENT_TIMESTAMP,
        status = 'processing'
    FROM (
        SELECT rq_inner.id
        FROM daz_retry_queue rq_inner
        WHERE rq_inner.status = 'pending'
          AND rq_inner.next_retry_at <= CURRENT_TIMESTAMP
          AND rq_inner.locked_by IS NULL
          AND rq_inner.expires_at > CURRENT_TIMESTAMP
        ORDER BY rq_inner.priority DESC, rq_inner.next_retry_at
        LIMIT batch_size
        FOR UPDATE SKIP LOCKED
    ) AS items
    WHERE rq.id = items.id
    RETURNING rq.id, rq.event_id, rq.event_type, rq.event_data, rq.retry_count;
END;
$$ LANGUAGE plpgsql;

-- Function to mark an item as successfully processed
CREATE OR REPLACE FUNCTION complete_retry_item(
    item_id BIGINT,
    worker_id VARCHAR(255)
) RETURNS BOOLEAN AS $$
DECLARE
    rows_updated INTEGER;
BEGIN
    UPDATE daz_retry_queue
    SET status = 'completed',
        completed_at = CURRENT_TIMESTAMP,
        locked_by = NULL,
        locked_at = NULL
    WHERE id = item_id
      AND locked_by = worker_id
      AND status = 'processing';
    
    GET DIAGNOSTICS rows_updated = ROW_COUNT;
    RETURN rows_updated > 0;
END;
$$ LANGUAGE plpgsql;

-- Function to mark an item as failed and schedule retry
CREATE OR REPLACE FUNCTION fail_retry_item(
    item_id BIGINT,
    worker_id VARCHAR(255),
    error_msg TEXT,
    error_code VARCHAR(50) DEFAULT NULL
) RETURNS BOOLEAN AS $$
DECLARE
    current_retry_count INTEGER;
    current_max_retries INTEGER;
    current_delay INTEGER;
    current_multiplier NUMERIC;
    rows_updated INTEGER;
BEGIN
    -- Get current retry configuration
    SELECT retry_count, max_retries, retry_delay_seconds, backoff_multiplier
    INTO current_retry_count, current_max_retries, current_delay, current_multiplier
    FROM daz_retry_queue
    WHERE id = item_id
      AND locked_by = worker_id
      AND status = 'processing';
    
    IF NOT FOUND THEN
        RETURN FALSE;
    END IF;
    
    -- Check if we've exceeded max retries
    IF current_retry_count >= current_max_retries THEN
        -- Move to dead letter
        UPDATE daz_retry_queue
        SET status = 'dead_letter',
            error_message = error_msg,
            last_error_code = error_code,
            last_attempted_at = CURRENT_TIMESTAMP,
            locked_by = NULL,
            locked_at = NULL
        WHERE id = item_id
          AND locked_by = worker_id
          AND status = 'processing';
    ELSE
        -- Schedule retry
        UPDATE daz_retry_queue
        SET status = 'pending',
            retry_count = retry_count + 1,
            error_message = error_msg,
            last_error_code = error_code,
            last_attempted_at = CURRENT_TIMESTAMP,
            next_retry_at = calculate_next_retry(
                current_retry_count + 1, 
                current_delay, 
                current_multiplier
            ),
            locked_by = NULL,
            locked_at = NULL
        WHERE id = item_id
          AND locked_by = worker_id
          AND status = 'processing';
    END IF;
    
    GET DIAGNOSTICS rows_updated = ROW_COUNT;
    RETURN rows_updated > 0;
END;
$$ LANGUAGE plpgsql;

-- View for monitoring retry queue health
CREATE OR REPLACE VIEW daz_retry_queue_stats AS
SELECT 
    status,
    event_type,
    plugin_name,
    COUNT(*) as count,
    AVG(retry_count) as avg_retry_count,
    MAX(retry_count) as max_retry_count,
    MIN(created_at) as oldest_item,
    MAX(created_at) as newest_item,
    COUNT(CASE WHEN locked_by IS NOT NULL THEN 1 END) as locked_count
FROM daz_retry_queue
GROUP BY status, event_type, plugin_name;

-- View for items requiring immediate attention
CREATE OR REPLACE VIEW daz_retry_queue_alerts AS
SELECT 
    id,
    event_id,
    event_type,
    plugin_name,
    channel_name,
    status,
    retry_count,
    max_retries,
    error_message,
    created_at,
    last_attempted_at,
    CASE 
        WHEN status = 'dead_letter' THEN 'Dead letter - manual intervention required'
        WHEN retry_count > max_retries * 0.8 THEN 'High retry count - approaching limit'
        WHEN locked_at < CURRENT_TIMESTAMP - INTERVAL '1 hour' THEN 'Stale lock - possible worker failure'
        WHEN created_at < CURRENT_TIMESTAMP - INTERVAL '1 day' AND status = 'pending' THEN 'Old pending item'
    END as alert_reason
FROM daz_retry_queue
WHERE status = 'dead_letter'
   OR retry_count > max_retries * 0.8
   OR (locked_at IS NOT NULL AND locked_at < CURRENT_TIMESTAMP - INTERVAL '1 hour')
   OR (created_at < CURRENT_TIMESTAMP - INTERVAL '1 day' AND status = 'pending');

-- Table for retry history (optional - for detailed tracking)
CREATE TABLE IF NOT EXISTS daz_retry_history (
    id BIGSERIAL PRIMARY KEY,
    retry_queue_id BIGINT NOT NULL,
    event_id UUID NOT NULL,
    attempt_number INTEGER NOT NULL,
    status VARCHAR(50) NOT NULL,
    error_message TEXT,
    error_code VARCHAR(50),
    worker_id VARCHAR(255),
    attempted_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    duration_ms INTEGER
);

CREATE INDEX idx_retry_history_queue_id ON daz_retry_history(retry_queue_id);
CREATE INDEX idx_retry_history_event_id ON daz_retry_history(event_id);
CREATE INDEX idx_retry_history_attempted_at ON daz_retry_history(attempted_at);

-- Function to clean up old completed/failed items
CREATE OR REPLACE FUNCTION cleanup_retry_queue(
    retention_days INTEGER DEFAULT 7
) RETURNS TABLE(
    deleted_count BIGINT,
    archived_count BIGINT
) AS $$
DECLARE
    del_count BIGINT;
    arch_count BIGINT := 0;
BEGIN
    -- Delete old completed and dead letter items
    DELETE FROM daz_retry_queue
    WHERE (status IN ('completed', 'dead_letter') 
           AND created_at < CURRENT_TIMESTAMP - (retention_days || ' days')::INTERVAL)
       OR (expires_at < CURRENT_TIMESTAMP);
    
    GET DIAGNOSTICS del_count = ROW_COUNT;
    
    -- Also clean up old history
    DELETE FROM daz_retry_history
    WHERE attempted_at < CURRENT_TIMESTAMP - (retention_days * 2 || ' days')::INTERVAL;
    
    RETURN QUERY SELECT del_count, arch_count;
END;
$$ LANGUAGE plpgsql;

-- Partitioning setup for high-volume scenarios (optional)
-- Uncomment and modify if you need partitioning by month

-- CREATE TABLE daz_retry_queue_template (LIKE daz_retry_queue INCLUDING ALL);
-- 
-- -- Example partition for current month
-- CREATE TABLE daz_retry_queue_2025_01 PARTITION OF daz_retry_queue
-- FOR VALUES FROM ('2025-01-01') TO ('2025-02-01');
-- 
-- -- Function to automatically create monthly partitions
-- CREATE OR REPLACE FUNCTION create_monthly_partition(table_name TEXT, start_date DATE)
-- RETURNS void AS $$
-- DECLARE
--     partition_name TEXT;
--     start_date_str TEXT;
--     end_date_str TEXT;
-- BEGIN
--     partition_name := table_name || '_' || to_char(start_date, 'YYYY_MM');
--     start_date_str := to_char(start_date, 'YYYY-MM-DD');
--     end_date_str := to_char(start_date + INTERVAL '1 month', 'YYYY-MM-DD');
--     
--     EXECUTE format('CREATE TABLE IF NOT EXISTS %I PARTITION OF %I FOR VALUES FROM (%L) TO (%L)',
--                    partition_name, table_name, start_date_str, end_date_str);
-- END;
-- $$ LANGUAGE plpgsql;

-- Grant permissions to daz_user
GRANT ALL ON TABLE daz_retry_queue TO daz_user;
GRANT ALL ON TABLE daz_retry_history TO daz_user;
GRANT ALL ON SEQUENCE daz_retry_queue_id_seq TO daz_user;
GRANT ALL ON SEQUENCE daz_retry_history_id_seq TO daz_user;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO daz_user;

-- Comments for documentation
COMMENT ON TABLE daz_retry_queue IS 'Main retry queue for failed operations with exponential backoff support';
COMMENT ON COLUMN daz_retry_queue.event_id IS 'Unique identifier for the event, useful for deduplication';
COMMENT ON COLUMN daz_retry_queue.priority IS 'Priority level 1-10, where 10 is highest priority';
COMMENT ON COLUMN daz_retry_queue.backoff_multiplier IS 'Multiplier for exponential backoff calculation';
COMMENT ON COLUMN daz_retry_queue.dead_letter IS 'Items that have exceeded max retries and require manual intervention';
COMMENT ON FUNCTION acquire_retry_items IS 'Atomically acquire items for processing with automatic lock management';