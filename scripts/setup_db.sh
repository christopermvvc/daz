#!/bin/bash

echo "This script needs to be run with sudo to fix PostgreSQL permissions"
echo "Run: sudo -u postgres psql -d daz < scripts/fix_permissions.sql"
echo ""
echo "Or if you have the postgres password, run:"
echo "psql -h localhost -U postgres -d daz < scripts/fix_permissions.sql"