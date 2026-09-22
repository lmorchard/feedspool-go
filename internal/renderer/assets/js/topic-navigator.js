/**
 * Topic Navigator Component
 * Automatically expands topic <details> elements when navigated to via pills or hash anchors,
 * and collapses them when the "↑ Top" link is clicked.
 *
 * Topics are anchored as #thread-N (stable across runs); a topic without a
 * thread falls back to #topic-N.
 */

const isTopicAnchor = (hash) => hash.startsWith('#thread-') || hash.startsWith('#topic-');

class TopicNavigator extends HTMLElement {
    connectedCallback() {
        this.handleHashChange = this.handleHashChange.bind(this);
        window.addEventListener('hashchange', this.handleHashChange);

        // Handle initial hash on page load
        if (window.location.hash) {
            setTimeout(() => this.handleHashChange(), 50);
        }

        // Delegate clicks
        this.addEventListener('click', (e) => {
            // Handle "Top" link click: collapse current topic and scroll to top
            const topLink = e.target.closest('a.back-to-top');
            if (topLink) {
                e.preventDefault();
                e.stopPropagation();
                const details = topLink.closest('details.topic-group-container');
                if (details) {
                    details.open = false;
                }
                const topElement = document.getElementById('top') || document.body;
                topElement.scrollIntoView({ behavior: 'smooth' });
                if (window.history.pushState) {
                    window.history.pushState(null, null, '#top');
                }
                return;
            }

            // Handle topic pill link click: expand target topic
            const link = e.target.closest('a[href^="#thread-"], a[href^="#topic-"]');
            if (link) {
                const targetId = link.getAttribute('href').slice(1);
                const targetDetails = document.getElementById(targetId);
                if (targetDetails && targetDetails.tagName === 'DETAILS') {
                    targetDetails.open = true;
                }
            }
        });
    }

    disconnectedCallback() {
        window.removeEventListener('hashchange', this.handleHashChange);
    }

    handleHashChange() {
        const hash = window.location.hash;
        if (!hash || !isTopicAnchor(hash)) return;

        const targetId = hash.slice(1);
        const targetDetails = document.getElementById(targetId);
        if (targetDetails && targetDetails.tagName === 'DETAILS') {
            targetDetails.open = true;
            targetDetails.scrollIntoView({ behavior: 'smooth' });
        }
    }
}

if (!customElements.get('topic-navigator')) {
    customElements.define('topic-navigator', TopicNavigator);
}
