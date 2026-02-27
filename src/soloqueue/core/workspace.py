import os
from pathlib import Path
from typing import Optional

from soloqueue.core.config import settings
from soloqueue.core.logger import logger


class WorkspaceError(Exception):
    """Raised when a workspace operation is invalid."""
    pass


class PermissionDenied(WorkspaceError):
    """Raised when attempting to access files outside the sandbox."""
    pass


class WorkspaceManager:
    """
    Manages the file system sandbox for SoloQueue agents.
    Ensures all file operations are confined within the project root.
    """
    
    def __init__(self, root_dir: Optional[str | Path] = None):
        if root_dir:
            self.root = Path(root_dir).resolve()
        elif settings.PROJECT_ROOT:
            self.root = Path(settings.PROJECT_ROOT).resolve()
        else:
            self.root = Path(os.getcwd()).resolve()
            
        logger.info(f"Workspace initialized at: {self.root}")

    def resolve_path(self, rel_path: str | Path) -> Path:
        """
        Resolve a relative path against the workspace root.
        
        Args:
            rel_path: Relative path to resolve.
            
        Returns:
            Absolute Path object.
            
        Raises:
            PermissionDenied: If the resolved path is outside the workspace root.
        """
        # Handle absolute paths that are already inside root
        path_obj = Path(rel_path)
        if path_obj.is_absolute():
            try:
                # relative_to will raise ValueError if not subpath
                path_obj.relative_to(self.root)
                # It is already absolute and safe
                target = path_obj.resolve()
            except ValueError:
                 # It's absolute but outside root
                 # We treat it as relative to root if possible, or reject
                 # Actually, better to strictly treat input as relative unless specified.
                 # Let's assume inputs should be relative. If absolute, we check strict containment.
                 target = path_obj.resolve()
        else:
            target = (self.root / rel_path).resolve()
        
        # Security Check: Ensure target is within root
        # Use os.path.commonpath or check with trailing separator to avoid
        # prefix false positives (e.g. /root-evil matching /root)
        try:
            target.relative_to(self.root)
        except ValueError:
            logger.warning(f"Security Alert: Path traversal attempt to {target}")
            raise PermissionDenied(f"Access denied: {rel_path} escapes workspace sandbox.")
            
        return target


# Global workspace instance
workspace = WorkspaceManager()
