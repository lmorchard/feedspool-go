# Render Layout Options - Session Plan

## Session Start: 2025-09-01 11:53

### Current Status
- Branch: `render-layout-options`
- Base: Built on completed unfurl-thumbnails feature
- Template: `/internal/renderer/templates/index.html`
- CSS: `/internal/renderer/assets/style.css`
- JS: `/internal/renderer/assets/index.js`

### Architecture Overview

**Existing Structure**:
- HTML template with `<details>` elements for items
- Custom elements: `<content-isolation-iframe>`, `<lazy-image-loader>`
- CSS with CSS custom properties for theming
- Progressive enhancement via JavaScript

**New Architecture**:
- Main `<layout-controller>` custom element wrapping main content
- Options menu in header using `<details>` with gear icon
- Class-based CSS for view switching (`view-list`, `view-card`)  
- `<lightbox-overlay>` custom element for card view descriptions
- localStorage for persistent preferences

## Implementation Plan

### Phase 1: Foundation Setup
**Goal**: Add basic structure without breaking existing functionality

#### Step 1.1: HTML Template Foundation
**Prompt**: 
```
We need to add a layout options menu to the header of our feed reader template. Looking at the current template structure in `/internal/renderer/templates/index.html`, replace the "Generated at" paragraph in the header with a collapsible options menu.

Current header structure:
```html
<header>
    <h1>feedspool</h1>
    <p>Generated at: {{.GeneratedAt.Format "2006-01-02 15:04:05"}} ({{.TimeWindow}})</p>
</header>
```

Replace the paragraph with:
- A `<details>` element with class "layout-options"
- Summary containing a gear Unicode symbol (⚙) and "Options" text
- Inside: checkbox for "Show thumbnails" and radio buttons for "List" / "Card" views
- Use semantic HTML with proper form elements and labels
- Add appropriate classes for styling hooks

Keep all existing functionality intact. The options should be positioned in the top-right corner where the generated date was.
```

#### Step 1.2: Wrap Content with Main Controller
**Prompt**:
```
We need to wrap the main content area with a custom element that will handle view state management. Looking at the current template, wrap the `<main>` element with a `<layout-controller>` custom element.

Current structure:
```html
<main>
    {{range .Feeds}}
    <!-- feed content -->
    {{end}}
</main>
```

Change to:
```html
<layout-controller>
    <main>
        {{range .Feeds}}
        <!-- feed content -->
        {{end}}
    </main>
</layout-controller>
```

This controller will be responsible for:
- Managing view mode classes (view-list, view-card)
- Handling thumbnail visibility
- Managing localStorage persistence

Don't implement the JavaScript yet - just add the HTML wrapper.
```

### Phase 2: Basic CSS Styling
**Goal**: Style the options menu and prepare for view switching

#### Step 2.1: Options Menu Styling
**Prompt**:
```
Style the new layout options menu in the header. Looking at the existing CSS variables and styling patterns in `/internal/renderer/assets/style.css`, add CSS for the options menu that:

1. **Header Layout**: Position the `<details class="layout-options">` in the top-right of the header
2. **Menu Button**: Style the summary element as a clean button with the gear icon
3. **Dropdown Menu**: Style the opened details content as a dropdown panel
4. **Form Elements**: Style the checkbox and radio buttons to match the existing design
5. **Responsive**: Ensure it works on mobile devices

Design requirements:
- Use existing CSS custom properties for theming
- Match the visual style of the existing design
- Dropdown should appear below the gear icon
- Use subtle borders and shadows consistent with the feed cards
- Form elements should be clearly labeled and accessible

The menu should be closed by default and not interfere with the existing layout.
```

#### Step 2.2: View Mode CSS Classes
**Prompt**:
```
Create CSS classes for the two view modes that will be applied to the `<layout-controller>` element. Add to the existing stylesheet:

1. **Default/List View** (`.view-list`):
   - Keep current styling for `.feed` and `.items` containers
   - Items display as they currently do in a vertical list
   - Details expand inline below the item summary

2. **Card View** (`.view-card`):
   - Transform `.items` container into a responsive card layout
   - CSS Grid: `repeat(auto-fit, minmax(320px, 1fr))` for desktop multi-column
   - Mobile: Single column of cards (graceful responsive behavior)
   - Cards show: thumbnail, title, published date (no description content visible)
   - Use `object-fit: contain` for thumbnails
   - Consistent card sizing and spacing

3. **Thumbnail Toggle** (`.hide-thumbnails`):
   - Hide all `.item-thumbnail` and `.feed-favicon` elements
   - Adjust spacing when thumbnails are hidden
   - Works in both list and card views

Don't worry about the lightbox behavior yet - focus on the basic card layout and thumbnail toggling.
```

### Phase 3: JavaScript Foundation
**Goal**: Implement the main controller and basic functionality

