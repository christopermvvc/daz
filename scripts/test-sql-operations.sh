#!/bin/bash

# Test SQL Operations
echo "Testing SQL Operations in Daz"
echo "============================"

# Start Daz in background
echo "Starting Daz..."
./bin/daz > /tmp/daz-sql-test.log 2>&1 &
DAZ_PID=$!

# Wait for startup
echo "Waiting for startup..."
sleep 5

# Check if SQL operations are happening
echo ""
echo "Checking for SQL table creation:"
grep -i "created.*table\|creating.*table" /tmp/daz-sql-test.log || echo "No table creation logs found"

echo ""
echo "Checking for successful SQL operations:"
grep -E "rows affected|Successfully delivered.*response|INSERT INTO|UPDATE.*SET" /tmp/daz-sql-test.log | head -10 || echo "No SQL operation logs found"

echo ""
echo "Checking Analytics plugin:"
grep -E "Analytics.*Created tables successfully|Analytics.*updated" /tmp/daz-sql-test.log | head -5 || echo "No Analytics SQL logs found"

echo ""
echo "Checking MediaTracker plugin:"
grep -E "MediaTracker.*Added.*to library|MediaTracker.*Updated" /tmp/daz-sql-test.log | head -5 || echo "No MediaTracker SQL logs found"

echo ""
echo "Checking for SQL errors:"
grep -iE "sql.*error|database.*error|timeout.*sql" /tmp/daz-sql-test.log | head -10 || echo "No SQL errors found (good!)"

# Check database directly
echo ""
echo "Checking database tables directly:"
psql -U ***REMOVED*** -d daz -h localhost -c "\dt daz_*" 2>/dev/null | grep -E "daz_" || echo "Could not query database tables"

# Stop Daz
echo ""
echo "Stopping Daz..."
kill $DAZ_PID 2>/dev/null
wait $DAZ_PID 2>/dev/null

echo ""
echo "Test complete. Full log at: /tmp/daz-sql-test.log"