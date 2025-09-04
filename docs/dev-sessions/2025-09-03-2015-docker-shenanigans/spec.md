# Docker Shenanigans Development Session Spec

Build a Docker container that runs `feedspool serve` as a web server to serve up the build directory, with automated feed fetching and rendering via cron.

## Goals

- Create a containerized feedspool deployment for easy hosting
- Enable automated feed updates without manual intervention  
- Provide simple Docker Hub distribution for tagged and rolling releases

## Requirements

### Core Functionality
- **Web Server**: Container runs `feedspool serve` to serve the build directory
- **PORT Environment Variable**: Add support for PORT env var to configure the server port (default: 8889)
- **Scheduled Updates**: Use crond to run `feedspool fetch && feedspool render` every 30 minutes (fixed interval)

### Docker Image
- **Multi-stage Build**: 
  - Stage 1: Go toolchain to build the feedspool binary
  - Stage 2: Minimal Alpine Linux runtime with crond
- **Base Image**: Alpine Linux (minimal but includes package manager for cron)
- **User**: Run as root (keeping it simple for now)

### File System & Configuration  
- **Single Mount Point**: All files in one mountpoint directory
- **Expected Files**: feedspool.yaml, feeds.txt/feeds.opml, SQLite database, build directory
- **Configuration**: Additional settings managed through feedspool.yaml, not container flags

### Logging & Error Handling
- **Logging**: All output (web server and cron jobs) goes to stdout/stderr for Docker logging
- **Error Handling**: If cron job fails, log error and continue - retry in next 30-minute cycle
- **Initial Run**: Run fetch and render immediately on startup for immediate content, then continue with scheduled updates

### Distribution
- **Docker Hub**: Publish to lmorchard/feedspool
- **Tagging**: 
  - Rolling release from main branch: `lmorchard/feedspool:latest`
  - Tagged releases: `lmorchard/feedspool:v0.0.5` (matching Git release tags)
- **GitHub Actions**: Single-job approach for Docker build and publish

## Success Criteria

- [ ] PORT environment variable support added to feedspool serve command
- [ ] Multi-stage Dockerfile builds working binary and minimal runtime image
- [ ] Container successfully runs web server on configurable port (default 8889)
- [ ] Cron job executes fetch && render every 30 minutes with proper logging
- [ ] File mount works correctly for config, database, and build output
- [ ] GitHub Actions workflow publishes both latest and tagged images to Docker Hub
- [ ] Container can be run with simple `docker run` command with volume mount
- [ ] All logging visible through `docker logs` command

## Implementation Notes

- Use `make build` (not `go build`) in Docker build stage for proper versioning
- Ensure cron job output is captured and forwarded to container stdout/stderr
- Test with actual feeds.txt/feedspool.yaml to verify full workflow
