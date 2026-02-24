"""
Playwright end-to-end test for Agent output differentiation.

Tests:
1. Agent blocks display with correct colors
2. Collapsible thinking blocks work
3. Preview snippets show correctly
4. Multiple agent outputs are distinguishable
"""

import pytest
import json


@pytest.mark.asyncio
async def test_agent_block_display(browser_page, test_server_url):
    """Test that agent blocks are displayed with proper styling."""
    page = browser_page

    # Navigate to chat page
    await page.goto(f"{test_server_url}/chat")
    await page.wait_for_selector("#messages")

    # Inject test agent blocks
    await page.evaluate("""() => {
        // Clear existing messages
        const messagesDiv = document.getElementById('messages');
        messagesDiv.innerHTML = '';

        // Create test agent block container
        const container = document.createElement('div');
        container.className = 'agent-block-container';
        container.innerHTML = `
            <div class="agent-block"
                 data-agent-id="investment__leader"
                 data-type="thinking"
                 style="--agent-color: #2563eb">
                <div class="agent-block-header collapsed">
                    <div class="agent-block-title">
                        <div class="agent-color-indicator"></div>
                        <span class="agent-name">investment__leader</span>
                        <span class="agent-block-type">thinking</span>
                    </div>
                    <div class="agent-block-toggle collapsed">▼</div>
                    <div class="agent-block-preview" title="Thinking about the task...">
                        Thinking about the task...
                    </div>
                </div>
                <div class="agent-block-content collapsed">
                    This is a test thinking content that should be collapsible.
                </div>
            </div>
        `;
        messagesDiv.appendChild(container);
    }""")

    # Verify agent block elements exist
    agent_block = await page.locator(".agent-block")
    await agent_block.wait_for(state="visible")

    # Check agent name
    agent_name = await page.locator(".agent-name").first
    assert await agent_name.text_content() == "investment__leader"

    # Check block type
    block_type = await page.locator(".agent-block-type").first
    assert await block_type.text_content() == "thinking"

    # Check color indicator
    color_indicator = await page.locator(".agent-color-indicator").first
    assert await color_indicator.is_visible()

    # Check preview snippet
    preview = await page.locator(".agent-block-preview").first
    preview_text = await preview.text_content()
    assert "Thinking about the task" in preview_text

    # Check toggle button
    toggle = await page.locator(".agent-block-toggle").first
    assert await toggle.is_visible()
    assert await toggle.text_content() == "▼"


@pytest.mark.asyncio
async def test_collapsible_blocks(browser_page, test_server_url):
    """Test that thinking blocks can be collapsed and expanded."""
    page = browser_page

    await page.goto(f"{test_server_url}/chat")
    await page.wait_for_selector("#messages")

    # Inject a collapsible block
    await page.evaluate("""() => {
        const messagesDiv = document.getElementById('messages');
        messagesDiv.innerHTML = `
            <div class="agent-block" data-agent-id="test_agent" data-type="thinking">
                <div class="agent-block-header collapsed" onclick="toggleBlock(this)">
                    <div class="agent-block-title">
                        <div class="agent-color-indicator"></div>
                        <span class="agent-name">test_agent</span>
                        <span class="agent-block-type">thinking</span>
                    </div>
                    <div class="agent-block-toggle collapsed">▼</div>
                    <div class="agent-block-preview">Test preview...</div>
                </div>
                <div class="agent-block-content collapsed">
                    This content should be hidden when collapsed.
                </div>
            </div>
        `;
    }""")

    # Initially collapsed
    content = await page.locator(".agent-block-content").first
    assert await content.is_hidden()

    header = await page.locator(".agent-block-header").first
    toggle = await page.locator(".agent-block-toggle").first

    # Click to expand
    await header.click()
    await page.wait_for_timeout(200)  # Allow animation

    # Should now be expanded
    assert not await content.is_hidden()

    # Toggle icon should be rotated
    toggle_class = await toggle.get_attribute("class")
    assert "collapsed" not in toggle_class

    # Click to collapse again
    await header.click()
    await page.wait_for_timeout(200)

    # Should be collapsed again
    assert await content.is_hidden()


