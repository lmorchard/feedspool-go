import { isElementVisible, createLazyLoadObserver } from './utils/lazy-load-observer.js';
import { LoadQueue } from './utils/load-queue.js';

// Module-level state for coordinating loads
let sharedObserver = null;
let loadQueue = null;

function getLoadQueue() {
    if (!loadQueue) {
        loadQueue = new LoadQueue({
            maxConcurrent: 1, // Serial loading (1 at a time)
            isVisible: (loader) => isElementVisible(loader),
            isLoaded: (loader) => loader.loaded,
            startLoad: (loader, onComplete) => {
                loader.loadContent(onComplete);
            }
        });
    }
    return loadQueue;
}

function getSharedObserver() {
    if (!sharedObserver) {
        sharedObserver = createLazyLoadObserver(
            (loader) => getLoadQueue().enqueue(loader),
            { rootMargin: '50px' }
        );
    }
    return sharedObserver;
}

class LinkLoader extends HTMLElement {
    constructor() {
        super();
        this.link = null;
        this.loaded = false;
    }

    connectedCallback() {
        // Find the first anchor tag within this element
        this.link = this.querySelector('a');
        if (!this.link) {
            console.warn('LinkLoader: No anchor tag found');
            return;
        }

        // Register with shared observer
        const observer = getSharedObserver();
        observer.observe(this);
    }

    disconnectedCallback() {
        // Unregister from shared observer
        if (sharedObserver) {
            sharedObserver.unobserve(this);
        }

        // Remove from queue if present
        if (loadQueue) {
            loadQueue.remove(this);
        }
    }

    async loadContent(onComplete) {
        if (this.loaded || !this.link) {
            if (onComplete) onComplete();
            return;
        }

        this.loaded = true;

        // Unobserve this element
        if (sharedObserver) {
            sharedObserver.unobserve(this);
        }

        // Change link text to loading state
        this.link.textContent = 'Loading...';

        try {
            const response = await fetch(this.link.href);
            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }

            const html = await response.text();
            const parser = new DOMParser();
            const doc = parser.parseFromString(html, 'text/html');

            // Extract fragment ID from URL
            const url = new URL(this.link.href);
            const fragmentId = url.hash.slice(1); // Remove #

            if (!fragmentId) {
                throw new Error('No fragment identifier found in URL');
            }

            // Find the element with the fragment ID
            const targetElement = doc.getElementById(fragmentId);
            if (!targetElement) {
                throw new Error(`Element with ID '${fragmentId}' not found`);
            }

            // Save reference to parent
            const parent = this.parentNode;

            // Extract all children from the target element
            const children = [];
            while (targetElement.firstChild) {
                children.push(targetElement.firstChild);
                targetElement.firstChild.remove();
            }

            // Insert children into parent before this element
            children.forEach(child => {
                parent.insertBefore(child, this);
            });

            // Dispatch custom event for other components that need to process new content
            if (children.length > 0) {
                const event = new CustomEvent('content-loaded', {
                    bubbles: true,
                    detail: { element: parent }
                });
                document.dispatchEvent(event);
            }

            // Remove this link-loader element now that content is loaded
            this.remove();

            // Notify queue that load is complete
            if (onComplete) onComplete();

        } catch (error) {
            console.error('LinkLoader failed to load content:', error);
            this.link.textContent = `Error: ${error.message}`;

            // Notify queue that load is complete (even on error)
            if (onComplete) onComplete();
        }
    }
}

// Register the custom element
customElements.define('link-loader', LinkLoader);

export { LinkLoader };