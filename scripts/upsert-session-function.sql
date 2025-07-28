-- Create a function to handle session upsert atomically
-- This prevents race conditions between checking for existing sessions and creating new ones

CREATE OR REPLACE FUNCTION upsert_user_session(
    p_channel VARCHAR(255),
    p_username VARCHAR(255),
    p_rank INT,
    p_now TIMESTAMP
) RETURNS TABLE (
    session_id INT,
    was_reactivated BOOLEAN
) AS $$
DECLARE
    v_session_id INT;
    v_was_reactivated BOOLEAN := FALSE;
BEGIN
    -- First try to reactivate the most recent inactive session
    UPDATE daz_user_tracker_sessions 
    SET is_active = TRUE, 
        left_at = NULL,
        last_activity = p_now,
        rank = p_rank
    WHERE id = (
        SELECT id FROM daz_user_tracker_sessions
        WHERE channel = p_channel 
          AND username = p_username
          AND is_active = FALSE
        ORDER BY joined_at DESC
        LIMIT 1
    )
    RETURNING id INTO v_session_id;
    
    -- If we found and reactivated a session, we're done
    IF FOUND THEN
        v_was_reactivated := TRUE;
        RETURN QUERY SELECT v_session_id, v_was_reactivated;
        RETURN;
    END IF;
    
    -- No existing inactive session found, ensure no active sessions exist
    UPDATE daz_user_tracker_sessions 
    SET is_active = FALSE
    WHERE channel = p_channel 
      AND username = p_username 
      AND is_active = TRUE;
    
    -- Create a new session
    INSERT INTO daz_user_tracker_sessions 
        (channel, username, rank, joined_at, last_activity, is_active)
    VALUES (p_channel, p_username, p_rank, p_now, p_now, TRUE)
    ON CONFLICT (channel, username, joined_at) 
    DO UPDATE SET 
        rank = EXCLUDED.rank,
        last_activity = EXCLUDED.last_activity,
        is_active = TRUE,
        left_at = NULL
    RETURNING id INTO v_session_id;
    
    RETURN QUERY SELECT v_session_id, v_was_reactivated;
END;
$$ LANGUAGE plpgsql;

-- Grant execute permission to the database user
-- Replace 'daz_user' with your actual database user if different
-- GRANT EXECUTE ON FUNCTION upsert_user_session TO daz_user;