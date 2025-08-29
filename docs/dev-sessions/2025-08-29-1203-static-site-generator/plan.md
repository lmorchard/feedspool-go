# Static Site Generator - Implementation Plan

## Project Overview

This plan implements a static site generator for feedspool that creates HTML pages from feed data using Go's `html/template` package. The implementation follows an incremental approach, building core functionality first and expanding features progressively.

## Architecture Decisions

- **Templates**: Embedded in binary using `embed` package, extractable to filesystem
- **Assets**: Static CSS/JS embedded alongside templates
- **Data Model**: Reuse existing `database.Feed` and `database.Item` structs
- **CLI Pattern**: Follow existing cobra command structure in `cmd/` directory
- **Configuration**: Extend existing viper-based config system

## Implementation Phases

### Phase 1: Foundation & Template System
Build the core template system with embedded assets and basic rendering infrastructure.

### Phase 2: Render Command
Implement the `feedspool render` command with database queries and HTML generation.

### Phase 3: Serve Command  
Add the static file server with `feedspool serve` command.

### Phase 4: Enhanced Init Command
Extend `feedspool init` to extract templates and assets.

---

## Detailed Implementation Steps

### Step 1: Create Template System Foundation

**Goal**: Set up the basic template system with embedded assets and rendering infrastructure.

**Prompt for LLM:**
```
Create a new internal package at `internal/renderer/` for the static site generation system. This package should:

1. Create `internal/renderer/templates.go` with:
   - Use Go's `embed` package to embed default templates and assets
   - Create template directories: `templates/` and `assets/` in the project root
   - Embed a basic `index.html` template that displays feeds and items using the existing `database.Feed` and `database.Item` structs
   - Embed basic CSS for styling the HTML output
   - Provide functions to access embedded templates and assets

2. Create `internal/renderer/renderer.go` with:
   - `Renderer` struct that handles template loading and rendering
   - Methods to load templates from embedded files or custom directories  
   - `TemplateContext` struct containing `Feeds`, `GeneratedAt`, `TimeWindow` fields
   - `Render()` method that generates HTML from template and context

3. Default template structure:
   - Create `templates/index.html` - main template showing feeds with items grouped underneath
   - Create `assets/style.css` - basic styling for the generated HTML
   - Template should show: feed title, link, description, last updated
   - Items should be in collapsible `<details>` elements with: linked title, publication date, content/summary

Ensure the template uses the existing `database.Feed` and `database.Item` struct fields. Use proper Go template syntax with range loops and conditionals.
```

### Step 2: Add Database Queries for Time Filtering

**Goal**: Extend database operations to support time-based filtering of feeds and items.

**Prompt for LLM:**
```
Extend the database operations in `internal/database/operations.go` to support time-based queries needed for the render command:

1. Add new query methods:
   - `GetFeedsWithItemsByTimeRange(start, end time.Time, feedURLs []string) ([]Feed, map[string][]Item, error)`
   - `GetFeedsWithItemsByMaxAge(maxAge time.Duration, feedURLs []string) ([]Feed, map[string][]Item, error)`

2. These methods should:
   - Filter feeds that have been updated within the specified time range
   - Return associated items for each feed within the time range
   - Support optional feed URL filtering (empty slice means all feeds)
   - Return feeds and a map of feed URL to items for efficient template rendering
   - Use efficient SQL queries with proper WHERE clauses and JOINs

3. Add helper function:
   - `ParseTimeWindow(maxAge string, start, end string) (time.Time, time.Time, error)` to parse CLI time arguments

Make sure to handle edge cases like empty results, invalid time formats, and database errors properly. Use the existing database connection patterns from the current codebase.
```

### Step 3: Create Render Command Structure

**Goal**: Create the basic CLI command structure for `feedspool render`.

**Prompt for LLM:**
```
Create the `feedspool render` command in `cmd/render.go`:

1. Command definition with cobra:
   - Command name: "render"
   - Short description: "Generate static HTML site from feeds"
   - Support for all flags from spec: --max-age, --start, --end, --output, --templates, --static, --feeds, --format

2. Configuration integration:
   - Bind all flags to viper for config file support
   - Set appropriate default values (output: "./build", format: "text")
   - Support duration parsing for --max-age (e.g., "24h", "7d")

3. Command execution skeleton:
   - Validate input parameters and time ranges
   - Load feed list from --feeds file using existing feedlist package
   - Create output directory if it doesn't exist
   - Initialize renderer with template/asset directories
   - Print basic status messages

4. Register the command in `cmd/root.go`:
   - Add import and register renderCmd in init() function

Follow the existing command patterns from other files in cmd/ directory. Don't implement the actual rendering logic yet - just the command structure and parameter handling.
```

### Step 4: Implement Core Rendering Logic

**Goal**: Connect the render command to the database queries and template system.

**Prompt for LLM:**
```
Complete the render command implementation by adding the core rendering logic:

1. In `cmd/render.go`, implement the command execution:
   - Parse time parameters using the database helper function
   - Load feed URLs from specified feed list file (if provided)
   - Query database using the new time-based query methods
   - Create TemplateContext with feeds, items, and metadata
   - Call renderer to generate HTML output
   - Copy static assets to output directory

2. Enhance `internal/renderer/renderer.go`:
   - Implement asset copying from embedded files or custom directories
   - Add error handling for template parsing and execution
   - Support both embedded and custom template loading
   - Ensure proper file permissions and directory creation

3. Add comprehensive error handling:
   - Database connection failures
   - Template parsing errors
   - File system permissions
   - Invalid time ranges or feed lists

4. Integration with existing systems:
   - Use existing database connection from config
   - Reuse feed list parsing from feedlist package
   - Follow existing logging patterns with logrus

Make sure the generated HTML is valid and the static assets are copied correctly to the output directory.
```

