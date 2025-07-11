#!/bin/bash

# Script to apply the retry queue schema to the database
# Usage: ./apply_retry_queue_schema.sh [--rollback]

set -e

# Configuration
DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-5432}"
DB_NAME="${DB_NAME:-daz}"
DB_USER="${DB_USER:-***REMOVED***}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

# Function to print colored output
print_status() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

# Function to check if schema exists
check_schema_exists() {
    local result=$(psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -tAc \
        "SELECT EXISTS (SELECT 1 FROM pg_tables WHERE tablename = 'daz_retry_queue');")
    echo "$result"
}

# Function to apply schema
apply_schema() {
    print_status "Applying retry queue schema to database..."
    
    if [[ $(check_schema_exists) == "t" ]]; then
        print_warning "Retry queue schema already exists!"
        read -p "Do you want to continue anyway? This may cause errors. (y/N): " -n 1 -r
        echo
        if [[ ! $REPLY =~ ^[Yy]$ ]]; then
            print_status "Aborted."
            exit 0
        fi
    fi
    
    if psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" \
        -f "$SCRIPT_DIR/create_retry_queue_schema.sql"; then
        print_status "Retry queue schema applied successfully!"
        
        # Show summary
        print_status "Verifying installation..."
        psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -c \
            "SELECT 'Tables:' as type, COUNT(*) as count FROM pg_tables WHERE tablename LIKE 'daz_retry%'
             UNION ALL
             SELECT 'Functions:', COUNT(*) FROM pg_proc WHERE proname LIKE '%retry%'
             UNION ALL  
             SELECT 'Indexes:', COUNT(*) FROM pg_indexes WHERE tablename LIKE 'daz_retry%'
             UNION ALL
             SELECT 'Views:', COUNT(*) FROM pg_views WHERE viewname LIKE 'daz_retry%';"
    else
        print_error "Failed to apply retry queue schema!"
        exit 1
    fi
}

# Function to rollback schema
rollback_schema() {
    print_warning "This will remove all retry queue tables and data!"
    read -p "Are you sure you want to rollback? (y/N): " -n 1 -r
    echo
    
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        print_status "Rollback cancelled."
        exit 0
    fi
    
    print_status "Rolling back retry queue schema..."
    
    if psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" \
        -f "$SCRIPT_DIR/rollback_retry_queue_schema.sql"; then
        print_status "Retry queue schema rolled back successfully!"
    else
        print_error "Failed to rollback retry queue schema!"
        exit 1
    fi
}

# Main script
main() {
    print_status "Retry Queue Schema Manager"
    print_status "Database: $DB_USER@$DB_HOST:$DB_PORT/$DB_NAME"
    
    # Check for rollback flag
    if [[ "$1" == "--rollback" ]]; then
        rollback_schema
    else
        apply_schema
    fi
}

# Check if psql is available
if ! command -v psql &> /dev/null; then
    print_error "psql command not found! Please install PostgreSQL client."
    exit 1
fi

# Check if required files exist
if [[ ! -f "$SCRIPT_DIR/create_retry_queue_schema.sql" ]]; then
    print_error "create_retry_queue_schema.sql not found in $SCRIPT_DIR"
    exit 1
fi

if [[ "$1" == "--rollback" ]] && [[ ! -f "$SCRIPT_DIR/rollback_retry_queue_schema.sql" ]]; then
    print_error "rollback_retry_queue_schema.sql not found in $SCRIPT_DIR"
    exit 1
fi

# Run main function
main "$@"