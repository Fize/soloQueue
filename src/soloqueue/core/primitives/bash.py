import subprocess

from soloqueue.core.primitives.base import ToolResult, success, failure
from soloqueue.core.config import settings
from soloqueue.core.security.approval import approval_manager
from soloqueue.core.security.allowlist import is_safe_command
from soloqueue.core.workspace import workspace

def bash(
    command: str, 
    timeout: int = 30, 
    require_approval: bool = True
) -> ToolResult:
    """
    Execute a bash command.
    
    Args:
        command: The shell command to execute.
        timeout: Execution timeout in seconds.
        require_approval: Whether to check permissions.
    """
    try:
        # Security Check
        should_approve = require_approval and settings.REQUIRE_APPROVAL
        
        # Check if allowlisted
        if is_safe_command(command):
            should_approve = False
            
        if should_approve:
            if not approval_manager.request_approval("BASH_EXEC", command):
                return failure("User permission denied")
                
        # Execute in workspace root
        result = subprocess.run(
            command,
            shell=True,
            cwd=workspace.root,
            capture_output=True,
            text=True,
            timeout=timeout
        )
        
        output = result.stdout
        if result.stderr:
            output += f"\nSTDERR:\n{result.stderr}"
            
        if result.returncode != 0:
            return failure(f"Command failed with code {result.returncode}", output)
            
        return success(output)
        
    except subprocess.TimeoutExpired:
        return failure(f"Command timed out after {timeout}s")
    except Exception as e:
        return failure(f"Execution error: {str(e)}")
