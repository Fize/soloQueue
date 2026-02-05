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


# Global approval manager
approval_manager: ApprovalBackend = TerminalApproval()
