class LinkLoader extends HTMLElement {
    constructor() {
        super();
        this.link = null;
        this.observer = null;
        this.loaded = false;
    }

    connectedCallback() {
        console.log('LinkLoader connected');
        
        // Find the first anchor tag within this element
        this.link = this.querySelector('a');
        if (!this.link) {
            console.warn('LinkLoader: No anchor tag found');
            return;
        }
        
        console.log('LinkLoader found link:', this.link.href);
        
        // Set up intersection observer to detect when visible
        this.observer = new IntersectionObserver((entries) => {
            entries.forEach((entry) => {
                if (entry.isIntersecting && !this.loaded) {
                    console.log('LinkLoader entered viewport, starting load...');
                    this.loadContent();
                }
            });
        });
        
        this.observer.observe(this);
    }

    disconnectedCallback() {
        if (this.observer) {
            this.observer.disconnect();
        }
    }

    async loadContent() {
        if (this.loaded || !this.link) return;
        
        this.loaded = true;
        this.observer.disconnect();
        
        // Change link text to loading state
        const originalText = this.link.textContent;
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
            
            // Remove the link and replace with loaded content
            this.link.remove();
            this.appendChild(targetElement);
            
            console.log('LinkLoader successfully loaded content for:', fragmentId);
            
        } catch (error) {
            console.error('LinkLoader failed to load content:', error);
            this.link.textContent = `Error: ${error.message}`;
        }
    }
}

// Register the custom element
customElements.define('link-loader', LinkLoader);

export { LinkLoader };