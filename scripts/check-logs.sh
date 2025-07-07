#!/bin/bash
# Check recent chat logs from the database

DB_HOST="${DB_HOST:-localhost}"
DB_NAME="${DB_NAME:-daz}"
DB_USER="${DB_USER:-***REMOVED***}"

echo "=== Recent Chat Messages (last 20) ==="
psql -h "$DB_HOST" -U "$DB_USER" -d "$DB_NAME" -c "
SELECT 
    timestamp,
    username,
    message
FROM daz_core_events
WHERE event_type = 'chatMsg'
    AND message IS NOT NULL
ORDER BY timestamp DESC
LIMIT 20;
"

echo -e "\n=== Chat Activity Summary ==="
psql -h "$DB_HOST" -U "$DB_USER" -d "$DB_NAME" -c "
SELECT 
    DATE(timestamp) as date,
    COUNT(*) as message_count,
    COUNT(DISTINCT username) as unique_users
FROM daz_core_events
WHERE event_type = 'chatMsg'
    AND timestamp > NOW() - INTERVAL '7 days'
GROUP BY DATE(timestamp)
ORDER BY date DESC;
"

echo -e "\n=== Database Size ==="
psql -h "$DB_HOST" -U "$DB_USER" -d "$DB_NAME" -c "
SELECT 
    pg_size_pretty(pg_database_size('$DB_NAME')) as database_size,
    COUNT(*) as total_events
FROM daz_core_events;
"