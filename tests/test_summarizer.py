"""
Tests for SessionSummarizer - LLM-driven session log summarization
"""

import pytest
import tempfile
import json
from datetime import datetime, timedelta
from pathlib import Path
from unittest.mock import patch, MagicMock


@pytest.fixture
def sample_events():
    """Create sample session events."""
    base_time = datetime.now()
    
    return [
        {
            "timestamp": base_time.isoformat(),
            "event_type": "start",
            "content": "Starting task: Implement JWT authentication",
            "metadata": {}
        },
        {
            "timestamp": (base_time + timedelta(seconds=10)).isoformat(),
            "event_type": "llm_call",
            "content": "Planning JWT implementation steps",
            "metadata": {"model": "gpt-4"}
        },
        {
            "timestamp": (base_time + timedelta(seconds=30)).isoformat(),
            "event_type": "tool_use",
            "content": "Created file: auth.py",
            "metadata": {"tool": "write_file"}
        },
        {
            "timestamp": (base_time + timedelta(seconds=45)).isoformat(),
            "event_type": "error",
            "content": "Missing SECRET_KEY environment variable",
            "metadata": {"error_type": "ConfigError"}
        },
        {
            "timestamp": (base_time + timedelta(seconds=60)).isoformat(),
            "event_type": "llm_call",
            "content": "Suggesting to add .env file with SECRET_KEY",
            "metadata": {"model": "gpt-4"}
        },
        {
            "timestamp": (base_time + timedelta(seconds=75)).isoformat(),
            "event_type": "tool_use",
            "content": "Created .env file with SECRET_KEY=...",
            "metadata": {"tool": "write_file"}
        },
        {
            "timestamp": (base_time + timedelta(seconds=90)).isoformat(),
            "event_type": "success",
            "content": "JWT authentication working correctly",
            "metadata": {}
        }
    ]


@pytest.fixture
def temp_log_file(sample_events):
    """Create temporary JSONL log file."""
    temp_file = tempfile.NamedTemporaryFile(mode='w', suffix='.jsonl', delete=False)
    
    for event in sample_events:
        temp_file.write(json.dumps(event) + '\n')
    
    temp_file.close()
    
    yield temp_file.name
    
    # Cleanup
    Path(temp_file.name).unlink(missing_ok=True)


@pytest.fixture
def mock_llm():
    """Mock OpenAI LLM calls."""
    with patch('openai.OpenAI') as mock_openai:
        # Mock response
        mock_client = MagicMock()
        mock_response = MagicMock()
        mock_message = MagicMock()
        
        mock_message.content = json.dumps({
            "objective": "Implement JWT authentication for API endpoints",
            "outcome": "success",
            "key_learnings": [
                "JWT requires SECRET_KEY in environment variables",
                "Never hardcode secrets in source code",
                "Use .env files for local development configuration"
            ],
            "difficulty": 6,
            "summary": (
                "## Session Summary\n\n"
                "Successfully implemented JWT authentication. "
                "Encountered missing SECRET_KEY error and resolved by creating .env file.\n\n"
                "**Key Steps:**\n"
                "1. Created auth.py module\n"
                "2. Added SECRET_KEY to .env\n"
                "3. Verified JWT token generation works"
            )
        })
        
        mock_response.choices = [MagicMock(message=mock_message)]
        mock_client.chat.completions.create.return_value = mock_response
        mock_openai.return_value = mock_client
        
        yield mock_client


def test_summarizer_initialization(mock_llm):
    """Test SessionSummarizer initialization."""
    from soloqueue.core.memory.summarizer import SessionSummarizer
    
    summarizer = SessionSummarizer(model="gpt-4o-mini")
    
    assert summarizer.model == "gpt-4o-mini"
    assert summarizer.max_events == 100


def test_load_events(temp_log_file):
    """Test loading events from JSONL file."""
    from soloqueue.core.memory.summarizer import SessionSummarizer
    
    with patch('openai.OpenAI'):
        summarizer = SessionSummarizer()
        events = summarizer._load_events(temp_log_file)
    
    assert len(events) == 7
    assert events[0].event_type == "start"
    assert events[-1].event_type == "success"


def test_load_events_nonexistent_file():
    """Test loading events from nonexistent file."""
    from soloqueue.core.memory.summarizer import SessionSummarizer
    
    with patch('openai.OpenAI'):
        summarizer = SessionSummarizer()
        events = summarizer._load_events("/nonexistent/file.jsonl")
    
    assert events == []


def test_extract_key_events(temp_log_file):
    """Test extracting key events from full log."""
    from soloqueue.core.memory.summarizer import SessionSummarizer
    
    with patch('openai.OpenAI'):
        summarizer = SessionSummarizer(max_events=5)
        events = summarizer._load_events(temp_log_file)
        key_events = summarizer._extract_key_events(events)
    
    # Should prioritize errors and successes
    assert len(key_events) <= 5
    event_types = [e.event_type for e in key_events]
    # Should have at least one critical event (error or success)
    assert "error" in event_types or "success" in event_types


def test_calculate_duration(sample_events):
    """Test duration calculation."""
    from soloqueue.core.memory.summarizer import SessionSummarizer, SessionEvent
    
    with patch('openai.OpenAI'):
        summarizer = SessionSummarizer()
        
        events = [
            SessionEvent(
                timestamp=sample_events[0]["timestamp"],
                event_type="start",
                content="",
                metadata={}
            ),
            SessionEvent(
                timestamp=sample_events[-1]["timestamp"],
                event_type="end",
                content="",
                metadata={}
            )
        ]
        
        duration = summarizer._calculate_duration(events)
        
        # Should be approximately 90 seconds
        assert 85 <= duration <= 95


