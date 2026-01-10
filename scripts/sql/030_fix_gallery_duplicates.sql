-- Fix gallery duplicate handling
-- Users should not be able to add the same URL multiple times across any channel
-- But different users can have the same URL

-- First, drop the existing constraints
ALTER TABLE daz_gallery_images DROP CONSTRAINT IF EXISTS daz_gallery_images_url_channel_key;
ALTER TABLE daz_gallery_images DROP CONSTRAINT IF EXISTS unique_user_channel_image;

-- Add new constraint: unique per username+url (regardless of channel)
ALTER TABLE daz_gallery_images 
ADD CONSTRAINT daz_gallery_images_username_url_key UNIQUE (username, url);

-- Update the add_gallery_image function to check for duplicates across all channels
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
    -- Use advisory lock to prevent race conditions
    -- Lock is specific to user using hash
    PERFORM pg_advisory_xact_lock(
        hashtext('gallery_' || p_username)::bigint
    );
    
    -- Check if URL already exists for this user (in ANY channel)
    SELECT id INTO v_image_id
    FROM daz_gallery_images
    WHERE username = p_username
      AND url = p_url
      AND is_active = TRUE;
    
    -- If image already exists for this user, just return its ID
    IF v_image_id IS NOT NULL THEN
        RETURN v_image_id;
    END IF;
    
    -- Count current active images for this user (across all channels)
    SELECT COUNT(*) INTO v_current_count
    FROM daz_gallery_images
    WHERE username = p_username
      AND is_active = TRUE;
    
    -- If at limit (25 images total across all channels), prune oldest image
    IF v_current_count >= p_max_images THEN
        UPDATE daz_gallery_images
        SET is_active = FALSE,
            pruned_reason = 'Gallery limit (25 images) reached',
            updated_at = NOW()
        WHERE id = (
            SELECT id FROM daz_gallery_images
            WHERE username = p_username
              AND is_active = TRUE
            ORDER BY posted_at ASC
            LIMIT 1
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
    ON CONFLICT (username, url) DO UPDATE
    SET most_recent_poster = p_username,
        updated_at = NOW()
    RETURNING id INTO v_image_id;
    
    -- Update stats for the specific channel
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

-- Add index for better performance on the new constraint
CREATE INDEX IF NOT EXISTS idx_gallery_username_url 
ON daz_gallery_images(username, url);