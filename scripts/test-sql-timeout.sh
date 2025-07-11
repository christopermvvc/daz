#!/bin/bash

# Test script to check if SQL operations are timing out

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== SQL Timeout Test ===${NC}"
echo -e "${YELLOW}This test will check if SQL operations are timing out${NC}"
echo

# First check if PostgreSQL is running
echo -e "${BLUE}Checking PostgreSQL status...${NC}"
if sudo systemctl is-active --quiet postgresql; then
    echo -e "${GREEN}✓ PostgreSQL is running${NC}"
else
    echo -e "${RED}✗ PostgreSQL is not running${NC}"
    echo -e "${YELLOW}Starting PostgreSQL...${NC}"
    sudo systemctl start postgresql
    sleep 2
fi

# Test direct database connection
echo -e "\n${BLUE}Testing direct database connection...${NC}"
PGPASSWORD=***REMOVED*** psql -h localhost -U ***REMOVED*** -d daz_db -c "SELECT 1;" >/dev/null 2>&1
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Direct database connection successful${NC}"
else
    echo -e "${RED}✗ Direct database connection failed${NC}"
    exit 1
fi

# Start daz in background
echo -e "\n${BLUE}Starting Daz...${NC}"
cd /home/user/Documents/daz
./bin/daz >/tmp/daz_test.log 2>&1 &
DAZ_PID=$!
echo "Daz PID: $DAZ_PID"

# Wait for plugins to initialize
echo -e "${YELLOW}Waiting for plugins to initialize...${NC}"
sleep 5

# Monitor logs in background
tail -f /tmp/daz_test.log | grep -E "(SQL|Query|Exec|took|at [0-9])" &
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

# Test 1: Simple query with timeout
echo -e "\n${BLUE}Test 1: Simple query (5 second timeout)...${NC}"
timeout 5s bash << 'EOF'
cd /home/user/Documents/daz
./scripts/test-sql-connectivity.sh 2>&1 | grep -E "(SQL|Query|Exec|took|at [0-9]|Timeout)"
EOF

if [ $? -eq 124 ]; then
    echo -e "${RED}✗ Query timed out after 5 seconds${NC}"
else
    echo -e "${GREEN}✓ Query completed within timeout${NC}"
fi

# Test 2: Check if SQL plugin is responding
echo -e "\n${BLUE}Test 2: Checking SQL plugin responsiveness...${NC}"
grep -E "SQL Plugin.*Ready|SQL Plugin.*connected" /tmp/daz_test.log
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ SQL plugin is ready${NC}"
else
    echo -e "${RED}✗ SQL plugin may not be ready${NC}"
fi

# Test 3: Look for timing information in logs
echo -e "\n${BLUE}Test 3: Analyzing timing logs...${NC}"
echo -e "${YELLOW}Recent SQL timing entries:${NC}"
grep -E "took [0-9]+.*s|Total.*execution time" /tmp/daz_test.log | tail -10

# Test 4: Check for hanging operations
echo -e "\n${BLUE}Test 4: Checking for hanging operations...${NC}"
STARTED_OPS=$(grep -c "Starting database" /tmp/daz_test.log)
COMPLETED_OPS=$(grep -c "Database.*completed" /tmp/daz_test.log)
echo -e "Started operations: $STARTED_OPS"
echo -e "Completed operations: $COMPLETED_OPS"

if [ $STARTED_OPS -gt $COMPLETED_OPS ]; then
    echo -e "${RED}✗ Some operations may be hanging (started: $STARTED_OPS, completed: $COMPLETED_OPS)${NC}"
else
    echo -e "${GREEN}✓ All operations completed${NC}"
fi

# Show last 20 lines of log
echo -e "\n${BLUE}Last 20 lines of Daz log:${NC}"
tail -20 /tmp/daz_test.log

echo -e "\n${BLUE}Test complete. Check the output above for timing information.${NC}"