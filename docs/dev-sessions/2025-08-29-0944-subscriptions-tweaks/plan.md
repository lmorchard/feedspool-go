# Implementation Plan: Subscription Tweaks

## Overview

This plan implements a unified subscription management system for feedspool, focusing on treating external OPML and text files as the source of truth while making the database ephemeral. The implementation is broken down into small, iterative steps that build upon each other.

## Architecture Changes

### Core Components to Build
1. **Text File Parser**: Parse plain text feed lists with comments
2. **Feed List Abstraction**: Unified interface for OPML and text formats  
3. **Configuration Extension**: Add feedlist defaults to config
4. **HTML Autodiscovery**: RSS/Atom link tag parsing
5. **Command Unification**: Merge fetch/update, add subscribe/unsubscribe/export

### Execution Strategy
- Build foundational components first
- Implement new commands before modifying existing ones
- Maintain backwards compatibility until final switchover
- Comprehensive testing at each step

---

## Implementation Steps

### Phase 1: Foundation Components

#### Step 1: Text File Parser Implementation

**Context**: Create the foundation for plain text feed list support. This parser will handle the simple format with comments and blank lines.

**Prompt:**
```
I need to implement a plain text feed list parser for feedspool. Looking at the existing OPML parser in `internal/opml/parser.go`, I want to create a similar text parser.

Create a new file `internal/textlist/parser.go` that implements:

1. A `ParseTextList(reader io.Reader) ([]string, error)` function that:
   - Reads lines from the input
   - Ignores blank lines
   - Ignores comment lines starting with '#'
   - Returns a slice of feed URLs (one per line)
   - Validates each URL is properly formatted

2. A `WriteTextList(writer io.Writer, urls []string) error` function that:
   - Writes URLs one per line
   - Adds a header comment with timestamp
   - Returns any write errors

3. Include comprehensive error handling and input validation

Also create `internal/textlist/parser_test.go` with tests covering:
- Valid text file parsing
- Comment and blank line handling  
- Invalid URL handling
- Write functionality
- Edge cases (empty files, malformed input)

Use the same code style and patterns as the existing OPML parser.
```

#### Step 2: Feed List Abstraction Layer

**Context**: Create a unified interface that can work with both OPML and text formats, making it easy for commands to work with either format.

**Prompt:**
```
Building on the text parser from Step 1, I need to create a unified feed list abstraction. 

Create `internal/feedlist/feedlist.go` that provides:

1. A `Format` type (enum-like constants for "opml" and "text")

2. A `FeedList` interface with methods:
   - `GetURLs() []string`
   - `AddURL(url string) error` 
   - `RemoveURL(url string) error`
   - `Save(filename string) error`

3. Concrete implementations:
   - `OPMLFeedList` (wraps existing OPML functionality)
   - `TextFeedList` (uses the new text parser)

4. Factory functions:
   - `LoadFeedList(format Format, filename string) (FeedList, error)`
   - `NewFeedList(format Format) FeedList`

5. A `DetectFormat(filename string) Format` helper that looks at file extension

The abstraction should handle all the complexity of different formats, exposing a simple interface for the commands to use. Include error handling for file operations and format validation.

Create corresponding tests in `internal/feedlist/feedlist_test.go`.
```

#### Step 3: Configuration Extension

**Context**: Extend the existing configuration to support default feed list settings, laying the groundwork for simplified command usage.

**Prompt:**
```
I need to extend the existing configuration system in `internal/config/config.go` to support default feed list settings.

Looking at the current `Config` struct, add:

1. A new `FeedList` field with these properties:
   - `Format string` (default feed list format: "opml" or "text")
   - `Filename string` (default feed list file path)

2. Update `LoadConfig()` to read these new viper settings:
   - `feedlist.format` 
   - `feedlist.filename`

3. Update `GetDefault()` to provide sensible defaults (empty strings so we can detect when not configured)

4. Add helper methods to the Config struct:
   - `HasDefaultFeedList() bool` - returns true if both format and filename are configured
   - `GetDefaultFeedList() (format, filename string)` - returns the configured defaults

5. Update the config test file to cover the new functionality

The changes should be minimal and backwards compatible - existing configurations should continue working unchanged.
```

### Phase 2: New Commands Implementation

#### Step 4: Subscribe Command

**Context**: Implement the new subscribe command with autodiscovery support. This is a completely new command that adds feeds to lists.

**Prompt:**
```
Using the feed list abstraction from Step 2 and config from Step 3, implement a new `subscribe` command.

Create `cmd/subscribe.go` with:

1. Command definition supporting:
   - `feedspool subscribe [--format opml|text] [--filename path] [--discover] <url>`
   - Use config defaults when format/filename not specified
   - Require explicit format/filename if no defaults configured

2. The `--discover` flag implementation:
   - Fetch the HTML page at the given URL
   - Parse HTML for `<link>` tags with type="application/rss+xml" or "application/atom+xml"  
   - Extract the href URLs and use those instead of the original URL
   - Simple implementation following RSS autodiscovery standard

3. Core functionality:
   - Load existing feed list (or create new if doesn't exist)
   - Add URL(s) to the list
   - Warn if URL already exists (don't error)
   - Save the updated list
   - Provide user feedback on what was added

4. Error handling for:
   - Network errors during autodiscovery
   - File operation errors
   - Missing configuration when required

Follow the existing command patterns and style. Include proper help text and flag descriptions.
```

