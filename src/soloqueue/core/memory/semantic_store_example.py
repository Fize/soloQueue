"""
Semantic Store: L3 Long-term Memory with Vector Database

This module provides semantic search over historical knowledge using ChromaDB.

Design Principles:
1. Local-first: All data stored locally (no cloud dependencies)
2. Automatic indexing: Extract knowledge from completed sessions
3. Hybrid search: Combine vector similarity + metadata filtering
4. Incremental updates: Add knowledge without full re-indexing
"""

from typing import Any, TypedDict, Optional
import chromadb
from chromadb.config import Settings
from sentence_transformers import SentenceTransformer
import uuid
from datetime import datetime
from loguru import logger


class MemoryEntry(TypedDict):
    """A single knowledge fragment in semantic memory."""
    id: str
    content: str
    metadata: dict[str, Any]


class SemanticStore:
    """
    Vector-based semantic memory for long-term knowledge retrieval.
    
    Uses ChromaDB for local vector storage and sentence-transformers
    for embedding generation.
    
    Example:
        >>> store = SemanticStore("/workspace/.soloqueue/semantic")
        >>> 
        >>> # Add knowledge
        >>> store.add_entry(
        ...     content="How to fix database connection pool exhaustion: increase pool size and add connection timeout",
        ...     metadata={"type": "solution", "tags": ["database", "performance"]}
        ... )
        >>> 
        >>> # Search
        >>> results = store.search("database timeout issues", top_k=3)
        >>> for entry in results:
        ...     print(entry['content'])
    """
    
    def __init__(
        self,
        storage_path: str,
        embedding_model: str = "all-MiniLM-L6-v2",
        collection_name: str = "knowledge_base"
    ):
        """
        Initialize semantic store.
        
        Args:
            storage_path: Directory for ChromaDB storage
            embedding_model: sentence-transformers model name
            collection_name: ChromaDB collection name
        """
        self.storage_path = storage_path
        
        # Initialize ChromaDB client
        self.client = chromadb.PersistentClient(
            path=storage_path,
            settings=Settings(
                anonymized_telemetry=False,
                allow_reset=False
            )
        )
        
        # Initialize embedding model
        logger.info(f"Loading embedding model: {embedding_model}")
        self.embedding_model = SentenceTransformer(embedding_model)
        
        # Get or create collection
        self.collection = self.client.get_or_create_collection(
            name=collection_name,
            metadata={"hnsw:space": "cosine"}  # Use cosine similarity
        )
        
        logger.info(f"SemanticStore initialized with {self.collection.count()} entries")
    
    def add_entry(
        self,
        content: str,
        metadata: dict[str, Any],
        entry_id: str | None = None
    ) -> str:
        """
        Add a knowledge entry to semantic memory.
        
        Args:
            content: Text content to embed and store
            metadata: Structured metadata (type, tags, session_id, etc.)
            entry_id: Optional ID (auto-generated if None)
        
        Returns:
            Entry ID
        """
        if not entry_id:
            entry_id = str(uuid.uuid4())
        
        # Generate embedding
        embedding = self.embedding_model.encode(content).tolist()
        
        # Add timestamp if not present
        if "timestamp" not in metadata:
            metadata["timestamp"] = datetime.utcnow().isoformat()
        
        # Add to ChromaDB
        self.collection.add(
            ids=[entry_id],
            embeddings=[embedding],
            documents=[content],
            metadatas=[metadata]
        )
        
        logger.debug(f"Added entry {entry_id} to semantic store")
        return entry_id
    
    def search(
        self,
        query: str,
        top_k: int = 5,
        filter_metadata: dict[str, Any] | None = None
    ) -> list[MemoryEntry]:
        """
        Semantic search over knowledge base.
        
        Args:
            query: Natural language query
            top_k: Number of results to return
            filter_metadata: Optional metadata filter (e.g., {"type": "solution"})
        
        Returns:
            List of memory entries, ordered by relevance
        """
        # Generate query embedding
        query_embedding = self.embedding_model.encode(query).tolist()
        
        # Build ChromaDB query
        where_clause = filter_metadata if filter_metadata else None
        
        # Execute search
        results = self.collection.query(
            query_embeddings=[query_embedding],
            n_results=top_k,
            where=where_clause
        )
        
        # Format results
        entries: list[MemoryEntry] = []
        if results["ids"] and len(results["ids"]) > 0:
            for i in range(len(results["ids"][0])):
                entries.append({
                    "id": results["ids"][0][i],
                    "content": results["documents"][0][i],
                    "metadata": results["metadatas"][0][i]
                })
        
        logger.debug(f"Search query '{query}' returned {len(entries)} results")
        return entries
    
    def delete_entry(self, entry_id: str):
        """Delete an entry from semantic memory."""
        self.collection.delete(ids=[entry_id])
        logger.debug(f"Deleted entry {entry_id}")
    
    def get_stats(self) -> dict[str, Any]:
        """Get semantic store statistics."""
        return {
            "total_entries": self.collection.count(),
            "model": self.embedding_model.get_sentence_embedding_dimension(),
            "storage_path": self.storage_path
        }


# Example usage for testing
if __name__ == "__main__":
    import tempfile
    
    # Create temporary store
    with tempfile.TemporaryDirectory() as tmpdir:
        store = SemanticStore(tmpdir)
        
        # Add some knowledge
        store.add_entry(
            content="To fix SQLite database locked error, enable WAL mode with PRAGMA journal_mode=WAL",
            metadata={"type": "solution", "tags": ["sqlite", "concurrency"]}
        )
        
        store.add_entry(
            content="Memory leak in Python caused by circular references. Solution: use weakref or explicit del",
            metadata={"type": "error", "tags": ["python", "memory"]}
        )
        
        # Search
        results = store.search("database locking issues", top_k=2)
        
        print(f"\n{'='*60}")
        print("SEMANTIC SEARCH DEMO")
        print(f"{'='*60}\n")
        
        for i, entry in enumerate(results, 1):
            print(f"Result {i}:")
            print(f"  Content: {entry['content']}")
            print(f"  Tags: {entry['metadata'].get('tags', [])}")
            print()
