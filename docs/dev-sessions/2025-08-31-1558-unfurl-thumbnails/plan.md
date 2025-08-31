# Unfurl Thumbnails - Implementation Plan

## High-Level Implementation Phases

1. **Foundation**: Database schema and basic data models
2. **HTTP Client**: Shared HTTP client for controlled fetching
3. **Metadata Parser**: Core unfurling logic with library integration
4. **Database Integration**: Repository layer for metadata storage
5. **CLI Command**: Unfurl subcommand with single URL and batch modes
6. **Purge Enhancement**: Update purge to handle metadata cleanup
7. **Template Updates**: Add thumbnails and favicons to HTML output
8. **Testing & Polish**: Integration tests and error handling

## Detailed Step-by-Step Implementation

### Phase 1: Database Foundation

#### Step 1.1: Create Database Migration
- Add migration file for url_metadata table
- Include proper indexes and constraints
- Add trigger for updated_at timestamp

#### Step 1.2: Create URLMetadata Model
- Define struct with proper JSON tags
- Add database tags for SQLite
- Include helper methods for JSON metadata

### Phase 2: HTTP Client Package

#### Step 2.1: Create Shared HTTP Client
- Extract common HTTP logic from fetcher
- Add configurable timeout and user-agent
- Implement redirect following

#### Step 2.2: Add Response Size Limiting
- Implement 100KB response limit
- Stream response for efficiency
- Handle partial content gracefully

### Phase 3: Metadata Parser

#### Step 3.1: Research and Select Library
- Evaluate unfurlist and alternatives
- Check MIT license compatibility
- Test with sample URLs

#### Step 3.2: Create Unfurl Package
- Wrapper around chosen library
- Parse OpenGraph and Twitter Cards
- Extract favicon URLs

#### Step 3.3: Add Robots.txt Support
- Check robots.txt before fetching
- Cache robots.txt results
- Respect crawl delays

### Phase 4: Database Integration

#### Step 4.1: Create Metadata Repository
- CRUD operations for url_metadata
- Handle upserts for existing URLs
- Query methods for batch operations

#### Step 4.2: Add Retry Logic
- Check last_fetch_at for retry eligibility
- Store failure status codes
- Implement 1-hour retry window

### Phase 5: CLI Command Implementation

#### Step 5.1: Create Unfurl Command Structure
- Add command to cobra CLI
- Define flags (limit, format, etc.)
- Handle single URL vs batch mode

#### Step 5.2: Implement Single URL Mode
- Fetch or retrieve metadata for URL
- JSON output formatting
- Store results in database

#### Step 5.3: Implement Batch Mode
- Query items without metadata
- Concurrent fetching with worker pool
- Progress reporting

### Phase 6: Purge Enhancement

#### Step 6.1: Update Purge Logic
- Query orphaned metadata
- Delete metadata without item references
- Maintain referential integrity

### Phase 7: Template Updates

#### Step 7.1: Update Item Template
- Add thumbnail display with CSS
- Implement lazy loading
- Add fallback to placeholder

#### Step 7.2: Update Feed Template
- Add favicon to feed headers
- Handle missing favicons gracefully

#### Step 7.3: Add CSS Styling
- 150x150 square thumbnails
- object-fit: contain
- Responsive layout adjustments

### Phase 8: Testing & Polish

#### Step 8.1: Unit Tests
- Test metadata parsing
- Test retry logic
- Test database operations

#### Step 8.2: Integration Tests
- Test full unfurl workflow
- Test concurrent fetching
- Test error scenarios

---

## LLM Implementation Prompts

### Prompt 1: Database Migration and Model

```
Create a database migration for a url_metadata table in a Go application using SQLite. The table should store:
- url (TEXT PRIMARY KEY)
- title, description, image_url, favicon_url (TEXT)
- metadata (JSON)
- last_fetch_at (TIMESTAMP)
- fetch_status_code (INTEGER)
- fetch_error (TEXT)
- created_at, updated_at (TIMESTAMP with defaults)

Also create a Go struct model for URLMetadata with appropriate tags for database and JSON serialization. Include a method to parse/store arbitrary metadata in the JSON field.

The migration should follow the existing pattern in internal/database/migrations.go and the model should be in internal/database/models.go.
```

### Prompt 2: Shared HTTP Client

```
Extract and refactor the HTTP client logic from internal/fetcher into a new shared package internal/httpclient. The client should:
- Support configurable timeout
- Use custom User-Agent "feedspool/1.0"
- Follow redirects automatically
- Limit response size to 100KB for metadata fetching
- Share configuration with existing feed fetcher

Create a clean interface that both feed fetching and metadata fetching can use. Ensure the existing fetcher continues to work with the new shared client.
```

### Prompt 3: Metadata Parser Package

