#!/bin/bash

# Demonstrate the retry mechanism working

set -e

echo "=== Retry Mechanism Demo ==="
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

print_query() {
    echo -e "${BLUE}[QUERY]${NC} $1"
}

# Function to execute SQL query
exec_sql() {
    PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -t -c "$1" 2>/dev/null | tr -d ' \n'
}

# Function to execute SQL query with output
exec_sql_with_output() {
    PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -c "$1"
}

# Step 1: Ensure retry queue schema exists
print_status "Ensuring retry queue schema exists..."
if [ -f "./scripts/sql/025_retry_persistence.sql" ]; then
    PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -f "./scripts/sql/025_retry_persistence.sql" >/dev/null 2>&1
    print_status "✅ Retry queue schema applied"
fi

# Step 2: Clean up demo entries
print_status "Cleaning up old demo entries..."
exec_sql "DELETE FROM daz_retry_queue WHERE correlation_id LIKE 'demo-%'" >/dev/null 2>&1 || true

# Step 3: Demonstrate retry queue functionality
print_info "=== Demonstrating Retry Queue Functionality ==="
echo

# Insert a high-priority retry that should be processed immediately
print_status "1. Adding high-priority SQL query that will fail..."
exec_sql "INSERT INTO daz_retry_queue (correlation_id, plugin_name, operation_type, event_type, event_data, max_retries, retry_after, priority, timeout_seconds, status)
VALUES ('demo-sql-fail', 'sql', 'sql_operation', 'sql.query', 
        '{\"query\": \"SELECT * FROM non_existent_table\", \"params\": [], \"correlation_id\": \"demo-sql-fail\"}'::jsonb, 
        3, NOW() - INTERVAL '1 minute', 10, 5, 'pending')" >/dev/null

# Insert a command routing retry
print_status "2. Adding command routing retry..."
exec_sql "INSERT INTO daz_retry_queue (correlation_id, plugin_name, operation_type, event_type, event_data, max_retries, retry_after, priority, timeout_seconds, status)
VALUES ('demo-cmd-route', 'eventfilter', 'command_routing', 'command.execute', 
        '{\"command\": \"unknown\", \"username\": \"testuser\", \"correlation_id\": \"demo-cmd-route\"}'::jsonb, 
        2, NOW(), 5, 3, 'pending')" >/dev/null

# Insert a PM response retry
print_status "3. Adding PM response retry that will wait..."
exec_sql "INSERT INTO daz_retry_queue (correlation_id, plugin_name, operation_type, event_type, event_data, max_retries, retry_after, priority, timeout_seconds, status)
VALUES ('demo-pm-delay', 'eventfilter', 'pm_response', 'cytube.send.pm', 
        '{\"username\": \"testuser\", \"message\": \"Test response\", \"correlation_id\": \"demo-pm-delay\"}'::jsonb, 
        3, NOW() + INTERVAL '2 minutes', 3, 5, 'pending')" >/dev/null

echo
print_info "Current retry queue status:"
exec_sql_with_output "SELECT correlation_id, operation_type, retry_count, status,
                      CASE WHEN retry_after <= NOW() THEN 'Ready' ELSE 'Waiting' END as state,
                      priority
               FROM daz_retry_queue 
               WHERE correlation_id LIKE 'demo-%'
               ORDER BY priority DESC, retry_after ASC"

# Step 4: Simulate retry processing
echo
print_status "Simulating retry batch acquisition..."
print_query "Acquiring batch of 5 retries..."
exec_sql_with_output "SELECT correlation_id, status, priority FROM acquire_retry_batch(5) WHERE correlation_id LIKE 'demo-%'"

echo
print_info "After batch acquisition:"
exec_sql_with_output "SELECT correlation_id, status, retry_count FROM daz_retry_queue WHERE correlation_id LIKE 'demo-%' ORDER BY correlation_id"

# Step 5: Simulate retry outcomes
echo
print_status "Simulating retry outcomes..."

# Fail the SQL query (it would fail due to non-existent table)
print_info "Marking SQL query as failed..."
exec_sql "UPDATE daz_retry_queue SET retry_count = retry_count + 1, 
          retry_after = NOW() + INTERVAL '4 seconds', 
          last_error = 'relation does not exist',
          status = 'pending'
          WHERE correlation_id = 'demo-sql-fail'" >/dev/null

# Complete the command routing
print_info "Marking command routing as completed..."
exec_sql "SELECT complete_retry((SELECT id FROM daz_retry_queue WHERE correlation_id = 'demo-cmd-route'))" >/dev/null

echo
print_info "Updated retry queue status:"
exec_sql_with_output "SELECT correlation_id, status, retry_count, 
                      SUBSTRING(last_error, 1, 30) as error_preview
               FROM daz_retry_queue 
               WHERE correlation_id LIKE 'demo-%'
               ORDER BY correlation_id"

# Step 6: Show retry statistics
echo
print_info "Retry queue statistics:"
exec_sql_with_output "SELECT plugin_name, operation_type, status, COUNT(*) as count,
                      AVG(retry_count) as avg_retries
               FROM daz_retry_queue
               WHERE correlation_id LIKE 'demo-%'
               GROUP BY plugin_name, operation_type, status
               ORDER BY plugin_name, operation_type, status"

# Step 7: Demonstrate exponential backoff
echo
print_status "Demonstrating exponential backoff calculation..."
print_info "For a retry with initial_delay=2s, backoff_multiplier=2.0:"
echo "  Attempt 1: 2 seconds"
echo "  Attempt 2: 4 seconds (2 * 2.0)"
echo "  Attempt 3: 8 seconds (4 * 2.0)"
echo "  Attempt 4: 16 seconds (8 * 2.0) - capped at max_delay"

# Clean up
echo
print_status "Cleaning up demo entries..."
exec_sql "DELETE FROM daz_retry_queue WHERE correlation_id LIKE 'demo-%'" >/dev/null

print_status "✅ Demo completed!"
echo
print_info "The retry mechanism provides:"
echo "  - Persistent retry queue with PostgreSQL backing"
echo "  - Exponential backoff with configurable policies"
echo "  - Priority-based processing"
echo "  - Atomic batch acquisition for concurrent workers"
echo "  - Failure event capture from all plugins"
echo "  - Dead letter queue for permanent failures"