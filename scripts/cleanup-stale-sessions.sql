-- Cleanup script for stale user sessions
-- This script ensures only one active session per user per channel

-- First, identify users with multiple active sessions
WITH duplicate_sessions AS (
    SELECT channel, username, COUNT(*) as active_count
    FROM daz_user_tracker_sessions
    WHERE is_active = TRUE
    GROUP BY channel, username
    HAVING COUNT(*) > 1
),
-- For each user with duplicates, keep only the most recent session
sessions_to_deactivate AS (
    SELECT s.id
    FROM daz_user_tracker_sessions s
    INNER JOIN duplicate_sessions d 
        ON s.channel = d.channel 
        AND s.username = d.username
    WHERE s.is_active = TRUE
    AND s.id NOT IN (
        -- Keep the most recent session for each user
        SELECT id
        FROM daz_user_tracker_sessions s2
        WHERE s2.channel = s.channel 
        AND s2.username = s.username
        AND s2.is_active = TRUE
        ORDER BY s2.joined_at DESC
        LIMIT 1
    )
)
-- Deactivate all but the most recent session
UPDATE daz_user_tracker_sessions
SET is_active = FALSE, 
    left_at = CASE 
        WHEN left_at IS NULL THEN last_activity 
        ELSE left_at 
    END
WHERE id IN (SELECT id FROM sessions_to_deactivate);

-- Report results
SELECT 'Cleaned up duplicate sessions' as status;