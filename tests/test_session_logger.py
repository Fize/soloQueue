import pytest
import json
from soloqueue.core.memory.manager import MemoryManager


def test_session_logger_basic_functionality(tmp_path):
    """
    Verify SessionLogger generates JSONL and MD files for basic interactions.
    """
    # Setup
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    
    memory = MemoryManager(str(workspace), group="test_group")
    
    # Log an agent interaction
    memory.save_interaction(
        agent_name="test_agent",
        input_msg="What is 2+2?",
        output_msg="The answer is 4.",
        tools=[{"name": "calculator", "args": {"expression": "2+2"}, "id": "call_1"}]
    )
    
    # Verify session directory exists (use the session_id from MemoryManager)
    session_dir = workspace / ".soloqueue" / "groups" / "test_group" / "sessions" / memory.session_id
    assert session_dir.exists(), f"Session directory not found: {session_dir}"
    
    # Verify log.jsonl exists
    jsonl_path = session_dir / "log.jsonl"
    assert jsonl_path.exists(), f"JSONL log not found: {jsonl_path}"
    
    # Parse JSONL and verify structure
    events = []
    with open(jsonl_path, 'r', encoding='utf-8') as f:
        for line in f:
            events.append(json.loads(line))
    
    assert len(events) >= 1, f"Expected at least 1 event, got {len(events)}"
    
    # Verify first event structure
    first_event = events[0]
    assert "type" in first_event
    assert "start_time" in first_event or "timestamp" in first_event
    
    # Verify detailed.md exists
    md_path = session_dir / "detailed.md"
    assert md_path.exists(), f"MD log not found: {md_path}"
    
    md_content = md_path.read_text(encoding='utf-8')
    
    # Check MD is not empty
    assert len(md_content) > 0, "MD file should not be empty"
    
    print(f"\n✓ SessionLogger verification successful")
    print(f"  - Session ID: {memory.session_id}")
    print(f"  - JSONL: {jsonl_path}")
    print(f"  - MD: {md_path}")
    print(f"  - Events logged: {len(events)}")


def test_session_logger_isolation(tmp_path):
    """
    Verify that different groups have isolated session logs.
    """
    workspace = tmp_path / "workspace"
    workspace.mkdir()
    
    # Create two separate MemoryManagers
    mem1 = MemoryManager(str(workspace), group="group_a")
    mem2 = MemoryManager(str(workspace), group="group_b")
    
    # Log to each
    mem1.save_interaction(agent_name="agent_a", input_msg="Group A message", output_msg="Response A")
    mem2.save_interaction(agent_name="agent_b", input_msg="Group B message", output_msg="Response B")
    
    # Verify separate directories
    session_dir_a = workspace / ".soloqueue" / "groups" / "group_a" / "sessions" / mem1.session_id
    session_dir_b = workspace / ".soloqueue" / "groups" / "group_b" / "sessions" / mem2.session_id
    
    assert session_dir_a.exists(), f"Session dir A not found: {session_dir_a}"
    assert session_dir_b.exists(), f"Session dir B not found: {session_dir_b}"
    
    # Verify content isolation
    jsonl_a = (session_dir_a / "log.jsonl").read_text()
    jsonl_b = (session_dir_b / "log.jsonl").read_text()
    
    assert "Group A message" in jsonl_a
    assert "Group A message" not in jsonl_b
    assert "Group B message" in jsonl_b
    assert "Group B message" not in jsonl_a
    
    print("\n✓ Session isolation verified")


if __name__ == "__main__":
    pytest.main([__file__, "-v", "-s"])
