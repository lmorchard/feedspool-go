/**
 * Feed Navigator Component
 *
 * Provides a floating button to navigate between feeds on the page.
 * Uses IntersectionObserver to track the current feed near the top of the viewport,
 * and MutationObserver to detect when new feeds are added to the page.
 *
 * Mobile-first design with responsive styling.
 */

class FeedNavigator extends HTMLElement {
    // Constants for feed container identification
    static FEED_CONTAINER_SELECTOR = ':scope > link-loader, :scope > lazy-image-loader';
    static FEED_CONTAINER_TAGS = ['LINK-LOADER', 'LAZY-IMAGE-LOADER'];

    constructor() {
        super();
        this.feedContainers = [];
        this.currentFeedIndex = -1;
        this.intersectionObserver = null;
        this.mutationObserver = null;
        this.prevButton = null;
        this.nextButton = null;
        this.feedSelector = null;
    }

    /**
     * Helper method to check if a node is a feed container
     * @param {Node} node - The DOM node to check
     * @returns {boolean} True if the node is a feed container
     */
    isFeedContainer(node) {
        if (node.nodeType !== Node.ELEMENT_NODE ||
            !FeedNavigator.FEED_CONTAINER_TAGS.includes(node.tagName)) {
            return false;
        }

        // Exclude page loaders (they have page-loader-placeholder children)
        if (node.querySelector('.page-loader-placeholder')) {
            return false;
        }

        return true;
    }

    /**
     * Get the scroll target for a feed container (header if present, container otherwise)
     * @param {Element} container - The feed container element
     * @returns {Element} The element to scroll to
     */
    getFeedScrollTarget(container) {
        const prevSibling = container.previousElementSibling;
        const hasHeaderClass = prevSibling && prevSibling.classList.contains('feed-header');
        return hasHeaderClass ? prevSibling : container;
    }

    /**
     * Get the feed title from a container
     * @param {Element} container - The feed container element
     * @param {number} fallbackIndex - Index to use for fallback name
     * @returns {string} The feed title
     */
    getFeedTitle(container, fallbackIndex) {
        // Check inside container first (collapsed feeds)
        let feedHeader = container.querySelector('.feed-header h2');

        // If not found, check previous sibling (expanded feeds)
        if (!feedHeader) {
            const prevSibling = container.previousElementSibling;
            if (prevSibling && prevSibling.classList.contains('feed-header')) {
                feedHeader = prevSibling.querySelector('h2');
            }
        }

        return feedHeader ? feedHeader.textContent.trim() : `Feed ${fallbackIndex + 1}`;
    }

    connectedCallback() {
        this.render();
        this.setupIntersectionObserver();
        this.setupMutationObserver();
        this.updateFeedContainers();
    }

    disconnectedCallback() {
        if (this.intersectionObserver) {
            this.intersectionObserver.disconnect();
        }
        if (this.mutationObserver) {
            this.mutationObserver.disconnect();
        }
    }

    render() {
        // Create fixed container for buttons (so they don't interfere with children layout)
        const buttonContainer = document.createElement('div');
        buttonContainer.className = 'feed-nav-container';

        // Create previous button
        this.prevButton = document.createElement('button');
        this.prevButton.className = 'feed-nav-button feed-nav-prev';
        this.prevButton.setAttribute('aria-label', 'Scroll to previous feed');
        this.prevButton.addEventListener('click', () => this.scrollToPreviousFeed());

        const prevText = document.createElement('span');
        prevText.className = 'feed-nav-text';
        prevText.textContent = 'Previous';

        const prevIcon = document.createElement('span');
        prevIcon.className = 'feed-nav-icon';
        prevIcon.textContent = '↑';

        this.prevButton.appendChild(prevIcon);
        this.prevButton.appendChild(prevText);

        // Create feed selector dropdown
        this.feedSelector = document.createElement('select');
        this.feedSelector.className = 'feed-nav-selector';
        this.feedSelector.setAttribute('aria-label', 'Jump to feed');
        this.feedSelector.addEventListener('change', (e) => this.scrollToFeed(parseInt(e.target.value)));

        // Will be populated in updateFeedContainers
        const defaultOption = document.createElement('option');
        defaultOption.textContent = 'Select feed...';
        defaultOption.value = '';
        defaultOption.disabled = true;
        this.feedSelector.appendChild(defaultOption);

        // Create next button
        this.nextButton = document.createElement('button');
        this.nextButton.className = 'feed-nav-button feed-nav-next';
        this.nextButton.setAttribute('aria-label', 'Scroll to next feed');
        this.nextButton.addEventListener('click', () => this.scrollToNextFeed());

        const nextText = document.createElement('span');
        nextText.className = 'feed-nav-text';
        nextText.textContent = 'Next';

        const nextIcon = document.createElement('span');
        nextIcon.className = 'feed-nav-icon';
        nextIcon.textContent = '↓';

        this.nextButton.appendChild(nextText);
        this.nextButton.appendChild(nextIcon);

        buttonContainer.appendChild(this.prevButton);
        buttonContainer.appendChild(this.feedSelector);
        buttonContainer.appendChild(this.nextButton);
        this.appendChild(buttonContainer);

        // Add styles
        this.addStyles();
    }

