#!/bin/sh
set -e

# Docker entrypoint script for feedspool container
# Manages both crond (for scheduled feed updates) and feedspool serve

echo "Starting feedspool container..."

# If the command is not 'serve', just run it directly without setting up cron
if [ "$1" != "serve" ]; then
    exec /usr/local/bin/feedspool "$@"
fi

# Set up cron job for feed updates (every 30 minutes)
echo "Setting up cron job for feed updates..."
# Disable email notifications and redirect output to Docker logs
cat > /etc/crontabs/root << EOF
# Disable email notifications
MAILTO=""
# Run fetch and render every 30 minutes, output to Docker logs
*/30 * * * * cd /data && /usr/local/bin/feedspool fetch && /usr/local/bin/feedspool render > /proc/1/fd/1 2> /proc/1/fd/2
EOF

# Start crond in background (level 8 = only errors, no info messages)
echo "Starting cron daemon..."
crond -f -d 8 &
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

# Initialize database if it doesn't exist
if [ ! -f "/data/feeds.db" ]; then
    echo "Initializing database..."
    /usr/local/bin/feedspool init || echo "Database initialization failed - continuing anyway"
fi

# Run initial fetch and render to populate content immediately
echo "Running initial fetch and render..."
/usr/local/bin/feedspool fetch || echo "Initial fetch failed - continuing anyway"
/usr/local/bin/feedspool render || echo "Initial render failed - continuing anyway"

# Start feedspool serve in the foreground
echo "Starting feedspool serve..."
exec /usr/local/bin/feedspool "$@"