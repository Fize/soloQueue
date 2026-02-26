"""
Web UI approval backend for write-action confirmations.

Provides user approval via web UI dialogs with fallback to terminal logging.
"""

import asyncio
from typing import Optional, Dict

from soloqueue.core.logger import logger
from soloqueue.core.security.approval import ApprovalBackend
from soloqueue.web.websocket.schemas import WriteActionRequest
from soloqueue.web.websocket.handlers import send_write_action_request


class WebUIApproval(ApprovalBackend):
    """
    Approval backend that sends confirmation requests to web UI via WebSocket.

    When a write-action requires approval:
    1. If web UI is connected, send WebSocket message and wait for response
    2. If no web UI connection or timeout, fall back to terminal prompt

    This class maintains a registry of pending requests and provides
    methods for WebSocket handlers to submit user responses.
    """

    def __init__(self):
        # Pending requests: request_id -> (future, timestamp, details)
        self._pending_requests: Dict[str, asyncio.Future] = {}
        # Timeout for web UI responses (seconds)
        self._webui_timeout = 30  # seconds
        # Whether web UI is currently connected
        self._webui_connected = False

    def set_webui_connected(self, connected: bool):
        """
        Update web UI connection status.

        Called by WebSocket connection handlers when clients connect/disconnect.
        """
        self._webui_connected = connected
        logger.debug(f"WebUI connection status: {connected}")

    async def send_webui_request(self, request_id: str, operation: str, agent_id: str, file_path: str) -> bool:
        """
        Send approval request to web UI and wait for response.

        Args:
            request_id: Unique identifier for this request
            operation: Operation type ("create", "update", "delete")
            agent_id: Agent requesting the write action
            file_path: File path to be written

        Returns:
            True if user approves, False if rejects or times out
        """
        if not self._webui_connected:
            logger.debug("WebUI not connected, falling back to terminal")
            return False

        # Create future on the running event loop
        loop = asyncio.get_running_loop()
        future = loop.create_future()
        self._pending_requests[request_id] = future

        try:
            # Create and send WebSocket request
            request = WriteActionRequest(
                id=request_id,
                agent_id=agent_id,
                file_path=file_path,
                operation=operation,
            )

            sent = await send_write_action_request(request)
            if not sent:
                logger.warning("Failed to send write-action request to web UI")
                return False

            # Wait for response with timeout
            logger.info(f"Waiting for web UI approval: {operation} - {file_path} (agent: {agent_id})")
            approved = await asyncio.wait_for(future, timeout=self._webui_timeout)
            return approved
        except asyncio.TimeoutError:
            logger.warning(f"WebUI approval timeout for request {request_id}")
            return False
        finally:
            # Clean up
            self._pending_requests.pop(request_id, None)

    def submit_webui_response(self, request_id: str, approved: bool):
        """
        Submit user response from web UI.

        Called by WebSocket handlers when they receive a write_action_response.

        Args:
            request_id: Request ID matching earlier request
            approved: User's decision

        Returns:
            True if request was found and response submitted,
            False if request ID not found (stale or already handled)
        """
        future = self._pending_requests.get(request_id)
        if future and not future.done():
            future.set_result(approved)
            logger.info(f"WebUI response received: request {request_id} -> {'approved' if approved else 'rejected'}")
            return True
        else:
            logger.warning(f"Received response for unknown or completed request: {request_id}")
            return False

    def request_approval(self, operation: str, details: str) -> bool:
        """
        Request user approval for a dangerous operation.

        Implementation of ApprovalBackend abstract method.
        This synchronous method submits the coroutine to the main FastAPI
        event loop via run_coroutine_threadsafe, which is safe to call from
        worker threads (e.g. ThreadPoolExecutor).

        Args:
            operation: Name of the operation (e.g., "WRITE").
            details: Context of the operation (e.g., file path, agent info).

        Returns:
            True if user approves, False otherwise.
        """
        try:
            from soloqueue.web.app import get_main_loop

            main_loop = get_main_loop()
            if main_loop is None or main_loop.is_closed():
                logger.warning("Main event loop not available, operation denied")
                return False

            # Submit coroutine to the main event loop from this worker thread
            future = asyncio.run_coroutine_threadsafe(
                self.request_approval_async(operation, details),
                main_loop,
            )
            # Block until result, with extra margin over the internal WebSocket timeout
            return future.result(timeout=self._webui_timeout + 5)
        except Exception as e:
            logger.error(f"WebUI approval failed: {e}")
            return False

    async def request_approval_async(self, operation: str, details: str, request_id: Optional[str] = None, agent_id: Optional[str] = None) -> bool:
        """
        Async version of request_approval for use with WebSocket handlers.

        Args:
            operation: Operation type ("create", "update", "delete", or general like "WRITE_FILE")
            details: Request details (file path)
            request_id: Optional request ID (generated if not provided)
            agent_id: Optional agent ID (defaults to "unknown")

        Returns:
            True if approved, False otherwise
        """
        if request_id is None:
            import uuid
            request_id = str(uuid.uuid4())

        # Extract file_path from details (assume details is the file path)
        file_path = details.strip()
        if agent_id is None:
            agent_id = "unknown"

        # Map general operation types to WebSocket operation types
        # For file operations, we need to determine if it's create, update, or delete
        # For now, map common operations:
        ws_operation = operation.lower()
        if "write" in ws_operation or "create" in ws_operation:
            ws_operation = "create"  # Default to create for writes
        elif "update" in ws_operation or "modify" in ws_operation:
            ws_operation = "update"
        elif "delete" in ws_operation or "remove" in ws_operation:
            ws_operation = "delete"
        else:
            # Default to create if unknown
            ws_operation = "create"

        # Only use web UI if connected
        if not self._webui_connected:
            logger.warning(f"WebUI not connected, operation denied: {operation}")
            return False

        try:
            return await self.send_webui_request(request_id, ws_operation, agent_id, file_path)
        except Exception as e:
            logger.error(f"WebUI approval failed: {e}")
            return False