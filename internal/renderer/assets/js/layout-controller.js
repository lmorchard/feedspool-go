/**
 * Layout Controller Custom Element
 * 
 * Manages view mode switching and preference persistence for the feed reader.
 * Handles:
 * - View mode switching (list/card)
 * - Thumbnail visibility toggle
 * - localStorage persistence
 * - CSS class management
 */

export class LayoutController extends HTMLElement {
    constructor() {
        super();
        
        // Default preferences
        this.preferences = {
            viewMode: 'card',
            showThumbnails: true
        };
        
        // Form elements references
        this.thumbnailCheckbox = null;
        this.viewModeRadios = [];
    }

    connectedCallback() {
        // Load preferences from localStorage
        this.loadPreferences();
        
        // Apply initial CSS classes
        this.updateClasses();
        
        // Find and setup form elements
        this.setupFormElements();
        
        // Initialize form values to match current state
        this.updateFormElements();
        
        // Setup event listeners
        this.setupEventListeners();
    }

    /**
     * Load preferences from localStorage with fallback to defaults
     */
    loadPreferences() {
        try {
            const saved = localStorage.getItem('feedspool-layout-preferences');
            if (saved) {
                const parsed = JSON.parse(saved);
                this.preferences = {
                    viewMode: parsed.viewMode || 'list',
                    showThumbnails: parsed.showThumbnails !== undefined ? parsed.showThumbnails : true
                };
            }
        } catch (error) {
            console.warn('Failed to load layout preferences:', error);
            // Use defaults on error
        }
    }

    /**
     * Save preferences to localStorage
     */
    savePreferences() {
        try {
            localStorage.setItem('feedspool-layout-preferences', JSON.stringify(this.preferences));
        } catch (error) {
            console.warn('Failed to save layout preferences:', error);
        }
    }

    /**
     * Update CSS classes based on current preferences
     */
    updateClasses() {
        // Remove all view mode classes
        this.classList.remove('view-list', 'view-card');
        
        // Add current view mode class
        this.classList.add(`view-${this.preferences.viewMode}`);
        
        // Handle thumbnail visibility
        if (this.preferences.showThumbnails) {
            this.classList.remove('hide-thumbnails');
        } else {
            this.classList.add('hide-thumbnails');
        }
    }

    /**
     * Find form elements in the document
     */
    setupFormElements() {
        this.thumbnailCheckbox = document.getElementById('show-thumbnails');
        this.viewModeRadios = Array.from(document.querySelectorAll('input[name="view-mode"]'));
    }

    /**
     * Update form elements to match current state
     */
    updateFormElements() {
        if (this.thumbnailCheckbox) {
            this.thumbnailCheckbox.checked = this.preferences.showThumbnails;
        }
        
        this.viewModeRadios.forEach(radio => {
            radio.checked = radio.value === this.preferences.viewMode;
        });
    }

    /**
     * Setup event listeners for form elements
     */
    setupEventListeners() {
        // Thumbnail checkbox
        if (this.thumbnailCheckbox) {
            this.thumbnailCheckbox.addEventListener('change', (e) => {
                this.preferences.showThumbnails = e.target.checked;
                this.updateClasses();
                this.savePreferences();
            });
        }
        
        // View mode radio buttons
        this.viewModeRadios.forEach(radio => {
            radio.addEventListener('change', (e) => {
                if (e.target.checked) {
                    this.preferences.viewMode = e.target.value;
                    this.updateClasses();
                    this.savePreferences();
                }
            });
        });
        
        // Close options menu when clicking outside
        document.addEventListener('click', (e) => {
            const optionsMenu = document.querySelector('.layout-options');
            if (optionsMenu && optionsMenu.open) {
                // Check if click was outside the options menu
                if (!optionsMenu.contains(e.target)) {
                    optionsMenu.open = false;
                }
            }
        });
    }
}

// Register the layout controller custom element
customElements.define('layout-controller', LayoutController);