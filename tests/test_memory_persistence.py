
import json
import shutil
import tempfile
import pytest
from pathlib import Path
from soloqueue.core.memory import MemoryManager

class TestMemoryPersistence:
    
    @pytest.fixture
    def workspace(self):
        """Create a temporary workspace."""
        temp_dir = tempfile.mkdtemp()
        yield temp_dir
        shutil.rmtree(temp_dir)

    def test_session_creation(self, workspace):
        """Verify session directory and files are created."""
        manager = MemoryManager(workspace, "test_group")
        manager.start_session()
        
        session_dir = Path(workspace) / ".soloqueue" / "groups" / "test_group" / "sessions" / manager.session_id
        assert session_dir.exists()
        assert (session_dir / "log.jsonl").exists()
        assert (session_dir / "detailed.md").exists()
        
        # Check explicit session start log
        with open(session_dir / "log.jsonl") as f:
            lines = f.readlines()
            # First line is metadata header
            header = json.loads(lines[0])
            assert header["type"] == "session_init"
            
            # Second line (if any) should be session_start
            start_event = json.loads(lines[1])
            assert start_event["type"] == "session_start"

    def test_interaction_logging(self, workspace):
        """Verify agent interactions are logged to both formats."""
        manager = MemoryManager(workspace, "test_group")
        
        input_msg = "Hello Agent"
        output_msg = "Hello User"
        manager.save_interaction("agent_1", input_msg, output_msg)
        
        session_dir = Path(workspace) / ".soloqueue" / "groups" / "test_group" / "sessions" / manager.session_id
        
        # 1. Check JSONL
        with open(session_dir / "log.jsonl") as f:
            lines = f.readlines()
            interaction = json.loads(lines[1]) # Skip header
            assert interaction["type"] == "agent_interaction"
            assert interaction["agent"] == "agent_1"
            assert interaction["input"] == input_msg
            assert interaction["response"] == output_msg
            
        # 2. Check Markdown
        with open(session_dir / "detailed.md") as f:
            content = f.read()
            assert f"### Agent: agent_1" in content
            assert f"**Response:**\n{output_msg}" in content

    def test_tool_output_logging(self, workspace):
        """Verify tool outputs are logged."""
        manager = MemoryManager(workspace, "test_group")
        
        tool_name = "calculator"
        tool_input = "2 + 2"
        tool_output = "4"
        
        manager.save_tool_output(tool_name, tool_input, tool_output)
        
        session_dir = Path(workspace) / ".soloqueue" / "groups" / "test_group" / "sessions" / manager.session_id
        
        # Check JSONL
        with open(session_dir / "log.jsonl") as f:
            lines = f.readlines()
            # Skip header
            tool_log = json.loads(lines[1])
            assert tool_log["type"] == "tool_output"
            assert tool_log["tool"] == tool_name
            assert tool_log["output"] == tool_output

        # Check Markdown
        with open(session_dir / "detailed.md") as f:
            content = f.read()
            assert f"#### Tool Output ({tool_name})" in content
            assert tool_output in content
