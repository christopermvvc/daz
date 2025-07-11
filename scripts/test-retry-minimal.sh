#!/bin/bash

# Minimal test of retry mechanism with reduced plugin set

set -e

echo "=== Minimal Retry Mechanism Test ==="
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
LOG_FILE="/tmp/daz-retry-minimal.log"
PID_FILE="/tmp/daz-retry-minimal.pid"

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

# Function to execute SQL query with output
exec_sql_with_output() {
    PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -c "$1"
}

# Cleanup function
cleanup() {
    print_status "Cleaning up..."
    stop_daz
    exec_sql "DELETE FROM daz_retry_queue WHERE correlation_id LIKE 'min-test-%'" >/dev/null 2>&1 || true
}

# Set up trap for cleanup
trap cleanup EXIT

# Build daz if needed
if [ ! -f "./bin/daz" ]; then
    print_status "Building daz..."
    ./scripts/build-daz.sh
fi

# Create minimal configuration with just core, sql, and retry plugins
print_status "Creating minimal test configuration..."
cat > /tmp/daz-retry-minimal-config.json << 'EOF'
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
      "*.failed": 200,
      "*.error": 200,
      "*.timeout": 200
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
      "logger_rules": []
    },
    "retry": {
      "enabled": true,
      "poll_interval_seconds": 3,
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
        }
      }
    }
  },
  "disabled_plugins": ["eventfilter", "usertracker", "mediatracker", "analytics", "about", "help", "uptime", "debug"]
}
EOF

# Start daz
print_status "Starting daz with minimal configuration..."
export DAZ_DB_NAME="$DB_NAME"
export DAZ_DB_USER="$DB_USER"
export DAZ_DB_PASSWORD="$DB_PASSWORD"
export DAZ_DB_HOST="localhost"
export DAZ_DB_PORT="5432"

./bin/daz -config /tmp/daz-retry-minimal-config.json > "$LOG_FILE" 2>&1 &
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

# Get baseline retry count
INITIAL_COUNT=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue" || echo "0")
print_info "Initial retry queue count: $INITIAL_COUNT"

# Simulate a SQL failure by trying to query a non-existent table
print_status "Simulating SQL failures..."

# Create a test that will fail
print_info "1. Attempting to query non-existent table (should fail)..."
exec_sql "INSERT INTO daz_retry_queue (correlation_id, plugin_name, operation_type, event_type, event_data, max_retries, retry_after, priority, timeout_seconds, status)
VALUES ('min-test-bad-query', 'sql', 'sql_operation', 'sql.query', '{\"query\": \"SELECT * FROM non_existent_table\", \"params\": []}'::jsonb, 3, NOW(), 10, 5, 'pending')" >/dev/null

# Monitor for 15 seconds
print_status "Monitoring retry activity..."
for i in {1..3}; do
    sleep 5
    print_info "Check $i/3 ($(($i*5))s)..."
    
    # Check retry queue
    CURRENT_COUNT=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE correlation_id LIKE 'min-test-%'" || echo "0")
    PENDING=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'pending' AND correlation_id LIKE 'min-test-%'" || echo "0")
    PROCESSING=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'processing' AND correlation_id LIKE 'min-test-%'" || echo "0")
    COMPLETED=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'completed' AND correlation_id LIKE 'min-test-%'" || echo "0")
    FAILED=$(exec_sql "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'failed' AND correlation_id LIKE 'min-test-%'" || echo "0")
    
    echo "  Test entries - Total: $CURRENT_COUNT, Pending: $PENDING, Processing: $PROCESSING, Completed: $COMPLETED, Failed: $FAILED"
    
    # Check for retry processing
    if grep -q "min-test-bad-query" "$LOG_FILE" 2>/dev/null; then
        print_info "Found test entry in logs:"
        grep "min-test-bad-query" "$LOG_FILE" | tail -3
    fi
done

# Final check
print_status "Final retry queue status:"
exec_sql_with_output "SELECT correlation_id, retry_count, status, 
                      SUBSTRING(last_error, 1, 60) as error_preview
               FROM daz_retry_queue 
               WHERE correlation_id LIKE 'min-test-%'
               ORDER BY correlation_id"

# Check for failure events
print_info "Checking for failure events in logs..."
grep -E "(\.failed|\.error|\.timeout)" "$LOG_FILE" | grep -v "Subscribe" | tail -10 || echo "No failure events found"

# Count actual retry processing
RETRY_COUNT=$(grep -c "Processing retry" "$LOG_FILE" 2>/dev/null || echo "0")
print_info "Total retry processing attempts: $RETRY_COUNT"

print_info "Full log available at: $LOG_FILE"