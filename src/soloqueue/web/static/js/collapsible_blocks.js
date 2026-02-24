/**
 * Collapsible agent block functionality
 */

/**
 * Toggle the collapsed state of an agent block
 * @param {HTMLElement} header - The block header element that was clicked
 */
function toggleBlock(header) {
    const block = header.closest('.agent-block');
    const content = block.querySelector('.agent-block-content');
    const toggle = block.querySelector('.agent-block-toggle');

    header.classList.toggle('collapsed');
    content.classList.toggle('collapsed');
    if (toggle) {
        toggle.classList.toggle('collapsed');
    }
}

/**
 * Initialize collapsible blocks on page load
 */
function initCollapsibleBlocks() {
    // Add click handlers to all collapsible block headers
    document.querySelectorAll('.agent-block-header[onclick]').forEach(header => {
        // Remove inline onclick and use event listener for better separation
        const originalOnClick = header.getAttribute('onclick');
        if (originalOnClick && originalOnClick.includes('toggleBlock')) {
            header.removeAttribute('onclick');
            header.addEventListener('click', () => toggleBlock(header));
        }
    });

    // Add keyboard support for accessibility
    document.querySelectorAll('.agent-block-header').forEach(header => {
        header.setAttribute('tabindex', '0');
        header.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                toggleBlock(header);
            }
        });
    });
}

// Initialize when DOM is ready
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initCollapsibleBlocks);
} else {
    initCollapsibleBlocks();
}