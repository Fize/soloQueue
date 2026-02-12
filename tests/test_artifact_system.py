
import pytest
import shutil
import tempfile
from pathlib import Path
from soloqueue.core.memory import MemoryManager
from soloqueue.core.tools.artifact_tools import create_artifact_tools

class TestArtifactSystem:
    
    @pytest.fixture
    def workspace(self):
        """Create a temporary workspace."""
        temp_dir = tempfile.mkdtemp()
        yield temp_dir
        shutil.rmtree(temp_dir)

    def test_artifact_lifecycle(self, workspace):
        """Test save, read, list cycle using tools."""
        manager = MemoryManager(workspace, "test_group")
        tools = create_artifact_tools(manager)
        
        save_tool = next(t for t in tools if t.name == "save_artifact")
        read_tool = next(t for t in tools if t.name == "read_artifact")
        list_tool = next(t for t in tools if t.name == "list_artifacts")
        
        # 1. Save Artifact
        content = "print('Hello World')"
        title = "hello_script"
        result = save_tool.invoke({
            "content": content,
            "title": title,
            "tags": "code, python",
            "artifact_type": "code"
        })
        
        # Parse ID from result string "Artifact saved successfully. ID: art_..."
        assert "Artifact saved successfully" in result
        art_id = result.split("ID: ")[1].strip()
        
        # Verify file exists
        session_dir = Path(workspace) / ".soloqueue" / "artifacts" / "blobs"
        # Since we don't know the exact date path easily without re-implementing logic, 
        # we trust read_tool or search recursively.
        # But let's use read_tool to verify content.
        
        # 2. Read Artifact
        read_result = read_tool.invoke({"artifact_id": art_id})
        assert "Title: hello_script" in read_result
        assert "Content:\nprint('Hello World')" in read_result
        
        # 3. List Artifacts
        list_result = list_tool.invoke({"tag": "python"})
        assert art_id in list_result
        assert "hello_script" in list_result
        
        # 4. List with wrong tag
        empty_list = list_tool.invoke({"tag": "java"})
        assert "No artifacts found" in empty_list

    def test_artifact_persistence(self, workspace):
        """Verify artifacts persist across memory manager instances."""
        # Session 1
        manager1 = MemoryManager(workspace, "group_a")
        tools1 = create_artifact_tools(manager1)
        save_tool = next(t for t in tools1 if t.name == "save_artifact")
        
        res = save_tool.invoke({"content": "data", "title": "persistent_data", "tags": "", "artifact_type": "text"})
        art_id = res.split("ID: ")[1].strip()
        
        # Session 2 (New Manager, Same Workspace)
        manager2 = MemoryManager(workspace, "group_a")
        tools2 = create_artifact_tools(manager2)
        read_tool = next(t for t in tools2 if t.name == "read_artifact")
        
        read_res = read_tool.invoke({"artifact_id": art_id})
        assert "data" in read_res
