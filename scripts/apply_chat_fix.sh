#!/bin/bash
# Script to apply chat duplicate fix to the database

set -e

echo "======================================"
echo "Applying Chat Duplicate Fix"
echo "======================================"

# Database connection details from config
DB_HOST="${DAZ_DB_HOST:-localhost}"
DB_PORT="${DAZ_DB_PORT:-5432}"
DB_NAME="${DAZ_DB_NAME:-daz}"
DB_USER="${DAZ_DB_USER:-***REMOVED***}"
export PGPASSWORD="${DAZ_DB_PASSWORD:?Error: DAZ_DB_PASSWORD environment variable is required}"

echo "Connecting to database: $DB_NAME@$DB_HOST:$DB_PORT"
echo ""

# Run the fix script
psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" -f ./fix_chat_duplicates.sql

echo ""
echo "======================================"
echo "Chat duplicate fix applied successfully!"
echo "======================================"
echo ""
echo "Next steps:"
echo "1. Build the updated bot binary: ./build-daz.sh"
echo "2. Restart the bot to test: ./run-console.sh"
echo "3. Verify no new duplicates are created"