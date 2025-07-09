-- Check if playlist data is in the database

-- First, check if we have any data in the mediatracker tables
SELECT 'Queue Table Count:' as info, COUNT(*) as count FROM daz_mediatracker_queue
UNION ALL
SELECT 'Library Table Count:', COUNT(*) FROM daz_mediatracker_library
UNION ALL
SELECT 'Plays Table Count:', COUNT(*) FROM daz_mediatracker_plays;

-- Show the current queue/playlist
SELECT 
    position,
    media_id,
    media_type,
    title,
    duration,
    queued_by,
    to_timestamp(queued_at) as queued_time
FROM daz_mediatracker_queue
ORDER BY position
LIMIT 20;

-- Show some items from the library
SELECT 
    id,
    url,
    title,
    media_type,
    media_id,
    duration,
    play_count,
    to_timestamp(first_seen) as first_seen_time,
    to_timestamp(last_played) as last_played_time
FROM daz_mediatracker_library
ORDER BY last_played DESC NULLS LAST
LIMIT 10;