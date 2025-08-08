-- Add stored procedure for exponential backoff calculation
-- This simplifies the recovery logic and centralizes the backoff algorithm

-- Function to calculate next check time with exponential backoff
CREATE OR REPLACE FUNCTION calculate_next_check_time(p_first_failure_at TIMESTAMP)
RETURNS INTERVAL AS $$
DECLARE
    v_time_since_failure NUMERIC;
BEGIN
    -- Calculate seconds since first failure
    v_time_since_failure := EXTRACT(EPOCH FROM (NOW() - COALESCE(p_first_failure_at, NOW())));
    
    -- Exponential backoff: 3h, 6h, 12h, 24h, 48h (max)
    IF v_time_since_failure < 10800 THEN  -- < 3 hours
        RETURN INTERVAL '3 hours';
    ELSIF v_time_since_failure < 21600 THEN  -- < 6 hours
        RETURN INTERVAL '6 hours';
    ELSIF v_time_since_failure < 43200 THEN  -- < 12 hours
        RETURN INTERVAL '12 hours';
    ELSIF v_time_since_failure < 86400 THEN  -- < 24 hours
        RETURN INTERVAL '24 hours';
    ELSE  -- >= 24 hours
        RETURN INTERVAL '48 hours';
    END IF;
END;
$$ LANGUAGE plpgsql IMMUTABLE;

-- Update the recovery tracking for dead images
CREATE OR REPLACE FUNCTION update_dead_image_recovery(p_image_id BIGINT)
RETURNS VOID AS $$
BEGIN
    UPDATE daz_gallery_images 
    SET next_check_at = NOW() + calculate_next_check_time(first_failure_at),
        last_check_at = NOW()
    WHERE id = p_image_id
      AND is_active = FALSE;
END;
$$ LANGUAGE plpgsql;

-- Add index to improve recovery query performance
CREATE INDEX IF NOT EXISTS idx_gallery_recovery_check 
ON daz_gallery_images(is_active, next_check_at)
WHERE is_active = FALSE AND pruned_reason IS NOT NULL;