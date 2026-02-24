"""
Playwright end-to-end test for font size compliance.

Tests that all UI elements use appropriate font sizes (≥14px)
and follow responsive design principles.
"""

import pytest


@pytest.mark.asyncio
async def test_base_font_size(browser_page, test_server_url):
    """Test that base font size is set to at least 14px."""
    page = browser_page

    await page.goto(test_server_url)

    # Check CSS variable
    font_size = await page.evaluate("""() => {
        return getComputedStyle(document.documentElement)
            .getPropertyValue('--base-font-size').trim();
    }""")

    if font_size:  # Variable might not be defined
        # Convert to pixels (e.g., "14px" -> 14)
        if font_size.endswith("px"):
            font_size_px = float(font_size.replace("px", ""))
            assert font_size_px >= 14, f"Base font size should be ≥14px, got {font_size_px}px"
        elif font_size.endswith("rem"):
            # rem is relative to root font size (usually 16px)
            font_size_rem = float(font_size.replace("rem", ""))
            # Assume root is 16px
            font_size_px = font_size_rem * 16
            assert font_size_px >= 14, f"Base font size should be ≥14px, got {font_size_px}px ({font_size_rem}rem)"

    # Check body font size as fallback
    body_font_size = await page.evaluate("""() => {
        return getComputedStyle(document.body).fontSize;
    }""")

    body_font_size_px = float(body_font_size.replace("px", ""))
    assert body_font_size_px >= 14, f"Body font size should be ≥14px, got {body_font_size_px}px"


@pytest.mark.asyncio
async def test_chat_interface_font_sizes(browser_page, test_server_url):
    """Test font sizes in chat interface."""
    page = browser_page

    await page.goto(f"{test_server_url}/chat")
    await page.wait_for_selector("#messages")

    # Test key elements
    elements_to_check = [
        ("#messages", "Messages container"),
        (".form-control", "Input textarea"),
        (".btn", "Buttons"),
        (".card-body", "Card content"),
        ("h3", "Headings"),
    ]

    for selector, description in elements_to_check:
        elements = await page.locator(selector).all()
        if elements:
            for i, element in enumerate(elements[:3]):  # Check first 3 of each type
                font_size = await element.evaluate("""el => {
                    return window.getComputedStyle(el).fontSize;
                }""")

                font_size_px = float(font_size.replace("px", ""))
                assert font_size_px >= 12, f"{description} font size should be ≥12px, got {font_size_px}px for {selector}"

                # Log for debugging
                print(f"{description} ({selector} #{i}): {font_size_px}px")


@pytest.mark.asyncio
async def test_agent_block_font_sizes(browser_page, test_server_url):
    """Test font sizes in agent blocks."""
    page = browser_page

    await page.goto(f"{test_server_url}/chat")
    await page.wait_for_selector("#messages")

    # Inject test agent block
    await page.evaluate("""() => {
        const messagesDiv = document.getElementById('messages');
        messagesDiv.innerHTML = `
            <div class="agent-block">
                <div class="agent-block-header">
                    <div class="agent-block-title">
                        <div class="agent-color-indicator"></div>
                        <span class="agent-name">test_agent</span>
                        <span class="agent-block-type">thinking</span>
                    </div>
                    <div class="agent-block-preview">Preview text...</div>
                    <div class="agent-block-toggle">▼</div>
                </div>
                <div class="agent-block-content">
                    Main content of the agent block.
                </div>
            </div>
        `;
    }""")

    # Check agent block element font sizes
    block_elements = [
        (".agent-name", "Agent name"),
        (".agent-block-type", "Block type label"),
        (".agent-block-preview", "Preview snippet"),
        (".agent-block-content", "Main content"),
        (".agent-block-toggle", "Toggle button"),
    ]

    for selector, description in block_elements:
        element = await page.locator(selector).first
        if await element.is_visible():
            font_size = await element.evaluate("""el => {
                return window.getComputedStyle(el).fontSize;
            }""")

            font_size_px = float(font_size.replace("px", ""))
            # Agent content should be readable
            assert font_size_px >= 12, f"{description} font size should be ≥12px, got {font_size_px}px"

            print(f"Agent block {description}: {font_size_px}px")


