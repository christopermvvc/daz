-- Rollback script for Retry Queue Schema
-- This script removes all retry queue related objects from the database
-- Run this if you need to completely remove the retry queue functionality

-- Drop views first (they depend on tables)
DROP VIEW IF EXISTS daz_retry_queue_alerts CASCADE;
DROP VIEW IF EXISTS daz_retry_queue_stats CASCADE;

-- Drop functions
DROP FUNCTION IF EXISTS cleanup_retry_queue(INTEGER) CASCADE;
DROP FUNCTION IF EXISTS fail_retry_item(BIGINT, VARCHAR, TEXT, VARCHAR) CASCADE;
DROP FUNCTION IF EXISTS complete_retry_item(BIGINT, VARCHAR) CASCADE;
DROP FUNCTION IF EXISTS acquire_retry_items(VARCHAR, INTEGER, INTEGER) CASCADE;
DROP FUNCTION IF EXISTS calculate_next_retry(INTEGER, INTEGER, NUMERIC) CASCADE;

-- Drop tables
DROP TABLE IF EXISTS daz_retry_history CASCADE;
DROP TABLE IF EXISTS daz_retry_queue CASCADE;

-- Drop custom types
DROP TYPE IF EXISTS retry_status CASCADE;

-- Note: This will remove all retry queue data permanently
-- Make sure to backup any important data before running this script