#### Step 5: Unsubscribe Command  

**Context**: Implement the unsubscribe command to remove feeds from lists, complementing the subscribe functionality.

**Prompt:**
```
Building on the subscribe command from Step 4, create the unsubscribe command.

Create `cmd/unsubscribe.go` with:

1. Command definition:
   - `feedspool unsubscribe [--format opml|text] [--filename path] <url>`
   - Same config default behavior as subscribe command
   - Single URL argument (no autodiscovery needed)

2. Core functionality:
   - Load existing feed list
   - Remove URL from the list
   - Warn if URL not found (don't error)
   - Save the updated list
   - Provide user feedback on what was removed

3. Error handling for:
   - File not found
   - File operation errors  
   - Missing configuration when required

4. Input validation:
   - Ensure URL is properly formatted
   - Handle edge cases gracefully

Use the same patterns as subscribe command for consistency. The implementation should be simpler since there's no autodiscovery complexity.
```

#### Step 6: Export Command

**Context**: Create the export command that writes database feeds to external lists, completing the bidirectional sync capabilities.

**Prompt:**
```
Using the feed list abstraction and existing database operations, implement the export command.

Create `cmd/export.go` with:

1. Command definition:
   - `feedspool export --format opml|text <filename>`
   - No config defaults for this command (explicit output required)
   - Format is required (no detection)

2. Core functionality:
   - Get all feeds from database using existing `database.GetAllFeeds()`
   - Create new feed list of specified format
   - Add all feed URLs to the list
   - For OPML format: include feed metadata (title, description) when available
   - For text format: simple URL list with optional header comment
   - Save to specified filename

3. Error handling:
   - Database connection issues
   - File write permissions
   - Empty database (warn but don't error)

4. User feedback:
   - Report number of feeds exported
   - Confirm successful write to file

The command should work regardless of config defaults and provide explicit control over the export process.
```

### Phase 3: Enhanced Existing Commands

#### Step 7: Enhanced Purge Command

**Context**: Extend the existing purge command to support feed list cleanup while maintaining existing functionality.

**Prompt:**
```
Looking at the existing `cmd/purge.go`, I need to extend it with new feed list cleanup functionality while preserving the current age-based purging.

Modify the existing purge command to support:

1. New flag options:
   - `--format opml|text` and corresponding filename argument
   - These should be mutually exclusive with the existing `--age` flag
   - Use config defaults when format/filename not specified (like other commands)

2. New feed list cleanup behavior:
   - When format/filename provided: load the feed list and get URLs
   - Get all feeds currently in database
   - Delete any database feeds (and their items) that are NOT in the loaded list
   - Use existing database operations where possible

3. Preserve existing functionality:
   - Age-based item purging should work exactly as before
   - All existing flags and behavior unchanged
   - Help text updated to describe both modes

4. Command validation:
   - Error if both age-based and list-based options provided
   - Error if list-based options are incomplete
   - Clear error messages for configuration issues

5. User feedback:
   - Report what would be deleted in dry-run mode
   - Report actual deletions performed
   - Distinguish between age-based and list-based cleanup in output

This should feel like a natural extension of the existing command rather than a completely new feature.
```

#### Step 8: Unified Fetch Command - Part 1 (File Support)

**Context**: Begin transforming the fetch command to support file inputs, preparing for the eventual removal of the update command.

**Prompt:**
```
Looking at the existing `cmd/fetch.go` and `cmd/update.go`, I need to extend fetch to support file inputs while preserving its current single-URL functionality.

Modify `cmd/fetch.go` to add:

1. New flag options (copied from update command):
   - `--format opml|text` with filename argument
   - `--concurrency`, `--timeout`, `--max-age`, `--max-items`, `--remove-missing`
   - Use config defaults for format/filename when not specified

2. Enhanced argument parsing:
   - No args: fetch all database feeds (existing behavior)
   - Single URL arg: fetch that URL (existing behavior)  
   - `--format` + filename: fetch from file (new behavior)
   - Validate argument combinations

3. File fetching logic (adapted from update command):
   - Use the feed list abstraction to load URLs from file
   - Apply all the same options as update command (max-age, remove-missing, etc.)
   - Use existing fetcher functionality
   - Same progress reporting and error handling

4. Preserve existing functionality:
   - Single URL fetching unchanged
   - Database-based fetching unchanged
   - All existing flags and behavior preserved

The goal is to make fetch a superset of both current fetch and update functionality, preparing for update command removal in the next step.
```

#### Step 9: Unified Fetch Command - Part 2 (Remove Update)

