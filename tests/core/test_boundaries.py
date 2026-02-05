
import os
import pytest
from pathlib import Path
from soloqueue.core.config import Settings
from soloqueue.core.workspace import WorkspaceManager, PermissionDenied

# --- Config Boundaries ---

def test_config_env_override(monkeypatch, tmp_path):
    # Setup .env file
    env_file = tmp_path / ".env"
    env_file.write_text("LOG_LEVEL=DEBUG\nDEFAULT_MODEL=gpt-3.5")
    
    # Override via OS environment
    monkeypatch.setenv("DEFAULT_MODEL", "gpt-4")
    
    # Load settings from tmp_path context
    # Note: pydantic BaseSettings usually looks at CWD for .env by default.
    # We can pass env_file argument dynamically or change CWD.
    curr_dir = os.getcwd()
    try:
        os.chdir(tmp_path)
        settings = Settings()
        assert settings.LOG_LEVEL == "DEBUG"  # From .env
        assert settings.DEFAULT_MODEL == "gpt-4"  # Env var overrides .env
    finally:
        os.chdir(curr_dir)

def test_config_empty_strings(monkeypatch):
    monkeypatch.setenv("OPENAI_API_KEY", "")
    settings = Settings()
    # Pydantic keeps it as empty string, which is technically not None
    assert settings.OPENAI_API_KEY == "" 

# --- Workspace Boundaries ---

def test_workspace_unicode_paths(tmp_path):
    """Test paths with spaces and unicode characters."""
    workspace = WorkspaceManager(root_dir=tmp_path)
    
    # Spaces
    assert workspace.resolve_path("folder with spaces/file.txt") == tmp_path / "folder with spaces/file.txt"
    
    # Unicode / Chinese
    assert workspace.resolve_path("测试目录/文件.txt") == tmp_path / "测试目录/文件.txt"

def test_workspace_complex_traversal(tmp_path):
    """Test convoluted relative paths that stay inside."""
    workspace = WorkspaceManager(root_dir=tmp_path)
    
    # a/b/../../c -> c (Safe)
    complex_path = "a/b/../../c/d.txt"
    resolved = workspace.resolve_path(complex_path)
    assert resolved == tmp_path / "c/d.txt"

def test_workspace_root_itself(tmp_path):
    """Test resolving empty string or '.' maps to root."""
    workspace = WorkspaceManager(root_dir=tmp_path)
    assert workspace.resolve_path(".") == tmp_path
    assert workspace.resolve_path("") == tmp_path

def test_workspace_case_sensitivity(tmp_path):
    """
    On Linux, Foo and foo are different. 
    Workspace should respect exact paths.
    """
    workspace = WorkspaceManager(root_dir=tmp_path)
    assert workspace.resolve_path("Foo") == tmp_path / "Foo"
    assert workspace.resolve_path("foo") == tmp_path / "foo"

def test_workspace_nested_symlinks(tmp_path):
    """
    Test a symlink inside the workspace that points to another file inside the workspace.
    This should be ALLOWED.
    """
    workspace = WorkspaceManager(root_dir=tmp_path)
    
    real_file = tmp_path / "real.txt"
    real_file.touch()
    
    link = tmp_path / "link_internal"
    link.symlink_to(real_file)
    
    # Access via link
    resolved = workspace.resolve_path("link_internal")
    # Python's resolve() follows symlinks by default
    assert resolved == real_file

def test_workspace_symlink_loop(tmp_path):
    """
    Test infinite symlink loop. Pathlib usually raises RuntimeError.
    """
    workspace = WorkspaceManager(root_dir=tmp_path)
    
    link1 = tmp_path / "link1"
    link2 = tmp_path / "link2"
    
    # Create loop: link1 -> link2 -> link1
    link1.symlink_to(link2)
    link2.symlink_to(link1)
    
    with pytest.raises(RuntimeError): # Pathlib raises RuntimeError on loops
        workspace.resolve_path("link1")
