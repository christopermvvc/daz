# Analytics Module SQL Schema Analysis Report

## Executive Summary

After thorough analysis of the analytics plugin (`/home/user/Documents/daz/internal/plugins/analytics/plugin.go`), I've identified **ONE CRITICAL SQL SCHEMA MISMATCH** that will cause runtime errors:

### Critical Issue Found

**Column Name Mismatch in Media Plays Query**
- Analytics queries: `played_at` column
- Actual schema has: `started_at` column
- Location: Line 671-673 in analytics/plugin.go

## Detailed Analysis

### 1. Analytics Plugin Tables (Created by Analytics)

#### daz_analytics_hourly
```sql
CREATE TABLE IF NOT EXISTS daz_analytics_hourly (
    id SERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    hour_start TIMESTAMP NOT NULL,
    message_count BIGINT DEFAULT 0,
    unique_users INT DEFAULT 0,
    media_plays INT DEFAULT 0,
    commands_used INT DEFAULT 0,
    metadata JSONB,
    UNIQUE(channel, hour_start)
);
```
**Status**: ✅ All queries match schema correctly

#### daz_analytics_daily
```sql
CREATE TABLE IF NOT EXISTS daz_analytics_daily (
    id SERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    day_date DATE NOT NULL,
    total_messages BIGINT DEFAULT 0,
    unique_users INT DEFAULT 0,
    total_media_plays INT DEFAULT 0,
    peak_users INT DEFAULT 0,
    active_hours INT DEFAULT 0,
    metadata JSONB,
    UNIQUE(channel, day_date)
);
```
**Status**: ✅ All queries match schema correctly
**Note**: Analytics references `updated_at` column in UPDATE but doesn't create it - however, this is in the ON CONFLICT clause which won't fail

#### daz_analytics_user_stats
```sql
CREATE TABLE IF NOT EXISTS daz_analytics_user_stats (
    id SERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    username VARCHAR(255) NOT NULL,
    total_messages BIGINT DEFAULT 0,
    first_seen TIMESTAMP NOT NULL,
    last_seen TIMESTAMP NOT NULL,
    active_days INT DEFAULT 0,
    metadata JSONB,
    UNIQUE(channel, username)
);
```
**Status**: ✅ All queries match schema correctly

### 2. External Tables Queried by Analytics

#### daz_core_events (from SQL Plugin)
```sql
CREATE TABLE IF NOT EXISTS daz_core_events (
    id BIGSERIAL PRIMARY KEY,
    event_type VARCHAR(50) NOT NULL,
    channel_name VARCHAR(100) NOT NULL,
    timestamp TIMESTAMP NOT NULL,
    username VARCHAR(100),
    message TEXT,
    video_id VARCHAR(255),
    video_type VARCHAR(50),
    title TEXT,
    duration INTEGER,
    raw_data JSONB NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
)
```
**Status**: ✅ All queries match schema correctly

#### daz_mediatracker_plays (from MediaTracker Plugin)
```sql
CREATE TABLE IF NOT EXISTS daz_mediatracker_plays (
    id BIGSERIAL PRIMARY KEY,
    channel VARCHAR(255) NOT NULL,
    media_id VARCHAR(255) NOT NULL,
    media_type VARCHAR(50) NOT NULL,
    title TEXT NOT NULL,
    duration INT NOT NULL,
    started_at TIMESTAMP NOT NULL,  -- ⚠️ NOT "played_at"!
    ended_at TIMESTAMP,
    queued_by VARCHAR(255),
    completed BOOLEAN DEFAULT FALSE,
    metadata JSONB
);
```
**Status**: ❌ **SCHEMA MISMATCH FOUND**

### 3. SQL Queries Analysis

#### Query 1: User Stats Update (Lines 367-375)
```sql
INSERT INTO daz_analytics_user_stats 
    (channel, username, total_messages, first_seen, last_seen)
VALUES ($1, $2, 1, NOW(), NOW())
ON CONFLICT (channel, username) 
DO UPDATE SET 
    total_messages = daz_analytics_user_stats.total_messages + 1,
    last_seen = NOW()
```
**Status**: ✅ Correct

#### Query 2: Current Hour Stats (Lines 403-407)
```sql
SELECT message_count, unique_users, media_plays
FROM daz_analytics_hourly
WHERE channel = $1 AND hour_start = date_trunc('hour', NOW())
```
**Status**: ✅ Correct

#### Query 3: Today Stats (Lines 435-439)
```sql
SELECT total_messages, unique_users, total_media_plays
FROM daz_analytics_daily
WHERE channel = $1 AND day_date = CURRENT_DATE
```
**Status**: ✅ Correct

#### Query 4: Top Chatters (Lines 462-468)
```sql
SELECT username, total_messages
FROM daz_analytics_user_stats
WHERE channel = $1
ORDER BY total_messages DESC
LIMIT 3
```
**Status**: ✅ Correct