### Step 5: Create Serve Command Foundation

**Goal**: Implement the basic static file server command.

**Prompt for LLM:**
```
Create the `feedspool serve` command in `cmd/serve.go`:

1. Command structure:
   - Command name: "serve"
   - Short description: "Serve static site files via HTTP"
   - Flags: --port (default 8080), --dir (directory to serve, default "./build")
   - Config file support for all options

2. HTTP server implementation:
   - Use Go's standard `net/http` package
   - Serve static files from specified directory
   - Add basic logging for requests (optional --verbose flag integration)
   - Graceful shutdown handling with signal catching
   - Security headers for static file serving

3. Server features:
   - Serve index.html for directory requests
   - Proper MIME type handling
   - Basic error pages (404, etc.)
   - Console output showing server URL and status

4. Register command:
   - Add to cmd/root.go init() function
   - Follow existing command registration patterns

Keep the server simple for now - just static file serving. We'll add API endpoints in a future session. Include proper error handling for port binding and file access issues.
```

### Step 6: Enhance Init Command for Template Extraction

**Goal**: Add template and asset extraction capabilities to the existing init command.

**Prompt for LLM:**
```
Enhance the existing `cmd/init.go` command to support template and asset extraction:

1. Add new flags to init command:
   - --extract-templates: Extract embedded templates to ./templates/ directory
   - --extract-assets: Extract embedded static assets to ./assets/ directory  
   - --templates-dir: Custom directory for template extraction
   - --assets-dir: Custom directory for asset extraction

2. Implement extraction logic:
   - Create extraction functions that write embedded files to filesystem
   - Preserve directory structure from embedded files
   - Handle file overwrites with confirmation prompts
   - Set proper file permissions

3. Integration with renderer package:
   - Use the embedded template/asset systems from internal/renderer
   - Create helper functions for filesystem extraction
   - Add validation for target directories

4. User experience:
   - Clear feedback on what files are being extracted
   - Confirmation prompts for overwriting existing files
   - Success/error reporting for each extracted file
   - Help text explaining the extraction process

5. Config file integration:
   - Support specifying extraction directories in config file
   - Bind new flags to viper configuration system

Follow existing patterns in the init command and maintain backwards compatibility with existing functionality.
```

### Step 7: Add Configuration Support and Polish

**Goal**: Complete configuration integration and add final polish to all commands.

**Prompt for LLM:**
```
Complete the configuration system integration and add final polish:

1. Configuration file support in `internal/config/config.go`:
   - Add render-specific config fields: output_dir, templates_dir, assets_dir, default_max_age
   - Add serve-specific config fields: port, serve_dir
   - Update config loading to handle new fields with appropriate defaults

2. Enhanced command validation:
   - Validate template directories exist when specified
   - Check output directory write permissions
   - Validate time format parsing with helpful error messages
   - Ensure feed list files exist and are readable

3. Improved user experience:
   - Add progress indicators for template parsing and rendering
   - Better error messages with suggestions for common issues
   - Status output showing number of feeds/items processed
   - File size and generation time reporting

4. Testing preparation:
   - Add basic validation functions that can be easily tested
   - Separate business logic from CLI handling
   - Create helper functions for common operations

5. Documentation updates:
   - Update help text for all modified commands
   - Add examples in command descriptions
   - Ensure consistency with existing command patterns

Focus on robustness, clear error messages, and maintaining consistency with the existing codebase patterns.
```

### Step 8: Integration Testing and Validation

**Goal**: Ensure all components work together properly and handle edge cases.

**Prompt for LLM:**
```
Add comprehensive integration and validation to ensure the complete system works:

1. End-to-end validation:
   - Create test functions to validate complete render workflow
   - Test template extraction and custom template loading
   - Validate serve command with generated static files
   - Test all command flag combinations and config file integration

2. Error handling improvements:
   - Add recovery for template parsing failures
   - Handle database connection issues gracefully
   - Provide fallbacks for missing static assets
   - Clear error messages for common user mistakes

3. Edge case handling:
   - Empty feed lists or no matching feeds
   - Invalid time ranges or formats
   - Missing or corrupted template files
   - File system permission issues
   - Large datasets that might impact performance

4. Performance considerations:
   - Add logging for performance-critical operations
   - Consider template caching for multiple renders
   - Efficient database query patterns
   - Memory usage for large item collections

5. Final integration:
   - Ensure all commands are properly registered
   - Verify config file precedence works correctly
   - Test with existing feedspool workflows
   - Validate that existing functionality remains unaffected

Create integration tests that can be run manually to validate the complete feature set works as specified.
```

---

## Implementation Notes

### Code Organization
- `internal/renderer/`: Core template and rendering logic
- `cmd/render.go`: Render command implementation  
- `cmd/serve.go`: Static file server command
- `cmd/init.go`: Enhanced with extraction capabilities
- `templates/`: Embedded default templates
- `assets/`: Embedded default static assets

### Key Dependencies
- Existing: `html/template`, `embed`, `cobra`, `viper`, `logrus`
- Database: Extend existing `internal/database` operations
- Configuration: Extend existing `internal/config` system

### Testing Strategy
- Unit tests for template rendering logic
- Integration tests for complete render workflow
- Manual testing with various feed configurations
- Performance testing with large feed datasets

### Future Considerations  
- Template caching for performance
- Additional output formats (RSS, JSON)
- API endpoints in serve command (next session)
- Advanced template features and themes

This plan ensures incremental development where each step builds on the previous ones, with no orphaned code and proper integration at each stage.