#!/bin/sh
set -e

# Docker entrypoint script for feedspool container
# Manages both crond (for scheduled feed updates) and feedspool serve

echo "Starting feedspool container..."

# If the command is not 'serve', just run it directly without setting up cron
if [ "$1" != "serve" ]; then
    exec /usr/local/bin/feedspool "$@"
fi

# Set up cron job for feed updates
# CRON_SCHEDULE env var allows customization, defaults to every 30 minutes
CRON_SCHEDULE="${CRON_SCHEDULE:-*/30 * * * *}"
echo "Setting up cron job for feed updates (schedule: $CRON_SCHEDULE)..."
# Disable email notifications and redirect output to Docker logs
cat > /etc/crontabs/root << EOF
# Disable email notifications
MAILTO=""
# Run fetch and render on schedule, output to Docker logs
$CRON_SCHEDULE (cd /data && /usr/local/bin/feedspool purge && /usr/local/bin/feedspool fetch && /usr/local/bin/feedspool render) > /proc/1/fd/1 2> /proc/1/fd/2
EOF

# Start crond in foreground mode in background to capture PID correctly
echo "Starting cron daemon..."
crond -f &
CRON_PID=$!

# Show the crontab for debugging
echo "Cron jobs registered:"
crontab -l

# Function to handle shutdown signals
shutdown_handler() {
    echo "Received shutdown signal, stopping services..."
    kill $CRON_PID 2>/dev/null || true
    kill $SERVER_PID 2>/dev/null || true
    wait $CRON_PID 2>/dev/null || true
    wait $SERVER_PID 2>/dev/null || true
    exit 0
}

# Set up signal traps for graceful shutdown
trap 'shutdown_handler' SIGTERM SIGINT

# Create a default configuration file if it doesn't exist
if [ ! -f "/data/feedspool.yaml" ]; then
    echo "Creating default feedspool.yaml configuration..."
    
    # Detect which feed file format is present and configure accordingly
    FEED_FORMAT="text"
    FEED_FILENAME="feeds.txt"
    
    if [ -f "/data/feeds.opml" ]; then
        echo "Detected feeds.opml - configuring for OPML format"
        FEED_FORMAT="opml"
        FEED_FILENAME="feeds.opml"
    elif [ -f "/data/feeds.txt" ]; then
        echo "Detected feeds.txt - configuring for text format"
        FEED_FORMAT="text"
        FEED_FILENAME="feeds.txt"
    else
        echo "No feed file detected - will use default configuration (feeds.txt)"
    fi
    
    cat > /data/feedspool.yaml << YAML
# Auto-generated feedspool configuration for Docker
database: /data/feeds.db

# Default feed list settings
feedlist:
  format: "$FEED_FORMAT"
  filename: "$FEED_FILENAME"

# Render output settings  
render:
  output_dir: "/data/build"
  default_max_age: "24h"
  
# Server settings (can be overridden with PORT env var)
serve:
  port: 8889
  dir: "/data/build"

# Fetch settings
fetch:
  with_unfurl: true       # Enable metadata extraction
  concurrency: 32
  max_items: 100

# Unfurl settings
unfurl:
  skip_robots: false
  retry_after: "1h"
  concurrency: 8
YAML
    echo "Default configuration created at /data/feedspool.yaml (format: $FEED_FORMAT, file: $FEED_FILENAME)"
fi

# Initialize database if it doesn't exist
if [ ! -f "/data/feeds.db" ]; then
    echo "Initializing database..."
    /usr/local/bin/feedspool init || echo "Database initialization failed - continuing anyway"
fi

# Check if feed file exists before running fetch
if [ -f "/data/feeds.txt" ] || [ -f "/data/feeds.opml" ]; then
    # Run initial fetch and render in background to populate content
    echo "Starting initial fetch and render in background..."
    (
        /usr/local/bin/feedspool fetch || echo "Initial fetch failed - continuing anyway"
        /usr/local/bin/feedspool render || echo "Initial render failed - continuing anyway"
        echo "Initial fetch and render completed"
    ) &
    FETCH_PID=$!
    
    # Wait for build directory to exist before starting server
    echo "Waiting for build directory to be created..."
    timeout=60  # Maximum wait time in seconds
    elapsed=0
    while [ ! -d "/data/build" ] && [ $elapsed -lt $timeout ]; do
        sleep 1
        elapsed=$((elapsed + 1))
    done
    
    if [ -d "/data/build" ]; then
        echo "Build directory ready"
    else
        echo "Warning: Build directory not created within $timeout seconds, starting server anyway"
    fi
    
    # Don't wait for the fetch process to complete, let it run in background
    echo "Initial content loading in progress (PID: $FETCH_PID)"
else
    echo "==========================================================================="
    echo "WARNING: No feed file found!"
    echo ""
    echo "Please create one of the following files in your mounted volume:"
    echo "  - feeds.txt   (one URL per line)"  
    echo "  - feeds.opml  (OPML format)"
    echo ""
    echo "Example feeds.txt:"
    echo "  https://feeds.bbci.co.uk/news/rss.xml"
    echo "  https://www.reddit.com/r/programming.rss"
    echo ""
    echo "The container will continue running but won't have any feeds to display."
    echo "==========================================================================="
    
    # Create empty build directory so server can start
    mkdir -p "/data/build"
fi

# Function to start the server with monitoring
start_server() {
    echo "Starting feedspool serve..."
    /usr/local/bin/feedspool "$@" &
    SERVER_PID=$!
    echo "Server started with PID: $SERVER_PID"
}

# Function to monitor and restart the server
monitor_server() {
    restart_count=0
    max_restarts=5
    restart_delay=5
    
    while true; do
        if ! kill -0 $SERVER_PID 2>/dev/null; then
            echo "Server process $SERVER_PID has died!"
            
            if [ $restart_count -ge $max_restarts ]; then
                echo "Maximum restart attempts ($max_restarts) reached. Giving up."
                exit 1
            fi
            
            restart_count=$((restart_count + 1))
            echo "Restarting server (attempt $restart_count/$max_restarts) in $restart_delay seconds..."
            sleep $restart_delay
            
            start_server "$@"
            # Exponential backoff for restart delay
            restart_delay=$((restart_delay * 2))
        fi
        
        sleep 5
    done
}

# Start the server and monitor it
start_server "$@"
monitor_server "$@"