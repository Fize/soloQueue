
import pytest
from unittest.mock import MagicMock, patch, PropertyMock
from soloqueue.orchestration.runner import AgentRunner
from soloqueue.core.registry import Registry
from soloqueue.core.loaders.schema import GroupSchema, AgentSchema
from soloqueue.orchestration.frame import TaskFrame
from langchain_core.messages import SystemMessage

@pytest.fixture
def mock_registry():
    registry = MagicMock(spec=Registry)
    
    # Setup Groups
    registry.groups = {
        "test_group": MagicMock(spec=GroupSchema, shared_context="**GROUP MISSION**: Be efficient."),
        "verbose_group": MagicMock(spec=GroupSchema, shared_context="A" * 1500) # Oversized context
    }
    
    return registry

@pytest.fixture
def mock_agent_config():
    config = MagicMock(spec=AgentSchema)
    config.name = "worker"
    config.group = "test_group"
    config.model = "test-model"
    config.system_prompt = "You are a worker."
    config.sub_agents = []
    # Mock node_id property
    type(config).node_id = PropertyMock(return_value="test_group__worker")
    return config

@pytest.fixture
def mock_frame():
    frame = MagicMock(spec=TaskFrame)
    frame.memory = []
    return frame

@patch("soloqueue.orchestration.runner.ModelAdapterFactory.create")
def test_group_context_injection(mock_adapter_create, mock_registry, mock_agent_config, mock_frame):
    """Verify that group shared_context is injected into System Prompt."""
    
    # Mock LLM and Adapter
    mock_llm = MagicMock()
    mock_llm.invoke.return_value = MagicMock(content="OK")
    mock_adapter_create.return_value = mock_llm

    runner = AgentRunner(
        config=mock_agent_config,
        tools=[],
        registry=mock_registry
    )
    
    # Mock LLM stream call to avoid actual invocation
    # stream returns an iterator of chunks
    mock_chunk = MagicMock()
    mock_chunk.content = "OK"
    mock_chunk.additional_kwargs = {}
    
    with patch.object(runner.llm, "stream", return_value=[mock_chunk]):
        runner.step(mock_frame)
        
        # Check the SystemMessage passed to LLM
        # The stream call args are: messages=[SystemMessage, ...]
        call_args = runner.llm.stream.call_args
        messages = call_args[0][0]
        system_msg = messages[0]
        
        assert isinstance(system_msg, SystemMessage)
        assert "**GROUP MISSION**: Be efficient." in system_msg.content
        assert "## Group Shared Context (test_group)" in system_msg.content

@patch("soloqueue.orchestration.runner.ModelAdapterFactory.create")
def test_oversized_context_warning(mock_adapter_create, mock_registry, mock_agent_config, mock_frame):
    """Verify that a warning is logged for oversized context."""
    
    # Mock LLM and Adapter
    mock_llm = MagicMock()
    mock_llm.invoke.return_value = MagicMock(content="OK")
    mock_adapter_create.return_value = mock_llm

    # Switch to verbose group
    mock_agent_config.group = "verbose_group"
    
    runner = AgentRunner(
        config=mock_agent_config,
        tools=[],
        registry=mock_registry
    )
    
    with patch("soloqueue.orchestration.runner.logger") as mock_logger:
        with patch.object(runner.llm, "invoke", return_value=MagicMock(content="OK")):
            runner.step(mock_frame)
            
            # Check for warning
            mock_logger.warning.assert_called()
            args, _ = mock_logger.warning.call_args
            assert "shared_context is too long" in args[0]