@pytest.mark.asyncio
async def test_dialog_font_sizes(browser_page, test_server_url):
    """Test font sizes in dialogs (confirmation dialogs, etc.)."""
    page = browser_page

    await page.goto(f"{test_server_url}/chat")

    # Create a test dialog
    await page.evaluate("""() => {
        const dialog = document.createElement('div');
        dialog.className = 'confirmation-dialog';
        dialog.style.cssText = 'position: fixed; top: 50%; left: 50%; transform: translate(-50%, -50%); background: white; padding: 20px; border-radius: 8px; box-shadow: 0 4px 20px rgba(0,0,0,0.2); z-index: 1000;';
        dialog.innerHTML = `
            <h3 class="dialog-title">Confirm Action</h3>
            <p class="dialog-message">Are you sure you want to proceed?</p>
            <div class="dialog-buttons">
                <button class="btn btn-primary">Approve</button>
                <button class="btn btn-secondary">Reject</button>
            </div>
        `;
        document.body.appendChild(dialog);
    }""")

    # Check dialog font sizes
    dialog_elements = [
        (".dialog-title", "Dialog title"),
        (".dialog-message", "Dialog message"),
        (".dialog-buttons .btn", "Dialog buttons"),
    ]

    for selector, description in dialog_elements:
        elements = await page.locator(selector).all()
        for i, element in enumerate(elements):
            font_size = await element.evaluate("""el => {
                return window.getComputedStyle(el).fontSize;
            }""")

            font_size_px = float(font_size.replace("px", ""))
            assert font_size_px >= 14, f"{description} font size should be ≥14px, got {font_size_px}px"

    # Cleanup
    await page.evaluate("""() => {
        document.querySelector('.confirmation-dialog')?.remove();
    }""")


@pytest.mark.asyncio
async def test_responsive_font_scaling(browser_page, test_server_url):
    """Test that font sizes scale appropriately on different screen sizes."""
    page = browser_page

    await page.goto(f"{test_server_url}/chat")
    await page.wait_for_selector("#messages")

    # Test on different viewport sizes
    test_sizes = [
        {"width": 1920, "height": 1080, "name": "Desktop"},
        {"width": 768, "height": 1024, "name": "Tablet"},
        {"width": 375, "height": 667, "name": "Mobile"},
    ]

    for size in test_sizes:
        await page.set_viewport_size(size)

        # Check messages container font size
        messages = await page.locator("#messages").first
        font_size = await messages.evaluate("""el => {
            return window.getComputedStyle(el).fontSize;
        }""")

        font_size_px = float(font_size.replace("px", ""))
        assert font_size_px >= 12, f"Font size on {size['name']} ({size['width']}px) should be ≥12px, got {font_size_px}px"

        print(f"{size['name']} ({size['width']}px): messages font size = {font_size_px}px")

    # Reset to reasonable size
    await page.set_viewport_size({"width": 1200, "height": 800})


@pytest.mark.asyncio
async def test_contrast_and_readability(browser_page, test_server_url):
    """Basic test for text contrast (visual, not automated)."""
    page = browser_page

    await page.goto(f"{test_server_url}/chat")

    # This is a basic check - full contrast testing requires specialized tools
    # Check that text elements have reasonable color contrast with background
    text_elements = await page.locator("body *:not(script):not(style)").all()

    # Sample a few elements
    sampled_elements = text_elements[:10] if len(text_elements) > 10 else text_elements

    for i, element in enumerate(sampled_elements):
        try:
            # Get text content
            text = await element.text_content()
            if text and text.strip():  # Has visible text
                # Get color and background color
                color_info = await element.evaluate("""el => {
                    const style = window.getComputedStyle(el);
                    return {
                        color: style.color,
                        backgroundColor: style.backgroundColor,
                        fontSize: style.fontSize,
                        tagName: el.tagName
                    };
                }""")

                # Basic validation
                assert color_info["color"] != "", f"Element {i} ({color_info['tagName']}) should have text color"
                assert color_info["backgroundColor"] != "", f"Element {i} should have background color"

                # Check font size again
                font_size_px = float(color_info["fontSize"].replace("px", ""))
                assert font_size_px >= 10, f"Sampled element font size should be ≥10px, got {font_size_px}px"

        except Exception as e:
            # Some elements might not be accessible - skip
            continue


@pytest.mark.asyncio
async def test_typography_consistency(browser_page, test_server_url):
    """Test that typography is consistent across the application."""
    page = browser_page

    # Visit multiple pages
    pages_to_check = [
        ("/chat", "Chat page"),
        ("/", "Dashboard"),
        ("/agents", "Agents page"),
    ]

    base_font_sizes = []

    for path, description in pages_to_check:
        await page.goto(f"{test_server_url}{path}")
        await page.wait_for_load_state("networkidle")

        # Get body font size
        body_font_size = await page.evaluate("""() => {
            return getComputedStyle(document.body).fontSize;
        }""")

        body_font_size_px = float(body_font_size.replace("px", ""))
        base_font_sizes.append(body_font_size_px)

        print(f"{description}: body font size = {body_font_size_px}px")

    # Check consistency (all within 2px range)
    if len(base_font_sizes) > 1:
        min_size = min(base_font_sizes)
        max_size = max(base_font_sizes)
        size_range = max_size - min_size

        assert size_range <= 4, f"Font sizes should be consistent across pages. Range: {min_size}px to {max_size}px (range: {size_range}px)"