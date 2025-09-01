# Parallel Fetch and Unfurl - Development Plan

## Architecture Overview

The implementation will extend the existing fetch command to optionally run unfurl operations in parallel. Key architectural decisions:

1. **Simple in-memory queue**: Use Go channels for unfurl queue management
2. **Worker pool pattern**: Start unfurl workers immediately, populate queue as feeds are processed
3. **Reuse existing components**: Leverage current unfurl and fetcher packages
4. **Synchronization**: Use sync.WaitGroup to coordinate completion of both operations

## Implementation Strategy

### Phase 1: Configuration and CLI Setup
- Add fetch configuration section to config system
- Add --with-unfurl CLI flag
- Validate integration with existing flag handling

### Phase 2: Unfurl Queue Infrastructure
- Create unfurl queue management using channels
- Implement worker pool for unfurl operations
- Add queue depth tracking and progress reporting

### Phase 3: Integration with Fetch Process
- Modify fetcher to enqueue new items for unfurl
- Add database checking to avoid redundant unfurls
- Implement coordination between fetch and unfurl completion

### Phase 4: Logging and Error Handling
- Add debug and info level logging for parallel operations
- Ensure unfurl error handling matches existing behavior
- Integrate with existing progress reporting

## Detailed Implementation Steps

### Step 1: Add Configuration Support
**Objective**: Add `fetch.with_unfurl` configuration property and validate config loading

**Prompt for implementation**:
```
I need to add configuration support for the new parallel unfurl feature in the fetch command.

Please examine the existing config structure in `internal/config/config.go` and add:

1. A new `FetchConfig` struct with a `WithUnfurl` boolean field
2. Add `FetchConfig` to the main `Config` struct  
3. Update the `LoadConfig()` function to read `fetch.with_unfurl` from viper
4. Follow the existing patterns used for other config sections like `UnfurlConfig`

The configuration should default to `false` if not specified.
```

### Step 2: Add CLI Flag Support  
**Objective**: Add `--with-unfurl` flag to fetch command and integrate with config

**Prompt for implementation**:
```
Building on the configuration support from Step 1, I need to add CLI flag support for the parallel unfurl feature.

In `cmd/fetch.go`, please:

1. Add a new boolean variable `fetchWithUnfurl` following the pattern of other fetch flags
2. Add the `--with-unfurl` flag in the `init()` function with appropriate help text
3. In the `runFetch` function, read both the CLI flag and config value, with CLI flag taking precedence
4. Pass this boolean value to the fetcher (we'll modify the fetcher interface in the next step)

Follow the existing patterns used for other fetch command flags.
```

### Step 3: Create Unfurl Queue Infrastructure
**Objective**: Create the core unfurl queue and worker pool infrastructure

**Prompt for implementation**:
```
I need to create the unfurl queue infrastructure that will run in parallel with feed fetching.

Create a new file `internal/unfurl/queue.go` that implements:

1. An `UnfurlQueue` struct with:
   - A buffered channel for unfurl jobs (URLs to process)
   - A counter for queue depth tracking
   - Context for cancellation
   - Logger for progress reporting

2. An `UnfurlJob` struct containing:
   - URL to unfurl
   - Any metadata needed for database operations

3. Methods:
   - `NewUnfurlQueue(ctx context.Context, concurrency int, logger Logger) *UnfurlQueue`
   - `Start()` - starts worker pool
   - `Enqueue(job UnfurlJob)` - adds job to queue
   - `Close()` - signals no more jobs will be added
   - `Wait()` - waits for all workers to complete
   - `QueueDepth() int` - returns current queue size

4. Worker pool logic that:
   - Starts `concurrency` number of goroutines
   - Each worker processes jobs from the channel
   - Uses existing unfurl package functions for actual unfurl operations
   - Logs progress at debug level for individual operations
   - Logs progress at info level for queue depth changes

Use the existing `internal/unfurl` package functions for the actual unfurl operations.
```

### Step 4: Modify Fetcher Interface
**Objective**: Update fetcher to accept unfurl queue and populate it with new items

**Prompt for implementation**:
```
I need to modify the fetcher package to support enqueuing unfurl jobs for new feed items.

In `internal/fetcher/fetcher.go`:

1. Examine the current fetcher interface and main processing function
2. Add an optional `UnfurlQueue` parameter to the main fetcher function/struct
3. When processing feed items, check if:
   - UnfurlQueue is provided (not nil)
   - The item is new (not already in database)
   - The item URL doesn't already have metadata in database

4. For qualifying items, enqueue them to the unfurl queue
5. Add debug logging when items are enqueued: "Enqueuing unfurl for: [URL]"
6. Add info logging for batch enqueue operations: "Enqueued X items for unfurl"

The changes should be backward compatible - existing code should work unchanged when no UnfurlQueue is provided.
```

