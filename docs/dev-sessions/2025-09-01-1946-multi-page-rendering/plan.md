# Dev Session Plan: Multi-Page Rendering

## Implementation Overview

This plan breaks down the multi-page rendering feature into small, safe, iterative steps. Each step builds on the previous one, ensuring no orphaned code and maintaining a working state throughout the implementation.

## High-Level Phases

1. **Phase 1: Prepare Templates** - Create the new feed.html template
2. **Phase 2: Modify Workflow** - Update the render workflow to generate multiple files
3. **Phase 3: Update Index Template** - Transform index.html into a feed directory
4. **Phase 4: Create Web Component** - Build the link-loader custom element
5. **Phase 5: Wire Everything Together** - Connect all pieces and test

## Detailed Implementation Steps

### Step 1: Create the feed.html template
- Copy index.html template as a starting point
- Modify to show a single feed with all its items
- Add the feed-specific ID attribute
- Keep header/footer for consistency
- Test that template compiles

### Step 2: Add feed ID generation to templates
- Update the existing index.html template to add `id="feed-{id}"` to feed articles
- This prepares for extraction later
- Verify IDs are correctly generated

### Step 3: Create feeds subdirectory during render
- Modify workflow.go to create `feeds/` subdirectory in output
- Add error handling for directory creation
- Test that directory is created on render

### Step 4: Add individual feed rendering loop
- In workflow.go, after rendering index.html, add a loop to render each feed
- Use the new feed.html template
- Write files to `feeds/{id}.html`
- Pass single feed data to template context

### Step 5: Create minimal index template
- Create a new index-directory.html template
- Show only feed headers with metadata
- Add placeholder links to feed pages
- Test rendering with new template

### Step 6: Add link-loader wrapper to index
- Update index-directory.html template
- Wrap each feed entry in `<link-loader>` tags
- Add "Read more..." links with proper href and fragment
- Verify HTML structure

### Step 7: Create basic link-loader.js component
- Create new JavaScript file for web component
- Define LinkLoader class extending HTMLElement
- Register custom element
- Add to assets directory

### Step 8: Implement intersection observer
- Add IntersectionObserver to link-loader
- Detect when component enters viewport
- Log to console for testing
- Verify detection works

### Step 9: Add fetch functionality
- When visible, fetch the linked page
- Handle loading states (change text to "Loading...")
- Handle errors (display error message)
- Test with network tab

### Step 10: Extract and insert content
- Parse fetched HTML
- Find element by fragment ID
- Replace link with extracted content
- Test complete flow

### Step 11: Polish and optimize
- Add CSS for smooth transitions
- Ensure loaded content stays loaded
- Test edge cases
- Verify graceful degradation

### Step 12: Update render command output
- Switch from index.html to index-directory.html as main template
- Update success messages
- Test complete workflow
- Verify all files are generated correctly

## LLM Implementation Prompts

---

## Prompt 1: Create the feed.html template

**Context:** We're implementing multi-page rendering for a Go-based feed reader. Currently, the render command produces a single index.html with all feeds. We need to split this into multiple pages.

**Current State:** 
- Templates are in `internal/renderer/templates/`
- Current index.html template shows all feeds with their items
- Template uses Go's html/template syntax

**Task:** Create a new template file `internal/renderer/templates/feed.html` that displays a single feed with all its items. This template should:
1. Copy the structure from index.html
2. Remove the feed loop ({{range .Feeds}})
3. Expect a single .Feed object and .Items for just that feed
4. Add `id="feed-{{.Feed.ID}}"` to the article element
5. Keep the same header, footer, and styling references

**Expected Output:** A new feed.html template file that can render a single feed independently.

---

## Prompt 2: Add feed ID attributes to existing templates

**Context:** We need to add ID attributes to feed containers so the link-loader component can extract specific content.

**Current State:**
- index.html template exists with feed articles
- Each feed is wrapped in `<article class="feed">`

**Task:** Modify `internal/renderer/templates/index.html` to add unique IDs:
1. Change `<article class="feed">` to `<article class="feed" id="feed-{{.ID}}">`
2. Ensure the ID uses the database ID field from the Feed struct

**Expected Output:** Updated index.html with ID attributes on feed articles.

---

## Prompt 3: Create feeds subdirectory in workflow

**Context:** The render workflow needs to create a subdirectory for individual feed pages.

**Current State:**
- workflow.go manages the render process
- Currently creates output directory
- generateSite function handles file generation

**Task:** In `internal/renderer/workflow.go`, modify the generateSite function:
1. After creating the main output directory, create a `feeds` subdirectory
2. Use `filepath.Join(config.OutputDir, "feeds")`
3. Add proper error handling
4. Use the same permissions as the parent directory

**Expected Output:** Modified workflow that creates the feeds subdirectory.

---

## Prompt 4: Add individual feed rendering loop

**Context:** After rendering the main index, we need to render each feed to its own file.

**Current State:**
- generateSite renders index.html
- We have a feed.html template ready
- Feeds subdirectory is created

**Task:** In `internal/renderer/workflow.go`, after rendering index.html:
1. Add a loop over feeds
2. For each feed, create a new TemplateContext with just that feed's data
3. Render using feed.html template
4. Save to `feeds/{feed.ID}.html`
5. Add error handling and progress output

**Expected Output:** Individual feed HTML files in the feeds directory.

---

## Prompt 5: Create minimal index directory template

**Context:** We need a new template that shows just a feed directory without full content.

**Current State:**
- index.html shows full feed content
- We need a lightweight directory page

