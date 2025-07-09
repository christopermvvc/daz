#!/bin/bash
# Simple script to run Daz for testing

# Build if needed
if [ ! -f "bin/daz" ]; then
    echo "Binary not found. Running centralized build..."
    ./scripts/build-daz.sh
fi

# Run Daz
echo "Starting Daz..."
./bin/daz \
    -channel ***REMOVED*** \
    -username ***REMOVED*** \
    -password ***REMOVED*** \
    -db-host localhost \
    -db-name daz \
    -db-user ***REMOVED*** \
    -db-pass ***REMOVED***