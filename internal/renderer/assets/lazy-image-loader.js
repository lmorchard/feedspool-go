/**
 * Lazy Image Loader Custom Element
 * Handles lazy loading of images within feed items using Intersection Observer
 * Provides better control over when images load compared to native loading="lazy"
 */

// Shared intersection observer for all lazy images
let sharedLazyImageIntersectionObserver = null;

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