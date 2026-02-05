from soloqueue.core.workspace import workspace, PermissionDenied as WorkspacePermissionDenied
from soloqueue.core.primitives.base import ToolResult, success, failure
from soloqueue.core.config import settings
from soloqueue.core.security.approval import approval_manager

def read_file(path: str) -> ToolResult:
    """
    Read the content of a file.
    
    Args:
        path: Path relative to workspace root.
    """
    try:
        file_path = workspace.resolve_path(path)
        if not file_path.exists():
            return failure(f"File not found: {path}")
        if not file_path.is_file():
            return failure(f"Not a file: {path}")
            
        content = file_path.read_text(encoding="utf-8")
        return success(content)
        
    except WorkspacePermissionDenied as e:
        return failure(str(e))
    except Exception as e:
        return failure(f"Read error: {str(e)}")


def write_file(path: str, content: str, require_approval: bool = True) -> ToolResult:
    """
    Write content to a file.
    
    Args:
        path: Path relative to workspace root.
        content: Text content to write.
        require_approval: Whether to ask user for permission.
    """
    try:
        file_path = workspace.resolve_path(path)
        
        # Security Check
        should_approve = require_approval and settings.REQUIRE_APPROVAL
        if should_approve:
            details = f"Target: {path}\nContent Preview:\n{content[:200]}..."
            if not approval_manager.request_approval("WRITE_FILE", details):
                return failure("User permission denied")
        
        # Ensure parent exists
        file_path.parent.mkdir(parents=True, exist_ok=True)
        file_path.write_text(content, encoding="utf-8")
        
        return success(f"Successfully wrote {len(content)} chars to {path}")
        
    except WorkspacePermissionDenied as e:
        return failure(str(e))
    except Exception as e:
        return failure(f"Write error: {str(e)}")
