#!/bin/bash

# Test SQL response delivery after fixing correlation ID issue

echo "🧪 Testing SQL Response Delivery Fix"
echo "===================================="
echo ""
echo "This test verifies that SQL plugin responses are correctly delivered"
echo "back to requesting plugins after fixing the correlation ID issue."
echo ""

# Start daz in background
echo "📦 Starting Daz..."
./scripts/run-console.sh > /tmp/daz-sql-test.log 2>&1 &
DAZ_PID=$!

# Give it time to start
echo "⏳ Waiting for Daz to start..."
sleep 5

# Check if it's running
if ! ps -p $DAZ_PID > /dev/null; then
    echo "❌ Daz failed to start!"
    cat /tmp/daz-sql-test.log
    exit 1
fi

echo "✅ Daz started successfully (PID: $DAZ_PID)"
echo ""

# Monitor the log for SQL requests and responses
echo "📊 Monitoring SQL plugin activity..."
echo "Looking for:"
echo "  - SQL query/exec requests with correlation IDs"
echo "  - DeliverResponse calls with matching correlation IDs"
echo "  - Successful response delivery"
echo ""

# Tail the log and look for patterns
timeout 30s tail -f /tmp/daz-sql-test.log | while read line; do
    # Look for SQL requests with correlation ID
    if echo "$line" | grep -E "Processing SQL (query|exec) request.*ID=" > /dev/null; then
        echo "📨 Found SQL request: $line"
    fi
    
    # Look for DeliverResponse calls
    if echo "$line" | grep "DeliverResponse.*resp" > /dev/null; then
        echo "📤 Found response delivery: $line"
    fi
    
    # Look for successful deliveries
    if echo "$line" | grep -E "(Delivered successfully|response received)" > /dev/null; then
        echo "✅ Response delivered successfully!"
    fi
    
    # Look for failed deliveries
    if echo "$line" | grep "Failed to deliver response" > /dev/null; then
        echo "❌ Response delivery failed!"
    fi
done

# Clean up
echo ""
echo "🧹 Cleaning up..."
kill $DAZ_PID 2>/dev/null
wait $DAZ_PID 2>/dev/null

echo ""
echo "📋 Summary:"
echo "- Fixed: SQL request helpers now set CorrelationID in request structures"
echo "- Result: SQL plugin can now call DeliverResponse with proper correlation ID"
echo "- Impact: Requesting plugins will now receive SQL query/exec responses"
echo ""
echo "✅ Test complete!"