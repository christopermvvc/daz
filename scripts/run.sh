#!/bin/bash
# Simple script to run Daz for testing

# Source environment variables if .env file exists
if [ -f .env ]; then
    export $(cat .env | grep -v '^#' | xargs)
fi

# Validate required environment variables
if [ -z "$DAZ_CYTUBE_USERNAME" ]; then
    echo "Error: DAZ_CYTUBE_USERNAME environment variable is not set"
    exit 1
fi

if [ -z "$DAZ_CYTUBE_PASSWORD" ]; then
    echo "Error: DAZ_CYTUBE_PASSWORD environment variable is not set"
    exit 1
fi

if [ -z "$DAZ_CYTUBE_CHANNEL" ]; then
    echo "Error: DAZ_CYTUBE_CHANNEL environment variable is not set"
    exit 1
fi

# Build if needed
if [ ! -f "bin/daz" ]; then
    echo "Binary not found. Running centralized build..."
    ./scripts/build-daz.sh
fi

# Run Daz
echo "Starting Daz..."
./bin/daz \
    -channel "$DAZ_CYTUBE_CHANNEL" \
    -username "$DAZ_CYTUBE_USERNAME" \
    -password "$DAZ_CYTUBE_PASSWORD" \
    -db-host "${DAZ_DB_HOST:-localhost}" \
    -db-port "${DAZ_DB_PORT:-5432}" \
    -db-user "$DAZ_DB_USER" \
    -db-pass "$DAZ_DB_PASSWORD" \
    -db-name "${DAZ_DB_NAME:-daz}"