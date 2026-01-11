-- Phase 7: Gallery System
-- Implements automatic image collection, health monitoring, and gallery generation

-- Main gallery images table
CREATE TABLE IF NOT EXISTS daz_gallery_images (
    id BIGSERIAL PRIMARY KEY,
    username VARCHAR(255) NOT NULL,
    url TEXT NOT NULL,
    channel VARCHAR(255) NOT NULL,
    
    -- Timestamps
    posted_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    
    -- Health monitoring
    is_active BOOLEAN DEFAULT TRUE,
    failure_count INTEGER DEFAULT 0,
    first_failure_at TIMESTAMP WITH TIME ZONE,
    last_check_at TIMESTAMP WITH TIME ZONE,
    next_check_at TIMESTAMP WITH TIME ZONE,
    pruned_reason TEXT,
    
    -- Sharing metadata
    original_poster VARCHAR(255),
    original_posted_at TIMESTAMP WITH TIME ZONE,
    most_recent_poster VARCHAR(255),
    
    -- Image metadata
    image_title TEXT,
    image_width INTEGER,
    image_height INTEGER,
    file_size BIGINT,
    
    -- Unique constraint per user/channel
    CONSTRAINT unique_user_channel_image UNIQUE(username, url, channel)
);

-- Gallery locks for user control
CREATE TABLE IF NOT EXISTS daz_gallery_locks (
    username VARCHAR(255) PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    is_locked BOOLEAN DEFAULT FALSE,
    locked_at TIMESTAMP WITH TIME ZONE,
    locked_by VARCHAR(255),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    
    -- Make lock unique per user/channel combo
    CONSTRAINT unique_user_channel_lock UNIQUE(username, channel)
);

-- Gallery statistics table
CREATE TABLE IF NOT EXISTS daz_gallery_stats (
    id BIGSERIAL PRIMARY KEY,
    username VARCHAR(255) NOT NULL,
    channel VARCHAR(255) NOT NULL,
    total_images INTEGER DEFAULT 0,
    active_images INTEGER DEFAULT 0,
    dead_images INTEGER DEFAULT 0,
    images_shared INTEGER DEFAULT 0,
    last_post_at TIMESTAMP WITH TIME ZONE,
    gallery_views INTEGER DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    
    CONSTRAINT unique_user_channel_stats UNIQUE(username, channel)
);

-- Health check history for debugging
CREATE TABLE IF NOT EXISTS daz_gallery_health_log (
    id BIGSERIAL PRIMARY KEY,
    image_id BIGINT REFERENCES daz_gallery_images(id) ON DELETE CASCADE,
    check_time TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    status_code INTEGER,
    response_time_ms INTEGER,
    error_message TEXT,
    previous_status BOOLEAN,
    new_status BOOLEAN
);

-- Create indexes for performance
CREATE INDEX IF NOT EXISTS idx_gallery_username ON daz_gallery_images(username);
CREATE INDEX IF NOT EXISTS idx_gallery_channel ON daz_gallery_images(channel);
CREATE INDEX IF NOT EXISTS idx_gallery_active ON daz_gallery_images(is_active);
CREATE INDEX IF NOT EXISTS idx_gallery_next_check ON daz_gallery_images(next_check_at) WHERE is_active = FALSE;
CREATE INDEX IF NOT EXISTS idx_gallery_posted ON daz_gallery_images(posted_at DESC);
CREATE INDEX IF NOT EXISTS idx_gallery_original_poster ON daz_gallery_images(original_poster) WHERE original_poster IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_gallery_locks_channel ON daz_gallery_locks(channel);
CREATE INDEX IF NOT EXISTS idx_gallery_health_image ON daz_gallery_health_log(image_id, check_time DESC);

-- Function to add image with automatic limit enforcement (25 per user)
CREATE OR REPLACE FUNCTION add_gallery_image(
    p_username VARCHAR(255),
    p_url TEXT,
    p_channel VARCHAR(255),
    p_title TEXT DEFAULT NULL,
    p_max_images INTEGER DEFAULT 25
) RETURNS BIGINT AS $$
DECLARE
    v_image_id BIGINT;
    v_current_count INTEGER;
