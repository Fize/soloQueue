"""
Integration tests for MemoryManager with Phase 3 features.

Tests the complete workflow:
1. Start session
2. Log events  
3. Close session (triggers summarization)
4. Verify summary generation
5. Verify knowledge indexing
6. Search for relevant knowledge
"""

import os
import tempfile
import json
from unittest.mock import patch, Mock
from pathlib import Path

import pytest

from soloqueue.core.memory.manager import MemoryManager
from soloqueue.core.memory.summarizer import SessionSummary


class TestMemoryManagerIntegration:
    """Integration tests for MemoryManager with semantic memory and summarization."""
    
    @pytest.fixture
    def temp_workspace(self):
        """Create a temporary workspace for testing."""
        with tempfile.TemporaryDirectory() as tmpdir:
            yield tmpdir
    
    def test_full_workflow_without_llm(self, temp_workspace):
        """Test full memory workflow without actual LLM calls."""
        # Mock embedding availability to disable semantic store for this test
        with patch('soloqueue.core.memory.manager.is_embedding_available', return_value=False):
            # Initialize manager (no semantic store, no summarizer)
            manager = MemoryManager(
                temp_workspace,
                group="test_group",
                enable_semantic=False,
                enable_summarization=False
            )
            
            assert manager.semantic_store is None
            assert manager.summarizer is None
            
            # Start session
            session_id = manager.start_session("test_session_1", agent_id="test_agent")
            assert session_id == "test_session_1"
            assert session_id in manager.active_sessions
            
            # Log some events
            manager.save_interaction(
                session_id,
                "agent_1",
                "Create a hello world program",
                "Here's a hello world program in Python..."
            )
            
            manager.save_tool_output(
                session_id,
                "write_file",
                "hello.py",
                "print('Hello, World!')"
            )
            
            manager.save_error(
                session_id,
                "File not found: config.py",
                {"error_type": "FileNotFoundError"}
            )
            
            manager.save_success(
                session_id,
                "Program created successfully"
            )
            
            # Close session
            summary = manager.close_session(session_id)
            assert summary is None  # No summarizer enabled
            assert session_id not in manager.active_sessions
            
            # Verify log file exists
            log_path = Path(temp_workspace) / ".soloqueue" / "groups" / "test_group" / "sessions" / session_id / "log.jsonl"
            assert log_path.exists()
            
            # Verify log contains events
            with open(log_path, 'r') as f:
                lines = f.readlines()
                assert len(lines) >= 5  # init + start + 3 events + end
    
    def test_session_summarization(self, temp_workspace):
        """Test session summarization with mocked LLM."""
        # Mock embedding (disable semantic store)
        with patch('soloqueue.core.memory.manager.is_embedding_available', return_value=False):
            # Mock LLM response
            mock_llm_response = Mock()
            mock_llm_response.choices = [Mock()]
            mock_llm_response.choices[0].message.content = json.dumps({
                "objective": "Create hello world program",
                "outcome": "success",
                "key_learnings": [
                    "Python print() function outputs to console",
                    "File I/O requires proper error handling"
                ],
                "difficulty": 3,
                "summary": "## Summary\\n\\nSuccessfully created a hello world program."
            })
            
            with patch('openai.OpenAI') as mock_openai_class:
                mock_client = Mock()
                mock_client.chat.completions.create.return_value = mock_llm_response
                mock_openai_class.return_value = mock_client
                
                # Initialize manager with summarization
                manager = MemoryManager(
                    temp_workspace,
                    group="test_group",
                    enable_semantic=False,
                    enable_summarization=True
                )
                
                assert manager.summarizer is not None
                
                # Start session and log events
                session_id = manager.start_session("test_session_2", agent_id="agent_1")
                
                manager.save_interaction(
                    session_id,
                    "agent_1",
                    "Create hello world",
                    "Created hello.py"
                )
                
                manager.save_success(session_id, "Done")
                
                # Close session (triggers summarization)
                summary = manager.close_session(session_id)
                
                # Verify summary was generated
                assert summary is not None
                assert isinstance(summary, SessionSummary)
                assert summary.objective == "Create hello world program"
                assert summary.outcome == "success"
                assert len(summary.key_learnings) == 2
                assert summary.difficulty == 3
                
                # Verify summary markdown was saved
                summary_path = Path(temp_workspace) / ".soloqueue" / "summaries" / "test_group" / f"{session_id}.md"
                assert summary_path.exists()
                
                with open(summary_path, 'r') as f:
                    content = f.read()
                    assert "Summary" in content
    
    def test_semantic_memory_search(self, temp_workspace):
        """Test semantic memory and knowledge search."""
        # Mock embedding model
        with patch('soloqueue.core.memory.manager.is_embedding_available', return_value=True):
            with patch('soloqueue.core.embedding.get_embedding_model') as mock_get_embed:
                mock_embed_model = Mock()
                mock_embed_model.embed.return_value = [0.1] * 768
                mock_get_embed.return_value = mock_embed_model
                
                # Initialize manager with semantic memory
                manager = MemoryManager(
                    temp_workspace,
                    group="test_group",
                    enable_semantic=True,
                    enable_summarization=False
                )
                
                assert manager.semantic_store is not None
                
                # Add some knowledge
                entry_id = manager.add_knowledge(
                    "Python uses indentation for code blocks",
                    metadata={"type": "lesson", "topic": "python"}
                )
                
                assert entry_id is not None
                
                # Search for knowledge
                results = manager.search_knowledge("python code structure", top_k=5)
                
                assert len(results) == 1
                assert "indentation" in results[0].content
                
                # Check stats
                stats = manager.get_knowledge_stats()
                assert stats["enabled"] is True
                assert stats["total_entries"] == 1
    
    def test_full_integration_with_mocks(self, temp_workspace):
        """Test complete integration: session → summary → knowledge index → search."""
        # Mock embedding
        with patch('soloqueue.core.memory.manager.is_embedding_available', return_value=True):
            with patch('soloqueue.core.embedding.get_embedding_model') as mock_get_embed:
                mock_embed_model = Mock()
                mock_embed_model.embed.return_value = [0.1] * 768
                mock_get_embed.return_value = mock_embed_model
                
                # Mock LLM
                mock_llm_response = Mock()
                mock_llm_response.choices = [Mock()]
                mock_llm_response.choices[0].message.content = json.dumps({
                    "objective": "Implement authentication system",
                    "outcome": "success",
                    "key_learnings": [
                        "JWT tokens require SECRET_KEY environment variable",
                        "Never hardcode secrets in source code",
                        "Use .env files for local development configuration"
                    ],
                    "difficulty": 7,
                    "summary": "## Summary\\n\\nImplemented JWT authentication successfully."
                })
                
                with patch('openai.OpenAI') as mock_openai_class:
                    mock_client = Mock()
                    mock_client.chat.completions.create.return_value = mock_llm_response
                    mock_openai_class.return_value = mock_client
                    
                    # Initialize full-featured manager
                    manager = MemoryManager(
                        temp_workspace,
                        group="production",
                        enable_semantic=True,
                        enable_summarization=True
                    )
                    
                    assert manager.semantic_store is not None
                    assert manager.summarizer is not None
                    
                    # Start session
                    session_id = manager.start_session("auth_task", agent_id="coder_1")
                    
                    # Log events
                    manager.save_interaction(
                        session_id,
                        "coder_1",
                        "Implement JWT authentication",
                        "Creating auth.py with JWT logic..."
                    )
                    
                    manager.save_error(
                        session_id,
                        "Missing SECRET_KEY environment variable"
                    )
                    
                    manager.save_interaction(
                        session_id,
                        "coder_1",
                        "Fix the SECRET_KEY issue",
                        "Adding .env file with SECRET_KEY=..."
                    )
                    
                    manager.save_success(
                        session_id,
                        "JWT authentication working correctly"
                    )
                    
                    # Close session → triggers summarization → indexes learnings
                    summary = manager.close_session(session_id)
                    
                    # Verify summary
                    assert summary is not None
                    assert summary.outcome == "success"
                    assert len(summary.key_learnings) == 3
                    
                    # Verify learnings were indexed to semantic store
                    stats = manager.get_knowledge_stats()
                    assert stats["total_entries"] == 3  # 3 learnings indexed
                    
                    # Search for relevant knowledge
                    results = manager.search_knowledge(
                        "how to handle secrets in authentication",
                        top_k=3
                    )
                    
                    assert len(results) > 0
                    # Should find the learning about not hardcoding secrets
                    content_combined = " ".join([r.content for r in results])
                    assert "secret" in content_combined.lower() or "SECRET_KEY" in content_combined
    
    def test_multiple_sessions(self, temp_workspace):
        """Test handling multiple concurrent sessions."""
        with patch('soloqueue.core.memory.manager.is_embedding_available', return_value=False):
            manager = MemoryManager(
                temp_workspace,
                group="multi_test",
                enable_semantic=False,
                enable_summarization=False
            )
            
            # Start multiple sessions
            session_1 = manager.start_session("session_1", agent_id="agent_1")
            session_2 = manager.start_session("session_2", agent_id="agent_2")
            
            assert len(manager.active_sessions) == 2
            
            # Log to different sessions
            manager.save_interaction(session_1, "agent_1", "input1", "output1")
            manager.save_interaction(session_2, "agent_2", "input2", "output2")
            
            # Close one session
            manager.close_session(session_1)
            assert len(manager.active_sessions) == 1
            assert session_2 in manager.active_sessions
            
            # Close second session
            manager.close_session(session_2)
            assert len(manager.active_sessions) == 0
