#!/bin/bash

echo "SQL Activity Test - Running for 20 seconds"
echo "=========================================="

# Start Daz
./bin/daz > /tmp/daz-activity.log 2>&1 &
DAZ_PID=$!

echo "Daz started with PID: $DAZ_PID"
echo "Waiting for activity..."

# Monitor for 20 seconds
for i in {1..20}; do
    echo -n "."
    sleep 1
done
echo ""

# Check for SQL activity
echo ""
echo "SQL INSERT operations:"
grep "INSERT INTO" /tmp/daz-activity.log | wc -l

echo ""
echo "SQL UPDATE operations:"  
grep "UPDATE" /tmp/daz-activity.log | wc -l

echo ""
echo "Recent SQL operations (last 10):"
grep -E "INSERT INTO|UPDATE|SELECT" /tmp/daz-activity.log | tail -10

echo ""
echo "User activity tracking:"
grep -E "userJoin|userLeave|Updated activity|Tracking user" /tmp/daz-activity.log | tail -10

echo ""
echo "Media tracking:"
grep -E "changeMedia|Added.*to library|play_count" /tmp/daz-activity.log | tail -10

# Stop Daz
kill $DAZ_PID 2>/dev/null
echo ""
echo "Test complete."