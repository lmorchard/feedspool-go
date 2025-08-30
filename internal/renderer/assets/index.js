/**
 * Feed Reader JavaScript Module
 * Contains custom elements and interactive features for the feed reader
 * 
 * Features:
 * - Auto-resizing iframe custom element (<auto-iframe>)
 * - Lazy loading for iframes in details elements
 * - Future: Additional custom elements and enhancements
 */

// Shared intersection observer for all auto-iframe elements
let sharedIntersectionObserver = null;
let sharedMessageHandler = null;

function getSharedIntersectionObserver() {
    if (!sharedIntersectionObserver) {
        sharedIntersectionObserver = new IntersectionObserver((entries) => {
            entries.forEach(entry => {
                if (entry.isIntersecting) {
                    const autoIframe = entry.target;
                    if (autoIframe.loadIframe && !autoIframe.isLoaded) {
                        autoIframe.loadIframe();
                    }
                }
            });
        }, {
            rootMargin: '100px', // Start loading 100px before entering viewport
            threshold: 0.01
        });
    }
    return sharedIntersectionObserver;
}

function getSharedMessageHandler() {
    if (!sharedMessageHandler) {
        sharedMessageHandler = (event) => {
            // Security: Only accept messages from our own iframes (data URLs)
            if (!event.origin.startsWith('data:') && event.origin !== 'null') {
                return;
            }

            // Check if this message is for iframe height adjustment
            if (event.data && event.data.type === 'iframe-height') {
                // Find all auto-iframe elements and check which one contains the source iframe
                const autoIframes = document.querySelectorAll('auto-iframe');
                for (let autoIframe of autoIframes) {
                    const iframe = autoIframe.querySelector('iframe');
                    if (iframe && iframe.contentWindow === event.source) {
                        autoIframe.adjustHeight(iframe, event.data.height);
                        break;
                    }
                }
            }
        };
        
        // Add the shared message listener
        window.addEventListener('message', sharedMessageHandler);
    }
    return sharedMessageHandler;
}

class AutoIframe extends HTMLElement {
    constructor() {
        super();
        this.iframe = null;
        this.isLoaded = false;
        this.adjustHeightTimeout = null;
    }

    connectedCallback() {
        // Find the iframe within this element
        this.iframe = this.querySelector('iframe');
        
        if (!this.iframe) {
            console.warn('auto-iframe: No iframe found within element');
            return;
        }

        // Give the iframe a unique ID if it doesn't have one
        if (!this.iframe.id) {
            this.iframe.id = `auto-iframe-${Date.now()}-${Math.random().toString(36).substring(2, 11)}`;
        }

        // Set initial styles
        this.style.display = 'block';
        this.style.overflow = 'hidden';
        
        // Check if we should lazy load
        const dataSrc = this.iframe.getAttribute('data-src');
        if (dataSrc) {
            // Set up lazy loading with Intersection Observer
            this.setupLazyLoading(dataSrc);
        } else {
            // If src is already set, proceed normally
            this.setupMessageHandler();
        }
        
        // Also handle details element opening/closing
        const details = this.closest('details');
        if (details) {
            details.addEventListener('toggle', () => {
                if (details.open && !this.isLoaded && this.iframe.hasAttribute('data-src')) {
                    // Load iframe when details opens
                    this.loadIframe();
                }
            });
        }
    }
    
    setupLazyLoading(dataSrc) {
        // Show a placeholder or loading state
        this.iframe.style.minHeight = '10px';
        this.iframe.style.background = 'var(--bg-tertiary, #f8f9fa)';
        
        // Use shared intersection observer
        const observer = getSharedIntersectionObserver();
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
        
        // Stop observing this element
        const observer = getSharedIntersectionObserver();
        observer.unobserve(this);
        
        // Ensure shared message handler is initialized for height adjustment
        getSharedMessageHandler();
    }
    

    disconnectedCallback() {
        // Stop observing this element when disconnected
        if (sharedIntersectionObserver) {
            sharedIntersectionObserver.unobserve(this);
        }
    }

    adjustHeight(iframe, height) {
        if (iframe && iframe === this.iframe) {
            // Clear any pending height adjustment
            if (this.adjustHeightTimeout) {
                clearTimeout(this.adjustHeightTimeout);
            }
            
            // Debounce height adjustments to prevent flickering
            this.adjustHeightTimeout = setTimeout(() => {
                iframe.style.height = `${height + 15}px`;
                
                // Remove min/max height constraints for auto-sizing
                iframe.style.minHeight = 'auto';
                iframe.style.maxHeight = 'none';
                
                this.adjustHeightTimeout = null;
            }, 50); // 50ms debounce
        }
    }

}

// Register the custom element
customElements.define('auto-iframe', AutoIframe);