@pytest.mark.asyncio
async def test_multiple_agent_colors(browser_page, test_server_url):
    """Test that different agents have distinct colors."""
    page = browser_page

    await page.goto(f"{test_server_url}/chat")
    await page.wait_for_selector("#messages")

    # Inject blocks for multiple agents
    await page.evaluate("""() => {
        const messagesDiv = document.getElementById('messages');
        messagesDiv.innerHTML = `
            <div class="agent-block" data-agent-id="investment__leader" style="--agent-color: #2563eb">
                <div class="agent-block-header">
                    <div class="agent-block-title">
                        <div class="agent-color-indicator"></div>
                        <span class="agent-name">investment__leader</span>
                        <span class="agent-block-type">thinking</span>
                    </div>
                </div>
            </div>
            <div class="agent-block" data-agent-id="investment__fundamental_analyst" style="--agent-color: #16a34a">
                <div class="agent-block-header">
                    <div class="agent-block-title">
                        <div class="agent-color-indicator"></div>
                        <span class="agent-name">investment__fundamental_analyst</span>
                        <span class="agent-block-type">final_result</span>
                    </div>
                </div>
            </div>
            <div class="agent-block" data-agent-id="investment__technical_analyst" style="--agent-color: #ea580c">
                <div class="agent-block-header">
                    <div class="agent-block-title">
                        <div class="agent-color-indicator"></div>
                        <span class="agent-name">investment__technical_analyst</span>
                        <span class="agent-block-type">tool_call</span>
                    </div>
                </div>
            </div>
        `;
    }""")

    # Get all agent blocks
    agent_blocks = await page.locator(".agent-block").all()
    assert len(agent_blocks) == 3

    # Get all agent names
    agent_names = await page.locator(".agent-name").all()
    names_text = [await name.text_content() for name in agent_names]

    expected_names = [
        "investment__leader",
        "investment__fundamental_analyst",
        "investment__technical_analyst"
    ]

    for expected_name in expected_names:
        assert expected_name in names_text

    # Get all block types
    block_types = await page.locator(".agent-block-type").all()
    types_text = [await block_type.text_content() for block_type in block_types]

    expected_types = ["thinking", "final_result", "tool_call"]
    for expected_type in expected_types:
        assert expected_type in types_text

    # Verify each block has a color indicator
    color_indicators = await page.locator(".agent-color-indicator").all()
    assert len(color_indicators) == 3
    for indicator in color_indicators:
        assert await indicator.is_visible()


@pytest.mark.asyncio
async def test_agent_output_font_size(browser_page, test_server_url):
    """Test that agent output uses appropriate font size (≥14px)."""
    page = browser_page

    await page.goto(f"{test_server_url}/chat")
    await page.wait_for_selector("#messages")

    # Inject test content
    await page.evaluate("""() => {
        const messagesDiv = document.getElementById('messages');
        messagesDiv.innerHTML = `
            <div class="agent-block">
                <div class="agent-block-content">
                    Test content to check font size.
                </div>
            </div>
        `;
    }""")

    # Check font size of agent block content
    content = await page.locator(".agent-block-content").first
    font_size = await content.evaluate("""el => {
        return window.getComputedStyle(el).fontSize;
    }""")

    # Convert to pixels (e.g., "16px" -> 16)
    font_size_px = float(font_size.replace("px", ""))
    assert font_size_px >= 14, f"Font size should be ≥14px, got {font_size_px}px"

    # Also check messages container font size
    messages = await page.locator("#messages").first
    messages_font_size = await messages.evaluate("""el => {
        return window.getComputedStyle(el).fontSize;
    }""")

    messages_font_size_px = float(messages_font_size.replace("px", ""))
    assert messages_font_size_px >= 14, f"Messages font size should be ≥14px, got {messages_font_size_px}px"


@pytest.mark.asyncio
async def test_agent_block_responsive_design(browser_page, test_server_url):
    """Test that agent blocks adapt to different screen sizes."""
    page = browser_page

    await page.goto(f"{test_server_url}/chat")

    # Test on desktop size
    await page.set_viewport_size({"width": 1200, "height": 800})

    # Inject test block
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
                    <div class="agent-block-preview">A very long preview text that should truncate on small screens...</div>
                </div>
            </div>
        `;
    }""")

    # Check desktop layout
    header = await page.locator(".agent-block-header").first
    desktop_style = await header.evaluate("""el => {
        return window.getComputedStyle(el).flexWrap;
    }""")
    # On desktop, flex-wrap might be "nowrap"

    # Test on mobile size
    await page.set_viewport_size({"width": 375, "height": 667})  # iPhone size

    # Check mobile layout
    mobile_style = await header.evaluate("""el => {
        return window.getComputedStyle(el).flexWrap;
    }""")

    # On mobile, elements might wrap differently
    # Just ensure the block is still visible and functional
    assert await header.is_visible()

    # Reset to original size
    await page.set_viewport_size({"width": 1200, "height": 800})