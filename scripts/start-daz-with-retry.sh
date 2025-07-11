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
DB_NAME="daz"
DB_USER="***REMOVED***"
DB_PASSWORD="***REMOVED***"
LOG_FILE="/tmp/daz-with-retry.log"
PID_FILE="/tmp/daz-with-retry.pid"

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
cat > /tmp/daz-with-retry-config.json << 'EOF'
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
        "database": "daz",
        "user": "***REMOVED***",
        "password": "***REMOVED***",
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

./bin/daz -config /tmp/daz-with-retry-config.json > "$LOG_FILE" 2>&1 &
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
print_info "Configuration: /tmp/daz-with-retry-config.json"

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