#!/bin/bash
# Simple script to run Daz for testing

# Build if needed
if [ ! -f "bin/daz" ]; then
    echo "Building Daz..."
    make build
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