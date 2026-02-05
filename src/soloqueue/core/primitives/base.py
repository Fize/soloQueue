from typing import TypedDict, Optional

class PrimitiveError(Exception):
    """Base exception for all primitive failures."""
    def __init__(self, message: str, output: str = ""):
        self.output = output
        super().__init__(message)

class PermissionDenied(PrimitiveError):
    """Raised when an operation is blocked by security policy or user."""
    pass

class ToolResult(TypedDict):
    """Standardized return type for all primitives."""
    success: bool
    output: str
    error: Optional[str]

def success(output: str) -> ToolResult:
    return {"success": True, "output": output, "error": None}

def failure(error: str, output: str = "") -> ToolResult:
    return {"success": False, "output": output, "error": error}
