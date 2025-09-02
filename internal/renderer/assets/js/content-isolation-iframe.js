/**
 * Content Isolation Iframe Custom Element
 * Provides iframe sandboxing and auto-resizing functionality for feed content
 */

import { debounce } from './utils.js';

// Registry to map iframe contentWindows to their parent contentIsolationIframe elements
const contentIsolationIframeRegistry = new Map();

// Shared intersection observer for all content-isolation-iframe elements
let sharedContentIsolationIframeIntersectionObserver = null;

// Shared message handler for iframe height adjustment
let sharedContentIsolationIframeMessageHandler = null;

export class ContentIsolationIframe extends HTMLElement {
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

// Set up document-level event delegation for details toggle events
document.addEventListener('toggle', (event) => {
    // Check if the toggled element is a details element that's being opened
    if (event.target.tagName === 'DETAILS' && event.target.open) {
        // Find all content-isolation-iframe elements within this details element
        const els = event.target.querySelectorAll('content-isolation-iframe');
        els.forEach(el => el.loadIframe());
    }
});

// Register the custom element
customElements.define('content-isolation-iframe', ContentIsolationIframe);