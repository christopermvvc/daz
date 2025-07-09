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
    queued_at
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
    first_seen,
    last_played
FROM daz_mediatracker_library
ORDER BY id DESC
LIMIT 10;

-- Check if mediatracker received any playlist events
SELECT 
    event_type,
    event_time,
    video_id,
    video_title
FROM daz_core_events 
WHERE event_type LIKE '%playlist%' 
   OR event_type = 'changeMedia'
ORDER BY event_time DESC 
LIMIT 20;