#### Step 3.1: Layout Controller Custom Element
**Prompt**:
```
Create the main `<layout-controller>` custom element in `/internal/renderer/assets/index.js`. This element should:

**Core Functionality**:
1. **State Management**: Track view mode ('list'/'card') and thumbnail visibility (true/false)
2. **CSS Class Application**: Apply appropriate classes to itself based on state:
   - `.view-list` or `.view-card` for view mode
   - `.hide-thumbnails` when thumbnails are off
3. **localStorage Integration**: 
   - Save preferences as single object: `{viewMode: 'list', showThumbnails: true}`
   - Load on initialization with defaults: thumbnails shown, list view
   - Save whenever preferences change

**Event Handling**:
- Listen for changes on the options form elements
- Update internal state and CSS classes accordingly
- Persist changes to localStorage immediately

**Progressive Enhancement**:
- Work without JavaScript (graceful degradation)
- Only enhance existing HTML, don't create it
- Follow the existing custom element patterns in the file

Start with basic functionality - don't worry about the lightbox behavior yet.
```

#### Step 3.2: Options Menu Integration
**Prompt**:
```
Connect the options menu form elements to the `<layout-controller>`. The layout controller needs to:

**Form Setup**:
1. **Find Form Elements**: Query for the checkbox and radio buttons in the options menu
2. **Initialize Values**: Set form elements to match the current state (from localStorage or defaults)
3. **Event Binding**: Listen for change events on form elements

**State Synchronization**:
- When checkbox changes: toggle `showThumbnails` state and `.hide-thumbnails` class
- When radio buttons change: update `viewMode` state and view classes
- Immediately save all changes to localStorage
- Update CSS classes to reflect new state

**Error Handling**:
- Handle missing form elements gracefully
- Provide fallbacks if localStorage is not available
- Maintain existing functionality if JavaScript fails to load

The options menu should now be fully functional for basic view switching and thumbnail toggling.
```

### Phase 4: Card View Polish
**Goal**: Perfect the card layout and prepare for lightbox

#### Step 4.1: Card Layout Refinements
**Prompt**:
```
Refine the card view CSS to handle edge cases and improve the user experience:

**Card Container Improvements**:
1. **Responsive Layout**: Use `grid-template-columns: repeat(auto-fit, minmax(320px, 1fr))` for desktop multi-column
2. **Mobile First**: On mobile (< 768px), cards naturally flow as single column
3. **Gap Management**: Add appropriate gap between cards (16px recommended)
4. **Container Padding**: Ensure proper spacing around the card container
5. **Touch Targets**: Ensure card elements are easily tappable on mobile

**Card Layout Polish**:
1. **Card Structure**: Each item should look like a proper card with padding and borders
2. **Thumbnail Sizing**: Consistent thumbnail dimensions within cards
3. **Text Hierarchy**: Clear visual hierarchy for title, date, and other elements
4. **Hover States**: Subtle hover effects for better interactivity
5. **Content Trimming**: Ensure long titles don't break card layout

**Mobile Considerations**:
- Cards should be easily readable on small screens
- Touch targets for buttons and links should be at least 44px
- Consider reducing card padding on mobile for more content density
- Ensure sufficient contrast for outdoor mobile reading

**Final Row Handling**:
- Ensure the last row of cards doesn't stretch to fill width
- Cards maintain consistent width regardless of how many are in the final row
- Use CSS Grid properties to prevent flex-style stretching issues

Test with various screen sizes and numbers of items to ensure consistent card behavior.
```

#### Step 4.2: Card-Specific Item Styling
**Prompt**:
```
Create card-specific styling for the feed items that shows condensed information:

**Card Content Layout**:
1. **Information Density**: In card view, show only: thumbnail, linked title, published date
2. **Visual Hierarchy**: Clear distinction between title (prominent) and date (subtle)
3. **Link Styling**: Title links should be prominent and clearly clickable
4. **Date Formatting**: Compact date display suitable for cards
5. **Thumbnail Integration**: Thumbnail should be a key visual element in the card

**Content Hiding**:
- Hide description/summary content in card view (prepare for lightbox)
- Ensure `<details>` elements don't show their toggle arrows in card view
- Hide the expand/collapse functionality in card view cards

**Mobile Adaptations**:
- Larger touch targets for links on mobile devices
- Appropriate text sizes for mobile reading
- Consider stacking vs side-by-side layouts for thumbnail and text

**Accessibility**:
- Maintain proper link contrast and focus states
- Ensure keyboard navigation works properly in card layout
- Keep semantic structure intact for screen readers

The cards should feel cohesive and scannable while maintaining all the original functionality.
```

### Phase 5: Lightbox Implementation
**Goal**: Add lightbox overlay for card view descriptions

