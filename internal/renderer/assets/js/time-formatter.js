/**
 * Time Formatter Custom Element
 * Finds all <time> elements with datetime attributes and formats them
 * as relative time ("2 hours ago") or localized absolute time in user's timezone
 *
 * Listens for 'content-loaded' events from other components to format newly loaded content.
 */

export class TimeFormatter extends HTMLElement {
    constructor() {
        super();
        this.updateInterval = null;
        this.handleContentLoaded = this.handleContentLoaded.bind(this);
    }

    connectedCallback() {
        // Format all existing time elements
        this.formatAllTimes();

        // Update relative times every minute
        this.updateInterval = setInterval(() => {
            this.formatAllTimes();
        }, 60000);

        // Listen for content-loaded events from components that dynamically load content
        document.addEventListener('content-loaded', this.handleContentLoaded);
    }

    disconnectedCallback() {
        if (this.updateInterval) {
            clearInterval(this.updateInterval);
            this.updateInterval = null;
        }

        document.removeEventListener('content-loaded', this.handleContentLoaded);
    }

    handleContentLoaded(event) {
        // Format time elements in the newly loaded content
        if (event.detail && event.detail.element) {
            const timeElements = event.detail.element.querySelectorAll('time[datetime]');
            timeElements.forEach(time => this.formatTimeElement(time));
        }
    }

    formatAllTimes() {
        const timeElements = this.querySelectorAll('time[datetime]');
        timeElements.forEach(time => this.formatTimeElement(time));
    }

    formatTimeElement(timeElement) {
        const datetime = timeElement.getAttribute('datetime');
        if (!datetime) return;

        const date = new Date(datetime);
        if (isNaN(date.getTime())) return;

        const formatted = this.formatDateTime(date);

        // Store original text as title for hover (only on first format)
        if (!timeElement.hasAttribute('title')) {
            timeElement.setAttribute('title', timeElement.textContent);
        }

        timeElement.textContent = formatted;
    }

    formatDateTime(date) {
        const now = new Date();
        const diffMs = now - date;
        const diffSeconds = Math.floor(diffMs / 1000);
        const diffMinutes = Math.floor(diffSeconds / 60);
        const diffHours = Math.floor(diffMinutes / 60);
        const diffDays = Math.floor(diffHours / 24);

        // Future dates
        if (diffMs < 0) {
            const absDiffMinutes = Math.abs(diffMinutes);
            const absDiffHours = Math.abs(diffHours);
            const absDiffDays = Math.abs(diffDays);

            if (absDiffMinutes < 60) {
                return `in ${absDiffMinutes} minute${absDiffMinutes !== 1 ? 's' : ''}`;
            } else if (absDiffHours < 24) {
                return `in ${absDiffHours} hour${absDiffHours !== 1 ? 's' : ''}`;
            } else if (absDiffDays < 7) {
                return `in ${absDiffDays} day${absDiffDays !== 1 ? 's' : ''}`;
            }
            // Fall through to absolute date for far future
        }

        // Less than a minute ago
        if (diffSeconds < 60) {
            return 'just now';
        }

        // Less than an hour ago
        if (diffMinutes < 60) {
            return `${diffMinutes} minute${diffMinutes !== 1 ? 's' : ''} ago`;
        }

        // Less than a day ago
        if (diffHours < 24) {
            return `${diffHours} hour${diffHours !== 1 ? 's' : ''} ago`;
        }

        // Less than a week ago
        if (diffDays < 7) {
            if (diffDays === 1) {
                return `yesterday at ${this.formatTime(date)}`;
            }
            return `${diffDays} days ago`;
        }

        // Less than a month ago - show day of week
        if (diffDays < 30) {
            const dayName = date.toLocaleDateString(undefined, { weekday: 'long' });
            return `${dayName} at ${this.formatTime(date)}`;
        }

        // Older - show full date in local timezone
        return date.toLocaleDateString(undefined, {
            year: 'numeric',
            month: 'short',
            day: 'numeric',
            hour: '2-digit',
            minute: '2-digit'
        });
    }

    formatTime(date) {
        return date.toLocaleTimeString(undefined, {
            hour: 'numeric',
            minute: '2-digit',
            hour12: true
        });
    }
}

// Register the custom element
customElements.define('time-formatter', TimeFormatter);
