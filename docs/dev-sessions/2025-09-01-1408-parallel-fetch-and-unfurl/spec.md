# Parallel Fetch and Unfurl - Session Specification

## Overview

Currently, we have separate fetch and unfurl commands to fetch feeds and URL metadata, respectively.

We'd like to add an option to fetch (both CLI option and config file property) to enable an unfurl queue that runs in parallel with feed fetching.

How it should work is that, as feeds are fetched, whenever new items are found, enqueue metadata fetching unfurl operations for each of those new items.

The process should wait for both pending feed fetch and unfurl operations to complete before exiting.

We should implement debug and info logging as necessary to narrate this process.

## Goals

- Enable parallel unfurl operations during feed fetching to improve efficiency
- Maintain consistency with existing unfurl command behavior
- Provide clear visibility into parallel operations through logging and progress reporting
- Keep implementation simple and reliable

## Requirements

### CLI Interface
- Add `--with-unfurl` flag to fetch command
- Reuse existing concurrency configuration from both fetch and unfurl commands

### Configuration
- Add `fetch.with_unfurl` property to config file for default behavior
- Create fetch config section if it doesn't already exist

### Unfurl Queue Behavior
- Unfurl metadata for all new feed items discovered during fetch
- Only unfurl items that don't already have URL metadata in the database
- Use simple in-memory queue/channel populated as feeds are processed
- Do not persist unfurl queue - keep it ephemeral for this session
- Start unfurl workers immediately when command begins (queue initially empty)

### Concurrency Control
- Use separate concurrency limits for feed fetching vs unfurl operations
- Reuse existing configuration properties and options from fetch and unfurl commands

### Error Handling
- Log unfurl operation failures but do not exit with error status
- Update database records exactly as the existing unfurl command does
- Maintain existing unfurl command error handling behavior
- Only exit with error status if feed fetching itself fails

### Process Coordination
- Wait indefinitely for all unfurl operations to complete before exiting
- Ensure both feed fetch and unfurl operations are complete before process termination

### Logging and Progress Reporting
- **Debug level**: Show individual operation details (e.g., "Unfurling metadata for https://example.com/article")
- **Info level**: Show high-level progress (e.g., "Starting unfurl queue", "X feeds fetched, Y unfurls pending")
- Display current depth of unfurl queue
- Narrate progress through the unfurl queue
- Integrate with existing progress reporting mechanisms

## Success Criteria

- Fetch command can run with parallel unfurl operations enabled via CLI flag
- Configuration file property allows setting parallel unfurl as default behavior
- All new feed items are queued for unfurl (excluding those with existing metadata)
- Process exits only after both feed fetching and unfurl operations complete
- Error handling matches existing unfurl command behavior
- Logging provides appropriate detail at debug and info levels
- Performance improvement over sequential fetch-then-unfurl workflow

## Out of Scope

- Persistent unfurl queue across command invocations
- Custom filtering beyond "new items without existing metadata"
- Timeout-based early exit from unfurl operations
- Complex error recovery mechanisms beyond existing unfurl behavior