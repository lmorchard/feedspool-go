/**
 * Utility Functions
 * Shared utilities used across the feed reader application
 */

/**
 * Creates a debounced version of a function
 * @param {Function} func - The function to debounce
 * @param {number} delay - The delay in milliseconds
 * @returns {Function} The debounced function with a cancel method
 */
export function debounce(func, delay) {
    let timeoutId = null;

    function debounced(...args) {
        // Clear any pending execution
        if (timeoutId) {
            clearTimeout(timeoutId);
        }

        // Schedule new execution
        timeoutId = setTimeout(() => {
            func.apply(this, args);
            timeoutId = null;
        }, delay);
    }

    // Add cancel method to the debounced function
    debounced.cancel = () => {
        if (timeoutId) {
            clearTimeout(timeoutId);
            timeoutId = null;
        }
    };

    return debounced;
}