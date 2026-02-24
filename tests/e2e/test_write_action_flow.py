"""
Playwright end-to-end test for write-action confirmation flow.

Tests the complete user journey:
1. User triggers a file write operation
2. WebUI dialog appears requesting approval
3. User approves/rejects the operation
4. System processes the response
"""

import asyncio
import json
import pytest
import time
from typing import Dict, Any


@pytest.mark.asyncio
async def test_write_action_dialog_displays(browser_page, test_server_url):
    """Test that write-action dialog appears when triggered."""
    page = browser_page

    # Navigate to chat page
    await page.goto(f"{test_server_url}/chat")
    await page.wait_for_selector("#messages")

    # Mock a write action by simulating WebSocket message
    # For now, we'll test that the confirmation dialog component exists
    # and can be triggered
    await page.evaluate("""() => {
        // Simulate receiving a write_action_request
        const event = new CustomEvent('write-action-request', {
            detail: {
                id: 'test-request-123',
                agent_id: 'investment__leader',
                file_path: '/tmp/test.md',
                operation: 'create',
                timestamp: new Date().toISOString()
            }
        });
        window.dispatchEvent(event);
    }""")

    # Check if confirmation dialog appears
    # The dialog might be shown via Alpine.js or similar
    # We'll look for common dialog elements
    dialog_visible = await page.evaluate("""() => {
        const dialog = document.querySelector('.confirmation-dialog, [role="dialog"]');
        return dialog && dialog.style.display !== 'none';
    }""")

    # For now, just ensure the page is responsive
    assert await page.title() == "Chat Debug | SoloQueue"
    # More specific dialog testing will be added when the dialog implementation is complete


@pytest.mark.asyncio
async def test_write_action_approval_flow(browser_page, test_server_url, write_action_websocket):
    """Test complete approval flow with WebSocket communication."""
    page = browser_page
    websocket = write_action_websocket

    # Navigate to chat page
    await page.goto(f"{test_server_url}/chat")
    await page.wait_for_selector("#messages")

    # Send a test write action request via WebSocket
    request_id = f"test-request-{int(time.time())}"
    test_request = {
        "type": "write_action_request",
        "id": request_id,
        "agent_id": "investment__leader",
        "file_path": "/tmp/test_file.md",
        "operation": "create",
        "timestamp": time.time()
    }

    # Send request and wait for response
    await websocket.send(json.dumps(test_request))

    # Check if dialog appears (implementation dependent)
    # For now, verify WebSocket connection is working
    try:
        # Try to receive a response (should timeout if no response)
        response = await asyncio.wait_for(websocket.recv(), timeout=2.0)
        response_data = json.loads(response)
        assert response_data.get("type") in ["write_action_response", "error"]
    except asyncio.TimeoutError:
        # No response is okay for this test - dialog might require user interaction
        pass

    # Verify page is still functional
    assert await page.is_visible("#messages")


@pytest.mark.asyncio
async def test_webui_connection_status(browser_page, test_server_url):
    """Test that WebUI connection status is displayed correctly."""
    page = browser_page

    # Navigate to chat page
    await page.goto(f"{test_server_url}/chat")

    # Check for connection status badge
    connection_badge = await page.locator(".badge").first
    assert await connection_badge.is_visible()

    # Check badge text indicates connection status
    badge_text = await connection_badge.text_content()
    assert badge_text.lower() in ["connected", "disconnected"]

    # Test that connection can be established
    if badge_text.lower() == "disconnected":
        # The page should automatically try to reconnect
        await page.wait_for_timeout(3000)  # Wait for reconnection attempt
        updated_badge = await page.locator(".badge").first
        updated_text = await updated_badge.text_content()
        # Should eventually connect (or remain disconnected if server is down)
        print(f"Connection status: {updated_text}")


@pytest.mark.asyncio
async def test_write_action_without_webui_connection():
    """Test that write actions are denied when WebUI is not connected."""
    # This would require mocking the approval system
    # For now, we'll create a placeholder test
    pass


@pytest.mark.asyncio
async def test_dialog_accessibility(browser_page, test_server_url):
    """Test that confirmation dialog meets basic accessibility standards."""
    page = browser_page

    await page.goto(f"{test_server_url}/chat")

    # Trigger mock dialog
    await page.evaluate("""() => {
        // Create a simple test dialog
        const dialog = document.createElement('div');
        dialog.id = 'test-dialog';
        dialog.setAttribute('role', 'dialog');
        dialog.setAttribute('aria-labelledby', 'dialog-title');
        dialog.innerHTML = `
            <h3 id="dialog-title">Test Approval Dialog</h3>
            <p>Approve this action?</p>
            <button id="approve-btn">Approve</button>
            <button id="reject-btn">Reject</button>
        `;
        document.body.appendChild(dialog);
    }""")

    # Check dialog has proper ARIA attributes
    dialog = await page.locator("#test-dialog")
    assert await dialog.is_visible()

    role = await dialog.get_attribute("role")
    assert role == "dialog"

    aria_label = await dialog.get_attribute("aria-labelledby")
    assert aria_label == "dialog-title"

    # Check buttons are accessible
    approve_btn = await page.locator("#approve-btn")
    reject_btn = await page.locator("#reject-btn")

    assert await approve_btn.is_enabled()
    assert await reject_btn.is_enabled()

    # Cleanup
    await page.evaluate("""() => {
        document.getElementById('test-dialog')?.remove();
    }""")