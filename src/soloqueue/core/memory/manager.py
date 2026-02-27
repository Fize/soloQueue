import os
from typing import Optional, Any

from loguru import logger

from soloqueue.core.memory.artifact_store import ArtifactStore
from soloqueue.core.memory.semantic_store import SemanticStore, MemoryEntry
from soloqueue.core.embedding import is_embedding_available


class MemoryManager:
    """
    Core memory manager for SoloQueue (Simplified Architecture).
    
    Coordinates access to Tiered Memory:
        L1: Working Memory (Agent Context) - In-memory, ephemeral
        L3: Semantic Memory (Vector Store) - Long-term knowledge
        L4: Artifact Repository - File storage
    
    Note: L2 Episodic Memory (Session Log) has been removed in favor of
    structured JSON logging via loguru.
    
    Example:
        manager = MemoryManager("/workspace", "dev")
        
        # Add knowledge to semantic memory
        manager.add_knowledge("Important learning", {"type": "lesson"})
        
        # Search knowledge
        results = manager.search_knowledge("how to fix error X", top_k=3)
    """
    
    def __init__(
        self,
        workspace_root: str,
        group: str = "default",
        enable_semantic: bool = True
    ):
        """
        Initialize MemoryManager.
        
        Args:
            workspace_root: Root directory for all memory storage
            group: Memory group name (isolates sessions)
            enable_semantic: Enable semantic memory (requires embedding)
        """
        self.workspace_root = workspace_root
        self.group = group
        
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
        
        logger.info(
            f"MemoryManager initialized for group '{group}' "
            f"(semantic={self.semantic_store is not None})"
        )
    
    # ==================== Semantic Memory ====================
    
    def search_knowledge(
        self,
        query: str,
        top_k: int = 5,
        filter_metadata: Optional[dict[str, Any]] = None,
        agent_id: Optional[str] = None
    ) -> list[MemoryEntry]:
        """
        Search semantic memory for relevant knowledge.
        
        Args:
            query: Search query
            top_k: Number of results to return
            filter_metadata: Optional metadata filters
            agent_id: Optional agent identifier for memory isolation
        
        Returns:
            List of MemoryEntry objects, sorted by relevance
        """
        if self.semantic_store is None:
            logger.warning("Semantic memory not available")
            return []
        
        try:
            return self.semantic_store.search(query, top_k, filter_metadata, agent_id)
        except Exception as e:
            logger.error(f"Knowledge search failed: {e}")
            return []
    
    def add_knowledge(
        self,
        content: str,
        metadata: Optional[dict[str, Any]] = None,
        agent_id: Optional[str] = None
    ) -> Optional[str]:
        """
        Add knowledge to semantic memory.
        
        Args:
            content: Knowledge content
            metadata: Optional metadata (type, topic, outcome, etc.)
            agent_id: Optional agent identifier for memory isolation
        
        Returns:
            Entry ID if successful, None otherwise
        """
        if self.semantic_store is None:
            logger.warning("Semantic memory not available")
            return None
        
        try:
            return self.semantic_store.add_entry(content, metadata or {}, agent_id=agent_id)
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

    def get_old_knowledge(self, days: int = 30) -> list:
        """Get knowledge entries older than specified days."""
        if self.semantic_store is None:
            return []
        return self.semantic_store.get_old_entries(days)

    def summarize_knowledge(self, llm, days: int = 30) -> dict[str, Any]:
        """Summarize and compact old knowledge entries using LLM.

        Args:
            llm: LangChain LLM instance
            days: Age threshold in days (default: 30)

        Returns:
            Statistics dict with summarized_count, failed_count, skipped_count
        """
        if self.semantic_store is None:
            logger.warning("Semantic memory not available for summarization")
            return {"summarized_count": 0, "failed_count": 0, "skipped_count": 0}

        return self.semantic_store.summarize_entries(llm, days)
    
    # ==================== Artifact Management ====================
    
    def save_artifact(
        self,
        content: str,
        title: str,
        author: str = "system",
        tags: Optional[list[str]] = None,
        artifact_type: str = "text"
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