**Task:** Create `internal/renderer/templates/index-directory.html`:
1. Copy header and basic structure from index.html
2. For each feed, show only:
   - Feed title with favicon
   - Latest item timestamp
   - A "Read more..." link to `feeds/{id}.html#feed-{id}`
3. Remove item listings and content
4. Keep the same CSS and JS references

**Expected Output:** A new lightweight index template.

---

## Prompt 6: Add link-loader wrapper to directory template

**Context:** Each feed entry needs to be wrapped in the custom element for lazy loading.

**Current State:**
- index-directory.html shows feed summaries
- Each feed has a "Read more..." link

**Task:** Update `internal/renderer/templates/index-directory.html`:
1. Wrap each feed entry in `<link-loader>` tags
2. Structure should be:
   ```html
   <link-loader>
     <div class="feed-header">...</div>
     <a href="feeds/{{.ID}}.html#feed-{{.ID}}">Read more...</a>
   </link-loader>
   ```

**Expected Output:** Updated template with link-loader wrappers.

---

## Prompt 7: Create basic link-loader web component

**Context:** We need a custom web component that will handle lazy loading of feed content.

**Current State:**
- Templates are ready with link-loader tags
- Assets are in internal/renderer/assets/
- index.js exists as the main JavaScript module

**Task:** Create `internal/renderer/assets/link-loader.js`:
1. Define a class LinkLoader extending HTMLElement
2. In connectedCallback, find the first anchor tag within
3. Store the link reference for later use
4. Register as 'link-loader' custom element
5. Export the LinkLoader class
6. Add console.log for debugging

**Expected Output:** Basic web component that identifies its target link, exported as a module and self-registered.

---

## Prompt 8: Implement intersection observer in link-loader

**Context:** The component needs to detect when it becomes visible.

**Current State:**
- Basic LinkLoader class exists
- Component finds its anchor tag

**Task:** Enhance `link-loader.js`:
1. Create IntersectionObserver in connectedCallback
2. Observe this element
3. When visible (intersectionRatio > 0), log to console
4. Disconnect observer after first trigger
5. Add disconnectedCallback to clean up

**Expected Output:** Component that detects visibility.

---

## Prompt 9: Add fetch functionality to link-loader

**Context:** When visible, the component should fetch the linked page.

**Current State:**
- Component detects visibility
- Has reference to anchor tag

**Task:** Update `link-loader.js`:
1. When visible, get href from anchor
2. Change link text to "Loading..."
3. Fetch the URL
4. On error, change link text to error message
5. On success, proceed to next step (just log for now)

**Expected Output:** Component that fetches content with loading states.

---

## Prompt 10: Extract and insert fetched content

**Context:** The component needs to extract specific content from fetched HTML.

**Current State:**
- Component fetches the linked page
- URL includes fragment identifier

**Task:** Complete `link-loader.js`:
1. Parse fetched HTML into DOM
2. Extract fragment from URL (after #)
3. Find element with that ID
4. Remove the anchor tag
5. Insert extracted element into link-loader
6. Handle case where element isn't found

**Expected Output:** Complete working link-loader component.

---

## Prompt 11: Add CSS styling for link-loader

**Context:** The component needs styling for smooth transitions.

**Current State:**
- link-loader.js is complete
- CSS file exists at internal/renderer/assets/index.css

**Task:** Add to `index.css`:
1. Style for link-loader element
2. Smooth transitions for content appearance
3. Loading state styles
4. Error state styles (red text)
5. Ensure loaded content displays properly

**Expected Output:** Polished visual experience.

---

## Prompt 12: Update workflow to use new templates

**Context:** The render workflow needs to use the new directory template as the main index.

**Current State:**
- generateSite uses "index.html" template
- We have index-directory.html ready
- Individual feeds are being generated

**Task:** Update `workflow.go`:
1. Change main template from "index.html" to "index-directory.html"
2. Ensure feed.html is used for individual feeds
3. Update success messages to mention multiple files

**Expected Output:** Complete working multi-page rendering system.

---

## Prompt 13: Import link-loader module in index.js

**Context:** The main index.js needs to import the link-loader component so it's available on the page.

**Current State:**
- link-loader.js exists as a module in assets
- index.js is the main JavaScript entry point
- link-loader.js self-registers the custom element

**Task:** Update `internal/renderer/assets/index.js`:
1. Add import statement: `import './link-loader.js'`
2. Place it with other module imports at the top
3. No need to do anything else - the module self-registers

**Expected Output:** index.js that loads the link-loader component.

---

## Prompt 14: Test and fix edge cases

**Context:** Ensure the system handles edge cases gracefully.

**Current State:**
- Multi-page rendering is implemented
- Link-loader component works

**Task:** Test and fix:
1. Feeds with no items
2. Missing metadata/favicons
3. JavaScript disabled (graceful degradation)
4. Network errors during fetch
5. Malformed HTML in feed content

**Expected Output:** Robust system that handles edge cases.

---

## Testing Strategy

After each step:
1. Run `go build` to ensure compilation
2. Run `./feedspool render` to test rendering
3. Open generated HTML in browser
4. Check browser console for errors
5. Verify expected files are created

## Success Metrics

- [ ] index.html loads quickly (< 100ms)
- [ ] Individual feed pages are generated
- [ ] Links work without JavaScript
- [ ] Content loads on scroll
- [ ] Clear loading states
- [ ] No console errors
- [ ] Graceful error handling

## Rollback Plan

Each step is designed to be atomic. If issues arise:
1. Git diff to see changes
2. Revert specific files if needed
3. Previous functionality remains intact until Step 12
4. Can disable link-loader by removing script tag