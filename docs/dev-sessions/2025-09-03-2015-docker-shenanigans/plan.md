# Docker Shenanigans Development Plan

## Overview

This plan implements Docker containerization for feedspool with automated feed updates via cron. The implementation is broken into small, iterative steps that build upon each other, ensuring safe incremental progress.

## Prerequisites Analysis

Based on codebase analysis:
- Current `serve` command uses port 8080 by default, needs PORT env var support  
- Command structure uses cobra/viper with config file integration
- Server config is in `/internal/server/server.go`
- Default port constant is in `/cmd/constants.go`

## Implementation Phases

### Phase 1: Environment Variable Support
Add PORT environment variable support to the existing serve command.

### Phase 2: Docker Infrastructure  
Create the Dockerfile with multi-stage build and cron setup.

### Phase 3: GitHub Actions Integration
Set up automated Docker Hub publishing workflow.

### Phase 4: Testing & Documentation
Validate the complete workflow and provide usage examples.

---

## Detailed Implementation Steps

### Step 1: Add PORT Environment Variable Support

**Context**: The feedspool serve command currently supports port configuration via flag and config file, but needs environment variable support as specified in the requirements.

**Prompt**: 
```
Add support for a PORT environment variable to the feedspool serve command. The priority order should be:
1. Command line flag (--port) - highest priority  
2. Environment variable (PORT)
3. Config file value (serve.port)
4. Default value (8889) - lowest priority

The current serve command is in cmd/serve.go and uses cobra/viper. The default port constant (currently 8080) is defined in cmd/constants.go and needs to be changed to 8889.

Update the buildServeConfig function to check for the PORT environment variable between the viper config values and the command line flag override. Use os.Getenv() and strconv.Atoi() for the environment variable parsing, with proper error handling.

Test the changes by running the serve command with different combinations of port settings to ensure the priority order is correct.
```

**Expected Output**: Modified cmd/serve.go and cmd/constants.go with PORT environment variable support and updated default port.

---

### Step 2: Create Multi-Stage Dockerfile

**Context**: Create a Dockerfile that builds the feedspool binary and sets up a minimal Alpine runtime with cron support.

**Prompt**:
```
Create a multi-stage Dockerfile in the project root with the following structure:

Stage 1 (Builder):
- Use golang:alpine as base image
- Set working directory to /app  
- Copy go.mod and go.sum, run go mod download
- Copy all source files
- Run `make build` to create the feedspool binary (not `go build`)

Stage 2 (Runtime):
- Use alpine:latest as base image
- Install cronie package for cron support
- Create /data directory for the volume mount
- Copy the feedspool binary from builder stage to /usr/local/bin/
- Set working directory to /data
- Create a cron entry that runs `feedspool fetch && feedspool render` every 30 minutes
- Set up proper logging so cron output goes to stdout/stderr  
- Expose port 8889
- Create an entrypoint script that starts crond and then runs `feedspool serve`

The entrypoint should:
1. Start crond in the background  
2. Execute `feedspool serve` in the foreground
3. Handle signals properly for graceful shutdown

Ensure all output from both the web server and cron jobs is visible through `docker logs`.
```

**Expected Output**: Complete Dockerfile with multi-stage build, cron setup, and proper entrypoint configuration.

---

### Step 3: Create Docker Entrypoint Script

**Context**: The Dockerfile needs a proper entrypoint script to manage both crond and the web server, with proper signal handling and logging.

**Prompt**:
```
Create a shell script at `docker-entrypoint.sh` that serves as the Docker container entrypoint. The script should:

1. Set up cron job dynamically:
   - Write the cron entry "*/30 * * * * cd /data && /usr/local/bin/feedspool fetch && /usr/local/bin/feedspool render" to /etc/crontabs/root
   - Ensure cron output is logged to stdout/stderr (redirect to /proc/1/fd/1 and /proc/1/fd/2)

2. Start crond in the background:
   - Use `crond -f -l 2 &` to run cron with logging to stderr, in background

3. Handle signals properly:
   - Set up trap for SIGTERM and SIGINT to gracefully shutdown both crond and feedspool serve
   - Forward signals to child processes

4. Start feedspool serve in the foreground:
   - Use `exec` to replace the script process with feedspool serve
   - Pass any arguments from docker run to the serve command

5. Make the script executable and robust:
   - Add proper shebang (#!/bin/sh)  
   - Add error handling with `set -e`
   - Include comments explaining each section

Update the Dockerfile to copy this script and set it as the ENTRYPOINT, with CMD ["serve"] as the default argument.
```

**Expected Output**: Shell script `docker-entrypoint.sh` with proper process management, signal handling, and logging setup.

---

### Step 4: Create GitHub Actions Docker Workflow

**Context**: Set up automated building and publishing of Docker images to Docker Hub for both tagged releases and rolling latest from main branch.

