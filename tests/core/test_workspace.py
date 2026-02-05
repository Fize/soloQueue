
import pytest
from pathlib import Path
from soloqueue.core.workspace import workspace, PermissionDenied

def test_resolve_simple_paths(tmp_path):
    # Override root for testing
    workspace.root = tmp_path
    
    assert workspace.resolve_path("foo.txt") == tmp_path / "foo.txt"
    assert workspace.resolve_path("./bar/baz") == tmp_path / "bar/baz"

def test_resolve_absolute_path_inside_root(tmp_path):
    workspace.root = tmp_path
    abs_path = tmp_path / "safe.txt"
    assert workspace.resolve_path(abs_path) == abs_path

def test_prevent_path_traversal(tmp_path):
    workspace.root = tmp_path
    
    with pytest.raises(PermissionDenied):
        workspace.resolve_path("../outside.txt")
        
    with pytest.raises(PermissionDenied):
        workspace.resolve_path("/etc/passwd")

def test_resolve_symlink_attack(tmp_path):
    workspace.root = tmp_path
    
    # Create a symlink pointing outside
    outside = tmp_path.parent / "secret.txt"
    outside.touch()
    link = tmp_path / "link"
    link.symlink_to(outside)
    
    # When resolved, it should point outside and thus be denied
    with pytest.raises(PermissionDenied):
        workspace.resolve_path("link")

