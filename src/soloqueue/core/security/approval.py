import sys
from abc import ABC, abstractmethod

from soloqueue.core.logger import logger

class ApprovalBackend(ABC):
    """
    Abstract base class for user approval mechanisms.
    """
    @abstractmethod
    def request_approval(self, operation: str, details: str) -> bool:
        """
        Request user approval for a dangerous operation.
        
        Args:
            operation: Name of the operation (e.g., "BASH", "WRITE").
            details: Context of the operation (e.g., command, file content).
            
        Returns:
            True if user approves, False otherwise.
        """
        pass


class TerminalApproval(ApprovalBackend):
    """
    Standard terminal-based interactive approval.
    """
    def request_approval(self, operation: str, details: str) -> bool:
        # Use simple print/input, avoiding logger for the prompt itself 
        # to ensure it's visible in raw output
        try:
            # Try to read from /dev/tty for interactive input if possible
            # This handles cases where stdin might be redirected or weirdly buffered
            try:
                with open("/dev/tty", "r") as tty:
                    print(f"\n\033[93m⚠️  APPROVAL REQUIRED: {operation}\033[0m", file=sys.stderr)
                    print("\033[90m--------------------------------------------------\033[0m", file=sys.stderr)
                    print(details, file=sys.stderr)
                    print("\033[90m--------------------------------------------------\033[0m", file=sys.stderr)
                    print("Allow execution? [y/N]: ", end="", file=sys.stderr, flush=True)
                    response = tty.readline().strip().lower()
            except IOError:
                # Fallback to standard input/output
                print(f"\n\033[93m⚠️  APPROVAL REQUIRED: {operation}\033[0m")
                print("\033[90m--------------------------------------------------\033[0m")
                print(details)
                print("\033[90m--------------------------------------------------\033[0m")
                response = input("Allow execution? [y/N]: ").strip().lower()
            
            allowed = response == "y"
            
            if allowed:
                logger.info(f"User APPROVED {operation}")
            else:
                logger.warning(f"User REJECTED {operation}")
                
            return allowed
        except (KeyboardInterrupt, EOFError):
            logger.error("Approval interrupted")
            return False


class DynamicApproval(ApprovalBackend):
    """
    Approval backend that dynamically chooses between WebUI and terminal.

    Uses WebUI approval when web UI is connected, falls back to terminal otherwise.
    """
    def __init__(self):
        self.webui_connected = False
        self._webui_approval = None
        self._terminal_approval = TerminalApproval()

    def set_webui_connected(self, connected: bool):
        """
        Update web UI connection status.

        When web UI connects, create WebUIApproval instance.
        When it disconnects, revert to terminal.
        """
        self.webui_connected = connected
        if connected and self._webui_approval is None:
            # Import here to avoid circular imports
            from soloqueue.core.security.webui_approval import WebUIApproval
            self._webui_approval = WebUIApproval()
            self._webui_approval.set_webui_connected(True)
        elif not connected and self._webui_approval is not None:
            self._webui_approval.set_webui_connected(False)

        logger.debug(f"DynamicApproval webui_connected={connected}")

    def get_webui_approval(self):
        """
        Get the WebUIApproval instance for WebSocket handlers.

        Returns:
            WebUIApproval instance or None if not initialized
        """
        return self._webui_approval

    def request_approval(self, operation: str, details: str) -> bool:
        """
        Request user approval, using WebUI if connected, otherwise denied.
        """
        if self.webui_connected and self._webui_approval is not None:
            # Use WebUI approval (pure WebUI, no terminal fallback)
            try:
                return self._webui_approval.request_approval(operation, details)
            except Exception as e:
                logger.error(f"WebUI approval failed: {e}")
                return False
        else:
            # WebUI not connected - operation denied
            logger.warning(f"WebUI not connected, operation denied: {operation}")
            return False


# Global approval manager
approval_manager: ApprovalBackend = DynamicApproval()


def get_webui_approval():
    """
    Get the WebUIApproval instance from the global approval manager.

    Returns:
        WebUIApproval instance if available, None otherwise
    """
    if isinstance(approval_manager, DynamicApproval):
        return approval_manager.get_webui_approval()
    return None


def set_webui_connected(connected: bool):
    """
    Update web UI connection status in the global approval manager.

    Args:
        connected: True if web UI is connected, False otherwise
    """
    if isinstance(approval_manager, DynamicApproval):
        approval_manager.set_webui_connected(connected)