**Context**: Complete the fetch command unification by removing the update command and ensuring fetch handles all use cases.

**Prompt:**
```
Building on the enhanced fetch command from Step 8, now complete the unification by removing the update command.

1. Remove `cmd/update.go` entirely from the codebase

2. Update `cmd/root.go` to remove the update command registration

3. Verify the fetch command now handles all previous update use cases:
   - OPML file processing with all options
   - Text file processing (new capability)
   - Database feed fetching (fallback when no args)
   - Single URL fetching (original fetch behavior)

4. Update help text and command descriptions:
   - Make it clear that fetch now handles all feed fetching scenarios
   - Update examples to show the different usage patterns
   - Remove any references to the old update command

5. Testing verification:
   - Run existing tests to ensure no regression
   - Verify all update command scenarios work with fetch
   - Test config defaults integration

This step completes the command unification, giving users a single powerful fetch command that handles all feed acquisition scenarios.
```

### Phase 4: Integration and Polish

#### Step 10: Integration Testing

**Context**: Comprehensive testing of all new functionality working together, ensuring the complete workflow functions properly.

**Prompt:**
```
Now that all components are implemented, I need comprehensive integration testing to verify everything works together.

Create `integration_subscription_test.go` in the root directory with tests covering:

1. **Complete subscription workflow**:
   - Subscribe to feeds (both direct URLs and with autodiscovery)
   - Fetch from the created lists  
   - Export database to lists
   - Purge database based on lists
   - Unsubscribe from feeds

2. **Configuration defaults testing**:
   - Test all commands with config defaults set
   - Test fallback behavior when no defaults configured
   - Test command-line overrides of config defaults

3. **File format interoperability**:
   - Create feeds in OPML format, export to text
   - Create feeds in text format, export to OPML  
   - Verify data consistency across formats

4. **Error handling scenarios**:
   - Missing files, malformed files
   - Network errors during autodiscovery
   - Permission errors, disk space issues

5. **Edge cases**:
   - Empty lists, duplicate URLs
   - Invalid URLs, unreachable feeds
   - Large lists (performance testing)

The tests should verify that the complete system works as designed and that all the pieces integrate properly. Use realistic test data and scenarios.
```

#### Step 11: Documentation Updates

**Context**: Update all documentation, help text, and examples to reflect the new unified command structure.

**Prompt:**
```
With all functionality implemented, update the documentation to reflect the new command structure.

1. **Update README.md**:
   - Replace update command examples with unified fetch examples
   - Add examples for all new commands (subscribe, unsubscribe, export)
   - Show configuration file examples with feedlist defaults
   - Update the workflow examples to show the new subscription management approach

2. **Update help text in all commands**:
   - Ensure all flag descriptions are clear and accurate
   - Add examples to command help where useful
   - Make sure format options are consistently documented

3. **Create a migration guide section in README**:
   - Show how old update command usage maps to new fetch usage
   - Provide examples of common migration scenarios
   - Explain the new config defaults feature

4. **Update any inline code comments**:
   - Ensure command descriptions match their new unified functionality
   - Update any outdated references or examples in code comments

5. **Verify command consistency**:
   - All commands use consistent flag names and patterns
   - Error messages are helpful and consistent
   - Success messages provide useful feedback

The documentation should make it clear how to use the new unified system while helping existing users migrate from the old update command pattern.
```

#### Step 12: Final Testing and Cleanup

**Context**: Final verification that everything works correctly and cleanup of any remaining issues.

**Prompt:**
```
Perform final testing and cleanup to ensure the implementation is production ready.

1. **Comprehensive testing**:
   - Run all existing tests to ensure no regressions
   - Run the new integration tests
   - Test all commands manually with various argument combinations
   - Test configuration defaults in different scenarios

2. **Code cleanup**:
   - Remove any unused imports or functions
   - Ensure consistent code style across all new files
   - Verify error messages are user-friendly
   - Check that all new code follows existing patterns

3. **Performance verification**:
   - Test with large OPML files (hundreds of feeds)
   - Test with large text files
   - Verify concurrent operations work correctly
   - Check memory usage with large datasets

4. **Edge case verification**:
   - Test with malformed OPML and text files
   - Test network timeout scenarios
   - Test file permission issues
   - Test disk space limitations

5. **User experience validation**:
   - Verify command help is clear and complete
   - Test the most common workflows end-to-end
   - Ensure error messages guide users toward solutions
   - Verify config defaults work intuitively

Fix any issues discovered and ensure the implementation meets all requirements from the specification.
```

---

## Summary

This plan breaks the implementation into 12 manageable steps that build upon each other:

**Foundation (Steps 1-3)**: Build the core components needed by all commands
**New Commands (Steps 4-6)**: Implement the completely new functionality  
**Enhancement (Steps 7-9)**: Modify existing commands to support new capabilities
**Integration (Steps 10-12)**: Ensure everything works together and is production ready

Each step is designed to be implementable independently while building toward the complete vision. The implementation maintains backward compatibility until the final switchover and includes comprehensive testing throughout.