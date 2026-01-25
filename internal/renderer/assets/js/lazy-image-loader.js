/**
 * Lazy Image Loader Custom Element
 * Handles lazy loading of images within feed items using Intersection Observer
 * Provides better control over when images load compared to native loading="lazy"
 */

import { isElementVisible, createLazyLoadObserver } from './utils/lazy-load-observer.js';
import { LoadQueue } from './utils/load-queue.js';

const MAX_CONCURRENT_IMAGE_LOADS = 2; // Configurable limit for parallel loads

// Shared intersection observer for all lazy images
let sharedLazyImageIntersectionObserver = null;
let imageLoadQueue = null;

function getImageLoadQueue() {
    if (!imageLoadQueue) {
        imageLoadQueue = new LoadQueue({
            maxConcurrent: MAX_CONCURRENT_IMAGE_LOADS,
            isVisible: (item) => isElementVisible(item.img),
            isLoaded: (item) => !item.img.hasAttribute('data-src'),
            startLoad: (item, onComplete) => {
                const { img, loader } = item;

                // Unobserve now that we're actually loading it
                if (sharedLazyImageIntersectionObserver) {
                    sharedLazyImageIntersectionObserver.unobserve(img);
                }

                // Start loading
                loader.loadImage(img, onComplete);
            },
            getKey: (item) => item.img // Use img element as unique key
        });
    }
    return imageLoadQueue;
}

export class LazyImageLoader extends HTMLElement {
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
        const queue = getImageLoadQueue();

        this.images.forEach(img => {
            observer.unobserve(img);

            // Remove from queue if present
            queue.remove({ img, loader: this });

            delete img._lazyImageLoader;
        });
    }

    loadImage(img, onComplete) {
        if (!img.hasAttribute('data-src')) {
            if (onComplete) onComplete();
            return;
        }

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

            // Call completion callback
            if (onComplete) onComplete();
        };

        tempImg.onerror = () => {
            // Remove placeholder on error too
            if (this.isElementConnected) {
                img.style.backgroundColor = '';
                img.removeAttribute('data-src');
            }

            // Call completion callback even on error
            if (onComplete) onComplete();
        };

        // Start loading
        tempImg.src = src;
    }
}

function getLazyImageSharedIntersectionObserver() {
    if (!sharedLazyImageIntersectionObserver) {
        sharedLazyImageIntersectionObserver = createLazyLoadObserver((img) => {
            // Get the parent LazyImageLoader element
            if (img._lazyImageLoader && img._lazyImageLoader.loadImage) {
                // Queue the image load
                getImageLoadQueue().enqueue({ img, loader: img._lazyImageLoader });
            }
            // Don't unobserve yet - let LoadQueue do it when actually loading
        });
    }
    return sharedLazyImageIntersectionObserver;
}

// Register the lazy image loader custom element
customElements.define('lazy-image-loader', LazyImageLoader);