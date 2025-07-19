#!/bin/bash
# Run Daz in console with output logging to file

# Log rotation configuration
MAX_LOG_AGE_DAYS=7
MAX_TOTAL_SIZE_MB=25
MAX_LOG_SIZE_KB=256

# Function to delete logs older than MAX_LOG_AGE_DAYS
cleanup_old_logs() {
    find logs/ -name "daz_*.log" -type f -mtime +$MAX_LOG_AGE_DAYS -delete 2>/dev/null
}

# Function to enforce total size limit
cleanup_by_size() {
    local max_size_bytes=$((MAX_TOTAL_SIZE_MB * 1024 * 1024))
    
    while true; do
        # Calculate total size of all log files
        local total_size=$(find logs/ -name "daz_*.log" -type f -exec stat -c%s {} + 2>/dev/null | awk '{sum+=$1} END {print sum}' || echo 0)
        
        if [ "$total_size" -le "$max_size_bytes" ]; then
            break
        fi
        
        # Find and delete the oldest log file
        local oldest_log=$(find logs/ -name "daz_*.log" -type f -printf '%T@ %p\n' 2>/dev/null | sort -n | head -1 | cut -d' ' -f2-)
        if [ -n "$oldest_log" ]; then
            rm -f "$oldest_log"
            echo "Removed old log to maintain size limit: $oldest_log"
        else
            break
        fi
    done
}

# Function to get file size in KB
get_file_size_kb() {
    local file="$1"
    if [ -f "$file" ]; then
        local size_bytes=$(stat -c%s "$file" 2>/dev/null || echo 0)
        echo $((size_bytes / 1024))
    else
        echo 0
    fi
}

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

# Create logs directory if it doesn't exist
mkdir -p logs

# Perform log rotation cleanup at startup
echo "Performing log rotation cleanup..."
cleanup_old_logs
cleanup_by_size

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

# Check for verbose flag
VERBOSE_FLAG=""
if [ "$1" = "-v" ] || [ "$1" = "--verbose" ] || [ "$1" = "-verbose" ]; then
    VERBOSE_FLAG="-verbose"
    echo "Running in verbose mode..."
fi

# Function to rotate log when it exceeds size limit
rotate_log_if_needed() {
    local current_log="$1"
    local size_kb=$(get_file_size_kb "$current_log")
    
    if [ "$size_kb" -ge "$MAX_LOG_SIZE_KB" ]; then
        # Generate new log filename
        local new_log="logs/daz_$(date +%Y%m%d_%H%M%S).log"
        echo "" >> "$current_log"
        echo "--- Log rotated due to size limit (${size_kb}KB >= ${MAX_LOG_SIZE_KB}KB) ---" >> "$current_log"
        echo "--- Continuing in $new_log ---" >> "$current_log"
        echo "--- Continued from $current_log ---" > "$new_log"
        echo "$new_log"
    else
        echo "$current_log"
    fi
}

# Create a named pipe for log rotation
PIPE_FILE="/tmp/daz_log_pipe_$$"
mkfifo "$PIPE_FILE"

# Clean up pipe on exit
trap "rm -f $PIPE_FILE" EXIT INT TERM

# Start the log writer process in background
(
    current_log="$LOG_FILE"
    while IFS= read -r line; do
        echo "$line" | tee -a "$current_log"
        
        # Check if rotation is needed every 100 lines
        if [ $((RANDOM % 100)) -eq 0 ]; then
            new_log=$(rotate_log_if_needed "$current_log")
            if [ "$new_log" != "$current_log" ]; then
                echo "Rotating log file to: $new_log"
                current_log="$new_log"
                # Run cleanup to maintain size limits
                cleanup_by_size
            fi
        fi
    done < "$PIPE_FILE"
) &

LOG_WRITER_PID=$!

# Run Daz and pipe output through our custom log handler
# Set FORCE_COLOR to preserve colors when piping
FORCE_COLOR=1 ./bin/daz \
    -channel "$DAZ_CYTUBE_CHANNEL" \
    -username "$DAZ_CYTUBE_USERNAME" \
    -password "$DAZ_CYTUBE_PASSWORD" \
    -db-host "${DAZ_DB_HOST:-localhost}" \
    -db-port "${DAZ_DB_PORT:-5432}" \
    -db-user "$DAZ_DB_USER" \
    -db-pass "$DAZ_DB_PASSWORD" \
    -db-name "${DAZ_DB_NAME:-daz}" \
    $VERBOSE_FLAG \
    2>&1 > "$PIPE_FILE"

# Wait for log writer to finish
wait $LOG_WRITER_PID