**Prompt**:
```
Create a new GitHub Actions workflow file at `.github/workflows/docker.yml` that:

1. Triggers on:
   - Pushes to main branch (for latest tag)
   - Published releases (for version tags)

2. Uses a single job approach with these steps:
   - Checkout code
   - Set up Docker Buildx
   - Log in to Docker Hub using secrets DOCKER_USERNAME and DOCKER_PASSWORD
   - Extract metadata for tags and labels using docker/metadata-action
   - Build and push Docker image using docker/build-push-action

3. For tagging strategy:
   - Main branch pushes: tag as `lmorchard/feedspool:latest`
   - Release tags: tag as `lmorchard/feedspool:v1.2.3` (matching the release tag)

4. Use proper Docker layer caching for faster builds
5. Include labels with build information (commit SHA, build date, etc.)

The workflow should be straightforward and maintainable, following GitHub Actions best practices for Docker builds.
```

**Expected Output**: Complete GitHub Actions workflow file for automated Docker Hub publishing.

---

### Step 5: Test Docker Build Locally  

**Context**: Validate that the Docker image builds correctly and all components work together before pushing to CI/CD.

**Prompt**:
```
Test the Docker implementation locally with these steps:

1. Build the Docker image:
   ```
   docker build -t feedspool-test .
   ```

2. Create a test directory with minimal config:
   - Create a test directory (e.g., `/tmp/feedspool-test`)  
   - Add a basic `feedspool.yaml` configuration file
   - Add a simple `feeds.txt` with 1-2 test feeds

3. Run the container with volume mount:
   ```
   docker run -d -p 8889:8889 -v /tmp/feedspool-test:/data --name feedspool-test feedspool-test
   ```

4. Verify functionality:
   - Check that the web server is accessible on port 8889
   - Examine logs with `docker logs feedspool-test` to see both cron and server output
   - Wait for a cron cycle and verify feeds are fetched/rendered  
   - Test with different PORT environment variable values

5. Clean up test resources when done

Document any issues found and their solutions. Ensure the container can be stopped and restarted cleanly.
```

**Expected Output**: Verified working Docker image with documented test results and any fixes applied.

---

### Step 6: Add Docker Documentation

**Context**: Create clear documentation for users on how to use the Docker container, including examples and configuration options.

**Prompt**:
```
Add a "Docker Usage" section to the main README.md file (or create docker-specific documentation if the README is long). The documentation should include:

1. Quick start example:
   ```bash
   # Pull and run the latest image
   docker run -d -p 8889:8889 -v ./feedspool-data:/data lmorchard/feedspool:latest
   ```

2. Configuration explanation:
   - Required files in the mounted directory (feedspool.yaml, feeds.txt/feeds.opml)
   - PORT environment variable usage
   - Volume mount structure

3. Complete docker-compose.yml example for easier deployment

4. Build from source instructions:
   ```bash  
   git clone <repo>
   cd feedspool-go
   docker build -t feedspool .
   ```

5. Troubleshooting section:
   - Common issues and solutions
   - How to view logs  
   - How to manually trigger feed updates

Keep the documentation concise but complete, with practical examples that users can copy-paste.
```

**Expected Output**: Clear, practical Docker documentation with examples and troubleshooting guidance.

---

### Step 7: Final Integration Testing

**Context**: Comprehensive testing to ensure all components work together properly and meet the success criteria from the spec.

**Prompt**:
```
Perform comprehensive testing of the complete Docker implementation:

1. **Environment Variable Testing**:
   - Test default port (8889)
   - Test PORT environment variable override  
   - Test command line flag override (highest priority)
   - Verify priority order is correct

2. **Docker Image Testing**:
   - Build image from clean state
   - Verify image size is reasonable (should be small due to Alpine base)
   - Test with different configurations (minimal and complex feedspool.yaml)

3. **Cron Job Testing**:  
   - Verify cron runs every 30 minutes
   - Check that cron output appears in docker logs
   - Test behavior when feeds are unreachable (should continue and retry)

4. **Volume Mount Testing**:
   - Test with different host directory structures
   - Verify all required files are accessible
   - Test file permissions (read/write access for database, build directory)

5. **GitHub Actions Testing** (if possible):
   - Create a test tag and verify the workflow triggers
   - Check that the built image is properly tagged and pushed

Create a simple test script or documented test procedure that can be used to validate the implementation meets all success criteria from the spec.
```

**Expected Output**: Comprehensive test validation confirming all requirements are met, with any issues documented and resolved.

---

## Integration Notes

- Each step builds on the previous one, ensuring no orphaned code
- The PORT environment variable support is foundational for Docker usage  
- The Dockerfile and entrypoint script work together to provide the complete container experience
- GitHub Actions workflow leverages the existing Dockerfile without duplication
- Testing validates the complete integration before considering the feature done

## Success Validation

After completing all steps, the implementation should satisfy all success criteria from the spec:
- [x] PORT environment variable support added to feedspool serve command
- [x] Multi-stage Dockerfile builds working binary and minimal runtime image  
- [x] Container successfully runs web server on configurable port (default 8889)
- [x] Cron job executes fetch && render every 30 minutes with proper logging
- [x] File mount works correctly for config, database, and build output
- [x] GitHub Actions workflow publishes both latest and tagged images to Docker Hub
- [x] Container can be run with simple `docker run` command with volume mount
- [x] All logging visible through `docker logs` command