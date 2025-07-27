-- Phase 6: Retry Persistence & Resilience
-- This implements persistent retry queuing for failed operations

-- Create retry queue table
CREATE TABLE IF NOT EXISTS daz_retry_queue (
    id BIGSERIAL PRIMARY KEY,
    correlation_id VARCHAR(255) UNIQUE NOT NULL,
    plugin_name VARCHAR(100) NOT NULL,
    operation_type VARCHAR(100) NOT NULL,
    event_type VARCHAR(255) NOT NULL,
    event_data JSONB NOT NULL,
    max_retries INT NOT NULL DEFAULT 3,
    retry_count INT NOT NULL DEFAULT 0,
    retry_after TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    last_error TEXT,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    priority INT NOT NULL DEFAULT 0,
    timeout_seconds INT NOT NULL DEFAULT 30,
    CONSTRAINT valid_status CHECK (status IN ('pending', 'processing', 'completed', 'failed', 'dead_letter'))
);

-- Create indexes for efficient querying
CREATE INDEX IF NOT EXISTS idx_retry_queue_status_retry_after 
    ON daz_retry_queue(status, retry_after) 
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_retry_queue_plugin_name 
    ON daz_retry_queue(plugin_name);

CREATE INDEX IF NOT EXISTS idx_retry_queue_operation_type 
    ON daz_retry_queue(operation_type);

CREATE INDEX IF NOT EXISTS idx_retry_queue_created_at 
    ON daz_retry_queue(created_at);

-- Function to acquire a batch of retry items atomically
CREATE OR REPLACE FUNCTION acquire_retry_batch(batch_size INT)
RETURNS TABLE (
    id BIGINT,
    correlation_id VARCHAR(255),
    plugin_name VARCHAR(100),
    operation_type VARCHAR(100),
    event_type VARCHAR(255),
    event_data JSONB,
    max_retries INT,
    retry_count INT,
    retry_after TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE,
    updated_at TIMESTAMP WITH TIME ZONE,
    last_error TEXT,
    status VARCHAR(50),
    priority INT,
    timeout_seconds INT
) AS $$
BEGIN
    RETURN QUERY
    WITH batch AS (
        SELECT r.id
        FROM daz_retry_queue r
        WHERE r.status = 'pending'
          AND r.retry_after <= NOW()
        ORDER BY r.priority DESC, r.retry_after ASC
        LIMIT batch_size
        FOR UPDATE SKIP LOCKED
    )
    UPDATE daz_retry_queue r
    SET status = 'processing',
        updated_at = NOW()
    FROM batch
    WHERE r.id = batch.id
    RETURNING r.*;
END;
$$ LANGUAGE plpgsql;

-- Function to mark a retry as completed
CREATE OR REPLACE FUNCTION complete_retry(retry_id BIGINT)
RETURNS VOID AS $$
BEGIN
    UPDATE daz_retry_queue
    SET status = 'completed',
        updated_at = NOW()
    WHERE id = retry_id;
END;
$$ LANGUAGE plpgsql;

-- Function to mark a retry as failed
CREATE OR REPLACE FUNCTION fail_retry(retry_id BIGINT, error_msg TEXT)
RETURNS VOID AS $$
BEGIN
    UPDATE daz_retry_queue
    SET status = 'failed',
        last_error = error_msg,
        updated_at = NOW()
    WHERE id = retry_id;
END;
$$ LANGUAGE plpgsql;

-- Function to clean up old retry records
CREATE OR REPLACE FUNCTION cleanup_old_retries(retention_days INT DEFAULT 7)
RETURNS INT AS $$
DECLARE
    deleted_count INT;
BEGIN
    DELETE FROM daz_retry_queue
    WHERE status IN ('completed', 'failed', 'dead_letter')
      AND updated_at < NOW() - (retention_days || ' days')::INTERVAL;
    
    GET DIAGNOSTICS deleted_count = ROW_COUNT;
    RETURN deleted_count;
END;
$$ LANGUAGE plpgsql;

-- Create a view for retry queue statistics
CREATE OR REPLACE VIEW daz_retry_queue_stats AS
SELECT 
    plugin_name,
    operation_type,
    status,
    COUNT(*) as count,
    AVG(retry_count) as avg_retry_count,
    MAX(retry_count) as max_retry_count,
    MIN(created_at) as oldest_entry,
    MAX(updated_at) as latest_update
FROM daz_retry_queue
GROUP BY plugin_name, operation_type, status;

-- Add retry statistics to plugin_stats
ALTER TABLE daz_plugin_stats 
ADD COLUMN IF NOT EXISTS retries_scheduled BIGINT DEFAULT 0,
ADD COLUMN IF NOT EXISTS retries_succeeded BIGINT DEFAULT 0,
ADD COLUMN IF NOT EXISTS retries_failed BIGINT DEFAULT 0;

-- Function to update retry statistics
CREATE OR REPLACE FUNCTION update_retry_stats()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        -- New retry scheduled
        UPDATE daz_plugin_stats
        SET retries_scheduled = retries_scheduled + 1,
            updated_at = NOW()
        WHERE plugin_name = NEW.plugin_name;
    ELSIF TG_OP = 'UPDATE' THEN
        -- Check if status changed
        IF OLD.status != NEW.status THEN
            IF NEW.status = 'completed' THEN
                UPDATE daz_plugin_stats
                SET retries_succeeded = retries_succeeded + 1,
                    updated_at = NOW()
                WHERE plugin_name = NEW.plugin_name;
            ELSIF NEW.status = 'failed' OR NEW.status = 'dead_letter' THEN
                UPDATE daz_plugin_stats
                SET retries_failed = retries_failed + 1,
                    updated_at = NOW()
                WHERE plugin_name = NEW.plugin_name;
            END IF;
        END IF;
    END IF;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger for retry statistics
CREATE TRIGGER update_retry_stats_trigger
AFTER INSERT OR UPDATE ON daz_retry_queue
FOR EACH ROW
EXECUTE FUNCTION update_retry_stats();

-- Grant permissions
GRANT SELECT, INSERT, UPDATE ON daz_retry_queue TO daz_user;
GRANT USAGE ON SEQUENCE daz_retry_queue_id_seq TO daz_user;
GRANT SELECT ON daz_retry_queue_stats TO daz_user;