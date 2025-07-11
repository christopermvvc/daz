#!/bin/bash

# Test SQL Plugin Event Processing
# This script tests if the SQL plugin is receiving and processing events

echo "SQL Plugin Event Processing Test"
echo "================================"
echo ""

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test configuration
HTTP_PORT=8081
PROMETHEUS_PORT=9091

echo -e "${YELLOW}Starting Daz with SQL plugin debug logging...${NC}"
echo ""

# Create a temporary log file to capture output
LOG_FILE="/tmp/daz-sql-test-$(date +%s).log"

# Start Daz in background, redirect output to log file
./bin/daz 2>&1 | tee "$LOG_FILE" &
DAZ_PID=$!

echo "Daz PID: $DAZ_PID"
echo "Log file: $LOG_FILE"
echo ""

# Wait for startup
echo "Waiting for Daz to start..."
sleep 5

# Function to check if a log message appears
check_log() {
    local pattern="$1"
    local description="$2"
    
    if grep -q "$pattern" "$LOG_FILE"; then
        echo -e "${GREEN}✓${NC} $description"
        return 0
    else
        echo -e "${RED}✗${NC} $description"
        return 1
    fi
}

# Check startup logs
echo ""
echo "Checking SQL Plugin Startup:"
echo "----------------------------"
check_log "\[SQL Plugin\] Starting database connection" "Database connection initiated"
check_log "\[SQL Plugin\] Database connected successfully" "Database connected"
check_log "\[SQL Plugin\] Subscribing to event handlers" "Event handler subscription started"
check_log "\[SQL Plugin\] Subscribed to sql.query.request" "Subscribed to query events"
check_log "\[SQL Plugin\] Subscribed to sql.exec.request" "Subscribed to exec events"
check_log "\[SQL Plugin\] Started successfully and ready" "Plugin ready"

# Test SQL query via HTTP API
echo ""
echo "Testing SQL Query via HTTP API:"
echo "-------------------------------"

QUERY_RESPONSE=$(curl -s -X POST "http://localhost:$HTTP_PORT/api/sql/query" \
    -H "Content-Type: application/json" \
    -d '{
        "query": "SELECT version() as postgres_version",
        "params": []
    }')

echo "Query Response: $QUERY_RESPONSE"
echo ""

# Check if query was received by plugin
sleep 2
echo "Checking SQL Query Processing:"
echo "-----------------------------"
check_log "\[SQL Plugin\] handleSQLQuery called" "SQL query handler invoked"
check_log "\[SQL Plugin\] Processing SQL query request" "Query processing started"

# Test SQL exec via HTTP API
echo ""
echo "Testing SQL Exec via HTTP API:"
echo "------------------------------"

EXEC_RESPONSE=$(curl -s -X POST "http://localhost:$HTTP_PORT/api/sql/exec" \
    -H "Content-Type: application/json" \
    -d '{
        "query": "CREATE TABLE IF NOT EXISTS test_table (id SERIAL PRIMARY KEY, name TEXT)",
        "params": []
    }')

echo "Exec Response: $EXEC_RESPONSE"
echo ""

# Check if exec was received by plugin
sleep 2
echo "Checking SQL Exec Processing:"
echo "----------------------------"
check_log "\[SQL Plugin\] handleSQLExec called" "SQL exec handler invoked"
check_log "\[SQL Plugin\] Processing SQL exec request" "Exec processing started"

# Check plugin.request routing
echo ""
echo "Checking Event Routing:"
echo "----------------------"
if grep -q "\[SQL Plugin\] Received plugin.request event" "$LOG_FILE"; then
    echo -e "${GREEN}✓${NC} Plugin request events are being received"
    
    if grep -q "\[SQL Plugin\] Routing to SQL" "$LOG_FILE"; then
        echo -e "${GREEN}✓${NC} Events are being routed to SQL handlers"
    else
        echo -e "${RED}✗${NC} Events are NOT being routed to SQL handlers"
    fi
else
    echo -e "${YELLOW}!${NC} No plugin.request events detected"
fi

# Display recent SQL plugin logs
echo ""
echo "Recent SQL Plugin Logs:"
echo "----------------------"
tail -n 50 "$LOG_FILE" | grep "\[SQL Plugin\]" | tail -n 20

# Cleanup
echo ""
echo "Stopping Daz..."
kill $DAZ_PID 2>/dev/null
wait $DAZ_PID 2>/dev/null

echo ""
echo "Test complete. Full log available at: $LOG_FILE"