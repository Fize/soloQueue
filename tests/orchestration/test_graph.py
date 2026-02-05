
import pytest
from unittest.mock import MagicMock, patch
from langchain_core.messages import HumanMessage, AIMessage

from soloqueue.orchestration.graph.builder import build_dynamic_graph, get_router
from soloqueue.orchestration.state import AgentState

@pytest.fixture
def mock_loader():
    with patch("soloqueue.orchestration.graph.builder.AgentLoader") as MockLoader:
        loader = MockLoader.return_value
        loader.load_all.return_value = {
            "leader": MagicMock(tools=[], sub_agents=["fundamental_analyst", "technical_analyst", "trader"], model=None),
            "fundamental_analyst": MagicMock(tools=[], sub_agents=[], model=None),
            "technical_analyst": MagicMock(tools=[], sub_agents=[], model=None),
            "trader": MagicMock(tools=[], sub_agents=[], model=None),
        }
        yield loader

@pytest.fixture
def mock_llm_factory():
    with patch("soloqueue.orchestration.graph.node.LLMFactory") as MockFactory:
        mock_llm = MagicMock()
        MockFactory.get_llm.return_value = mock_llm
        mock_llm.bind_tools.return_value = mock_llm
        yield MockFactory

def test_graph_construction(mock_loader, mock_llm_factory):
    """Test that the graph compiles without error given mock agents."""
    graph = build_dynamic_graph()
    assert graph is not None

def test_router_logic():
    """Test the router logic function directly."""
    router = get_router("leader")
    
    # Case 1: No tool calls -> __end__
    state = AgentState(messages=[AIMessage(content="Hello")])
    assert router(state) == "__end__"
    
    # Case 2: Delegate
    state = AgentState(messages=[AIMessage(
        content="", 
        tool_calls=[{
            "name": "delegate_to", 
            "args": {"target": "cto", "instruction": "do it"}, 
            "id": "call_123"
        }]
    )])
    assert router(state) == "delegate_node"
    
    # Case 3: Proper Tool
    state = AgentState(messages=[AIMessage(
        content="", 
        tool_calls=[{
            "name": "read_file", 
            "args": {"path": "test.txt"}, 
            "id": "call_456"
        }]
    )])
    assert router(state) == "leader_tools"
