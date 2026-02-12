"""
Test Context Builder.
"""

import pytest
from langchain_core.messages import SystemMessage, HumanMessage, AIMessage

from soloqueue.core.context.token_counter import TokenCounter
from soloqueue.core.context.builder import ContextBuilder

@pytest.fixture
def token_counter():
    return TokenCounter(model="gpt-4o")

@pytest.fixture
def builder(token_counter):
    return ContextBuilder(token_counter, response_buffer=100, safety_margin=0.9)

def test_system_prompt_always_included(builder):
    """Test that system prompt is always included even if budget is tight."""
    system_prompt = "You are a helpful assistant."
    history = [HumanMessage(content="Hi")]
    
    context = builder.build_context(system_prompt, history, model_limit=1000)
    
    assert len(context) >= 1
    assert isinstance(context[0], SystemMessage)
    assert context[0].content == system_prompt

def test_priority_newest_first(builder):
    """Test that newest messages are prioritized when budget is limited."""
    system_prompt = "System"
    history = [
        HumanMessage(content=f"Message {i}") 
        for i in range(100)
    ]
    
    # Small limit forces truncation
    context = builder.build_context(system_prompt, history, model_limit=500)
    
    # Should have system + some recent messages
    assert len(context) > 1
    assert len(context) < len(history) + 1  # Not all history fits
    
    # Last message should be the most recent
    assert "99" in context[-1].content

def test_safety_margin_applied(builder):
    """Test that safety margin prevents exceeding limits."""
    system_prompt = "S" * 100  # ~100 chars
    history = [HumanMessage(content="H" * 100) for _ in range(10)]
    
    # Builder has 90% safety margin
    context = builder.build_context(system_prompt, history, model_limit=1000)
    
    total_tokens = builder.estimate_tokens(context)
    
    # Should be well under 1000 due to safety margin and response buffer
    assert total_tokens < 900  # 90% of 1000

def test_empty_history(builder):
    """Test handling of empty history."""
    system_prompt = "System"
    history = []
    
    context = builder.build_context(system_prompt, history)
    
    assert len(context) == 1
    assert isinstance(context[0], SystemMessage)
