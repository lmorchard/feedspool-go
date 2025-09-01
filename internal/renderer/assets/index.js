/**
 * Feed Reader JavaScript Module
 * Contains custom elements and interactive features for the feed reader
 * 
 * Features:
 * - Auto-resizing iframe custom element (<content-isolation-iframe>)
 * - Lazy loading for iframes in details elements
 * - Future: Additional custom elements and enhancements
 */

class ContentIsolationIframe extends HTMLElement {
    constructor() {
        super();
        this.iframe = null;
        this.isLoaded = false;

        // Create a debounced version of the height adjustment function
        this.debouncedAdjustHeight = debounce((iframe, height) => {
            iframe.style.height = `${height + 15}px`;

            // Remove min/max height constraints for auto-sizing
            iframe.style.minHeight = 'auto';
            iframe.style.maxHeight = 'none';
        }, 50); // 50ms debounce
    }

    connectedCallback() {
        // Find the iframe within this element
        this.iframe = this.querySelector('iframe');

        if (!this.iframe) {
            console.warn('content-isolation-iframe: No iframe found within element');
            return;
        }

        // Ensure the iframe has a unique ID (required for message routing)
        if (!this.iframe.id) {
            this.iframe.id = `content-isolation-iframe-${Date.now()}-${Math.random().toString(36).substring(2, 11)}`;
        }

        // Set initial styles
        this.style.display = 'block';
        this.style.overflow = 'hidden';

        // Check if we should lazy load
        const dataSrc = this.iframe.getAttribute('data-src');
        if (dataSrc) {
            // Set up lazy loading with Intersection Observer
            this.setupLazyLoading();
        } else {
            // If src is already set, proceed normally
            setupSharedContentIsolationIframeMessageHandler();
        }
    }

    setupLazyLoading() {
        // Use shared intersection observer
        const observer = getContentIsolationIframeSharedIntersectionObserver();
        observer.observe(this);

        // Also check if parent details is already open
        const details = this.closest('details');
        if (!details || details.open) {
            // If not in details or details is open, rely on intersection observer
            // Check if already in viewport on load
            const rect = this.getBoundingClientRect();
            if (rect.top < window.innerHeight && rect.bottom > 0) {
                // Already in viewport, load immediately
                this.loadIframe();
            }
        }
    }

    loadIframe() {
        if (this.isLoaded) return;

        const dataSrc = this.iframe.getAttribute('data-src');
        if (!dataSrc) return;

        // Set the actual src from data-src
        this.iframe.src = dataSrc;
        this.iframe.removeAttribute('data-src');
        this.isLoaded = true;

        // Register contentWindow after iframe loads
        this.iframe.addEventListener('load', () => {
            if (this.iframe.contentWindow) {
                contentIsolationIframeRegistry.set(this.iframe.contentWindow, this);
            }
        });

        // Stop observing this element
        const observer = getContentIsolationIframeSharedIntersectionObserver();
        observer.unobserve(this);

        // Ensure shared message handler is initialized for height adjustment
        setupSharedContentIsolationIframeMessageHandler();
    }

    disconnectedCallback() {
        // Unregister from the registry
        if (this.iframe && this.iframe.contentWindow) {
            contentIsolationIframeRegistry.delete(this.iframe.contentWindow);
        }

        // Stop observing this element when disconnected
        if (sharedContentIsolationIframeIntersectionObserver) {
            sharedContentIsolationIframeIntersectionObserver.unobserve(this);
        }

        // Cancel any pending height adjustments
        if (this.debouncedAdjustHeight && this.debouncedAdjustHeight.cancel) {
            this.debouncedAdjustHeight.cancel();
        }
    }

    adjustHeight(iframe, height) {
        if (!iframe || iframe !== this.iframe) return;

        // Use the debounced version to adjust height
        this.debouncedAdjustHeight(iframe, height);
    }

}

