#!/bin/bash
# Kill all running instances of daz

echo "Looking for running daz processes..."

# Find all daz processes
PIDS=$(pgrep -f "bin/daz|./daz" | grep -v $$)

if [ -z "$PIDS" ]; then
    echo "No daz processes found running."
else
    echo "Found daz process(es): $PIDS"
    echo "Killing processes..."
    
    # Kill the processes
    echo $PIDS | xargs kill -TERM 2>/dev/null
    
    # Give them time to terminate gracefully
    sleep 2
    
    # Force kill any remaining
    REMAINING=$(pgrep -f "bin/daz|./daz" | grep -v $$)
    if [ ! -z "$REMAINING" ]; then
        echo "Force killing remaining processes: $REMAINING"
        echo $REMAINING | xargs kill -KILL 2>/dev/null
    fi
    
    echo "All daz processes have been terminated."
fi

# Also check for tmux sessions
if tmux has-session -t "daz-bot" 2>/dev/null; then
    echo "Found tmux session 'daz-bot', killing it..."
    tmux kill-session -t "daz-bot"
fi

# Check systemd service if running as root
if [ "$EUID" -eq 0 ]; then
    if systemctl is-active --quiet daz; then
        echo "Stopping daz systemd service..."
        systemctl stop daz
    fi
fi

echo "Done."