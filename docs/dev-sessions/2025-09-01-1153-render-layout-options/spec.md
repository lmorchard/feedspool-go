# Render Layout Options - Session Spec

## Session Start: 2025-09-01 11:53

I want to add a little layout and view options menu to the page header in the default rendering template.

First option is to hide / show thumbnails.

Next option is to switch between the current list view and a new card / grid view of items.

We will want to add more options in the future, so let's organize these options in a collapsible menu in the header.

Store choices in localStorage and restore them on page load.

In general, let's do as much as we can in CSS styles and classnames and with basic HTML elements like <details>. Where we do need JS enhancement, build this using custom elements that wrap the affected content.

### Session Goals
- Add collapsible layout options menu to page header
- Implement thumbnail show/hide toggle
- Create list/grid view switching with responsive cards
- Implement lightbox overlay for grid view descriptions
- Persist user preferences in localStorage

### Acceptance Criteria
✅ **Options Menu**:
- Replaces generated date in top-right header corner
- Uses Unicode gear symbol (⚙) as trigger for `<details>` element
- Contains thumbnail checkbox and view mode radio buttons

✅ **Thumbnail Toggle**:
- Simple checkbox labeled "Show thumbnails" 
- Hides/shows thumbnail images when toggled
- Works in both list and grid views

✅ **View Mode Switching**:
- Radio buttons for "List" / "Grid" views
- List view: current behavior with iframe descriptions below items
- Grid view: fixed-width cards with natural wrapping using CSS Grid

✅ **Grid View Layout**:
- Cards show: thumbnail, linked title, published date
- Fixed card width with CSS Grid to prevent final row width issues
- Responsive wrapping based on container width

✅ **Grid View Lightbox**:
- Opening item `<details>` in grid view shows 80% viewport centered overlay
- Contains iframe description content
- Clicking outside overlay closes all `<details>` elements
- Custom element handles click-outside-to-close behavior

✅ **Persistence**:
- Store preferences as single localStorage object for future extensibility
- Defaults: thumbnails shown, list view
- Restore settings on page load

### Technical Requirements

**Architecture**:
- Hybrid custom element approach:
  - Main element handles view state and localStorage
  - Smaller custom elements handle specific behaviors (lightbox, etc.)
- Prefer CSS and class name changes over complex JavaScript
- Use semantic HTML elements (`<details>`, radio buttons, checkboxes)

**CSS Strategy**:
- CSS Grid for grid view to avoid flex final-row width issues
- Class-based styling for view mode switching
- Lightbox styling with backdrop and centered positioning

**JavaScript Enhancement**:
- Custom elements wrap existing content for progressive enhancement
- localStorage object structure for easy future option additions
- Click-outside-to-close custom element for lightbox behavior