### Step 5: Database Integration
**Objective**: Add database checking to avoid unfurling URLs that already have metadata

**Prompt for implementation**:
```
I need to add database functionality to check if URLs already have unfurl metadata to avoid redundant operations.

In `internal/database` or as part of the unfurl package:

1. Create a function `HasUnfurlMetadata(db *sql.DB, url string) (bool, error)` that:
   - Checks if the given URL already has unfurl metadata stored
   - Returns true if metadata exists, false if not
   - Handles database errors appropriately

2. Create a batch version `HasUnfurlMetadataBatch(db *sql.DB, urls []string) (map[string]bool, error)` for efficiency

3. Integrate this check into the fetcher logic from Step 4:
   - Before enqueuing items for unfurl, batch check which ones already have metadata
   - Only enqueue items that don't have existing metadata
   - Log how many items were filtered out: "Filtered X items that already have metadata"

Follow the existing database patterns and error handling used in the codebase.
```

### Step 6: Integrate Parallel Operations in Fetch Command
**Objective**: Wire together fetch and unfurl operations with proper coordination

**Prompt for implementation**:
```
Now I need to integrate the unfurl queue with the main fetch command to enable parallel operations.

In `cmd/fetch.go`, modify the `runFetch` function to:

1. When `--with-unfurl` is enabled:
   - Create an UnfurlQueue with the unfurl concurrency settings (reuse existing unfurl concurrency config)
   - Start the unfurl queue workers
   - Pass the queue to the fetcher

2. Add proper synchronization:
   - Use sync.WaitGroup or similar to track both fetch and unfurl completion
   - Ensure the process waits for all unfurl operations to complete before exiting
   - Close the unfurl queue after fetch operations complete
   - Wait for unfurl queue to drain before program exit

3. Add high-level progress logging:
   - Info level: "Starting fetch with parallel unfurl (concurrency: X)"
   - Info level: "Fetch completed, waiting for Y unfurl operations to complete"
   - Info level: "All operations completed"

4. Handle errors appropriately:
   - Fetch errors should still cause non-zero exit
   - Unfurl errors should be logged but not affect exit status
   - Ensure cleanup happens even if errors occur

Follow existing error handling and logging patterns in the fetch command.
```

### Step 7: Enhanced Logging and Progress Reporting
**Objective**: Add comprehensive logging for visibility into parallel operations

**Prompt for implementation**:
```
I need to enhance the logging and progress reporting for the parallel unfurl operations.

1. In the UnfurlQueue (from Step 3), add:
   - Periodic info-level progress reports: "Unfurl progress: X completed, Y pending, Z in queue"
   - Debug-level logging for individual operations: "Starting unfurl for: [URL]", "Completed unfurl for: [URL]"
   - Error logging that matches existing unfurl command behavior

2. In the fetcher integration (Step 4/6), add:
   - Info-level logging when unfurl queue starts: "Starting unfurl queue with X workers"
   - Periodic updates during fetch: "Fetched X feeds, enqueued Y items for unfurl"

3. Create a progress ticker (using time.Ticker) that:
   - Reports queue depth every 10-30 seconds during active processing
   - Shows both fetch progress and unfurl queue status
   - Stops automatically when operations complete

4. Ensure all logging integrates with existing logging configuration:
   - Respects existing log levels
   - Uses existing logger instance from config
   - Follows existing log format patterns

The goal is to provide clear visibility into the parallel process without being too verbose.
```

### Step 8: Error Handling and Edge Cases
**Objective**: Ensure robust error handling and edge case management

**Prompt for implementation**:
```
I need to ensure robust error handling and manage edge cases for the parallel unfurl feature.

1. **Graceful shutdown handling**:
   - Handle SIGINT/SIGTERM signals properly
   - Ensure unfurl workers can be cancelled cleanly
   - Save progress before shutdown when possible

2. **Database connection handling**:
   - Ensure unfurl workers properly handle database connection errors
   - Implement connection pooling considerations if needed
   - Handle database locks gracefully

3. **Memory management**:
   - Prevent unbounded queue growth with reasonable buffer sizes
   - Handle cases where unfurl queue fills up faster than it can be processed
   - Add safeguards against memory leaks in worker goroutines

4. **Edge cases**:
   - Handle empty feeds (no new items to unfurl)
   - Handle malformed URLs gracefully
   - Handle network timeouts in unfurl operations
   - Ensure proper cleanup if fetch operations fail early

5. **Configuration validation**:
   - Validate that unfurl concurrency settings are reasonable
   - Handle cases where unfurl is enabled but no feeds are configured
   - Provide helpful error messages for configuration problems

Follow existing error handling patterns and ensure the system degrades gracefully under various failure conditions.
```

