#!/bin/bash

# Simple SQL test with timing logs

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== SQL Timing Test ===${NC}"

# Start daz with our new binary
echo -e "${BLUE}Starting Daz with timing logs...${NC}"
cd /home/user/Documents/daz
./bin/daz >/tmp/daz_timing_test.log 2>&1 &
DAZ_PID=$!
echo "Daz PID: $DAZ_PID"

# Monitor logs in background with timing info
tail -f /tmp/daz_timing_test.log | grep -E "(SQL Plugin|took|Starting database|Database.*completed|Total.*execution time|at [0-9])" &
TAIL_PID=$!

# Function to cleanup
cleanup() {
    echo -e "\n${YELLOW}Cleaning up...${NC}"
    kill $DAZ_PID 2>/dev/null
    kill $TAIL_PID 2>/dev/null
    wait $DAZ_PID 2>/dev/null
    wait $TAIL_PID 2>/dev/null
}

# Set trap for cleanup
trap cleanup EXIT

# Wait for plugins to initialize
echo -e "${YELLOW}Waiting for plugins to initialize...${NC}"
sleep 8

# Test SQL connectivity
echo -e "\n${BLUE}Running SQL connectivity test...${NC}"
cd /home/user/Documents/daz
timeout 10s ./scripts/test-sql-connectivity.sh

# Give some time to see all timing logs
sleep 2

# Show timing analysis
echo -e "\n${BLUE}=== Timing Analysis ===${NC}"
echo -e "${YELLOW}Database query operations:${NC}"
grep -E "Starting database query|Database query completed.*took" /tmp/daz_timing_test.log

echo -e "\n${YELLOW}Database exec operations:${NC}"
grep -E "Starting database exec|Database exec completed.*took" /tmp/daz_timing_test.log

echo -e "\n${YELLOW}Row processing times:${NC}"
grep -E "Row processing completed.*took" /tmp/daz_timing_test.log

echo -e "\n${YELLOW}Response delivery times:${NC}"
grep -E "delivered.*took" /tmp/daz_timing_test.log

echo -e "\n${YELLOW}Total execution times:${NC}"
grep -E "Total.*execution time" /tmp/daz_timing_test.log

echo -e "\n${BLUE}Test complete.${NC}"