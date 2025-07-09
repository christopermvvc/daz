#!/bin/bash
# Run Daz in console with output logging to file

# Create logs directory if it doesn't exist
mkdir -p logs

# Build if needed
if [ ! -f "bin/daz" ]; then
    echo "Binary not found. Running centralized build..."
    ./scripts/build-daz.sh
fi

# Generate log filename with timestamp
LOG_FILE="logs/daz_$(date +%Y%m%d_%H%M%S).log"

echo "Starting Daz..."
echo "Logging output to: $LOG_FILE"
echo "Press Ctrl+C to stop"
echo "----------------------------------------"

# Run Daz and tee output to both console and log file
./bin/daz \
    -channel ***REMOVED*** \
    -username ***REMOVED*** \
    -password ***REMOVED*** \
    -db-host localhost \
    -db-name daz \
    -db-user ***REMOVED*** \
    -db-pass ***REMOVED*** \
    2>&1 | tee "$LOG_FILE"