    addStyles() {
        // Check if styles already exist
        if (document.getElementById('feed-navigator-styles')) {
            return;
        }

        const style = document.createElement('style');
        style.id = 'feed-navigator-styles';
        style.textContent = `
            feed-navigator {
                /* Use display: contents so the component doesn't interfere with children layout */
                display: contents;
            }

            .feed-nav-container {
                position: fixed;
                bottom: 0;
                left: 0;
                right: 0;
                display: flex;
                justify-content: center;
                align-items: center;
                gap: 1rem;
                pointer-events: none;
                z-index: 1000;
                padding-bottom: max(1rem, env(safe-area-inset-bottom));
            }

            .feed-nav-button {
                pointer-events: auto;
                padding: 0.75rem 1.5rem;
                background: rgba(0, 0, 0, 0.8);
                color: white;
                border: 1px solid rgba(255, 255, 255, 0.2);
                border-radius: 2rem;
                font-size: 1rem;
                font-weight: 500;
                cursor: pointer;
                transition: all 0.2s ease;
                box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
                min-height: 44px;
                min-width: 44px;
                display: flex;
                align-items: center;
                gap: 0.5rem;
            }

            .feed-nav-text {
                display: inline;
            }

            .feed-nav-icon {
                display: inline;
            }

            .feed-nav-selector {
                pointer-events: auto;
                padding: 0.875rem 1.25rem;
                background: rgba(0, 0, 0, 0.8);
                color: white;
                border: 1px solid rgba(255, 255, 255, 0.2);
                border-radius: 2rem;
                font-size: 0.9rem;
                font-weight: 500;
                cursor: pointer;
                transition: all 0.2s ease;
                box-shadow: 0 4px 12px rgba(0, 0, 0, 0.3);
                max-width: 300px;
                min-width: 150px;
            }

            .feed-nav-selector:hover {
                background: rgba(0, 0, 0, 0.9);
                box-shadow: 0 6px 16px rgba(0, 0, 0, 0.4);
            }

            .feed-nav-selector:disabled {
                opacity: 0.5;
                cursor: not-allowed;
            }

            .feed-nav-selector.hidden {
                opacity: 0;
                pointer-events: none;
            }

            .feed-nav-selector option {
                background: #1a1a1a;
                color: white;
            }

            .feed-nav-button:hover {
                background: rgba(0, 0, 0, 0.9);
                transform: translateY(-2px);
                box-shadow: 0 6px 16px rgba(0, 0, 0, 0.4);
            }

            .feed-nav-button:active {
                transform: translateY(0);
            }

            .feed-nav-button:disabled {
                opacity: 0.5;
                cursor: not-allowed;
            }

            .feed-nav-button.hidden {
                opacity: 0;
                pointer-events: none;
            }

            @media (prefers-color-scheme: light) {
                .feed-nav-button {
                    background: rgba(255, 255, 255, 0.95);
                    color: #333;
                    border-color: rgba(0, 0, 0, 0.1);
                }

                .feed-nav-button:hover {
                    background: rgba(255, 255, 255, 1);
                }

                .feed-nav-selector {
                    background: rgba(255, 255, 255, 0.95);
                    color: #333;
                    border-color: rgba(0, 0, 0, 0.1);
                }

                .feed-nav-selector:hover {
                    background: rgba(255, 255, 255, 1);
                }

                .feed-nav-selector option {
                    background: #ffffff;
                    color: #333;
                }
            }

            /* Mobile/narrow screen adjustments */
            @media (max-width: 768px) {
                .feed-nav-container {
                    padding-bottom: max(1.5rem, env(safe-area-inset-bottom));
                    gap: 0.5rem;
                }

                .feed-nav-text {
                    display: none;
                }

                .feed-nav-button {
                    padding: 0.75rem;
                    min-width: 48px;
                    justify-content: center;
                }

                .feed-nav-icon {
                    font-size: 1.25rem;
                }

                .feed-nav-selector {
                    min-width: 120px;
                    max-width: 200px;
                    font-size: 0.85rem;
                    padding: 0.75rem 0.5rem;
                }
            }
        `;

        document.head.appendChild(style);
    }

