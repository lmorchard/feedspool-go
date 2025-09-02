/**
 * Feed Reader JavaScript Module
 * Main entry point that imports and initializes all custom elements and features
 * 
 * Features:
 * - Content isolation iframe with auto-resizing and lazy loading
 * - Lazy image loading with intersection observer
 * - Layout controller for view mode switching and preference persistence
 * - Lightbox overlay for card view modal display
 * - Shared utilities for debouncing and other common functions
 */

// Import all custom elements and utilities
import './content-isolation-iframe.js';
import './lazy-image-loader.js';
import './layout-controller.js';
import './lightbox-overlay.js';

// All custom elements are automatically registered when their modules are imported
// No additional initialization needed - the modules handle their own setup