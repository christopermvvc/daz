#!/bin/bash

# Script to trigger a failure event in a running daz instance

set -e

echo "=== Trigger Failure Event Test ==="
echo

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
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

# Ensure daz is running
if ! pgrep -f "bin/daz" > /dev/null; then
    print_error "Daz is not running. Please start daz first."
    exit 1
fi

print_status "Daz is running. Triggering failure events..."

# Get baseline retry count
INITIAL_COUNT=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue" || echo "0")
print_info "Initial retry queue count: $INITIAL_COUNT"

# Trigger SQL failure event by inserting a row that simulates a failed SQL operation
print_status "Simulating SQL query failure event..."
exec_sql "INSERT INTO daz_core_events (event_type, event_data, received_at, created_at)
VALUES ('sql.query.failed', 
        '{\"correlation_id\":\"trigger-test-1\",\"source\":\"test-script\",\"operation_type\":\"sql.query\",\"error\":\"simulated database error\",\"timestamp\":\"'$(date -u +%Y-%m-%dT%H:%M:%SZ)'\"}'::jsonb,
        NOW(),
        NOW())" >/dev/null

sleep 2

# Trigger command routing failure event
print_status "Simulating command routing failure event..."
exec_sql "INSERT INTO daz_core_events (event_type, event_data, received_at, created_at)
VALUES ('eventfilter.command.failed', 
        '{\"correlation_id\":\"trigger-test-2\",\"source\":\"test-script\",\"operation_type\":\"command_routing\",\"error\":\"unknown command\",\"timestamp\":\"'$(date -u +%Y-%m-%dT%H:%M:%SZ)'\"}'::jsonb,
        NOW(),
        NOW())" >/dev/null

sleep 2

# Check if retry queue has new entries
print_status "Checking retry queue for new entries..."
NEW_COUNT=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue" || echo "0")
print_info "Current retry queue count: $NEW_COUNT (was $INITIAL_COUNT)"

if [ "$NEW_COUNT" -gt "$INITIAL_COUNT" ]; then
    print_status "✅ Failure events were captured by retry mechanism!"
    
    print_info "New retry entries:"
    exec_sql_with_output "SELECT correlation_id, plugin_name, operation_type, retry_count, status
                   FROM daz_retry_queue 
                   WHERE correlation_id LIKE 'trigger-test-%'
                   ORDER BY created_at DESC"
else
    print_error "❌ No new retry entries detected"
    print_info "The retry mechanism may not be capturing failure events from the events table"
fi

# Clean up test entries
print_status "Cleaning up test entries..."
exec_sql "DELETE FROM daz_core_events WHERE event_type LIKE '%.failed' AND event_data->>'source' = 'test-script'" >/dev/null
exec_sql "DELETE FROM daz_retry_queue WHERE correlation_id LIKE 'trigger-test-%'" >/dev/null

print_status "Test completed"