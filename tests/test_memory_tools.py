"""
Tests for Memory Tools - search_memory and remember tools
"""

import pytest
import tempfile
import shutil
from unittest.mock import patch, MagicMock

from soloqueue.orchestration.tools import (
    create_memory_tools,
    resolve_tools_for_agent,
    _format_search_results,
    _should_store,
)
from soloqueue.core.loaders import AgentConfig


@pytest.fixture
def temp_storage():
    """Create temporary storage directory."""
    temp_dir = tempfile.mkdtemp()
    yield temp_dir
    # Cleanup
    shutil.rmtree(temp_dir, ignore_errors=True)


@pytest.fixture
def mock_embedding():
    """Mock the global embedding system."""
    with patch('soloqueue.core.memory.semantic_store.is_embedding_available', return_value=True), \
         patch('soloqueue.core.memory.semantic_store.get_embedding_dimension', return_value=1536), \
         patch('soloqueue.core.memory.manager.is_embedding_available', return_value=True), \
         patch('soloqueue.core.memory.semantic_store.embed_text') as mock_embed:
        
        # Mock embed_text to return deterministic embeddings
        def mock_embed_fn(texts):
            if isinstance(texts, str):
                texts = [texts]
            # Return simple embeddings based on text hash
            return [[hash(text) % 100 / 100.0] * 1536 for text in texts]
        
        mock_embed.side_effect = mock_embed_fn
        yield mock_embed


@pytest.fixture
def memory_manager(temp_storage, mock_embedding):
    """Create MemoryManager with mocked embedding."""
    from soloqueue.core.memory import MemoryManager
    return MemoryManager(temp_storage, "test_group")


class TestCreateMemoryTools:
    """Tests for create_memory_tools factory function."""
    
    def test_creates_two_tools(self, memory_manager):
        """Test that create_memory_tools returns two tools."""
        tools = create_memory_tools(memory_manager, "test_agent")
        
        assert len(tools) == 2
        tool_names = {t.name for t in tools}
        assert "search_memory" in tool_names
        assert "remember" in tool_names
    
    def test_raises_on_empty_agent_id(self, memory_manager):
        """Test that empty agent_id raises ValueError."""
        with pytest.raises(ValueError, match="agent_id 不能为空"):
            create_memory_tools(memory_manager, "")
        
        with pytest.raises(ValueError, match="agent_id 不能为空"):
            create_memory_tools(memory_manager, None)


class TestSearchMemoryTool:
    """Tests for search_memory tool."""
    
    def test_search_returns_formatted_results(self, memory_manager):
        """Test that search returns human-readable format."""
        tools = create_memory_tools(memory_manager, "test_agent")
        search_tool = next(t for t in tools if t.name == "search_memory")
        
        # Add some knowledge first
        memory_manager.add_knowledge("Test knowledge entry", agent_id="test_agent")
        
        # Search
        result = search_tool.invoke({"query": "Test"})
        
        assert isinstance(result, str)
        assert "搜索结果" in result or "未找到相关记忆" in result
    
    def test_search_empty_result(self, memory_manager):
        """Test search with no results returns friendly message."""
        tools = create_memory_tools(memory_manager, "test_agent")
        search_tool = next(t for t in tools if t.name == "search_memory")
        
        result = search_tool.invoke({"query": "nonexistent content xyz123"})
        
        assert "未找到相关记忆" in result
        assert "remember" in result
    
    def test_search_agent_isolation(self, memory_manager):
        """Test that search only returns own agent's memories."""
        # Add memories for different agents
        memory_manager.add_knowledge("Agent A knowledge", agent_id="agent_a")
        memory_manager.add_knowledge("Agent B knowledge", agent_id="agent_b")
        
        # Create tools for agent_a
        tools_a = create_memory_tools(memory_manager, "agent_a")
        search_a = next(t for t in tools_a if t.name == "search_memory")
        
        # Search should only return agent_a's memory
        result = search_a.invoke({"query": "knowledge"})
        assert "Agent A" in result
        assert "Agent B" not in result


class TestRememberTool:
    """Tests for remember tool."""
    
    def test_remember_success(self, memory_manager):
        """Test successful memory storage."""
        tools = create_memory_tools(memory_manager, "test_agent")
        remember_tool = next(t for t in tools if t.name == "remember")
        
        result = remember_tool.invoke({"content": "Test memory content"})
        
        assert "success" in result
        assert "记忆已存储" in result
    
    def test_remember_with_importance(self, memory_manager):
        """Test memory storage with importance level."""
        tools = create_memory_tools(memory_manager, "test_agent")
        remember_tool = next(t for t in tools if t.name == "remember")
        
        result = remember_tool.invoke({
            "content": "Important memory",
            "importance": "critical"
        })
        
        assert "success" in result
        assert "critical" in result
    
    def test_remember_duplicate_detection(self, memory_manager):
        """Test that duplicate content is detected."""
        tools = create_memory_tools(memory_manager, "test_agent")
        remember_tool = next(t for t in tools if t.name == "remember")
        
        # Store same content twice
        content = "This is unique content for testing"
        result1 = remember_tool.invoke({"content": content})
        result2 = remember_tool.invoke({"content": content})
        
        assert "success" in result1
        assert "duplicate" in result2


class TestFormatSearchResults:
    """Tests for _format_search_results helper."""
    
    def test_empty_entries(self):
        """Test formatting empty results."""
        result = _format_search_results([])
        assert "未找到相关记忆" in result
    
    def test_single_entry(self):
        """Test formatting single entry."""
        from soloqueue.core.memory.semantic_store import MemoryEntry
        
        entry = MemoryEntry(
            id="test_1",
            content="Test content",
            score=0.95,
            metadata={"timestamp": "2024-01-15T10:30:00Z"},
            timestamp="2024-01-15T10:30:00Z"
        )
        
        result = _format_search_results([entry])
        
        assert "搜索结果 (找到 1 条相关记忆)" in result
        assert "0.95" in result
        assert "2024-01-15T10:30:00Z" in result
        assert "Test content" in result


class TestResolveToolsForAgent:
    """Tests for resolve_tools_for_agent with memory support."""
    
    def test_includes_memory_tools_when_available(self, memory_manager):
        """Test that memory tools are included when memory is available."""
        config = AgentConfig(
            name="test_agent",
            description="Test agent",
            model="gpt-4",
            tools=[],
            group="test"
        )
        
        tools = resolve_tools_for_agent(config, memory=memory_manager, agent_id="test__agent")
        
        tool_names = {t.name for t in tools}
        assert "search_memory" in tool_names
        assert "remember" in tool_names
    
    def test_excludes_memory_tools_when_no_memory(self):
        """Test that memory tools are excluded when memory is None."""
        config = AgentConfig(
            name="test_agent",
            description="Test agent",
            model="gpt-4",
            tools=[],
            group="test"
        )
        
        tools = resolve_tools_for_agent(config, memory=None, agent_id="test__agent")
        
        tool_names = {t.name for t in tools}
        assert "search_memory" not in tool_names
        assert "remember" not in tool_names
    
    def test_excludes_memory_tools_when_no_agent_id(self, memory_manager):
        """Test that memory tools are excluded when agent_id is None."""
        config = AgentConfig(
            name="test_agent",
            description="Test agent",
            model="gpt-4",
            tools=[],
            group="test"
        )
        
        tools = resolve_tools_for_agent(config, memory=memory_manager, agent_id=None)
        
        tool_names = {t.name for t in tools}
        assert "search_memory" not in tool_names
        assert "remember" not in tool_names


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
