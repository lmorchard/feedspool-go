/**
 * Lightbox Overlay Custom Element
 * 
 * Handles modal dialog display for card view item descriptions.
 * Only activates in card view mode.
 */

export class LightboxOverlay extends HTMLElement {
    constructor() {
        super();
        this.isVisible = false;
        this.currentDetails = null;
        this.layoutController = null;
        
        // Create overlay structure
        this.innerHTML = `
            <div class="lightbox-backdrop">
                <div class="lightbox-content">
                    <header class="lightbox-header">
                        <div class="lightbox-title-section"></div>
                        <button class="lightbox-close" aria-label="Close">×</button>
                    </header>
                    <main class="lightbox-body"></main>
                </div>
            </div>
        `;
        
        this.backdrop = this.querySelector('.lightbox-backdrop');
        this.content = this.querySelector('.lightbox-content');
        this.header = this.querySelector('.lightbox-header');
        this.titleSection = this.querySelector('.lightbox-title-section');
        this.closeButton = this.querySelector('.lightbox-close');
        this.body = this.querySelector('.lightbox-body');
        
        // Hide by default
        this.style.display = 'none';
    }

    connectedCallback() {
        // Find the parent layout controller
        this.layoutController = this.closest('layout-controller');
        
        // Setup event listeners
        this.setupEventListeners();
        
        // Monitor details elements
        this.monitorDetailsElements();
    }

    setupEventListeners() {
        // Close button
        this.closeButton.addEventListener('click', () => {
            this.closeLightbox();
        });
        
        // Click outside to close
        this.backdrop.addEventListener('click', (e) => {
            if (e.target === this.backdrop) {
                this.closeLightbox();
            }
        });
        
        // Escape key to close
        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape' && this.isVisible) {
                this.closeLightbox();
            }
        });
    }

    monitorDetailsElements() {
        // Find all item details elements
        const detailsElements = document.querySelectorAll('.item');
        
        detailsElements.forEach(details => {
            details.addEventListener('toggle', (e) => {
                if (e.target.open && this.isCardView()) {
                    // Close other details first
                    this.closeAllDetails();
                    // Open this one in lightbox
                    this.openLightbox(e.target);
                }
            });
        });
    }

    isCardView() {
        return this.layoutController && this.layoutController.classList.contains('view-card');
    }

    closeAllDetails() {
        const allDetails = document.querySelectorAll('.item[open]');
        allDetails.forEach(details => {
            if (details !== this.currentDetails) {
                details.removeAttribute('open');
            }
        });
    }

    openLightbox(detailsElement) {
        this.currentDetails = detailsElement;
        
        // Extract content from the details element
        this.populateLightbox(detailsElement);
        
        // Show lightbox
        this.style.display = 'flex';
        this.isVisible = true;
        
        // Focus management
        this.closeButton.focus();
        
        // Prevent body scrolling
        document.body.style.overflow = 'hidden';
    }

    populateLightbox(detailsElement) {
        // Extract item information
        const titleElement = detailsElement.querySelector('.item-title');
        const dateElement = detailsElement.querySelector('.item-date');
        const contentElement = detailsElement.querySelector('.item-content');
        
        // Clear previous content
        this.titleSection.innerHTML = '';
        this.body.innerHTML = '';
        
        // Add title and date to header
        if (titleElement) {
            const titleClone = titleElement.cloneNode(true);
            this.titleSection.appendChild(titleClone);
        }
        
        if (dateElement) {
            const dateClone = dateElement.cloneNode(true);
            dateClone.classList.add('lightbox-date');
            this.titleSection.appendChild(dateClone);
        }
        
        // Add content to body
        if (contentElement) {
            const contentClone = contentElement.cloneNode(true);
            this.body.appendChild(contentClone);
            
            // Reinitialize any custom elements in the cloned content
            const iframes = contentClone.querySelectorAll('content-isolation-iframe');
            iframes.forEach(iframe => {
                // Trigger reconnection for proper iframe handling
                if (iframe.connectedCallback) {
                    iframe.connectedCallback();
                }
            });
        }
    }

    closeLightbox() {
        if (!this.isVisible) return;
        
        // Hide lightbox
        this.style.display = 'none';
        this.isVisible = false;
        
        // Close the associated details element
        if (this.currentDetails) {
            this.currentDetails.removeAttribute('open');
            this.currentDetails = null;
        }
        
        // Restore body scrolling
        document.body.style.overflow = '';
        
        // Clear content
        this.titleSection.innerHTML = '';
        this.body.innerHTML = '';
    }
}

// Register the lightbox overlay custom element
customElements.define('lightbox-overlay', LightboxOverlay);