#### Step 5.1: Lightbox Overlay Custom Element
**Prompt**:
```
Create a `<lightbox-overlay>` custom element that handles the modal dialog for card view item descriptions:

**Core Functionality**:
1. **Overlay Structure**: Create a full-screen overlay with backdrop
2. **Content Container**: 80% viewport width/height centered container for content
3. **Close Behavior**: Click outside content area to close
4. **Keyboard Support**: Escape key to close

**Integration with Details Elements**:
- Monitor all `<details class="item">` elements for open/close state
- When details opens in card view: show lightbox with that item's content
- When lightbox closes: close the corresponding details element
- Handle multiple details elements (close all when one opens)

**Progressive Enhancement**:
- Only activate in card view mode
- Gracefully degrade to normal details behavior in list view
- Don't break existing iframe functionality

**Mobile Considerations**:
- On mobile, lightbox should use more viewport space (95% width/height)
- **Close Button Required**: Prominent close button (X) in top-right corner of lightbox content
- Close button should be at least 44px touch target size
- Consider swipe gestures for closing (optional enhancement)
- Click-outside-to-close less reliable on mobile due to large lightbox size

**Styling Foundation**:
- Semi-transparent backdrop
- Centered content container with proper z-index
- Basic styling that matches the existing design theme
- Smooth transitions for open/close

Start with basic functionality - we'll enhance the content display in the next step.
```

#### Step 5.2: Lightbox Content Integration
**Prompt**:
```
Enhance the `<lightbox-overlay>` to properly display the item content from the opened details element:

**Content Extraction**:
1. **Source Content**: Get the `.item-content` from the opened details element
2. **Clone Content**: Copy the content into the lightbox (don't move the original)
3. **Iframe Handling**: Ensure `<content-isolation-iframe>` elements work properly in the lightbox
4. **Metadata Display**: Include item title and metadata in the lightbox header

**Lightbox Layout**:
1. **Header Section**: Item title, link, published date, and close button (X)
2. **Content Section**: The iframe content from the original details element
3. **Close Button**: Prominent X button in header, essential for mobile usability
4. **Responsive Sizing**: 80% viewport on desktop, 95% on mobile for better touch interaction

**State Management**:
- Track which details element is currently open
- Ensure only one lightbox can be open at a time
- Properly clean up when closing (remove cloned content, reset state)

**Integration Testing**:
- Verify iframes load properly in the lightbox
- Test with various content types (HTML content, plain text, etc.)
- Ensure lazy loading still works for iframe content

The lightbox should provide a better reading experience for item content in grid view.
```

### Phase 6: Testing and Polish
**Goal**: Ensure everything works together and handle edge cases

#### Step 6.1: Integration Testing and Bug Fixes
**Prompt**:
```
Test the complete layout options feature and fix any issues:

**Functionality Testing**:
1. **Options Persistence**: Verify localStorage saves and restores correctly
2. **View Switching**: Test smooth transitions between list and grid views
3. **Thumbnail Toggle**: Ensure thumbnail hiding/showing works in both views
4. **Lightbox Behavior**: Test lightbox opening, closing, and content display
5. **Mobile Responsiveness**: Test all features on mobile screen sizes

**Edge Cases**:
1. **No Thumbnails**: Handle items/feeds without thumbnail images
2. **Long Titles**: Ensure proper text wrapping and truncation
3. **Many Items**: Test performance with large numbers of feed items
4. **No JavaScript**: Verify graceful degradation when JavaScript is disabled
5. **localStorage Disabled**: Handle browsers with localStorage restrictions

**Browser Compatibility**:
- Test custom elements in various browsers
- Ensure CSS Grid works properly across browsers  
- Verify details/summary elements work consistently

**Performance**:
- Check for any layout thrashing during view switches
- Ensure smooth animations and transitions
- Verify iframe loading performance in lightbox

Fix any bugs discovered and optimize performance where needed.
```

#### Step 6.2: Final Polish and Documentation
**Prompt**:
```
Add final polish touches and prepare the feature for production:

**Visual Polish**:
1. **Animation Refinements**: Smooth transitions for view switching and lightbox
2. **Focus Management**: Proper focus handling for accessibility
3. **Loading States**: Handle any loading states gracefully
4. **Error States**: Provide user feedback for any errors

**Code Organization**:
1. **Comments**: Add clear comments to the JavaScript code
2. **CSS Organization**: Group related styles logically
3. **Performance**: Minimize reflows and optimize CSS selectors

**Documentation**:
1. **Update Notes**: Document what was implemented in the session notes
2. **Known Issues**: Document any limitations or future improvements needed
3. **Usage Instructions**: Brief instructions for users about the new features

**Final Testing**:
- Complete end-to-end testing of all features
- Verify no regressions in existing functionality
- Test with real feed data to ensure everything works as expected

The feature should be ready for production use with all requirements met.
```

## Success Criteria

✅ **Complete Implementation**:
- Options menu replaces generated date in header
- Thumbnail toggle works in both views
- List/Card view switching functions properly
- Card view shows responsive card layout (multi-column on desktop, single column on mobile)
- Lightbox displays item content in card view with prominent close button
- Preferences persist across page loads

✅ **Quality Assurance**:
- No regressions in existing functionality
- Responsive design works on all screen sizes
- Graceful degradation without JavaScript
- Accessible to keyboard and screen reader users
- Performance remains good with large numbers of items

✅ **Technical Excellence**:
- Clean, maintainable code following existing patterns
- Progressive enhancement principles
- Semantic HTML with proper ARIA attributes
- CSS follows existing design system
- JavaScript uses modern best practices