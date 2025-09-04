#!/bin/sh
set -e

# Docker entrypoint script for feedspool container
# Manages both crond (for scheduled feed updates) and feedspool serve

echo "Starting feedspool container..."

# Set up cron job for feed updates (every 30 minutes)
echo "Setting up cron job for feed updates..."
echo "*/30 * * * * cd /data && /usr/local/bin/feedspool fetch && /usr/local/bin/feedspool render" > /etc/crontabs/root

# Ensure cron output goes to container logs
echo "*/30 * * * * cd /data && /usr/local/bin/feedspool fetch && /usr/local/bin/feedspool render > /proc/1/fd/1 2> /proc/1/fd/2" > /etc/crontabs/root

# Start crond in background with logging to stderr
echo "Starting cron daemon..."
crond -f -l 2 &
CRON_PID=$!

# Function to handle shutdown signals
shutdown_handler() {
    echo "Received shutdown signal, stopping services..."
    kill $CRON_PID 2>/dev/null || true
    wait $CRON_PID 2>/dev/null || true
    exit 0
}

# Set up signal traps for graceful shutdown
trap 'shutdown_handler' SIGTERM SIGINT

# Start feedspool serve in the foreground
echo "Starting feedspool serve..."
exec /usr/local/bin/feedspool "$@"