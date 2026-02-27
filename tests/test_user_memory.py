"""Tests for UserMemoryStore."""

import pytest
import tempfile
import shutil
from pathlib import Path

from soloqueue.core.memory.user_memory import UserMemoryStore


@pytest.fixture
def temp_workspace():
    """Create temporary workspace directory."""
    temp_dir = tempfile.mkdtemp()
    yield temp_dir
    shutil.rmtree(temp_dir, ignore_errors=True)


class TestUserMemoryStore:
    """Test UserMemoryStore functionality."""

    def test_read_returns_empty_when_file_not_exists(self, temp_workspace):
        """Test that read() returns empty string when file doesn't exist."""
        store = UserMemoryStore(workspace_root=temp_workspace)
        result = store.read()
        assert result == ""

    def test_read_returns_content_when_file_exists(self, temp_workspace):
        """Test that read() returns file content when file exists."""
        # Create the USER.md file
        user_md = Path(temp_workspace) / ".soloqueue" / "USER.md"
        user_md.parent.mkdir(parents=True, exist_ok=True)
        user_md.write_text("# User Profile\n\n- Name: Test User", encoding="utf-8")

        store = UserMemoryStore(workspace_root=temp_workspace)
        result = store.read()

        assert result == "# User Profile\n\n- Name: Test User"

    def test_exists_returns_false_when_file_not_exists(self, temp_workspace):
        """Test that exists() returns False when file doesn't exist."""
        store = UserMemoryStore(workspace_root=temp_workspace)
        assert store.exists() is False

    def test_exists_returns_true_when_file_exists(self, temp_workspace):
        """Test that exists() returns True when file exists."""
        user_md = Path(temp_workspace) / ".soloqueue" / "USER.md"
        user_md.parent.mkdir(parents=True, exist_ok=True)
        user_md.write_text("# User Profile", encoding="utf-8")

        store = UserMemoryStore(workspace_root=temp_workspace)
        assert store.exists() is True

    def test_create_template_creates_file(self, temp_workspace):
        """Test that create_template() creates the template file."""
        store = UserMemoryStore(workspace_root=temp_workspace)
        store.create_template()

        assert store.exists()
        content = store.read()
        assert "# User Profile" in content
        assert "## 基础信息" in content

    def test_create_template_does_not_overwrite_existing(self, temp_workspace):
        """Test that create_template() doesn't overwrite existing file."""
        user_md = Path(temp_workspace) / ".soloqueue" / "USER.md"
        user_md.parent.mkdir(parents=True, exist_ok=True)
        original_content = "# Custom Profile\n- Custom: Value"
        user_md.write_text(original_content, encoding="utf-8")

        store = UserMemoryStore(workspace_root=temp_workspace)
        store.create_template()

        assert store.read() == original_content

    def test_file_path_property(self, temp_workspace):
        """Test that file_path returns correct path."""
        store = UserMemoryStore(workspace_root=temp_workspace)
        expected = Path(temp_workspace) / ".soloqueue" / "USER.md"
        assert store.file_path == expected