    setupIntersectionObserver() {
        // Observer to track which feed header is near the top of viewport
        const options = {
            root: null,
            rootMargin: '-10% 0px -80% 0px', // Top 10% of viewport (10%-90% region)
            threshold: 0
        };

        this.intersectionObserver = new IntersectionObserver((entries) => {
            entries.forEach((entry) => {
                if (entry.isIntersecting) {
                    const index = this.feedContainers.indexOf(entry.target);
                    if (index !== -1) {
                        this.currentFeedIndex = index;
                        this.updateButtonState();
                    }
                }
            });
        }, options);
    }

    setupMutationObserver() {
        // Observer to detect when feed containers are added/removed
        const config = {
            childList: true,
            subtree: false // Only watch direct children
        };

        this.mutationObserver = new MutationObserver((mutations) => {
            const hasRelevantChanges = mutations.some(mutation => {
                if (mutation.type !== 'childList') return false;

                // Check if any added or removed nodes are feed containers
                const nodes = [...mutation.addedNodes, ...mutation.removedNodes];
                return nodes.some(node => this.isFeedContainer(node));
            });

            if (hasRelevantChanges) {
                this.updateFeedContainers();
            }
        });

        this.mutationObserver.observe(this, config);
    }

    updateFeedContainers() {
        // Disconnect existing observations
        if (this.intersectionObserver) {
            this.feedContainers.forEach(container => {
                this.intersectionObserver.unobserve(container);
            });
        }

        // Find all feed containers (link-loader or lazy-image-loader) as direct children
        // Filter out page loaders
        this.feedContainers = Array.from(this.querySelectorAll(FeedNavigator.FEED_CONTAINER_SELECTOR))
            .filter(container => this.isFeedContainer(container));

        // Observe all feed containers
        if (this.intersectionObserver) {
            this.feedContainers.forEach(container => {
                this.intersectionObserver.observe(container);
            });
        }

        this.updateFeedSelector();
        this.updateButtonState();
    }

    updateFeedSelector() {
        if (!this.feedSelector) return;

        // Clear existing options except the default
        while (this.feedSelector.options.length > 1) {
            this.feedSelector.remove(1);
        }

        // Populate with feed titles
        this.feedContainers.forEach((container, index) => {
            const option = document.createElement('option');
            option.value = index;
            option.textContent = this.getFeedTitle(container, index);
            this.feedSelector.appendChild(option);
        });

        // Update selected value to match current feed
        if (this.currentFeedIndex >= 0 && this.currentFeedIndex < this.feedContainers.length) {
            this.feedSelector.value = this.currentFeedIndex;
        }
    }

    updateButtonState() {
        if (!this.prevButton || !this.nextButton || !this.feedSelector) return;

        const hasFeeds = this.feedContainers.length > 0;
        const isFirstFeed = this.currentFeedIndex <= 0;
        const isLastFeed = this.currentFeedIndex >= this.feedContainers.length - 1;

        // Previous button state
        if (!hasFeeds || isFirstFeed) {
            this.prevButton.classList.add('hidden');
            this.prevButton.disabled = true;
        } else {
            this.prevButton.classList.remove('hidden');
            this.prevButton.disabled = false;
        }

        // Next button state
        if (!hasFeeds || isLastFeed) {
            this.nextButton.classList.add('hidden');
            this.nextButton.disabled = true;
        } else {
            this.nextButton.classList.remove('hidden');
            this.nextButton.disabled = false;
        }

        // Selector state
        if (!hasFeeds) {
            this.feedSelector.classList.add('hidden');
            this.feedSelector.disabled = true;
        } else {
            this.feedSelector.classList.remove('hidden');
            this.feedSelector.disabled = false;
            // Update to show current feed
            if (this.currentFeedIndex >= 0 && this.currentFeedIndex < this.feedContainers.length) {
                this.feedSelector.value = this.currentFeedIndex;
            }
        }
    }

    scrollToFeed(index) {
        if (index < 0 || index >= this.feedContainers.length) {
            return;
        }

        const targetFeed = this.feedContainers[index];
        if (targetFeed) {
            const scrollTarget = this.getFeedScrollTarget(targetFeed);
            scrollTarget.scrollIntoView({
                behavior: 'smooth',
                block: 'start'
            });
        }
    }

    scrollToPreviousFeed() {
        if (this.currentFeedIndex > 0) {
            this.scrollToFeed(this.currentFeedIndex - 1);
        }
    }

    scrollToNextFeed() {
        if (this.currentFeedIndex < this.feedContainers.length - 1) {
            this.scrollToFeed(this.currentFeedIndex + 1);
        }
    }
}

// Register the custom element
customElements.define('feed-navigator', FeedNavigator);

export { FeedNavigator };
