"""
Integration test for write-action confirmation flow.

Tests the interaction between approval manager, WebUI approval backend,
and fallback to terminal when web UI is not connected.
"""

import pytest
from unittest.mock import Mock, patch, AsyncMock, MagicMock

# Import the actual classes
from soloqueue.core.security.approval import (
    approval_manager,
    DynamicApproval,
    set_webui_connected,
    get_webui_approval,
)
from soloqueue.core.security.webui_approval import WebUIApproval


class TestWriteActionFlow:
    """Integration tests for write-action confirmation flow."""

    def setup_method(self):
        """Reset the global approval manager to a fresh DynamicApproval."""
        # We cannot replace the global approval_manager directly because
        # other tests might depend on it. Instead, we'll mock its methods
        # for our tests.
        self.original_approval_manager = approval_manager
        # Create a fresh DynamicApproval for testing
        self.test_approval_manager = DynamicApproval()
        # Patch the global approval_manager with our test instance
        self.patcher = patch(
            'soloqueue.core.security.approval.approval_manager',
            self.test_approval_manager
        )
        self.patcher.start()
        # Also patch the functions that use approval_manager
        self.patcher_set = patch(
            'soloqueue.core.security.approval.set_webui_connected',
            self.test_approval_manager.set_webui_connected
        )
        self.patcher_set.start()
        self.patcher_get = patch(
            'soloqueue.core.security.approval.get_webui_approval',
            self.test_approval_manager.get_webui_approval
        )
        self.patcher_get.start()

    def teardown_method(self):
        """Restore the original approval manager."""
        self.patcher.stop()
        self.patcher_set.stop()
        self.patcher_get.stop()

    def test_approval_manager_is_dynamic(self):
        """Test that the global approval manager is a DynamicApproval instance."""
        from soloqueue.core.security.approval import approval_manager as mgr
        assert isinstance(mgr, DynamicApproval)

    def test_set_webui_connected(self):
        """Test that web UI connection status can be updated."""
        mgr = self.test_approval_manager
        # Initial state should be False
        assert mgr.webui_connected is False
        assert mgr._webui_approval is None

        # Connect web UI
        mgr.set_webui_connected(True)
        assert mgr.webui_connected is True
        assert isinstance(mgr._webui_approval, WebUIApproval)

        # Disconnect web UI
        mgr.set_webui_connected(False)
        assert mgr.webui_connected is False
        # Instance still exists but connection status should be False
        assert mgr._webui_approval is not None

    def test_get_webui_approval_instance(self):
        """Test that WebUIApproval instance can be retrieved."""
        mgr = self.test_approval_manager
        # Initially no instance
        webui_approval = mgr.get_webui_approval()
        assert webui_approval is None

        # Connect web UI to create instance
        mgr.set_webui_connected(True)
        webui_approval = mgr.get_webui_approval()
        assert isinstance(webui_approval, WebUIApproval)

        # Disconnect - instance still exists
        mgr.set_webui_connected(False)
        webui_approval = mgr.get_webui_approval()
        assert isinstance(webui_approval, WebUIApproval)
        assert mgr.webui_connected is False

    def test_fallback_to_terminal_when_webui_disconnected(self):
        """Test that approval manager denies operation when web UI is disconnected."""
        # Ensure web UI is disconnected
        mgr = self.test_approval_manager
        mgr.set_webui_connected(False)
        assert mgr._webui_approval is None

        # Request approval - should be denied since WebUI is not connected
        result = mgr.request_approval("WRITE", "Create /tmp/test.md")

        # Should return False (operation denied)
        assert result is False

    @patch('soloqueue.core.security.webui_approval.WebUIApproval.request_approval')
    def test_use_webui_when_connected(self, mock_webui_request):
        """Test that approval manager uses WebUI when connected."""
        # Set up mock webui approval
        mock_webui_request.return_value = True

        # Connect web UI
        mgr = self.test_approval_manager
        mgr.set_webui_connected(True)
        webui_approval = mgr.get_webui_approval()
        assert webui_approval is not None

        # Request approval
        result = mgr.request_approval("WRITE", "Create /tmp/test.md")

        # Should call webui approval's request_approval
        mock_webui_request.assert_called_once_with("WRITE", "Create /tmp/test.md")
        assert result is True

    @patch('soloqueue.core.security.webui_approval.WebUIApproval.request_approval')
    def test_webui_fallback_to_terminal_on_exception(self, mock_webui_request):
        """
        Test that WebUI approval returns False when its own request fails.
        """
        # Set up mock
        mock_webui_request.side_effect = Exception("WebUI error")

        # Connect web UI
        mgr = self.test_approval_manager
        mgr.set_webui_connected(True)

        # Request approval
        result = mgr.request_approval("WRITE", "Create /tmp/test.md")

        # WebUI approval should have been attempted
        mock_webui_request.assert_called_once_with("WRITE", "Create /tmp/test.md")
        # Should return False (operation denied)
        assert result is False

    def test_webui_approval_async_methods(self):
        """Test async methods of WebUIApproval."""
        import asyncio

        # Connect web UI
        mgr = self.test_approval_manager
        mgr.set_webui_connected(True)
        webui_approval = mgr.get_webui_approval()
        assert webui_approval is not None

        # Mock the async send_webui_request to simulate user approval
        with patch.object(webui_approval, 'send_webui_request', AsyncMock(return_value=True)):
            # Run async method synchronously
            result = asyncio.run(webui_approval.request_approval_async(
                operation="create",
                details="/tmp/test.md",
                request_id="test-id-123",
            ))
            assert result is True
            webui_approval.send_webui_request.assert_called_once_with(
                "test-id-123", "create", "unknown", "/tmp/test.md"
            )

    def test_webui_approval_submit_response(self):
        """Test submitting a response to WebUI approval."""
        # Connect web UI
        mgr = self.test_approval_manager
        mgr.set_webui_connected(True)
        webui_approval = mgr.get_webui_approval()
        assert webui_approval is not None

        # Submit response for non-existent request (should return False)
        result = webui_approval.submit_webui_response("non-existent-id", True)
        assert result is False