### Step 9: Testing and Validation
**Objective**: Create tests and validation for the parallel functionality

**Prompt for implementation**:
```
I need to add testing for the parallel fetch and unfurl functionality.

1. **Unit tests**:
   - Test UnfurlQueue operations (enqueue, worker processing, completion)
   - Test configuration loading for fetch.with_unfurl
   - Test database metadata checking functions
   - Test fetcher integration with unfurl queue

2. **Integration tests**:
   - Test end-to-end fetch with --with-unfurl flag
   - Test that unfurl operations complete before process exit
   - Test error handling scenarios
   - Test configuration file integration

3. **Performance/load tests**:
   - Test with large numbers of feed items
   - Verify memory usage stays reasonable
   - Test concurrent access to database
   - Validate that parallel processing provides performance benefits

4. **Validation scenarios**:
   - Create test feeds with various numbers of items
   - Verify only new items without existing metadata are unfurled
   - Confirm existing unfurl behavior is unchanged
   - Test graceful shutdown scenarios

Follow existing testing patterns in the codebase and use appropriate test fixtures.
```

### Step 10: Documentation and Final Integration
**Objective**: Complete documentation and final integration testing

**Prompt for implementation**:
```
I need to complete the documentation and perform final integration of the parallel fetch and unfurl feature.

1. **Update command help and documentation**:
   - Update fetch command help text to describe --with-unfurl flag
   - Add example usage scenarios to command descriptions
   - Document the new fetch.with_unfurl configuration option

2. **Configuration file examples**:
   - Update example config files to show the new fetch section
   - Document recommended concurrency settings
   - Provide guidance on when to enable parallel unfurl

3. **Final integration testing**:
   - Test all combinations of CLI flags and config file settings
   - Verify backward compatibility (existing scripts should work unchanged)
   - Test performance with real-world feeds
   - Validate logging output at different verbosity levels

4. **Code cleanup**:
   - Remove any debugging code or TODO comments
   - Ensure consistent code formatting
   - Add any missing error checks or edge case handling
   - Verify all public functions have appropriate documentation

5. **Performance verification**:
   - Compare performance of sequential vs parallel operations
   - Ensure the parallel version provides measurable benefits
   - Validate that resource usage (memory, database connections) is reasonable

The goal is to have a production-ready feature that seamlessly integrates with existing functionality.
```

## Testing Strategy

### Unit Testing
- Test unfurl queue operations in isolation
- Test configuration parsing and validation
- Test database metadata checking functions
- Test fetcher integration with mocked unfurl queue

### Integration Testing  
- Test complete fetch command with parallel unfurl enabled
- Test error scenarios and graceful degradation
- Test with various feed sizes and configurations
- Validate performance improvements over sequential operation

### Manual Testing
- Test with real-world feeds of various sizes
- Verify logging output at different verbosity levels
- Test configuration file and CLI flag combinations
- Validate database state after parallel operations

## Success Metrics

1. **Functional Requirements**:
   - Fetch command accepts --with-unfurl flag
   - Configuration file supports fetch.with_unfurl property
   - Only new items without existing metadata are unfurled
   - Process waits for all operations before exit

2. **Performance Requirements**:
   - Parallel processing shows measurable improvement over sequential
   - Memory usage remains reasonable even with large feeds
   - Database operations don't create excessive load

3. **Operational Requirements**:
   - Logging provides appropriate visibility into operations
   - Error handling maintains existing unfurl command behavior
   - Graceful shutdown works correctly
   - Integration with existing tooling is seamless

## Risk Mitigation

### Database Concurrency
- Use appropriate database connection pooling
- Handle database locks gracefully
- Test with realistic concurrent loads

### Memory Management
- Use buffered channels with reasonable limits
- Ensure proper cleanup of goroutines
- Monitor memory usage during testing

### Error Recovery
- Ensure partial failures don't leave system in inconsistent state
- Provide clear error messages for configuration issues
- Maintain existing behavior when unfurl operations fail

### Performance Impact
- Ensure parallel operations don't negatively impact fetch performance
- Validate that resource usage scales appropriately
- Provide configuration options to tune performance