// Registry to map iframe contentWindows to their parent contentIsolationIframe elements
const contentIsolationIframeRegistry = new Map();

// Set up document-level event delegation for details toggle events
document.addEventListener('toggle', (event) => {
    // Check if the toggled element is a details element that's being opened
    if (event.target.tagName === 'DETAILS' && event.target.open) {
        // Find all content-isolation-iframe elements within this details element
        const els = event.target.querySelectorAll('content-isolation-iframe');
        els.forEach(el => el.loadIframe());
    }
});

// Shared intersection observer for all content-isolation-iframe elements
let sharedContentIsolationIframeIntersectionObserver = null;
function getContentIsolationIframeSharedIntersectionObserver() {
    if (!sharedContentIsolationIframeIntersectionObserver) {
        sharedContentIsolationIframeIntersectionObserver = new IntersectionObserver((entries) => {
            entries.forEach(entry => {
                if (entry.isIntersecting) {
                    const target = entry.target;
                    if (target.loadIframe && !target.isLoaded) {
                        target.loadIframe();
                    }
                }
            });
        }, {
            rootMargin: '100px', // Start loading 100px before entering viewport
            threshold: 0.01
        });
    }
    return sharedContentIsolationIframeIntersectionObserver;
}

let sharedContentIsolationIframeMessageHandler = null;
function setupSharedContentIsolationIframeMessageHandler() {
    if (!sharedContentIsolationIframeMessageHandler) {
        sharedContentIsolationIframeMessageHandler = (event) => {
            // Security: Only accept messages from our own iframes (data URLs)
            if (!event.origin.startsWith('data:') && event.origin !== 'null') {
                return;
            }

            // Check if this message is for iframe height adjustment
            if (event.data && event.data.type === 'iframe-height') {
                // Use the contentWindow (event.source) to directly look up the contentIsolationIframe element
                if (contentIsolationIframeRegistry.has(event.source)) {
                    const contentIsolationIframe = contentIsolationIframeRegistry.get(event.source);
                    contentIsolationIframe.adjustHeight(contentIsolationIframe.iframe, event.data.height);
                } else {
                    // Fallback: find the iframe by searching all content-isolation-iframe elements
                    const allIframes = document.querySelectorAll('content-isolation-iframe iframe');
                    
                    for (const iframe of allIframes) {
                        if (iframe.contentWindow === event.source) {
                            const parent = iframe.closest('content-isolation-iframe');
                            if (parent && parent.adjustHeight) {
                                parent.adjustHeight(iframe, event.data.height);
                                // Register for next time to avoid future fallback lookups
                                contentIsolationIframeRegistry.set(event.source, parent);
                                return;
                            }
                        }
                    }
                    
                    // Only warn if fallback also failed
                    console.warn('contentIsolationIframe not found for source:', event.source);
                }
            }
        };

        // Add the shared message listener
        window.addEventListener('message', sharedContentIsolationIframeMessageHandler);
    }
    return sharedContentIsolationIframeMessageHandler;
}

// Register the custom element
customElements.define('content-isolation-iframe', ContentIsolationIframe);

/**
 * Lazy Image Loader Web Component
 * Handles lazy loading of images within feed items using Intersection Observer
 * Provides better control over when images load compared to native loading="lazy"
 */
class LazyImageLoader extends HTMLElement {
    constructor() {
        super();
        this.images = [];
        this.isElementConnected = false;
    }

    connectedCallback() {
        this.isElementConnected = true;
        
        // Find all images with data-src within this element
        this.images = Array.from(this.querySelectorAll('img[data-src]'));
        
        if (this.images.length === 0) {
            return; // No lazy images to load
        }

        // Use shared intersection observer for all lazy images
        const observer = getLazyImageSharedIntersectionObserver();
        
        this.images.forEach(img => {
            // Add loading placeholder style
            img.style.backgroundColor = 'var(--bg-tertiary, #f0f0f0)';
            
            // Store reference to parent element on the image
            img._lazyImageLoader = this;
            
            // Start observing the image
            observer.observe(img);
        });
    }

