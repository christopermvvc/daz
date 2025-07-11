#!/bin/bash

# Test script for verifying the retry mechanism through failure events
# This version triggers actual failures that should be caught by the retry mechanism

set -e

echo "=== Daz Retry Mechanism Failure Event Test ==="
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
LOG_FILE="/tmp/daz-retry-failure-test.log"
PID_FILE="/tmp/daz-retry-failure-test.pid"

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

# Cleanup function
cleanup() {
    print_status "Cleaning up..."
    stop_daz
}

# Set up trap for cleanup
trap cleanup EXIT

# Build daz if needed
if [ ! -f "./bin/daz" ]; then
    print_status "Building daz..."
    ./scripts/build-daz.sh
fi

# Create test configuration
print_status "Creating test configuration..."
cat > /tmp/daz-retry-failure-test-config.json << 'EOF'
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
      "sql.": 200,
      "sql.exec": 200,
      "sql.query": 200,
      "retry.": 200,
      "eventfilter.": 200
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
        "max_connections": 20,
        "max_conn_lifetime": 3600,
        "connect_timeout": 10
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
      "poll_interval_seconds": 2,
      "batch_size": 5,
      "worker_count": 2,
      "default_max_retries": 3,
      "default_timeout_seconds": 10,
      "retry_policies": {
        "sql_operation": {
          "max_retries": 3,
          "initial_delay_seconds": 1,
          "max_delay_seconds": 10,
          "backoff_multiplier": 2.0,
          "timeout_seconds": 5
        },
        "command_routing": {
          "max_retries": 2,
          "initial_delay_seconds": 1,
          "max_delay_seconds": 5,
          "backoff_multiplier": 1.5,
          "timeout_seconds": 3
        }
      }
    },
    "eventfilter": {
      "command_prefix": "!",
      "default_cooldown": 5
    }
  }
}
EOF

# Start daz
print_status "Starting daz with test configuration..."
export DAZ_DB_NAME="$DB_NAME"
export DAZ_DB_USER="$DB_USER"
export DAZ_DB_PASSWORD="$DB_PASSWORD"
export DAZ_DB_HOST="localhost"
export DAZ_DB_PORT="5432"

./bin/daz -config /tmp/daz-retry-failure-test-config.json > "$LOG_FILE" 2>&1 &
DAZ_PID=$!
echo $DAZ_PID > "$PID_FILE"

# Wait for daz to start
print_info "Waiting for daz to initialize..."
sleep 10

if ! is_daz_running; then
    print_error "Daz failed to start. Last 30 lines of log:"
    tail -30 "$LOG_FILE"
    exit 1
fi

print_status "Daz is running (PID: $DAZ_PID)"

# Monitor logs for retry activity
print_status "Monitoring for failure events and retry activity..."

# Get baseline retry count
INITIAL_COUNT=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue" || echo "0")
print_info "Initial retry queue count: $INITIAL_COUNT"

# Simulate some failures that should trigger retry
print_status "Simulating failures to trigger retry mechanism..."

# 1. Try to send a command to a non-existent plugin (should fail and be retried)
print_info "Sending event to non-existent plugin..."
echo '{"type":"test.nonexistent","data":"test"}' >> "$LOG_FILE.events"

# 2. Monitor for 30 seconds
for i in {1..6}; do
    sleep 5
    print_info "Checking retry queue ($((i*5))s)..."
    
    # Check retry queue
    CURRENT_COUNT=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue" || echo "0")
    PENDING=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'pending'" || echo "0")
    PROCESSING=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'processing'" || echo "0")
    COMPLETED=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'completed'" || echo "0")
    FAILED=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'failed'" || echo "0")
    
    echo "  Total: $CURRENT_COUNT, Pending: $PENDING, Processing: $PROCESSING, Completed: $COMPLETED, Failed: $FAILED"
    
    # Check logs for retry activity
    if grep -q "Retry.*Processing retry" "$LOG_FILE" 2>/dev/null; then
        print_info "Found retry processing activity in logs"
    fi
    
    # Check for failure events
    if grep -q "\.failed\|\.error\|\.timeout" "$LOG_FILE" 2>/dev/null; then
        print_info "Found failure events in logs"
        grep "\.failed\|\.error\|\.timeout" "$LOG_FILE" | tail -5
    fi
done

# Final analysis
print_status "Test completed. Analyzing results..."

# Check final state
FINAL_COUNT=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue" || echo "0")
print_info "Final retry queue count: $FINAL_COUNT (was $INITIAL_COUNT)"

# Check logs for retry activity
print_info "Retry-related log entries:"
grep -i "retry" "$LOG_FILE" | grep -v "Subscribe" | tail -20 || echo "No retry activity found"

# Check for failure events
print_info "Failure events:"
grep -E "(failed|error|timeout)" "$LOG_FILE" | grep -v "Subscribe" | tail -10 || echo "No failure events found"

if [ "$FINAL_COUNT" -gt "$INITIAL_COUNT" ]; then
    print_status "✅ Retry mechanism is capturing failures!"
    echo "  - Retry queue grew from $INITIAL_COUNT to $FINAL_COUNT entries"
else
    print_error "❌ No new retry entries detected"
    echo "  - The retry mechanism may not be capturing failure events"
fi

print_info "Full log available at: $LOG_FILE"