BEGIN
    -- Check current image count for user
    SELECT COUNT(*) INTO v_current_count
    FROM daz_gallery_images
    WHERE username = p_username
      AND channel = p_channel
      AND is_active = TRUE;
    
    -- If at limit, prune oldest image
    IF v_current_count >= p_max_images THEN
        UPDATE daz_gallery_images
        SET is_active = FALSE,
            pruned_reason = 'Gallery limit (25 images) reached',
            updated_at = NOW()
        WHERE id = (
            SELECT id FROM daz_gallery_images
            WHERE username = p_username
              AND channel = p_channel
              AND is_active = TRUE
            ORDER BY posted_at ASC
            LIMIT 1
        );
    END IF;
    
    -- Insert new image
    INSERT INTO daz_gallery_images (username, url, channel, image_title)
    VALUES (p_username, p_url, p_channel, p_title)
    ON CONFLICT (username, url, channel) 
    DO UPDATE SET 
        updated_at = NOW(),
        most_recent_poster = p_username
    RETURNING id INTO v_image_id;
    
    -- Update stats
    INSERT INTO daz_gallery_stats (username, channel, total_images, active_images, last_post_at)
    VALUES (p_username, p_channel, 1, 1, NOW())
    ON CONFLICT (username, channel)
    DO UPDATE SET
        total_images = daz_gallery_stats.total_images + 1,
        active_images = (
            SELECT COUNT(*) FROM daz_gallery_images
            WHERE username = p_username AND channel = p_channel AND is_active = TRUE
        ),
        last_post_at = NOW(),
        updated_at = NOW();
    
    RETURN v_image_id;
END;
$$ LANGUAGE plpgsql;

-- Function to mark image for health check
CREATE OR REPLACE FUNCTION mark_image_for_health_check(
    p_image_id BIGINT,
    p_failed BOOLEAN,
    p_error_message TEXT DEFAULT NULL
) RETURNS VOID AS $$
BEGIN
    IF p_failed THEN
        UPDATE daz_gallery_images
        SET failure_count = failure_count + 1,
            first_failure_at = COALESCE(first_failure_at, NOW()),
            last_check_at = NOW(),
            next_check_at = NOW() + INTERVAL '3 hours' * POWER(2, LEAST(failure_count, 4)),
            updated_at = NOW()
        WHERE id = p_image_id;
        
        -- Mark as inactive after 3 failures
        UPDATE daz_gallery_images
        SET is_active = FALSE,
            pruned_reason = COALESCE(p_error_message, 'Failed 3 health checks'),
            updated_at = NOW()
        WHERE id = p_image_id
          AND failure_count >= 3;
    ELSE
        -- Image is healthy, reset failure count
        UPDATE daz_gallery_images
        SET failure_count = 0,
            first_failure_at = NULL,
            last_check_at = NOW(),
            next_check_at = NULL,
            is_active = TRUE,
            pruned_reason = NULL,
            updated_at = NOW()
        WHERE id = p_image_id;
    END IF;
    
    -- Log health check
    INSERT INTO daz_gallery_health_log (image_id, status_code, error_message, previous_status, new_status)
    SELECT 
        p_image_id,
        CASE WHEN p_failed THEN 0 ELSE 200 END,
        p_error_message,
        is_active,
        CASE WHEN p_failed AND failure_count >= 2 THEN FALSE ELSE is_active END
    FROM daz_gallery_images
    WHERE id = p_image_id;
    
    -- Update stats
    UPDATE daz_gallery_stats
    SET active_images = (
            SELECT COUNT(*) FROM daz_gallery_images
            WHERE username = daz_gallery_stats.username 
              AND channel = daz_gallery_stats.channel 
              AND is_active = TRUE
        ),
        dead_images = (
            SELECT COUNT(*) FROM daz_gallery_images
            WHERE username = daz_gallery_stats.username 
              AND channel = daz_gallery_stats.channel 
              AND is_active = FALSE
        ),
        updated_at = NOW()
    WHERE (username, channel) IN (
        SELECT username, channel FROM daz_gallery_images WHERE id = p_image_id
    );
END;
$$ LANGUAGE plpgsql;