    disconnectedCallback() {
        this.isElementConnected = false;
        
        // Stop observing all images when disconnected
        const observer = getLazyImageSharedIntersectionObserver();
        this.images.forEach(img => {
            observer.unobserve(img);
            delete img._lazyImageLoader;
        });
    }

    loadImage(img) {
        if (!img.hasAttribute('data-src')) return;
        
        const src = img.getAttribute('data-src');
        
        // Create a new image to preload
        const tempImg = new Image();
        
        tempImg.onload = () => {
            // Only update if still connected
            if (this.isElementConnected) {
                img.src = src;
                img.removeAttribute('data-src');
                
                // Remove placeholder background once loaded
                img.style.backgroundColor = '';
                
                // Add fade-in effect
                img.style.opacity = '0';
                img.style.transition = 'opacity 0.3s';
                
                // Force reflow then fade in
                img.offsetHeight;
                img.style.opacity = '1';
                
                // Clean up transition after animation
                setTimeout(() => {
                    if (this.isElementConnected) {
                        img.style.transition = '';
                    }
                }, 300);
            }
        };
        
        tempImg.onerror = () => {
            // Remove placeholder on error too
            if (this.isElementConnected) {
                img.style.backgroundColor = '';
                img.removeAttribute('data-src');
            }
        };
        
        // Start loading
        tempImg.src = src;
    }
}

// Shared intersection observer for all lazy images
let sharedLazyImageIntersectionObserver = null;
function getLazyImageSharedIntersectionObserver() {
    if (!sharedLazyImageIntersectionObserver) {
        sharedLazyImageIntersectionObserver = new IntersectionObserver((entries) => {
            entries.forEach(entry => {
                if (entry.isIntersecting) {
                    const img = entry.target;
                    
                    // Get the parent LazyImageLoader element
                    if (img._lazyImageLoader && img._lazyImageLoader.loadImage) {
                        img._lazyImageLoader.loadImage(img);
                    }
                    
                    // Stop observing this image
                    sharedLazyImageIntersectionObserver.unobserve(img);
                }
            });
        }, {
            // Start loading images 200px before they enter viewport
            rootMargin: '200px',
            threshold: 0.01
        });
    }
    return sharedLazyImageIntersectionObserver;
}

// Register the lazy image loader custom element
customElements.define('lazy-image-loader', LazyImageLoader);

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
class LayoutController extends HTMLElement {
    constructor() {
        super();
        
        // Default preferences
        this.preferences = {
            viewMode: 'list',
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
    }
}

// Register the layout controller custom element
customElements.define('layout-controller', LayoutController);

/**
 * Lightbox Overlay Custom Element
 * 
 * Handles modal dialog display for card view item descriptions.
 * Only activates in card view mode.
 */
class LightboxOverlay extends HTMLElement {
    constructor() {
        super();
        this.isVisible = false;
        this.currentDetails = null;
        this.layoutController = null;
        
        // Create overlay structure
        this.innerHTML = `
            <div class="lightbox-backdrop">
                <div class="lightbox-content">
                    <header class="lightbox-header">
                        <div class="lightbox-title-section"></div>
                        <button class="lightbox-close" aria-label="Close">×</button>
                    </header>
                    <main class="lightbox-body"></main>
                </div>
            </div>
        `;
        
        this.backdrop = this.querySelector('.lightbox-backdrop');
        this.content = this.querySelector('.lightbox-content');
        this.header = this.querySelector('.lightbox-header');
        this.titleSection = this.querySelector('.lightbox-title-section');
        this.closeButton = this.querySelector('.lightbox-close');
        this.body = this.querySelector('.lightbox-body');
        
        // Hide by default
        this.style.display = 'none';
    }

    connectedCallback() {
        // Find the parent layout controller
        this.layoutController = this.closest('layout-controller');
        
        // Setup event listeners
        this.setupEventListeners();
        
        // Monitor details elements
        this.monitorDetailsElements();
    }