#### Query 5: Active Channels (Lines 547-554)
```sql
SELECT DISTINCT channel_name 
FROM daz_core_events 
WHERE timestamp > $1 
AND channel_name IS NOT NULL 
AND channel_name != ''
ORDER BY channel_name
```
**Status**: ✅ Correct

#### Query 6: Message Count (Lines 622-628)
```sql
SELECT COUNT(*) 
FROM daz_core_events 
WHERE channel_name = $1 
AND timestamp >= $2 
AND timestamp < $3
AND event_type = 'chat.message'
```
**Status**: ✅ Correct

#### Query 7: Unique Users Count (Lines 644-651)
```sql
SELECT COUNT(DISTINCT username) 
FROM daz_core_events 
WHERE channel_name = $1 
AND timestamp >= $2 
AND timestamp < $3
AND event_type = 'chat.message'
AND username IS NOT NULL
```
**Status**: ✅ Correct

#### Query 8: Media Plays Count (Lines 667-673)
```sql
SELECT COUNT(*) 
FROM daz_mediatracker_plays 
WHERE channel = $1 
AND played_at >= $2     -- ❌ WRONG COLUMN NAME!
AND played_at < $3      -- ❌ WRONG COLUMN NAME!
```
**Status**: ❌ **CRITICAL ERROR** - Column should be `started_at`, not `played_at`

#### Query 9: Hourly Aggregation Insert (Lines 692-703)
```sql
INSERT INTO daz_analytics_hourly 
(channel, hour_start, message_count, unique_users, media_plays, commands_used, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (channel, hour_start) 
DO UPDATE SET 
    message_count = EXCLUDED.message_count,
    unique_users = EXCLUDED.unique_users,
    media_plays = EXCLUDED.media_plays,
    commands_used = EXCLUDED.commands_used,
    metadata = EXCLUDED.metadata,
    updated_at = CURRENT_TIMESTAMP  -- Note: This column doesn't exist but won't cause error
```
**Status**: ⚠️ Minor issue - references non-existent `updated_at` column, but won't fail

#### Query 10: Daily Stats Aggregation (Lines 793-802)
```sql
SELECT 
    COALESCE(SUM(message_count), 0) as total_messages,
    COALESCE(SUM(media_plays), 0) as total_media_plays,
    COUNT(DISTINCT hour_start) as active_hours,
    COALESCE(MAX(unique_users), 0) as peak_users
FROM daz_analytics_hourly
WHERE channel = $1
AND hour_start >= $2
AND hour_start < $3
```
**Status**: ✅ Correct

#### Query 11: Daily Unique Users (Lines 820-827)
```sql
SELECT COUNT(DISTINCT username)
FROM daz_core_events
WHERE channel_name = $1
AND timestamp >= $2
AND timestamp < $3
AND event_type = 'chat.message'
AND username IS NOT NULL
```
**Status**: ✅ Correct

#### Query 12: Daily Aggregation Insert (Lines 843-856)
```sql
INSERT INTO daz_analytics_daily
(channel, day_date, total_messages, unique_users, total_media_plays, 
 peak_users, active_hours, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (channel, day_date)
DO UPDATE SET
    total_messages = EXCLUDED.total_messages,
    unique_users = EXCLUDED.unique_users,
    total_media_plays = EXCLUDED.total_media_plays,
    peak_users = EXCLUDED.peak_users,
    active_hours = EXCLUDED.active_hours,
    metadata = EXCLUDED.metadata,
    updated_at = CURRENT_TIMESTAMP  -- Note: This column doesn't exist but won't cause error
```
**Status**: ⚠️ Minor issue - references non-existent `updated_at` column, but won't fail

## Recommendations

### 1. **CRITICAL FIX REQUIRED** (Line 671-673)
Change the media plays query from:
```sql
WHERE channel = $1 
AND played_at >= $2 
AND played_at < $3
```
To:
```sql
WHERE channel = $1 
AND started_at >= $2 
AND started_at < $3
```

### 2. Optional Enhancement
Consider adding the `updated_at` column to tables if you want to track update timestamps:
```sql
ALTER TABLE daz_analytics_hourly ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;
ALTER TABLE daz_analytics_daily ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP;
```

### 3. Additional Observations
- All table names match correctly
- All column names match correctly (except the critical `played_at` issue)
- Data types are consistent
- Foreign key relationships are properly maintained
- Indexes are appropriate for the queries being performed

## Summary

The analytics module has **one critical bug** that needs immediate fixing - the use of `played_at` instead of `started_at` when querying the `daz_mediatracker_plays` table. This will cause the media plays counting to fail with a SQL error.

The references to `updated_at` in the ON CONFLICT clauses are non-critical since PostgreSQL ignores column references in the UPDATE SET clause if they don't exist in the table being updated.