def test_summarize_session(temp_log_file, mock_llm):
    """Test full session summarization."""
    from soloqueue.core.memory.summarizer import SessionSummarizer
    
    summarizer = SessionSummarizer(model="gpt-4o-mini")
    summary = summarizer.summarize("test_session_123", temp_log_file)
    
    assert summary.session_id == "test_session_123"
    assert summary.objective == "Implement JWT authentication for API endpoints"
    assert summary.outcome == "success"
    assert len(summary.key_learnings) == 3
    assert summary.difficulty == 6
    assert "JWT" in summary.markdown
    assert summary.duration_seconds > 0


def test_summarize_empty_log(mock_llm):
    """Test summarizing empty log file."""
    from soloqueue.core.memory.summarizer import SessionSummarizer
    
    # Create empty log file
    temp_file = tempfile.NamedTemporaryFile(mode='w', suffix='.jsonl', delete=False)
    temp_file.close()
    
    try:
        summarizer = SessionSummarizer()
        summary = summarizer.summarize("empty_session", temp_file.name)
        
        assert summary.session_id == "empty_session"
        assert summary.outcome == "failure"
        assert summary.duration_seconds == 0
        assert "Empty" in summary.markdown
    finally:
        Path(temp_file.name).unlink(missing_ok=True)


def test_llm_call_failure(temp_log_file):
    """Test handling of LLM call failure."""
    from soloqueue.core.memory.summarizer import SessionSummarizer
    
    with patch('openai.OpenAI') as mock_openai:
        # Make LLM call fail
        mock_client = MagicMock()
        mock_client.chat.completions.create.side_effect = Exception("API Error")
        mock_openai.return_value = mock_client
        
        summarizer = SessionSummarizer()
        summary = summarizer.summarize("test_session", temp_log_file)
        
        # Should use fallback summary
        assert summary.outcome == "partial"
        assert "Unable to generate summary" in summary.key_learnings[0]


def test_format_events_for_llm():
    """Test formatting events for LLM prompt."""
    from soloqueue.core.memory.summarizer import SessionSummarizer, SessionEvent
    
    with patch('openai.OpenAI'):
        summarizer = SessionSummarizer()
        
        events = [
            SessionEvent("2024-01-01T00:00:00", "start", "Starting task", {}),
            SessionEvent("2024-01-01T00:01:00", "error", "An error occurred", {}),
        ]
        
        formatted = summarizer._format_events_for_llm(events)
        
        assert "1. [start]" in formatted
        assert "2. [error]" in formatted
        assert "Starting task" in formatted


def test_convenience_function(temp_log_file, mock_llm):
    """Test convenience summarize_session function."""
    from soloqueue.core.memory.summarizer import summarize_session
    
    summary = summarize_session("test_123", temp_log_file)
    
    assert summary.session_id == "test_123"
    assert summary.outcome == "success"


def test_key_learnings_extraction(temp_log_file, mock_llm):
    """Test that key learnings are properly extracted."""
    from soloqueue.core.memory.summarizer import SessionSummarizer
    
    summarizer = SessionSummarizer()
    summary = summarizer.summarize("test_session", temp_log_file)
    
    # Check key learnings
    learnings = summary.key_learnings
    assert len(learnings) > 0
    
    # Should be meaningful strings
    for learning in learnings:
        assert isinstance(learning, str)
        assert len(learning) > 10  # Not too short


def test_markdown_summary_format(temp_log_file, mock_llm):
    """Test that markdown summary is well-formatted."""
    from soloqueue.core.memory.summarizer import SessionSummarizer
    
    summarizer = SessionSummarizer()
    summary = summarizer.summarize("test_session", temp_log_file)
    
    # Should be markdown
    assert "##" in summary.markdown or "#" in summary.markdown
    assert len(summary.markdown) > 50  # Should be substantial


def test_outcome_classification(temp_log_file, mock_llm):
    """Test that outcome is properly classified."""
    from soloqueue.core.memory.summarizer import SessionSummarizer
    
    summarizer = SessionSummarizer()
    summary = summarizer.summarize("test_session", temp_log_file)
    
    # Should be one of the valid outcomes
    assert summary.outcome in ("success", "failure", "partial")


def test_difficulty_rating(temp_log_file, mock_llm):
    """Test that difficulty rating is reasonable."""
    from soloqueue.core.memory.summarizer import SessionSummarizer
    
    summarizer = SessionSummarizer()
    summary = summarizer.summarize("test_session", temp_log_file)
    
    # Should be between 1-10
    assert 1 <= summary.difficulty <= 10


def test_long_event_truncation():
    """Test that long event content is truncated for LLM."""
    from soloqueue.core.memory.summarizer import SessionSummarizer, SessionEvent
    
    with patch('openai.OpenAI'):
        summarizer = SessionSummarizer()
        
        # Create event with very long content
        long_content = "x" * 500
        events = [SessionEvent("2024-01-01", "test", long_content, {})]
        
        formatted = summarizer._format_events_for_llm(events)
        
        # Should be truncated
        assert len(formatted) < len(long_content)
        assert "..." in formatted


def test_max_events_limit(temp_log_file):
    """Test that max_events limit is respected."""
    from soloqueue.core.memory.summarizer import SessionSummarizer
    
    with patch('openai.OpenAI'):
        summarizer = SessionSummarizer(max_events=3)
        events = summarizer._load_events(temp_log_file)
        key_events = summarizer._extract_key_events(events)
        
        assert len(key_events) <= 3


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
