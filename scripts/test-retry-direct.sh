#!/bin/bash

# Direct test of retry mechanism by inserting test entries into the retry queue

set -e

echo "=== Direct Retry Mechanism Test ==="
echo

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
DB_NAME="daz"
DB_USER="***REMOVED***"
DB_PASSWORD="***REMOVED***"

# Function to print colored output
print_status() {
    echo -e "${GREEN}[STATUS]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_info() {
    echo -e "${YELLOW}[INFO]${NC} $1"
}

# Function to execute SQL query
exec_sql() {
    PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -t -c "$1" 2>/dev/null | tr -d ' \n'
}

# Function to execute SQL query with output
exec_sql_with_output() {
    PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -c "$1"
}

# Step 1: Apply retry queue schema
print_status "Applying retry queue schema..."
if [ -f "./scripts/sql/025_retry_persistence.sql" ]; then
    PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -f "./scripts/sql/025_retry_persistence.sql" >/dev/null 2>&1
    print_status "✅ Retry queue schema applied"
else
    print_error "Retry queue schema file not found!"
    exit 1
fi

# Step 2: Clean up old test entries
print_status "Cleaning up old test entries..."
exec_sql "DELETE FROM daz_retry_queue WHERE correlation_id LIKE 'direct-test-%'" >/dev/null 2>&1 || true

# Step 3: Insert test retry entries directly
print_status "Inserting test retry entries..."

# Test 1: Entry that should be ready for immediate retry
exec_sql "INSERT INTO daz_retry_queue (correlation_id, plugin_name, operation_type, event_type, event_data, max_retries, retry_after, priority, timeout_seconds, status)
VALUES ('direct-test-1', 'sql', 'sql_operation', 'sql.query', '{\"query\": \"SELECT 1\", \"params\": []}'::jsonb, 3, NOW() - INTERVAL '1 minute', 10, 5, 'pending')" >/dev/null

# Test 2: Entry that should wait before retry
exec_sql "INSERT INTO daz_retry_queue (correlation_id, plugin_name, operation_type, event_type, event_data, max_retries, retry_after, priority, timeout_seconds, status)
VALUES ('direct-test-2', 'eventfilter', 'command_routing', 'command.execute', '{\"command\": \"test\"}'::jsonb, 2, NOW() + INTERVAL '2 minutes', 5, 3, 'pending')" >/dev/null

# Test 3: Entry that has already failed once
exec_sql "INSERT INTO daz_retry_queue (correlation_id, plugin_name, operation_type, event_type, event_data, max_retries, retry_count, retry_after, priority, timeout_seconds, status, last_error)
VALUES ('direct-test-3', 'sql', 'database_query', 'sql.exec', '{\"query\": \"INSERT INTO test\"}'::jsonb, 3, 1, NOW() - INTERVAL '30 seconds', 8, 10, 'pending', 'connection timeout')" >/dev/null

print_status "✅ Inserted 3 test retry entries"

# Step 4: Query the retry queue
print_info "Current retry queue status:"
exec_sql_with_output "SELECT correlation_id, plugin_name, operation_type, retry_count, status, 
                      retry_after < NOW() as ready_for_retry,
                      EXTRACT(EPOCH FROM (retry_after - NOW())) as seconds_until_retry
               FROM daz_retry_queue 
               WHERE correlation_id LIKE 'direct-test-%'
               ORDER BY priority DESC, retry_after ASC"

# Step 5: Test the acquire_retry_batch function
print_status "Testing acquire_retry_batch function..."
exec_sql_with_output "SELECT correlation_id, status, retry_count FROM acquire_retry_batch(5) WHERE correlation_id LIKE 'direct-test-%'"

# Step 6: Check which entries were marked as processing
print_info "After acquiring batch:"
exec_sql_with_output "SELECT correlation_id, status, retry_count
               FROM daz_retry_queue 
               WHERE correlation_id LIKE 'direct-test-%'
               ORDER BY correlation_id"

# Step 7: Simulate completion of one entry
print_status "Simulating successful retry completion..."
exec_sql "SELECT complete_retry((SELECT id FROM daz_retry_queue WHERE correlation_id = 'direct-test-1' LIMIT 1))" >/dev/null

# Step 8: Simulate failure of another entry
print_status "Simulating retry failure..."
exec_sql "SELECT fail_retry((SELECT id FROM daz_retry_queue WHERE correlation_id = 'direct-test-3' LIMIT 1), 'Simulated failure for testing')" >/dev/null

# Step 9: Final status
print_info "Final retry queue status:"
exec_sql_with_output "SELECT correlation_id, status, retry_count, last_error
               FROM daz_retry_queue 
               WHERE correlation_id LIKE 'direct-test-%'
               ORDER BY correlation_id"

# Step 10: Check retry statistics
print_info "Retry queue statistics:"
exec_sql_with_output "SELECT * FROM daz_retry_queue_stats WHERE plugin_name IN ('sql', 'eventfilter')"

# Clean up
print_status "Cleaning up test entries..."
exec_sql "DELETE FROM daz_retry_queue WHERE correlation_id LIKE 'direct-test-%'" >/dev/null

print_status "✅ Direct retry mechanism test completed"