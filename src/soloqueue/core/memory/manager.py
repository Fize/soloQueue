import os
from typing import Optional, Any
from pathlib import Path

from loguru import logger

from soloqueue.core.memory.session_logger import SessionLogger
from soloqueue.core.memory.artifact_store import ArtifactStore
from soloqueue.core.memory.semantic_store import SemanticStore, MemoryEntry
from soloqueue.core.memory.summarizer import SessionSummarizer, SessionSummary
from soloqueue.core.embedding import is_embedding_available


class MemoryManager:
    """
    Core memory manager for SoloQueue (Production Architecture).
    
    Coordinates access to Tiered Memory:
        L1: Working Memory (Agent Context) - In-memory, ephemeral
        L2: Episodic Memory (Session Log) - JSONL + summary
        L3: Semantic Memory (Vector Store) - Long-term knowledge
        L4: Artifact Repository - File storage
    
    Advanced Features (Phase 3):
        - Automatic session summarization on close
        - Semantic search for knowledge retrieval
        - Learning extraction and indexing
    
    Example:
        manager = MemoryManager("/workspace", "dev")
        
        # Start session
        session_id = manager.start_session("task_123", agent_id="agent_1")
        
        # Log events
        manager.save_interaction("agent_1", "Input", "Output")
        manager.save_error("Error occurred")
        
        # Close session (triggers summarization)
        manager.close_session(session_id)
        
        # Search knowledge
        results = manager.search_knowledge("how to fix error X", top_k=3)
    """
    
    def __init__(
        self,
        workspace_root: str,
        group: str = "default",
        enable_semantic: bool = True,
        enable_summarization: bool = True
    ):
        """
        Initialize MemoryManager.
        
        Args:
            workspace_root: Root directory for all memory storage
            group: Memory group name (isolates sessions)
            enable_semantic: Enable semantic memory (requires embedding)
            enable_summarization: Enable automatic session summarization
        """
        self.workspace_root = workspace_root
        self.group = group
        self.active_sessions: dict[str, SessionLogger] = {}
        
        # L4: Artifact Store (shared across groups)
        self.artifact_store = ArtifactStore(workspace_root)
        
        # L3: Semantic Store (group-specific)
        self.semantic_store: Optional[SemanticStore] = None
        if enable_semantic and is_embedding_available():
            semantic_path = os.path.join(
                workspace_root,
                ".soloqueue",
                "semantic",
                group
            )
            try:
                self.semantic_store = SemanticStore(semantic_path)
                logger.info(f"Semantic memory enabled for group: {group}")
            except Exception as e:
                logger.warning(f"Failed to initialize semantic store: {e}")
                self.semantic_store = None
        else:
            if enable_semantic:
                logger.info("Semantic memory disabled (embedding not configured)")
        
        # Session Summarizer
        self.summarizer: Optional[SessionSummarizer] = None
        if enable_summarization:
            try:
                self.summarizer = SessionSummarizer(model="gpt-4o-mini")
                logger.info("Session summarization enabled")
            except Exception as e:
                logger.warning(f"Failed to initialize summarizer: {e}")
                self.summarizer = None
        
        logger.info(
            f"MemoryManager initialized for group '{group}' "
            f"(semantic={self.semantic_store is not None}, "
            f"summarization={self.summarizer is not None})"
        )
    
    # ==================== Session Management ====================
    
    def start_session(
        self,
        session_id: str,
        agent_id: str = "default",
        metadata: Optional[dict[str, Any]] = None
    ) -> str:
        """
        Start a new session.
        
        Args:
            session_id: Unique session identifier
            agent_id: ID of the agent running this session
            metadata: Optional session metadata
        
        Returns:
            session_id
        """
        if session_id in self.active_sessions:
            logger.warning(f"Session {session_id} already active")
            return session_id
        
        # Create session logger
        session_logger = SessionLogger(
            self.workspace_root,
            self.group,
            session_id
        )
        
        # Log session start
        session_logger.log_step({
            "type": "session_start",
            "agent_id": agent_id,
            "metadata": metadata or {},
            "message": f"Session started by {agent_id}"
        })
        
        self.active_sessions[session_id] = session_logger
        logger.info(f"Session started: {session_id} (agent={agent_id})")
        
        return session_id

    def end_session(self, session_id: Optional[str] = None):
        """End a session. If no ID provided, tries to end last active."""
        if session_id:
             if session_id in self.active_sessions:
                 del self.active_sessions[session_id]
        else:
            self.active_sessions.clear()


    
    def close_session(self, session_id: str) -> Optional[SessionSummary]:
        """
        Close session and trigger summarization.
        
        Args:
            session_id: Session to close
        
        Returns:
            SessionSummary if summarization enabled, None otherwise
        """
        if session_id not in self.active_sessions:
            logger.warning(f"Session {session_id} not found")
            return None
        
        session_logger = self.active_sessions[session_id]
        
        # Log session end
        session_logger.log_step({
            "type": "session_end",
            "message": "Session ended"
        })
        
        # Get log file path
        log_path = session_logger.get_log_path()
        
        # Remove from active sessions
        del self.active_sessions[session_id]
        logger.info(f"Session closed: {session_id}")
        
        # Generate summary if enabled
        summary = None
        if self.summarizer:
            try:
                summary = self.summarizer.summarize(session_id, log_path)
                
                # Save summary as markdown
                summary_path = os.path.join(
                    self.workspace_root,
                    ".soloqueue",
                    "summaries",
                    self.group,
                    f"{session_id}.md"
                )
                os.makedirs(os.path.dirname(summary_path), exist_ok=True)
                with open(summary_path, 'w', encoding='utf-8') as f:
                    f.write(summary.markdown)
                
                logger.info(
                    f"Summary generated for {session_id}: "
                    f"{summary.outcome}, {len(summary.key_learnings)} learnings"
                )
                
                # Index learnings to semantic store
                if self.semantic_store and summary.key_learnings:
                    for learning in summary.key_learnings:
                        self.semantic_store.add_entry(
                            content=learning,
                            metadata={
                                "type": "session_learning",
                                "session_id": session_id,
                                "outcome": summary.outcome,
                                "difficulty": summary.difficulty,
                                "timestamp": summary.timestamp
                            }
                        )
                    
                    logger.info(
                        f"Indexed {len(summary.key_learnings)} learnings "
                        f"from session {session_id}"
                    )
            
            except Exception as e:
                logger.error(f"Failed to summarize session {session_id}: {e}")
        
        return summary
    
    # ==================== Event Logging ====================
    
    def save_interaction(
        self,
        session_id: str,
        agent_name: str,
        input_msg: str,
        output_msg: str,
        tools: Optional[list[dict[str, Any]]] = None
    ):
        """Save an agent interaction."""
        if session_id not in self.active_sessions:
            logger.warning(f"Session {session_id} not active")
            return
        
        step_data: dict[str, Any] = {
            "type": "agent_interaction",
            "agent": agent_name,
            "input": input_msg,
            "response": output_msg,
            "tool_calls": tools or []
        }
        self.active_sessions[session_id].log_step(step_data)
    
    def save_tool_output(
        self,
        session_id: str,
        tool_name: str,
        tool_input: str,
        tool_output: str
    ):
        """Save a tool execution result."""
        if session_id not in self.active_sessions:
            logger.warning(f"Session {session_id} not active")
            return
        
        step_data = {
            "type": "tool_output",
            "tool": tool_name,
            "args": tool_input,
            "output": tool_output
        }
        self.active_sessions[session_id].log_step(step_data)
    
    def save_error(
        self,
        session_id: str,
        error_msg: str,
        context: Optional[dict[str, Any]] = None
    ):
        """Save an error event."""
        if session_id not in self.active_sessions:
            logger.warning(f"Session {session_id} not active")
            return
        
        step_data: dict[str, Any] = {
            "type": "error",
            "error": error_msg,
            "context": context or {}
        }
        self.active_sessions[session_id].log_step(step_data)
    
    def save_success(
        self,
        session_id: str,
        message: str,
        metadata: Optional[dict[str, Any]] = None
    ):
        """Save a success event."""
        if session_id not in self.active_sessions:
            logger.warning(f"Session {session_id} not active")
            return
        
        step_data: dict[str, Any] = {
            "type": "success",
            "message": message,
            "metadata": metadata or {}
        }
        self.active_sessions[session_id].log_step(step_data)
    
    # ==================== Semantic Memory ====================
    
    def search_knowledge(
        self,
        query: str,
        top_k: int = 5,
        filter_metadata: Optional[dict[str, Any]] = None
    ) -> list[MemoryEntry]:
        """
        Search semantic memory for relevant knowledge.
        
        Args:
            query: Search query
            top_k: Number of results to return
            filter_metadata: Optional metadata filters
        
        Returns:
            List of MemoryEntry objects, sorted by relevance
        """
        if self.semantic_store is None:
            logger.warning("Semantic memory not available")
            return []
        
        try:
            return self.semantic_store.search(query, top_k, filter_metadata)
        except Exception as e:
            logger.error(f"Knowledge search failed: {e}")
            return []
    
    def add_knowledge(
        self,
        content: str,
        metadata: Optional[dict[str, Any]] = None
    ) -> Optional[str]:
        """
        Add knowledge to semantic memory.
        
        Args:
            content: Knowledge content
            metadata: Optional metadata (type, topic, outcome, etc.)
        
        Returns:
            Entry ID if successful, None otherwise
        """
        if self.semantic_store is None:
            logger.warning("Semantic memory not available")
            return None
        
        try:
            return self.semantic_store.add_entry(content, metadata or {})
        except Exception as e:
            logger.error(f"Failed to add knowledge: {e}")
            return None
    
    def get_knowledge_stats(self) -> dict[str, Any]:
        """Get semantic memory statistics."""
        if self.semantic_store is None:
            return {"enabled": False}
        
        stats = self.semantic_store.get_stats()
        stats["enabled"] = True
        return stats
    
    # ==================== Artifact Management ====================
    
    def save_artifact(
        self,
        content: str,
        title: str,
        author: str = "system",
        tags: Optional[list[str]] = None,
        artifact_type: str = "text",
        session_id: Optional[str] = None
    ) -> str:
        """Save content as an artifact."""
        art_id = self.artifact_store.save_artifact(
            content=content,
            title=title,
            author=author,
            group_id=self.group,
            tags=tags or [],
            artifact_type=artifact_type
        )
        
        # Log to session if provided
        if session_id and session_id in self.active_sessions:
            self.active_sessions[session_id].log_artifact(
                artifact_name=title,
                path=f"artifact:{art_id}"
            )
        
        return art_id
    
    def get_artifact(self, artifact_id: str) -> Optional[dict[str, Any]]:
        """Retrieve artifact."""
        return self.artifact_store.get_artifact(artifact_id)
    
    def list_artifacts(self, tag: Optional[str] = None) -> list[dict[str, Any]]:
        """List artifacts for this group."""
        return self.artifact_store.list_artifacts(group_id=self.group, tag=tag)
    
    def delete_artifact(self, artifact_id: str) -> bool:
        """Delete an artifact by ID."""
        return self.artifact_store.delete_artifact(artifact_id)
