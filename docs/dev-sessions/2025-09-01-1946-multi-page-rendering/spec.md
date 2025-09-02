# Dev Session Spec: Multi-Page Rendering

## Overview

The render command currently produces a single index.html file containing all feeds and their items. This spec defines a new multi-page rendering approach where:

1. index.html becomes a lightweight feed directory
2. Each feed gets its own HTML page with full content
3. A custom web component enables lazy-loading of feed content on demand

## Goals

1. Split monolithic index.html into multiple pages for better performance
2. Implement progressive enhancement with graceful fallback
3. Create a reusable `<link-loader>` component for dynamic content loading
4. Maintain standalone viewability of all generated pages

## File Structure

### Output Structure
```
output/
├── index.html           # Feed directory page
└── feeds/
    ├── 1.html          # Individual feed page (ID-based)
    ├── 2.html
    └── ...
```

### Naming Convention
- Individual feed files: `feeds/{database_id}.html`
- Example: `feeds/12.html` for feed with ID 12

## index.html Structure

### Feed Entry Format
Each feed in the directory will be structured as:

```html
<link-loader>
  <div class="feed-header">
    <img src="favicon.ico" alt="">
    <h2>Feed Title</h2>
    <time>Latest item timestamp</time>
  </div>
  <a href="feeds/12.html#feed-12">Read more...</a>
</link-loader>
```

### Key Changes
- Becomes a feed directory/listing page
- Contains lightweight feed metadata only
- Each feed wrapped in `<link-loader>` custom element
- Links use fragment identifiers to specify extraction target

## Individual Feed Pages (feed.html template)

### Requirements
- Complete standalone HTML pages
- Include same header/footer as index.html for consistency
- Can be viewed independently without JavaScript
- Contains full feed content and all items

### Content Structure
```html
<article class="feed" id="feed-12">
  <!-- Full feed content and items -->
</article>
```

### ID Pattern
- Format: `id="feed-{database_id}"`
- Must match fragment in link from index.html
- Used by `<link-loader>` to extract correct content

## Custom Element: `<link-loader>`

### Basic Behavior
1. Wraps content containing a link
2. Finds first anchor tag within itself
3. Observes visibility using Intersection Observer
4. When visible, fetches linked page
5. Extracts element specified by fragment ID
6. Replaces link with extracted content

### Loading States
- **Before loading**: Shows original "Read more..." link
- **During loading**: Changes link text to "Loading..."
- **Success**: Replaces link with fetched content
- **Error**: Replaces link text with error message
- **Already loaded**: Keeps loaded content (no reload)

### Implementation Details
- Component remains as wrapper after loading
- Original link is completely removed after successful load
- No configuration attributes in initial version
- Targets first anchor tag found within component

### Example Usage
```html
<link-loader>
  <a href="feeds/12.html#feed-12">Read more...</a>
</link-loader>
```

After loading:
```html
<link-loader>
  <article class="feed" id="feed-12">
    <!-- Loaded content -->
  </article>
</link-loader>
```

## Render Command Changes

### Required Modifications
1. Generate index.html as feed directory
2. Create `feeds/` subdirectory
3. Generate individual feed HTML files
4. Add unique IDs to feed containers
5. Update links to use fragment identifiers

### Data Flow
- Uses same items query as current implementation
- Maintains existing feed/item relationships
- No changes to database queries or data structure

## Success Criteria

1. ✅ index.html loads quickly with just feed directory
2. ✅ Each feed has its own viewable HTML page
3. ✅ Pages work without JavaScript (graceful degradation)
4. ✅ Dynamic loading triggered by scroll position
5. ✅ Smooth user experience with clear loading states
6. ✅ Reusable `<link-loader>` component

## Scope

### In Scope
- Render command modifications
- New feed.html template
- Basic `<link-loader>` web component
- Intersection Observer implementation
- Simple loading states

### Out of Scope
- Pagination within feed pages
- Refresh/reload functionality
- Collapse/expand features
- Caching strategies
- Configuration options for `<link-loader>`
- Performance optimizations beyond lazy loading

## Notes
<!-- Any additional context or constraints -->