-- Function to get images needing health check
CREATE OR REPLACE FUNCTION get_images_for_health_check(
    p_limit INTEGER DEFAULT 50
) RETURNS TABLE (
    id BIGINT,
    url TEXT,
    failure_count INTEGER
) AS $$
BEGIN
    RETURN QUERY
    SELECT 
        gi.id,
        gi.url,
        gi.failure_count
    FROM daz_gallery_images gi
    WHERE (
        -- Active images not checked in last hour
        (gi.is_active = TRUE AND (gi.last_check_at IS NULL OR gi.last_check_at < NOW() - INTERVAL '1 hour'))
        OR
        -- Inactive images due for recheck
        (gi.is_active = FALSE AND gi.next_check_at IS NOT NULL AND gi.next_check_at <= NOW())
    )
    ORDER BY 
        gi.is_active DESC,  -- Check active images first
        gi.last_check_at ASC NULLS FIRST
    LIMIT p_limit;
END;
$$ LANGUAGE plpgsql;

-- Function to restore dead image if space available
CREATE OR REPLACE FUNCTION restore_dead_image(
    p_image_id BIGINT,
    p_max_images INTEGER DEFAULT 25
) RETURNS BOOLEAN AS $$
DECLARE
    v_username VARCHAR(255);
    v_channel VARCHAR(255);
    v_current_count INTEGER;
BEGIN
    -- Get image details
    SELECT username, channel INTO v_username, v_channel
    FROM daz_gallery_images
    WHERE id = p_image_id;
    
    -- Check current active count
    SELECT COUNT(*) INTO v_current_count
    FROM daz_gallery_images
    WHERE username = v_username
      AND channel = v_channel
      AND is_active = TRUE;
    
    -- Only restore if under limit
    IF v_current_count < p_max_images THEN
        UPDATE daz_gallery_images
        SET is_active = TRUE,
            failure_count = 0,
            first_failure_at = NULL,
            pruned_reason = NULL,
            updated_at = NOW()
        WHERE id = p_image_id;
        
        RETURN TRUE;
    END IF;
    
    RETURN FALSE;
END;
$$ LANGUAGE plpgsql;

-- View for gallery display
CREATE OR REPLACE VIEW daz_gallery_view AS
SELECT 
    gi.id,
    gi.username,
    gi.url,
    gi.channel,
    gi.posted_at,
    gi.image_title,
    gi.is_active,
    gi.original_poster,
    CASE 
        WHEN gi.original_poster IS NOT NULL AND gi.original_poster != gi.username 
        THEN gi.original_poster
        ELSE NULL
    END as shared_from,
    gl.is_locked,
    gs.total_images,
    gs.active_images
FROM daz_gallery_images gi
LEFT JOIN daz_gallery_locks gl ON gi.username = gl.username AND gi.channel = gl.channel
LEFT JOIN daz_gallery_stats gs ON gi.username = gs.username AND gi.channel = gs.channel
WHERE gi.is_active = TRUE
ORDER BY gi.posted_at DESC;

-- Grant permissions
GRANT SELECT, INSERT, UPDATE, DELETE ON daz_gallery_images TO daz_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON daz_gallery_locks TO daz_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON daz_gallery_stats TO daz_user;
GRANT SELECT, INSERT ON daz_gallery_health_log TO daz_user;
GRANT SELECT ON daz_gallery_view TO daz_user;
GRANT USAGE ON SEQUENCE daz_gallery_images_id_seq TO daz_user;
GRANT USAGE ON SEQUENCE daz_gallery_stats_id_seq TO daz_user;
GRANT USAGE ON SEQUENCE daz_gallery_health_log_id_seq TO daz_user;
GRANT EXECUTE ON FUNCTION add_gallery_image(VARCHAR, TEXT, VARCHAR, TEXT, INTEGER) TO daz_user;
GRANT EXECUTE ON FUNCTION mark_image_for_health_check(BIGINT, BOOLEAN, TEXT) TO daz_user;
GRANT EXECUTE ON FUNCTION get_images_for_health_check(INTEGER) TO daz_user;
GRANT EXECUTE ON FUNCTION restore_dead_image(BIGINT, INTEGER) TO daz_user;
