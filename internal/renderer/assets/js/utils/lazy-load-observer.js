/**
 * Shared utilities for lazy loading with IntersectionObserver
 */

/**
 * Check if an element is visible in the viewport (with margin)
 * @param {Element} element - The element to check
 * @returns {boolean} True if element is visible or near-visible
 */
export function isElementVisible(element) {
    const rect = element.getBoundingClientRect();
    const viewportHeight = window.innerHeight || document.documentElement.clientHeight;
    // Add a generous margin to account for smooth scrolling
    const margin = viewportHeight;
    return (
        rect.top < (viewportHeight + margin) &&
        rect.bottom > -margin
    );
}

/**
 * Create a shared IntersectionObserver for lazy loading
 * @param {Function} onIntersect - Callback when element intersects (receives entry.target)
 * @param {Object} options - IntersectionObserver options
 * @param {string} options.rootMargin - Margin around viewport (default: viewport height)
 * @param {number} options.threshold - Intersection threshold (default: 0.01)
 * @returns {IntersectionObserver} Configured observer
 */
export function createLazyLoadObserver(onIntersect, options = {}) {
    const {
        rootMargin = null, // null = use viewport height
        threshold = 0.01
    } = options;

    // Calculate root margin if not provided
    let margin = rootMargin;
    if (margin === null) {
        const viewportHeight = window.innerHeight || document.documentElement.clientHeight;
        margin = `${viewportHeight}px`;
    }

    return new IntersectionObserver((entries) => {
        entries.forEach((entry) => {
            if (entry.isIntersecting) {
                onIntersect(entry.target);
            }
        });
    }, {
        rootMargin: margin,
        threshold: threshold
    });
}
