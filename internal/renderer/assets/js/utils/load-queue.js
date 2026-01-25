/**
 * Generic load queue with visibility checking and configurable concurrency
 */

export class LoadQueue {
    /**
     * @param {Object} options - Configuration options
     * @param {number} options.maxConcurrent - Max items to load concurrently (1 = serial)
     * @param {Function} options.isVisible - Check if item should be loaded (item) => boolean
     * @param {Function} options.isLoaded - Check if item is already loaded (item) => boolean
     * @param {Function} options.startLoad - Start loading an item (item, onComplete) => void
     * @param {Function} options.onLoadComplete - Optional callback when item completes (item) => void
     * @param {Function} options.getKey - Optional function to get unique key for dedup (item) => any
     */
    constructor(options) {
        this.maxConcurrent = options.maxConcurrent || 1;
        this.queue = [];
        this.currentlyLoading = new Set();
        this.isVisible = options.isVisible || (() => true);
        this.isLoaded = options.isLoaded;
        this.startLoad = options.startLoad;
        this.onLoadComplete = options.onLoadComplete;
        this.getKey = options.getKey || ((item) => item);
    }

    /**
     * Add an item to the queue
     * @param {*} item - Item to load (can be any type)
     */
    enqueue(item) {
        // Check if already loaded
        if (this.isLoaded(item)) {
            return;
        }

        // Check if already in queue (using key for comparison)
        const key = this.getKey(item);
        const alreadyQueued = this.queue.some(queuedItem => this.getKey(queuedItem) === key);
        if (alreadyQueued) {
            return;
        }

        // Add to queue
        this.queue.push(item);

        // Try to process
        this.process();
    }

    /**
     * Process the queue - load up to maxConcurrent items
     */
    process() {
        while (this.currentlyLoading.size < this.maxConcurrent && this.queue.length > 0) {
            const item = this.queue.shift();

            // Skip if already loaded
            if (this.isLoaded(item)) {
                continue;
            }

            // Check visibility before loading
            if (!this.isVisible(item)) {
                // Skip this one, try next
                continue;
            }

            // Mark as loading
            const key = this.getKey(item);
            this.currentlyLoading.add(key);

            // Start load with completion callback
            this.startLoad(item, () => {
                // Remove from loading set
                this.currentlyLoading.delete(key);

                // Call optional completion callback
                if (this.onLoadComplete) {
                    this.onLoadComplete(item);
                }

                // Process next in queue
                this.process();
            });
        }
    }

    /**
     * Remove an item from the queue (e.g., on disconnect)
     * @param {*} item - Item to remove
     */
    remove(item) {
        const key = this.getKey(item);

        // Remove from queue
        const queueIndex = this.queue.findIndex(queuedItem => this.getKey(queuedItem) === key);
        if (queueIndex !== -1) {
            this.queue.splice(queueIndex, 1);
        }

        // Remove from loading set
        this.currentlyLoading.delete(key);
    }
}
