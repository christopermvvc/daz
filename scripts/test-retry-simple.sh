#!/bin/bash

# Simple test script for verifying the retry mechanism in daz
# This version doesn't require sudo and uses application-level testing

set -e

echo "=== Daz Retry Mechanism Simple Test ==="
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
LOG_FILE="/tmp/daz-retry-simple-test.log"
PID_FILE="/tmp/daz-retry-simple-test.pid"
TEST_DURATION=60  # How long to run the test in seconds

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

# Function to check if daz is running
is_daz_running() {
    if [ -f "$PID_FILE" ]; then
        local pid=$(cat "$PID_FILE")
        if ps -p "$pid" > /dev/null 2>&1; then
            return 0
        fi
    fi
    return 1
}

# Function to stop daz
stop_daz() {
    if is_daz_running; then
        print_status "Stopping daz..."
        kill $(cat "$PID_FILE") 2>/dev/null || true
        sleep 2
        rm -f "$PID_FILE"
    fi
}

# Function to execute SQL query
exec_sql() {
    PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -t -c "$1" 2>/dev/null | tr -d ' \n'
}

# Function to execute SQL query with output
exec_sql_with_output() {
    PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -c "$1"
}

# Cleanup function
cleanup() {
    print_status "Cleaning up..."
    stop_daz
    
    # Clean up test data
    print_info "Cleaning up test database entries..."
    exec_sql "DELETE FROM daz_retry_queue WHERE correlation_id LIKE 'test-retry-%'" >/dev/null 2>&1 || true
}

# Set up trap for cleanup
trap cleanup EXIT

# Step 1: Build daz if needed
if [ ! -f "./bin/daz" ]; then
    print_status "Building daz..."
    ./scripts/build-daz.sh
fi

# Step 2: Check database connection
print_status "Checking database connection..."
if ! exec_sql "SELECT 1" >/dev/null; then
    print_error "Cannot connect to database. Please check that PostgreSQL is running."
    exit 1
fi

# Step 3: Ensure retry queue table exists
print_status "Checking retry queue schema..."
if ! exec_sql "SELECT 1 FROM daz_retry_queue LIMIT 1" >/dev/null 2>&1; then
    print_info "Creating retry queue schema..."
    # First apply the old schema if it exists
    if [ -f "./scripts/create_retry_queue_schema.sql" ]; then
        PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -f "./scripts/create_retry_queue_schema.sql" >/dev/null 2>&1
    fi
    # Then apply the new schema with functions
    if [ -f "./scripts/sql/025_retry_persistence.sql" ]; then
        PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -f "./scripts/sql/025_retry_persistence.sql" >/dev/null 2>&1
        print_status "Retry queue schema created successfully"
    else
        print_error "Retry queue schema file not found!"
        exit 1
    fi
fi

# Step 4: Create test configuration with aggressive retry settings
print_status "Creating test configuration..."
cat > /tmp/daz-retry-test-config.json << 'EOF'
{
  "core": {
    "rooms": [
      {
        "channel": "RIFFTRAX_MST3K",
        "username": "***REMOVED***",
        "password": "***REMOVED***",
        "enabled": true,
        "reconnect_attempts": 3,
        "cooldown_minutes": 1
      }
    ]
  },
  "event_bus": {
    "buffer_sizes": {
      "sql.": 100,
      "sql.exec": 100,
      "sql.query": 100,
      "retry.": 100
    }
  },
  "plugins": {
    "sql": {
      "database": {
        "host": "localhost",
        "port": 5432,
        "database": "daz",
        "user": "***REMOVED***",
        "password": "***REMOVED***",
        "max_connections": 10,
        "max_conn_lifetime": 3600,
        "connect_timeout": 5
      },
      "logger_rules": [
        {
          "event_pattern": "cytube.event.*",
          "enabled": true,
          "table": "daz_core_events"
        }
      ]
    },
    "retry": {
      "enabled": true,
      "poll_interval_seconds": 3,
      "batch_size": 5,
      "worker_count": 2,
      "default_max_retries": 3,
      "default_timeout_seconds": 5,
      "retry_policies": {
        "sql_operation": {
          "max_retries": 3,
          "initial_delay_seconds": 2,
          "max_delay_seconds": 20,
          "backoff_multiplier": 2.0,
          "timeout_seconds": 5
        },
        "cytube_command": {
          "max_retries": 5,
          "initial_delay_seconds": 3,
          "max_delay_seconds": 30,
          "backoff_multiplier": 1.5,
          "timeout_seconds": 10
        }
      }
    }
  }
}
EOF

# Step 5: Start daz
print_status "Starting daz with test configuration..."
# Set environment variables for daz
export DAZ_DB_NAME="$DB_NAME"
export DAZ_DB_USER="$DB_USER"
export DAZ_DB_PASSWORD="$DB_PASSWORD"
export DAZ_DB_HOST="localhost"
export DAZ_DB_PORT="5432"

./bin/daz -config /tmp/daz-retry-test-config.json > "$LOG_FILE" 2>&1 &
DAZ_PID=$!
echo $DAZ_PID > "$PID_FILE"

# Wait for daz to start
print_info "Waiting for daz to initialize..."
sleep 8

if ! is_daz_running; then
    print_error "Daz failed to start. Last 30 lines of log:"
    tail -30 "$LOG_FILE"
    exit 1
fi

print_status "Daz is running (PID: $DAZ_PID)"

# Step 6: Get baseline retry queue count
print_status "Getting baseline retry queue metrics..."
INITIAL_PENDING=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'pending'")
INITIAL_COMPLETED=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'completed'")
INITIAL_FAILED=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'failed'")

print_info "Initial state:"
echo "  - Pending: $INITIAL_PENDING"
echo "  - Completed: $INITIAL_COMPLETED"  
echo "  - Failed: $INITIAL_FAILED"

