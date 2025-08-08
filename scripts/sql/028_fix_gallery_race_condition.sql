-- Fix race condition in add_gallery_image function
-- Adds proper locking to prevent exceeding 25-image limit

CREATE OR REPLACE FUNCTION add_gallery_image(
    p_username VARCHAR(255),
    p_url TEXT,
    p_channel VARCHAR(255),
    p_title TEXT DEFAULT NULL
) RETURNS BIGINT AS $$
DECLARE
    v_image_id BIGINT;
    v_current_count INTEGER;
BEGIN
    -- Use advisory lock to prevent race conditions
    -- Lock is specific to user+channel combination using hash
    PERFORM pg_advisory_xact_lock(
        hashtext('gallery_' || p_username || '_' || p_channel)::bigint
    );
    
    -- Check if URL already exists for this user
    SELECT id INTO v_image_id
    FROM daz_gallery_images
    WHERE username = p_username
      AND url = p_url
      AND channel = p_channel
      AND is_active = TRUE;
    
    -- If image already exists, just return its ID
    IF v_image_id IS NOT NULL THEN
        RETURN v_image_id;
    END IF;
    
    -- Count current active images (no lock on aggregate)
    SELECT COUNT(*) INTO v_current_count
    FROM daz_gallery_images
    WHERE username = p_username
      AND channel = p_channel
      AND is_active = TRUE;
    
    -- If at limit, prune oldest image
    IF v_current_count >= 25 THEN
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
            FOR UPDATE SKIP LOCKED -- Don't wait if locked
        );
    END IF;
    
    -- Insert new image
    INSERT INTO daz_gallery_images (
        username, url, channel, posted_at, image_title,
        original_poster, original_posted_at, most_recent_poster,
        created_at, updated_at
    ) VALUES (
        p_username, p_url, p_channel, NOW(), p_title,
        p_username, NOW(), p_username,
        NOW(), NOW()
    )
    ON CONFLICT (url, channel) DO UPDATE
    SET most_recent_poster = p_username,
        updated_at = NOW()
    RETURNING id INTO v_image_id;
    
    -- Update stats
    INSERT INTO daz_gallery_stats (username, channel, total_images, active_images, last_post_at)
    VALUES (p_username, p_channel, 1, 1, NOW())
    ON CONFLICT (username, channel)
    DO UPDATE SET
        total_images = daz_gallery_stats.total_images + 1,
        active_images = (
            SELECT COUNT(*) FROM daz_gallery_images
            WHERE username = p_username
              AND channel = p_channel
              AND is_active = TRUE
        ),
        last_post_at = NOW(),
        updated_at = NOW();
    
    -- Advisory lock released automatically at transaction end
    RETURN v_image_id;
END;
$$ LANGUAGE plpgsql;

-- Add index to improve lock performance
CREATE INDEX IF NOT EXISTS idx_gallery_user_channel_active 
ON daz_gallery_images(username, channel, is_active)
WHERE is_active = TRUE;