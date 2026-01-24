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
    constructor() {
        super();
        this.feedContainers = [];
        this.currentFeedIndex = -1;
        this.intersectionObserver = null;
        this.mutationObserver = null;
        this.prevButton = null;
        this.nextButton = null;
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
            }
        `;

        document.head.appendChild(style);
    }

    setupIntersectionObserver() {
        // Observer to track which feed header is near the top of viewport
        const options = {
            root: null,
            rootMargin: '-10% 0px -80% 0px', // Top 20% of viewport
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
            let shouldUpdate = false;

            for (const mutation of mutations) {
                if (mutation.type === 'childList') {
                    // Check if any added nodes are feed containers
                    for (const node of mutation.addedNodes) {
                        if (node.nodeType === Node.ELEMENT_NODE) {
                            if (node.tagName === 'LINK-LOADER' || node.tagName === 'LAZY-IMAGE-LOADER') {
                                shouldUpdate = true;
                                break;
                            }
                        }
                    }

                    // Check if any removed nodes were feed containers
                    for (const node of mutation.removedNodes) {
                        if (node.nodeType === Node.ELEMENT_NODE) {
                            if (node.tagName === 'LINK-LOADER' || node.tagName === 'LAZY-IMAGE-LOADER') {
                                shouldUpdate = true;
                                break;
                            }
                        }
                    }
                }

                if (shouldUpdate) break;
            }

            if (shouldUpdate) {
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
        this.feedContainers = Array.from(this.querySelectorAll(':scope > link-loader, :scope > lazy-image-loader'));

        // Observe all feed containers
        if (this.intersectionObserver) {
            this.feedContainers.forEach(container => {
                this.intersectionObserver.observe(container);
            });
        }

        this.updateButtonState();
    }

    updateButtonState() {
        if (!this.prevButton || !this.nextButton) return;

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
    }

    scrollToPreviousFeed() {
        // Refresh the containers to ensure we have the current DOM state
        this.updateFeedContainers();

        if (this.currentFeedIndex <= 0) {
            return;
        }

        // Find the previous container in the actual DOM order
        const currentContainer = this.feedContainers[this.currentFeedIndex];
        if (!currentContainer) return;

        // Walk backwards to find the previous feed container
        let prevFeed = currentContainer.previousElementSibling;
        while (prevFeed) {
            if (prevFeed.tagName === 'LINK-LOADER' || prevFeed.tagName === 'LAZY-IMAGE-LOADER') {
                prevFeed.scrollIntoView({
                    behavior: 'smooth',
                    block: 'start'
                });
                return;
            }
            prevFeed = prevFeed.previousElementSibling;
        }
    }

    scrollToNextFeed() {
        // Refresh the containers to ensure we have the current DOM state
        this.updateFeedContainers();

        if (this.currentFeedIndex < 0 || this.currentFeedIndex >= this.feedContainers.length - 1) {
            return;
        }

        // Find the next container in the actual DOM order
        const currentContainer = this.feedContainers[this.currentFeedIndex];
        if (!currentContainer) return;

        // Walk forwards to find the next feed container
        let nextFeed = currentContainer.nextElementSibling;
        while (nextFeed) {
            if (nextFeed.tagName === 'LINK-LOADER' || nextFeed.tagName === 'LAZY-IMAGE-LOADER') {
                nextFeed.scrollIntoView({
                    behavior: 'smooth',
                    block: 'start'
                });
                return;
            }
            nextFeed = nextFeed.nextElementSibling;
        }
    }
}

// Register the custom element
customElements.define('feed-navigator', FeedNavigator);

export { FeedNavigator };
