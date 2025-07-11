#!/bin/bash

# Simple test to verify SQL plugin connectivity

set -e

echo "=== SQL Plugin Connectivity Test ==="
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

# Function to execute SQL query
exec_sql() {
    PGPASSWORD="$DB_PASSWORD" psql -U "$DB_USER" -d "$DB_NAME" -c "$1" 2>&1
}

# Step 1: Test direct database connection
print_status "Testing direct database connection..."
if exec_sql "SELECT version()"; then
    print_status "✅ Direct database connection successful"
else
    print_error "❌ Cannot connect to database directly"
    exit 1
fi

# Step 2: Test if daz tables exist
print_status "Checking daz tables..."
if exec_sql "\dt daz_*" | grep -q daz_; then
    print_status "✅ Daz tables exist"
else
    print_info "No daz tables found, will be created on startup"
fi

# Step 3: Create minimal test configuration
print_status "Creating minimal test configuration..."
cat > /tmp/daz-sql-test-config.json << 'EOF'
{
  "core": {
    "rooms": []
  },
  "event_bus": {
    "buffer_sizes": {
      "sql.": 200,
      "sql.exec": 200,
      "sql.query": 200
    }
  },
  "plugins": {
    "sql": {
      "database": {
        "host": "localhost",
        "port": 5432,
        "database": "daz",
        "user": "***REMOVED***",
        "password": "***REMOVED***",
        "max_connections": 10,
        "max_conn_lifetime": 3600,
        "connect_timeout": 30
      },
      "logger_rules": []
    }
  }
}
EOF

# Step 4: Test SQL operations
print_status "Testing SQL operations..."

# Create a test table
print_info "Creating test table..."
if exec_sql "CREATE TABLE IF NOT EXISTS daz_sql_test (id SERIAL PRIMARY KEY, test_data TEXT, created_at TIMESTAMP DEFAULT NOW())"; then
    print_status "✅ Test table created"
else
    print_error "❌ Failed to create test table"
fi

# Insert test data
print_info "Inserting test data..."
if exec_sql "INSERT INTO daz_sql_test (test_data) VALUES ('Test from $(date)')"; then
    print_status "✅ Test data inserted"
else
    print_error "❌ Failed to insert test data"
fi

# Query test data
print_info "Querying test data..."
if exec_sql "SELECT * FROM daz_sql_test ORDER BY id DESC LIMIT 5"; then
    print_status "✅ Test data queried successfully"
else
    print_error "❌ Failed to query test data"
fi

# Clean up test table
print_info "Cleaning up test table..."
exec_sql "DROP TABLE IF EXISTS daz_sql_test" >/dev/null 2>&1

print_status "SQL connectivity test completed"