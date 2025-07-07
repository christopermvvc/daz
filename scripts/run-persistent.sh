#!/bin/bash
# Run Daz in a tmux session for testing

SESSION_NAME="daz"

# Kill existing session if running
tmux kill-session -t "$SESSION_NAME" 2>/dev/null

# Build if needed
if [ ! -f "bin/daz" ]; then
    echo "Building Daz..."
    make build
fi

# Create new session and run
echo "Starting Daz in tmux session '$SESSION_NAME'..."
tmux new-session -d -s "$SESSION_NAME" ./scripts/run.sh

echo "Daz is running in background"
echo "Commands:"
echo "  tmux attach -t $SESSION_NAME    # View output"
echo "  tmux kill-session -t $SESSION_NAME    # Stop"
echo "  ./scripts/check-logs.sh    # Check database logs"