    setupEventListeners() {
        // Close button
        this.closeButton.addEventListener('click', () => {
            this.closeLightbox();
        });
        
        // Click outside to close
        this.backdrop.addEventListener('click', (e) => {
            if (e.target === this.backdrop) {
                this.closeLightbox();
            }
        });
        
        // Escape key to close
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape' && this.isVisible) {
                this.closeLightbox();
            }
        });
    }

    monitorDetailsElements() {
        // Find all item details elements
        const detailsElements = document.querySelectorAll('.item');
        
        detailsElements.forEach(details => {
            details.addEventListener('toggle', (e) => {
                if (e.target.open && this.isCardView()) {
                    // Close other details first
                    this.closeAllDetails();
                    // Open this one in lightbox
                    this.openLightbox(e.target);
                }
            });
        });
    }

    isCardView() {
        return this.layoutController && this.layoutController.classList.contains('view-card');
    }

    closeAllDetails() {
        const allDetails = document.querySelectorAll('.item[open]');
        allDetails.forEach(details => {
            if (details !== this.currentDetails) {
                details.removeAttribute('open');
            }
        });
    }

    openLightbox(detailsElement) {
        this.currentDetails = detailsElement;
        
        // Extract content from the details element
        this.populateLightbox(detailsElement);
        
        // Show lightbox
        this.style.display = 'flex';
        this.isVisible = true;
        
        // Focus management
        this.closeButton.focus();
        
        // Prevent body scrolling
        document.body.style.overflow = 'hidden';
    }

    populateLightbox(detailsElement) {
        // Extract item information
        const titleElement = detailsElement.querySelector('.item-title');
        const dateElement = detailsElement.querySelector('.item-date');
        const contentElement = detailsElement.querySelector('.item-content');
        
        // Clear previous content
        this.titleSection.innerHTML = '';
        this.body.innerHTML = '';
        
        // Add title and date to header
        if (titleElement) {
            const titleClone = titleElement.cloneNode(true);
            this.titleSection.appendChild(titleClone);
        }
        
        if (dateElement) {
            const dateClone = dateElement.cloneNode(true);
            dateClone.classList.add('lightbox-date');
            this.titleSection.appendChild(dateClone);
        }
        
        // Add content to body
        if (contentElement) {
            const contentClone = contentElement.cloneNode(true);
            this.body.appendChild(contentClone);
            
            // Reinitialize any custom elements in the cloned content
            const iframes = contentClone.querySelectorAll('content-isolation-iframe');
            iframes.forEach(iframe => {
                // Trigger reconnection for proper iframe handling
                if (iframe.connectedCallback) {
                    iframe.connectedCallback();
                }
            });
        }
    }

    closeLightbox() {
        if (!this.isVisible) return;
        
        // Hide lightbox
        this.style.display = 'none';
        this.isVisible = false;
        
        // Close the associated details element
        if (this.currentDetails) {
            this.currentDetails.removeAttribute('open');
            this.currentDetails = null;
        }
        
        // Restore body scrolling
        document.body.style.overflow = '';
        
        // Clear content
        this.titleSection.innerHTML = '';
        this.body.innerHTML = '';
    }
}

// Register the lightbox overlay custom element
customElements.define('lightbox-overlay', LightboxOverlay);

// Utility function to create a debounced version of a function
function debounce(func, delay) {
    let timeoutId = null;

    return function debounced(...args) {
        // Clear any pending execution
        if (timeoutId) {
            clearTimeout(timeoutId);
        }

        // Schedule new execution
        timeoutId = setTimeout(() => {
            func.apply(this, args);
            timeoutId = null;
        }, delay);

        // Return a cancel function
        debounced.cancel = () => {
            if (timeoutId) {
                clearTimeout(timeoutId);
                timeoutId = null;
            }
        };
    };
}
