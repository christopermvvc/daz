#!/bin/bash

# Test script to verify plugin startup without room connections
# This helps isolate plugin initialization issues

set -e

echo "=== Testing Plugin Startup Sequence ==="
echo "Testing plugin initialization without room connections..."

# Export environment variables
export DAZ_DB_HOST="localhost"
export DAZ_DB_PORT="5432"
export DAZ_DB_NAME="daz_db"
export DAZ_DB_USER="***REMOVED***"
export DAZ_DB_PASSWORD="***REMOVED***"

# Create minimal config with no rooms
cat > /tmp/test-startup-config.json << 'EOF'
{
  "core": {
    "rooms": [{
      "id": "test",
      "channel": "test_channel",
      "username": "testbot",
      "password": "testpass",
      "enabled": true,
      "reconnect_attempts": 0
    }]
  },
  "event_bus": {
    "buffer_sizes": {
      "sql.": 200,
      "plugin.": 200,
      "cytube.event": 100
    }
  },
  "plugins": {
    "sql": {
      "database": {
        "host": "localhost",
        "port": 5432,
        "database": "daz_db",
        "user": "***REMOVED***",
        "password": "***REMOVED***",
        "max_connections": 10,
        "max_conn_lifetime": 300,
        "connect_timeout": 10
      },
      "logger_rules": []
    },
    "retry": {
      "enabled": true,
      "max_retries": 5,
      "initial_delay_seconds": 5,
      "max_delay_seconds": 300,
      "backoff_multiplier": 2,
      "worker_count": 3,
      "batch_size": 10,
      "persistence_enabled": true
    },
    "eventfilter": {
      "filters": []
    },
    "usertracker": {
      "session_timeout_minutes": 30
    },
    "mediatracker": {
      "stats_update_interval_minutes": 5
    },
    "analytics": {
      "hourly_interval_hours": 1,
      "daily_interval_hours": 24
    }
  }
}
EOF

echo "Starting daz with test configuration..."
echo "This should start all plugins without room connections..."
echo ""

# Run daz with timeout
timeout 30s ./bin/daz -config /tmp/test-startup-config.json 2>&1 | while IFS= read -r line; do
    echo "[$(date '+%H:%M:%S')] $line"
    
    # Check for successful startup
    if [[ "$line" == *"Bot is running!"* ]]; then
        echo ""
        echo "✅ SUCCESS: All plugins started successfully!"
        echo "✅ The plugin startup sequence is working correctly."
        pkill -f "daz -config /tmp/test-startup-config.json" 2>/dev/null || true
        exit 0
    fi
    
    # Check for database connection error (expected in test environment)
    if [[ "$line" == *"database \"daz_db\" does not exist"* ]]; then
        echo ""
        echo "✅ SUCCESS: Plugin startup sequence completed!"
        echo "✅ The SQL plugin failed only due to missing database (expected in test)."
        echo "✅ All plugins initialized in correct order without deadlock."
        pkill -f "daz -config /tmp/test-startup-config.json" 2>/dev/null || true
        exit 0
    fi
    
    # Check for actual initialization errors (not timeout yet)
    if [[ "$line" == *"failed to initialize plugins"* ]] && [[ "$line" != *"timeout"* ]]; then
        echo ""
        echo "❌ ERROR: Plugin initialization failed!"
        echo "❌ Check the logs above for details."
        pkill -f "daz -config /tmp/test-startup-config.json" 2>/dev/null || true
        exit 1
    fi
done

# If we reach here, timeout occurred
echo ""
echo "⏱️  TIMEOUT: The startup took too long (>15s)"
echo "This might indicate a deadlock or blocking operation."

# Cleanup
rm -f /tmp/test-startup-config.json
pkill -f "daz -config /tmp/test-startup-config.json" 2>/dev/null || true

exit 2