```
Create a new package internal/unfurl that wraps the github.com/Doist/unfurlist library (or similar) to extract metadata from HTML pages. The package should:
- Parse OpenGraph tags (og:title, og:description, og:image)
- Parse Twitter Card tags
- Find favicon URLs (multiple strategies)
- Return a structured result matching our URLMetadata model
- Handle errors gracefully
- Work within the 100KB response limit

Include a simple interface that accepts HTML content and returns parsed metadata.
```

### Prompt 4: Robots.txt Support

```
Add robots.txt checking to the internal/unfurl package. Before fetching a URL for metadata:
- Check if robots.txt allows fetching
- Cache robots.txt results to avoid repeated fetches
- Use a simple in-memory cache with TTL
- Respect User-Agent specific rules for "feedspool"
- Fall back to "*" rules if no specific match

Integrate this check into the metadata fetching workflow.
```

### Prompt 5: Metadata Repository

```
Create internal/database/metadata_repository.go with methods for:
- GetMetadata(url string) - retrieve existing metadata
- UpsertMetadata(metadata *URLMetadata) - insert or update
- GetURLsNeedingFetch(limit int) - find URLs without metadata or due for retry
- DeleteOrphanedMetadata() - remove metadata for URLs with no item references
- ShouldRetryFetch(metadata *URLMetadata) - check if 1 hour has passed since failure

Include proper error handling and use existing database connection patterns.
```

### Prompt 6: Unfurl CLI Command - Basic Structure

```
Create cmd/unfurl.go implementing a new "unfurl" subcommand using cobra. The command should:
- Accept optional URL argument for single URL mode
- Support --limit flag for batch processing
- Support --format json flag for output formatting
- Use existing command patterns from other cmd/*.go files

Start with the command structure and flag parsing, preparing for the actual implementation logic.
```

### Prompt 7: Unfurl CLI Command - Single URL Mode

```
Implement single URL mode in cmd/unfurl.go:
- If URL provided as argument, fetch/retrieve metadata for that specific URL
- Check database first, return cached if exists
- If not cached or needs retry, fetch fresh metadata
- Store result in database
- If --format json, output result to stdout as JSON
- Handle errors appropriately with clear messages

Use the previously created unfurl and repository packages.
```

### Prompt 8: Unfurl CLI Command - Batch Mode

```
Implement batch mode in cmd/unfurl.go:
- Query database for item URLs without metadata
- Respect --limit flag if provided
- Use worker pool for concurrent fetching (similar to feed fetcher)
- Show progress updates
- Handle errors without stopping entire batch
- Store all results in database

Reuse concurrent patterns from cmd/fetch.go where appropriate.
```

### Prompt 9: Update Purge Command

```
Modify cmd/purge.go to handle metadata cleanup:
- After purging items, identify orphaned metadata
- Delete metadata rows where URL doesn't exist in any remaining items
- Add logging for metadata cleanup statistics
- Ensure this doesn't affect performance significantly

Maintain backward compatibility with existing purge functionality.
```

### Prompt 10: Update HTML Templates - Items

```
Update internal/renderer/templates/items.html to display thumbnails:
- Add thumbnail image for each item if image_url exists
- Use 150x150px square with CSS object-fit: contain
- Implement lazy loading with loading="lazy"
- Add fallback to placeholder image if no thumbnail
- Ensure responsive layout works on mobile

Also update the CSS file to style the thumbnails appropriately.
```

### Prompt 11: Update HTML Templates - Feeds

```
Update internal/renderer/templates/feeds.html to display favicons:
- Add favicon next to each feed title if favicon_url exists
- Size favicons appropriately (16x16 or 24x24)
- Handle missing favicons gracefully
- Ensure layout remains clean and readable

Update CSS as needed for favicon styling.
```

### Prompt 12: Integration and Testing

```
Create integration tests for the unfurl feature:
- Test database operations (insert, update, query)
- Test metadata parsing with sample HTML
- Test retry logic with failed fetches
- Test concurrent batch processing
- Test template rendering with metadata

Add the tests to internal/unfurl/unfurl_test.go and cmd/unfurl_test.go following existing test patterns.
```

### Prompt 13: Final Integration

```
Wire everything together:
- Ensure unfurl command is registered in cmd/root.go
- Update any configuration needed in internal/config
- Add example usage to README or documentation
- Test full workflow: fetch feeds -> unfurl metadata -> render HTML
- Verify thumbnails and favicons appear correctly

Make any final adjustments for a smooth user experience.
```

---

## Implementation Notes

- Each prompt builds on previous work
- No orphaned code - everything integrates
- Incremental testing possible at each step
- Follows existing codebase patterns
- Maintains backward compatibility
- Progressive enhancement approach

## Risk Mitigation

- Start with database and models (low risk)
- HTTP client refactor tested against existing fetcher
- Library integration isolated in unfurl package
- CLI command can be tested independently
- Template changes are visual only, won't break functionality