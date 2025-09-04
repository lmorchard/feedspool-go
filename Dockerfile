# Build stage
FROM docker.io/golang:alpine AS builder

# Install make and git for the build process
RUN apk add --no-cache make git

# Set working directory
WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary using make (not go build) for proper versioning
RUN make build

# Runtime stage
FROM docker.io/alpine:latest

# Install cronie for cron support
RUN apk add --no-cache cronie

# Create data directory for volume mount
RUN mkdir -p /data

# Copy binary from builder stage
COPY --from=builder /app/feedspool /usr/local/bin/feedspool

# Copy entrypoint script
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Set working directory to data mount point
WORKDIR /data

# Expose the default port
EXPOSE 8889

# Set entrypoint and default command
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["serve"]