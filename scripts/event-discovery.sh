#!/bin/bash
# Event Discovery Tool for CyTube
# This script analyzes logs to discover all CyTube event types

LOG_DIR="${1:-logs}"
OUTPUT_FILE="${2:-cytube-events-discovered.txt}"

echo "CyTube Event Discovery Report" > "$OUTPUT_FILE"
echo "Generated at: $(date)" >> "$OUTPUT_FILE"
echo "=============================" >> "$OUTPUT_FILE"
echo "" >> "$OUTPUT_FILE"

# Find all unknown events
echo "Unknown Events:" >> "$OUTPUT_FILE"
echo "--------------" >> "$OUTPUT_FILE"
grep -h "Unknown event type" "$LOG_DIR"/*.log 2>/dev/null | \
    sed -n "s/.*Unknown event type '\([^']*\)'.*/\1/p" | \
    sort | uniq -c | sort -nr >> "$OUTPUT_FILE"

echo "" >> "$OUTPUT_FILE"

# Find all received events (both known and unknown)
echo "All Received Events:" >> "$OUTPUT_FILE"
echo "-------------------" >> "$OUTPUT_FILE"
grep -h "Received.*event with data:" "$LOG_DIR"/*.log 2>/dev/null | \
    sed -n "s/.*Received \([^ ]*\) event with data:.*/\1/p" | \
    sort | uniq -c | sort -nr >> "$OUTPUT_FILE"

echo "" >> "$OUTPUT_FILE"

# Extract sample data for unknown events
echo "Sample Data for Unknown Events:" >> "$OUTPUT_FILE"
echo "-------------------------------" >> "$OUTPUT_FILE"
for event in $(grep -h "Unknown event type" "$LOG_DIR"/*.log 2>/dev/null | \
    sed -n "s/.*Unknown event type '\([^']*\)'.*/\1/p" | sort | uniq); do
    echo "" >> "$OUTPUT_FILE"
    echo "Event: $event" >> "$OUTPUT_FILE"
    grep -h "Unknown event type '$event'" "$LOG_DIR"/*.log 2>/dev/null | \
        head -1 | sed 's/.*with data: //' >> "$OUTPUT_FILE"
done

echo "" >> "$OUTPUT_FILE"
echo "Report saved to: $OUTPUT_FILE"
cat "$OUTPUT_FILE"