# Step 7: Insert test retry entries directly
print_status "Creating test retry entries..."

# Create a mix of different retry scenarios
print_query "Inserting test retry items..."

# Test 1: SQL operation that should succeed after retry
exec_sql "INSERT INTO daz_retry_queue (correlation_id, plugin_name, operation_type, event_type, event_data, max_retries, retry_after, priority, timeout_seconds, status)
VALUES ('test-retry-sql-1', 'sql', 'sql_operation', 'sql.query', '{\"query\": \"SELECT 1\", \"correlation_id\": \"test-1\"}'::jsonb, 3, NOW(), 5, 5, 'pending')" >/dev/null

# Test 2: Invalid operation that will fail permanently
exec_sql "INSERT INTO daz_retry_queue (correlation_id, plugin_name, operation_type, event_type, event_data, max_retries, retry_after, priority, timeout_seconds, status)
VALUES ('test-retry-fail-1', 'nonexistent', 'unknown', 'test.fail', '{\"error\": \"This should fail\"}'::jsonb, 2, NOW(), 5, 5, 'pending')" >/dev/null

# Test 3: Operation with immediate retry
exec_sql "INSERT INTO daz_retry_queue (correlation_id, plugin_name, operation_type, event_type, event_data, max_retries, retry_after, priority, timeout_seconds, status)
VALUES ('test-retry-immediate-1', 'sql', 'sql_operation', 'sql.exec', '{\"query\": \"SELECT NOW()\"}'::jsonb, 3, NOW() - INTERVAL '1 second', 10, 5, 'pending')" >/dev/null

print_info "Created 3 test retry entries"

# Step 8: Monitor retry processing
print_status "Monitoring retry processing..."
START_TIME=$(date +%s)
MONITOR_INTERVAL=5

while true; do
    CURRENT_TIME=$(date +%s)
    ELAPSED=$((CURRENT_TIME - START_TIME))
    
    if [ $ELAPSED -gt $TEST_DURATION ]; then
        break
    fi
    
    print_info "Time elapsed: ${ELAPSED}s / ${TEST_DURATION}s"
    
    # Check current queue status
    CURRENT_PENDING=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'pending'")
    CURRENT_PROCESSING=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'processing'")
    CURRENT_COMPLETED=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'completed'")
    CURRENT_FAILED=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'failed'")
    CURRENT_DEAD=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'dead_letter'")
    
    echo "  Queue Status: Pending=$CURRENT_PENDING Processing=$CURRENT_PROCESSING Completed=$CURRENT_COMPLETED Failed=$CURRENT_FAILED DeadLetter=$CURRENT_DEAD"
    
    # Show recent retry activity
    print_query "Recent retry activity (last 5):"
    exec_sql_with_output "SELECT id, correlation_id, operation_type, retry_count, status, 
                          retry_after AT TIME ZONE 'UTC' as retry_after_utc,
                          SUBSTRING(last_error, 1, 50) as error_preview
                   FROM daz_retry_queue 
                   WHERE correlation_id LIKE 'test-retry-%'
                   ORDER BY updated_at DESC 
                   LIMIT 5"
    
    # Check logs for retry activity
    echo
    print_info "Recent retry log entries:"
    grep -i "retry" "$LOG_FILE" | grep -v "Subscribe" | tail -5 || echo "  No retry activity in logs yet"
    
    echo
    echo "---"
    sleep $MONITOR_INTERVAL
done

# Step 9: Final analysis
print_status "Test completed. Analyzing results..."

# Get final counts
FINAL_PENDING=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'pending' AND correlation_id LIKE 'test-retry-%'")
FINAL_COMPLETED=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'completed' AND correlation_id LIKE 'test-retry-%'")
FINAL_FAILED=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'failed' AND correlation_id LIKE 'test-retry-%'")
FINAL_DEAD=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'dead_letter' AND correlation_id LIKE 'test-retry-%'")

print_info "Final test results:"
echo "  - Pending: $FINAL_PENDING"
echo "  - Completed: $FINAL_COMPLETED"
echo "  - Failed: $FINAL_FAILED"
echo "  - Dead Letter: $FINAL_DEAD"

# Show detailed results
print_query "Detailed test results:"
exec_sql_with_output "SELECT correlation_id, operation_type, retry_count, max_retries, status,
                      created_at AT TIME ZONE 'UTC' as created_utc,
                      updated_at AT TIME ZONE 'UTC' as updated_utc,
                      SUBSTRING(last_error, 1, 80) as error
               FROM daz_retry_queue 
               WHERE correlation_id LIKE 'test-retry-%'
               ORDER BY correlation_id"

# Check if retry mechanism is working
echo
if [ "$FINAL_COMPLETED" -gt 0 ] || [ "$FINAL_DEAD" -gt 0 ]; then
    print_status "✅ Retry mechanism is working!"
    echo "  - Some operations were retried and processed"
else
    print_error "❌ Retry mechanism may not be working properly"
    echo "  - No operations were completed or moved to dead letter"
    echo
    print_info "Checking logs for errors..."
    grep -i "error" "$LOG_FILE" | tail -10 || true
fi

# Summary
echo
print_status "Test Summary:"
echo "- Test duration: ${TEST_DURATION} seconds"
echo "- Operations completed: $FINAL_COMPLETED"
echo "- Operations failed permanently: $FINAL_DEAD"
echo "- Operations still pending: $FINAL_PENDING"
echo
print_info "Full log available at: $LOG_FILE"
print_info "To view retry-related logs: grep -i retry $LOG_FILE"

# Offer to keep running for debugging
echo
read -p "Press Enter to stop daz and cleanup..." </dev/tty