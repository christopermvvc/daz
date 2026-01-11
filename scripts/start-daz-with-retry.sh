#!/bin/bash

# Start daz with retry mechanism enabled

set -e

echo "=== Starting Daz with Retry Mechanism ==="
echo

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
PROJECT_ROOT=$(cd "$SCRIPT_DIR/.." && pwd)
DATA_DIR="$PROJECT_ROOT/data"
LOG_DIR="$DATA_DIR/logs"
TMP_DIR="$DATA_DIR/tmp"

DB_NAME="${DAZ_DB_NAME:-daz}"
DB_USER="${DAZ_DB_USER:-***REMOVED***}"
DB_PASSWORD="${DAZ_DB_PASSWORD:?Error: DAZ_DB_PASSWORD environment variable is required}"
CYTUBE_USERNAME="${DAZ_CYTUBE_USERNAME:?Error: DAZ_CYTUBE_USERNAME environment variable is required}"
CYTUBE_PASSWORD="${DAZ_CYTUBE_PASSWORD:?Error: DAZ_CYTUBE_PASSWORD environment variable is required}"

MAX_LOG_AGE_DAYS=7
MAX_TOTAL_SIZE_MB=25
MAX_LOG_SIZE_KB=256

mkdir -p "$LOG_DIR" "$TMP_DIR"

LOG_FILE="$LOG_DIR/daz-with-retry_$(date +%Y%m%d_%H%M%S).log"
PID_FILE="$TMP_DIR/daz-with-retry.pid"
CONFIG_FILE="$(mktemp "$TMP_DIR/daz-with-retry-config.XXXXXX.json")"

cleanup_old_logs
cleanup_by_size

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

cleanup_old_logs() {
    find "$LOG_DIR" -name "daz-with-retry_*.log" -type f -mtime +$MAX_LOG_AGE_DAYS -delete 2>/dev/null
}

cleanup_by_size() {
    local max_size_bytes=$((MAX_TOTAL_SIZE_MB * 1024 * 1024))

    while true; do
        local total_size=$(find "$LOG_DIR" -name "daz-with-retry_*.log" -type f -exec stat -c%s {} + 2>/dev/null | awk '{sum+=$1} END {print sum}' || echo 0)

        if [ "$total_size" -le "$max_size_bytes" ]; then
            break
        fi

        local oldest_log=$(find "$LOG_DIR" -name "daz-with-retry_*.log" -type f -printf '%T@ %p\n' 2>/dev/null | sort -n | head -1 | cut -d' ' -f2-)
        if [ -n "$oldest_log" ]; then
            rm -f "$oldest_log"
            print_info "Removed old log to maintain size limit: $oldest_log"
        else
            break
        fi
    done
}

get_file_size_kb() {
    local file="$1"
    if [ -f "$file" ]; then
        local size_bytes=$(stat -c%s "$file" 2>/dev/null || echo 0)
        echo $((size_bytes / 1024))
    else
        echo 0
    fi
}

