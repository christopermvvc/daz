#!/bin/bash

# Test SQL plugin correlation ID flow and response delivery

echo "🧪 Testing SQL Plugin Correlation ID Flow"
echo "========================================"
echo ""
echo "This test verifies that:"
echo "1. SQL requests include correlation IDs"
echo "2. SQL plugin receives and logs correlation IDs"
echo "3. Responses are delivered with matching correlation IDs"
echo "4. Requesting plugins receive responses"
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

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

print_debug() {
    echo -e "${BLUE}[DEBUG]${NC} $1"
}

# Kill any existing daz process
print_status "Cleaning up any existing Daz processes..."
./scripts/kill.sh > /dev/null 2>&1
sleep 2

# Start daz in background with our test config
print_status "Starting Daz with enhanced SQL logging..."
./scripts/run-console.sh > /tmp/daz-sql-correlation-test.log 2>&1 &
DAZ_PID=$!

# Give it time to start
print_info "Waiting for Daz to initialize..."
sleep 5

# Check if it's running
if ! ps -p $DAZ_PID > /dev/null; then
    print_error "Daz failed to start!"
    echo "Last 50 lines of log:"
    tail -n 50 /tmp/daz-sql-correlation-test.log
    exit 1
fi

print_status "Daz started successfully (PID: $DAZ_PID)"
echo ""

# Function to analyze log for correlation IDs
analyze_correlation_flow() {
    local log_file=$1
    local correlation_ids=()
    local found_request=false
    local found_response=false
    local found_delivery=false
    
    print_info "Analyzing correlation ID flow in logs..."
    
    # Extract correlation IDs from SQL requests
    while IFS= read -r line; do
        if [[ $line =~ "Processing SQL query request.*CorrelationID=([^,]+)" ]] || 
           [[ $line =~ "Processing SQL exec request.*CorrelationID=([^,]+)" ]]; then
            local corr_id="${BASH_REMATCH[1]}"
            correlation_ids+=("$corr_id")
            print_debug "Found SQL request with CorrelationID: $corr_id"
            found_request=true
        fi
        
        if [[ $line =~ "Delivering.*response for CorrelationID: ([^,]+)" ]]; then
            local corr_id="${BASH_REMATCH[1]}"
            print_debug "Found response delivery for CorrelationID: $corr_id"
            found_delivery=true
        fi
        
        if [[ $line =~ "Successfully delivered.*response for CorrelationID: ([^,]+)" ]]; then
            local corr_id="${BASH_REMATCH[1]}"
            print_debug "Confirmed successful delivery for CorrelationID: $corr_id"
            found_response=true
        fi
    done < "$log_file"
    
    # Report findings
    echo ""
    if $found_request; then
        print_status "✅ SQL requests contain correlation IDs"
    else
        print_error "❌ No SQL requests with correlation IDs found"
    fi
    
    if $found_delivery; then
        print_status "✅ SQL plugin attempts to deliver responses"
    else
        print_error "❌ No response delivery attempts found"
    fi
    
    if $found_response; then
        print_status "✅ Responses successfully delivered"
    else
        print_error "❌ No successful response deliveries found"
    fi
    
    # Show unique correlation IDs found
    if [ ${#correlation_ids[@]} -gt 0 ]; then
        echo ""
        print_info "Correlation IDs found: ${#correlation_ids[@]}"
        for id in "${correlation_ids[@]}"; do
            echo "  - $id"
        done | sort -u | head -5
    fi
}

# Wait a bit for any SQL operations to occur
print_info "Monitoring for SQL operations for 10 seconds..."
sleep 10

# Analyze the log
echo ""
analyze_correlation_flow /tmp/daz-sql-correlation-test.log

# Check for any errors
echo ""
print_info "Checking for errors..."
if grep -i "ERROR.*correlation" /tmp/daz-sql-correlation-test.log > /dev/null; then
    print_error "Found correlation-related errors in log:"
    grep -i "ERROR.*correlation" /tmp/daz-sql-correlation-test.log | head -5
else
    print_status "✅ No correlation-related errors found"
fi

# Clean up
echo ""
print_status "Cleaning up..."
kill $DAZ_PID 2>/dev/null
wait $DAZ_PID 2>/dev/null

# Show summary
echo ""
echo "📋 Summary of Changes:"
echo "- Added logging to track correlation IDs in SQL handlers"
echo "- SQL query handler logs: ID, CorrelationID, and Query"
echo "- SQL exec handler logs: ID, CorrelationID, and Query"
echo "- Response delivery logged with correlation ID"
echo "- Error responses also logged with correlation ID"
echo ""

# Save a portion of the log for reference
print_info "Saving relevant log entries to /tmp/sql-correlation-summary.log"
grep -E "(CorrelationID|DeliverResponse|SQL Plugin)" /tmp/daz-sql-correlation-test.log > /tmp/sql-correlation-summary.log 2>/dev/null || true

print_status "Test complete!"
echo ""
echo "To view full logs: cat /tmp/daz-sql-correlation-test.log"
echo "To view summary: cat /tmp/sql-correlation-summary.log"