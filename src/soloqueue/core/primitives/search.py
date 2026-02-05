import re

from soloqueue.core.workspace import workspace, PermissionDenied
from soloqueue.core.primitives.base import ToolResult, success, failure

def glob(pattern: str, directory: str = ".") -> ToolResult:
    """
    Find files matching a glob pattern.
    """
    try:
        base_dir = workspace.resolve_path(directory)
        if not base_dir.is_dir():
            return failure(f"Not a directory: {directory}")
            
        # Use rglob if pattern contains **
        if "**" in pattern:
            matches = list(base_dir.rglob(pattern))
        else:
            matches = list(base_dir.glob(pattern))
            
        # Return relative paths for cleaner output
        rel_matches = [str(p.relative_to(workspace.root)) for p in matches]
        return success("\n".join(rel_matches))
        
    except PermissionDenied as e:
        return failure(str(e))
    except Exception as e:
        return failure(f"Glob error: {str(e)}")


def grep(
    pattern: str, 
    path: str, 
    recursive: bool = False,
    max_results: int = 100
) -> ToolResult:
    """
    Search for a regex pattern in files.
    """
    try:
        search_path = workspace.resolve_path(path)
        
        files_to_search = []
        if search_path.is_file():
            files_to_search.append(search_path)
        elif search_path.is_dir():
            if recursive:
                files_to_search.extend([p for p in search_path.rglob("*") if p.is_file()])
            else:
                files_to_search.extend([p for p in search_path.glob("*") if p.is_file()])
        else:
            return failure(f"Path not found: {path}")
            
        results = []
        count = 0
        regex = re.compile(pattern)
        
        for file in files_to_search:
            try:
                # Skip binary files roughly
                try:
                    content = file.read_text(encoding="utf-8")
                except UnicodeDecodeError:
                    continue
                    
                lines = content.splitlines()
                for i, line in enumerate(lines):
                    if regex.search(line):
                        rel_path = str(file.relative_to(workspace.root))
                        results.append(f"{rel_path}:{i+1}: {line.strip()}")
                        count += 1
                        if count >= max_results:
                            break
            except Exception:
                continue
            if count >= max_results:
                break
                
        return success("\n".join(results))

    except Exception as e:
        return failure(f"Grep error: {str(e)}")