rotate_log_if_needed() {
    local current_log="$1"
    local size_kb=$(get_file_size_kb "$current_log")

    if [ "$size_kb" -ge "$MAX_LOG_SIZE_KB" ]; then
        local new_log="$LOG_DIR/daz-with-retry_$(date +%Y%m%d_%H%M%S).log"
        echo "" >> "$current_log"
        echo "--- Log rotated due to size limit (${size_kb}KB >= ${MAX_LOG_SIZE_KB}KB) ---" >> "$current_log"
        echo "--- Continuing in $new_log ---" >> "$current_log"
        echo "--- Continued from $current_log ---" > "$new_log"
        echo "$new_log"
    else
        echo "$current_log"
    fi
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

# Stop any existing daz instance
if is_daz_running; then
    print_status "Stopping existing daz instance..."
    kill $(cat "$PID_FILE") 2>/dev/null || true
    sleep 2
    rm -f "$PID_FILE"
fi

# Build daz if needed
if [ ! -f "./bin/daz" ]; then
    print_status "Building daz..."
    ./scripts/build-daz.sh
fi

# Apply retry schema
print_status "Ensuring retry queue schema exists..."
if [ -f "./scripts/sql/025_retry_persistence.sql" ]; then
    PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -f "./scripts/sql/025_retry_persistence.sql" >/dev/null 2>&1
fi

# Create configuration
print_status "Creating configuration with retry enabled..."
cat > "$CONFIG_FILE" << EOF
{
  "core": {
    "rooms": [
      {
        "channel": "RIFFTRAX_MST3K",
        "username": "$CYTUBE_USERNAME",
        "password": "$CYTUBE_PASSWORD",
        "enabled": true,
        "reconnect_attempts": 3,
        "cooldown_minutes": 1
      }
    ]
  },
  "event_bus": {
    "buffer_sizes": {
      "sql.": 500,
      "sql.exec": 500,
      "sql.query": 500,
      "retry.": 500,
      "retry.schedule": 500,
      "*.failed": 500,
      "*.error": 500,
      "*.timeout": 500,
      "plugin.request": 500,
      "plugin.response": 500
    }
  },
  "plugins": {
    "core": {
      "enabled": true
    },
    "sql": {
      "enabled": true,
      "database": {
        "host": "localhost",
        "port": 5432,
        "database": "$DB_NAME",
        "user": "$DB_USER",
        "password": "$DB_PASSWORD",
        "max_connections": 20,
        "max_conn_lifetime": 3600,
        "connect_timeout": 30
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
      "batch_size": 10,
      "worker_count": 3,
      "default_max_retries": 3,
      "default_timeout_seconds": 30,
      "retry_policies": {
        "sql_operation": {
          "max_retries": 3,
          "initial_delay_seconds": 2,
          "max_delay_seconds": 60,
          "backoff_multiplier": 2.0,
          "timeout_seconds": 15
        },
        "command_routing": {
          "max_retries": 2,
          "initial_delay_seconds": 1,
          "max_delay_seconds": 30,
          "backoff_multiplier": 1.5,
          "timeout_seconds": 10
        },
        "pm_response": {
          "max_retries": 3,
          "initial_delay_seconds": 1,
          "max_delay_seconds": 20,
          "backoff_multiplier": 2.0,  
          "timeout_seconds": 5
        }
      }
    },
    "eventfilter": {
      "enabled": true,
      "command_prefix": "!",
      "default_cooldown": 5
    },
    "usertracker": {
      "enabled": true,
      "inactivity_timeout_minutes": 30
    },
    "about": {
      "enabled": true
    },
    "uptime": {
      "enabled": true
    }
  }
}
EOF

# Start daz
print_status "Starting daz..."
export DAZ_DB_NAME="$DB_NAME"
export DAZ_DB_USER="$DB_USER"
export DAZ_DB_PASSWORD="$DB_PASSWORD"
export DAZ_DB_HOST="localhost"
export DAZ_DB_PORT="5432"

PIPE_FILE="$TMP_DIR/daz-with-retry_pipe_$$"
mkfifo "$PIPE_FILE"
trap "rm -f $PIPE_FILE $CONFIG_FILE" EXIT INT TERM

(
    current_log="$LOG_FILE"
    while IFS= read -r line; do
        echo "$line" | tee -a "$current_log"

        if [ $((RANDOM % 100)) -eq 0 ]; then
            new_log=$(rotate_log_if_needed "$current_log")
            if [ "$new_log" != "$current_log" ]; then
                print_info "Rotating log file to: $new_log"
                current_log="$new_log"
                cleanup_by_size
            fi
        fi
    done < "$PIPE_FILE"
) &

LOG_WRITER_PID=$!

./bin/daz -config "$CONFIG_FILE" > "$PIPE_FILE" 2>&1 &
DAZ_PID=$!
echo $DAZ_PID > "$PID_FILE"

# Wait for startup
print_info "Waiting for daz to initialize..."
sleep 10

if ! is_daz_running; then
    print_error "Daz failed to start. Last 50 lines of log:"
    tail -50 "$LOG_FILE"
    exit 1
fi

print_status "✅ Daz is running (PID: $DAZ_PID)"
print_info "Log file: $LOG_FILE"
print_info "Configuration: $CONFIG_FILE"

# Check if retry plugin started
if grep -q "Retry.*Started" "$LOG_FILE"; then
    print_status "✅ Retry plugin started successfully"
    grep "Retry" "$LOG_FILE" | tail -5
else
    print_error "❌ Retry plugin may not have started properly"
fi

# Show current retry queue status
print_info "Current retry queue status:"
PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -c "SELECT COUNT(*) as total, 
       COUNT(CASE WHEN status = 'pending' THEN 1 END) as pending,
       COUNT(CASE WHEN status = 'processing' THEN 1 END) as processing,
       COUNT(CASE WHEN status = 'completed' THEN 1 END) as completed,
       COUNT(CASE WHEN status = 'failed' THEN 1 END) as failed
FROM daz_retry_queue"

print_status "Daz is ready for testing"
print_info "To stop: kill $DAZ_PID"
print_info "To test failures: ./scripts/trigger-failure-event.sh"