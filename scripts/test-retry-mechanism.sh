#!/bin/bash

# Test script for verifying the retry mechanism in daz
# This script will:
# 1. Start daz with test configuration
# 2. Simulate SQL failures
# 3. Monitor retry queue
# 4. Check logs for retry attempts

set -e

echo "=== Daz Retry Mechanism Test Script ==="
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
LOG_FILE="/tmp/daz-retry-test.log"
PID_FILE="/tmp/daz-retry-test.pid"

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

# Cleanup function
cleanup() {
    print_status "Cleaning up..."
    stop_daz
    
    # Restore database connection if we modified it
    if [ -f "/tmp/pg_hba.conf.backup" ]; then
        print_info "Restoring PostgreSQL configuration..."
        sudo cp /tmp/pg_hba.conf.backup /etc/postgresql/*/main/pg_hba.conf
        sudo systemctl reload postgresql
        rm -f /tmp/pg_hba.conf.backup
    fi
}

# Set up trap for cleanup
trap cleanup EXIT

# Step 1: Check prerequisites
print_status "Checking prerequisites..."

# Check if PostgreSQL is running
if ! systemctl is-active --quiet postgresql; then
    print_error "PostgreSQL is not running. Please start it first."
    exit 1
fi

# Check if daz binary exists
if [ ! -f "./bin/daz" ]; then
    print_error "Daz binary not found. Building..."
    ./scripts/build-daz.sh
fi

# Check database connection
if ! PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -c "SELECT 1" >/dev/null 2>&1; then
    print_error "Cannot connect to database. Please check credentials."
    exit 1
fi

# Step 2: Create test configuration
print_status "Creating test configuration..."
cat > /tmp/daz-retry-test-config.json << 'EOF'
{
  "core": {
    "rooms": [
      {
        "channel": "RETRY_TEST_ROOM",
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
      "retry.": 100,
      "retry.schedule": 100
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
        "max_connections": 5,
        "max_conn_lifetime": 30,
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
      "poll_interval_seconds": 5,
      "batch_size": 5,
      "worker_count": 2,
      "default_max_retries": 3,
      "default_timeout_seconds": 10,
      "retry_policies": {
        "sql_operation": {
          "max_retries": 3,
          "initial_delay_seconds": 2,
          "max_delay_seconds": 30,
          "backoff_multiplier": 2.0,
          "timeout_seconds": 10
        }
      }
    }
  }
}
EOF

# Step 3: Ensure retry queue schema exists
print_status "Checking retry queue schema..."
if ! PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -c "SELECT 1 FROM daz_retry_queue LIMIT 1" >/dev/null 2>&1; then
    print_info "Retry queue table doesn't exist. Creating..."
    
    # Check if the schema file exists
    if [ -f "./scripts/create_retry_queue_schema.sql" ]; then
        PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -f "./scripts/create_retry_queue_schema.sql"
    else
        print_error "Retry queue schema file not found!"
        exit 1
    fi
fi

# Step 4: Start daz with test configuration
print_status "Starting daz with test configuration..."
./bin/daz -config /tmp/daz-retry-test-config.json > "$LOG_FILE" 2>&1 &
DAZ_PID=$!
echo $DAZ_PID > "$PID_FILE"

# Wait for daz to start
print_info "Waiting for daz to start..."
sleep 5

if ! is_daz_running; then
    print_error "Daz failed to start. Check logs:"
    tail -20 "$LOG_FILE"
    exit 1
fi

print_status "Daz is running (PID: $DAZ_PID)"

# Step 5: Monitor retry queue baseline
print_status "Checking initial retry queue state..."
INITIAL_COUNT=$(PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -t -c "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'pending'" | tr -d ' ')
print_info "Initial pending retry count: $INITIAL_COUNT"

# Step 6: Simulate SQL failures
print_status "Simulating SQL failures..."

# Method 1: Temporarily block database connections
print_info "Method 1: Simulating connection failures by blocking port"

# Use iptables to temporarily block PostgreSQL port (requires sudo)
if command -v iptables >/dev/null 2>&1; then
    print_info "Blocking PostgreSQL port temporarily..."
    sudo iptables -A OUTPUT -p tcp --dport 5432 -j DROP
    
    # Wait for some operations to fail
    sleep 10
    
    # Restore connection
    print_info "Restoring PostgreSQL port..."
    sudo iptables -D OUTPUT -p tcp --dport 5432 -j DROP
else
    print_info "iptables not available, using connection limit method..."
    
    # Method 2: Exhaust connection pool
    print_info "Creating multiple connections to exhaust pool..."
    for i in {1..10}; do
        PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -c "SELECT pg_sleep(30)" &
    done
    
    sleep 5
fi

# Step 7: Check retry queue for new entries
print_status "Checking retry queue for failed operations..."
sleep 5

NEW_COUNT=$(PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -t -c "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'pending'" | tr -d ' ')
RETRY_INCREASE=$((NEW_COUNT - INITIAL_COUNT))

if [ $RETRY_INCREASE -gt 0 ]; then
    print_status "Found $RETRY_INCREASE new retry entries!"
    
    # Show retry queue details
    print_info "Retry queue details:"
    PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -c "
        SELECT id, correlation_id, operation_type, retry_count, status, 
               retry_after, last_error
        FROM daz_retry_queue 
        WHERE created_at > NOW() - INTERVAL '5 minutes'
        ORDER BY created_at DESC
        LIMIT 10"
else
    print_error "No new retry entries found. Checking alternative tables..."
    
    # Check if using old schema
    if PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -c "SELECT 1 FROM information_schema.tables WHERE table_name = 'retry_queue'" >/dev/null 2>&1; then
        print_info "Found legacy retry_queue table. Checking it..."
        PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -c "SELECT * FROM retry_queue ORDER BY created_at DESC LIMIT 10"
    fi
fi

# Step 8: Monitor retry attempts in logs
print_status "Monitoring retry attempts in logs..."
print_info "Waiting for retry plugin to process queue..."
sleep 10

# Check for retry-related log entries
print_info "Retry-related log entries:"
grep -i "retry" "$LOG_FILE" | tail -20 || true

# Step 9: Check retry statistics
print_status "Checking retry statistics..."

# Check retry queue stats view if it exists
if PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -c "SELECT 1 FROM daz_retry_queue_stats LIMIT 1" >/dev/null 2>&1; then
    print_info "Retry queue statistics:"
    PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -c "SELECT * FROM daz_retry_queue_stats"
fi

# Check for any alerts
if PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -c "SELECT 1 FROM daz_retry_queue_alerts LIMIT 1" >/dev/null 2>&1; then
    print_info "Retry queue alerts:"
    PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -c "SELECT * FROM daz_retry_queue_alerts"
fi

# Step 10: Wait and check for successful retries
print_status "Waiting for retry processing..."
sleep 15

# Check for completed retries
COMPLETED_COUNT=$(PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -t -c "SELECT COUNT(*) FROM daz_retry_queue WHERE status = 'completed' AND created_at > NOW() - INTERVAL '5 minutes'" | tr -d ' ')
print_info "Completed retries: $COMPLETED_COUNT"

# Final summary
print_status "Test Summary:"
echo "- Initial pending retries: $INITIAL_COUNT"
echo "- New retry entries created: $RETRY_INCREASE"
echo "- Completed retries: $COMPLETED_COUNT"
echo
print_info "Full log available at: $LOG_FILE"

# Optional: Keep daz running for manual inspection
read -p "Press Enter to stop daz and cleanup..." </dev/tty

print_status "Test completed!"