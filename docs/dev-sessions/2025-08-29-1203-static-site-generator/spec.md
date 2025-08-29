# Static Site Generator - Session Specification

## Overview

Let's implement a simple static site generator for feeds and items. Let's also include a very simple static web server to serve up the static site. Consider these TODOs:

- [ ] implement a static site generator to render HTML from feeds
- [ ] implement a simple HTTP server to serve up static site and feeds API
- [ ] enhance `init` command to create database, default config, and default feed list files
- [ ] `init` can also dump static site generation templates to a directory for customization

I'd like to use go's built-in templating packages to support a set of customizable templates for the site, as well as support including in a set of JS & CSS assets produced out-of-band

I'm not sure how flexible `html/template` is, but I'd like to be able to support a few scenarios for iterating through feed and item content. For example:

- one big HTML page of all feeds updated in the last 24 hours with recent items listed under each
- one HTML page listing feeds updated in the last 24 hours with separate HTML resources for each feed listing items, which are dynamically loaded and inserted into the page via JavaScript on demand
- Separate HTML pages for each feed and respective items
- one big HTML page of all items from the last 12 hours in a reverse chronological river

We may need to think through how the database queries, iteration, and file naming works for this.

## Requirements

### Core Functionality
- Use Go's `html/template` package for templating
- Support customizable templates for different feed types
- Include static assets (JS/CSS) in the generated site
- Implement a simple HTTP server to serve the static site
- Initial priority: one big HTML page listing all feeds with items grouped beneath for a configurable time period

### CLI Commands

#### `feedspool render`
- Generate static site from feeds data
- Options:
  - `--max-age <duration>` (e.g., 24h) - configurable time window
  - `--start <time>` / `--end <time>` - explicit time range
  - `--output <dir>` - output directory (default: `./build`)
  - `--templates <dir>` - custom templates directory
  - `--static <dir>` - custom static assets directory  
  - `--feeds <file>` - feed list file for filtering
  - `--format opml|text` - feed list format (consistent with other commands)
- Config file support for all options

#### `feedspool serve`
- Simple HTTP server to serve static site
- Separate from render command for clean separation
- Plan for future API endpoints (but start with static file serving only)

#### Enhanced `feedspool init`
- New flags:
  - `--extract-templates` - extract embedded templates to `./templates/`
  - `--extract-assets` - extract embedded static assets to `./assets/`
- Support template/asset directory options consistent with render command

### Template System
- Default templates and static assets embedded in binary
- Easy switching between embedded defaults and custom templates
- Template extraction via `init` command for customization
- Default extraction directories: `./templates/` and `./assets/`

### Data Structure
- Template context includes:
  - `Feeds` - array of feeds with associated items (using existing internal models)
  - `GeneratedAt` - timestamp of generation
  - `TimeWindow` - filter criteria used
  - Additional metadata from generation options
- All feed and item properties available to templates

### Default Template Design
- Feed display: title, link, description, last updated time
- Items display: linked title, publication date, content/summary in HTML `<details>` elements
- Items grouped under respective feeds

### Output Structure
- Configurable output directory (default: `./build`)
- Generate `index.html` in output directory
- Copy static assets to output directory
- Simple, flat structure for initial implementation

## Acceptance Criteria

- [ ] `feedspool render` command generates static HTML from feeds
- [ ] Support time-based filtering (max-age, start/end times)
- [ ] Feed list filtering support with OPML/text formats
- [ ] Embedded default templates and assets in binary
- [ ] Template/asset extraction via enhanced `init` command
- [ ] Custom template/asset directory support
- [ ] Generated site includes all feed and item data in structured HTML
- [ ] `feedspool serve` command serves static files
- [ ] All options configurable via command line and config file
- [ ] Default template shows feeds with collapsible item details
