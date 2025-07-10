-- Fix Chat Duplicates Script
-- This script removes duplicate chat messages from daz_core_events table
-- and adds a unique constraint to prevent future duplicates

-- Step 1: Show current duplicate count
SELECT 'Before cleanup - Total duplicate groups:' as status, COUNT(*) as count
FROM (
    SELECT channel_name, message_time, username
    FROM daz_core_events
    WHERE event_type = 'cytube.event.chatMsg'
    GROUP BY channel_name, message_time, username
    HAVING COUNT(*) > 1
) as dups;

-- Step 2: Show total rows that will be deleted
SELECT 'Rows to be deleted:' as status, COUNT(*) as count
FROM daz_core_events a
WHERE EXISTS (
    SELECT 1
    FROM daz_core_events b
    WHERE b.id < a.id
    AND a.event_type = b.event_type
    AND a.channel_name = b.channel_name
    AND a.message_time = b.message_time
    AND a.username = b.username
    AND a.event_type = 'cytube.event.chatMsg'
);

-- Step 3: Delete duplicates, keeping only the earliest entry (lowest id)
DELETE FROM daz_core_events a
USING daz_core_events b
WHERE a.id > b.id
  AND a.event_type = b.event_type
  AND a.channel_name = b.channel_name
  AND a.message_time = b.message_time
  AND a.username = b.username
  AND a.event_type = 'cytube.event.chatMsg';

-- Step 4: Verify cleanup
SELECT 'After cleanup - Remaining duplicate groups:' as status, COUNT(*) as count
FROM (
    SELECT channel_name, message_time, username
    FROM daz_core_events
    WHERE event_type = 'cytube.event.chatMsg'
    GROUP BY channel_name, message_time, username
    HAVING COUNT(*) > 1
) as dups;

-- Step 5: Add unique constraint to prevent future duplicates
-- Using a partial unique index for better performance
CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_chat_message
ON daz_core_events (channel_name, message_time, username)
WHERE event_type = 'cytube.event.chatMsg';

-- Step 6: Show final message count
SELECT 'Total chat messages after cleanup:' as status, COUNT(*) as count
FROM daz_core_events
WHERE event_type = 'cytube.event.chatMsg';