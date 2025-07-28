-- Migration script to convert existing timestamps to UTC
-- This assumes the server is in PST (UTC-8) timezone

-- First, let's check current data
SELECT 'Current sample data:' as info;
SELECT channel, username, joined_at, last_activity, 
       joined_at AT TIME ZONE 'America/Los_Angeles' AT TIME ZONE 'UTC' as joined_at_utc,
       last_activity AT TIME ZONE 'America/Los_Angeles' AT TIME ZONE 'UTC' as last_activity_utc
FROM daz_user_tracker_sessions 
LIMIT 5;

-- Update sessions table to use UTC
UPDATE daz_user_tracker_sessions
SET 
    joined_at = joined_at AT TIME ZONE 'America/Los_Angeles' AT TIME ZONE 'UTC',
    last_activity = last_activity AT TIME ZONE 'America/Los_Angeles' AT TIME ZONE 'UTC',
    left_at = CASE 
        WHEN left_at IS NOT NULL 
        THEN left_at AT TIME ZONE 'America/Los_Angeles' AT TIME ZONE 'UTC'
        ELSE NULL 
    END;

-- Update history table to use UTC
UPDATE daz_user_tracker_history
SET timestamp = timestamp AT TIME ZONE 'America/Los_Angeles' AT TIME ZONE 'UTC';

-- Verify the changes
SELECT 'After migration:' as info;
SELECT channel, username, joined_at, last_activity
FROM daz_user_tracker_sessions 
LIMIT 5;

-- Count affected rows
SELECT 'Sessions migrated: ' || COUNT(*) as result FROM daz_user_tracker_sessions;
SELECT 'History entries migrated: ' || COUNT(*) as result FROM daz_user_tracker_history;