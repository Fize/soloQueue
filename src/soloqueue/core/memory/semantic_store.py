"""
Semantic Store - L3 Long-term Semantic Memory using Vector Database

Provides semantic search capabilities for knowledge retrieval.
Uses ChromaDB for vector storage and the global embedding model for text encoding.
"""

from typing import Optional, Any
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path

from loguru import logger
import chromadb
from chromadb.config import Settings

from soloqueue.core.embedding import embed_text, is_embedding_available, get_embedding_dimension


@dataclass
class MemoryEntry:
    """A single semantic memory entry."""
    id: str
    content: str
    score: float  # Similarity score (0-1, higher is better)
    metadata: dict[str, Any]
    timestamp: str


class SemanticStore:
    """
    Semantic memory store using ChromaDB vector database.
    
    Enables semantic search over past knowledge, learnings, and experiences.
    Automatically uses the global embedding model configured in the system.
    
    Example:
        store = SemanticStore("/workspace/.soloqueue/semantic")
        
        # Add knowledge
        store.add_entry(
            content="JWT authentication requires secret key in .env file",
            metadata={"type": "lesson", "topic": "auth", "outcome": "success"}
        )
        
        # Search
        results = store.search("how to setup authentication", top_k=5)
        for result in results:
            print(f"Score: {result.score:.3f}")
            print(f"Content: {result.content}")
    """
    
    def __init__(
        self,
        storage_path: str,
        collection_name: str = "knowledge_base"
    ):
        """
        Initialize semantic store.
        
        Args:
            storage_path: Path to ChromaDB persistent storage
            collection_name: Name of the collection (default: "knowledge_base")
        
        Raises:
            RuntimeError: If embedding is not available
        """
        if not is_embedding_available():
            raise RuntimeError(
                "Semantic memory requires embedding model to be configured. "
                "Please configure embedding in config/system.yaml or environment variables."
            )
        
        self.storage_path = Path(storage_path)
        self.collection_name = collection_name
        
        # Create storage directory
        self.storage_path.mkdir(parents=True, exist_ok=True)
        
        # Initialize ChromaDB client
        self.client = chromadb.PersistentClient(
            path=str(self.storage_path),
            settings=Settings(
                anonymized_telemetry=False,
                allow_reset=True
            )
        )
        
        # Get or create collection
        dimension = get_embedding_dimension()
        self.collection = self.client.get_or_create_collection(
            name=collection_name,
            metadata={
                "hnsw:space": "cosine",  # Cosine similarity
                "dimension": dimension
            }
        )
        
        logger.info(
            f"Initialized SemanticStore at {storage_path} "
            f"(dimension={dimension}, collection={collection_name})"
        )
    
    def add_entry(
        self,
        content: str,
        metadata: dict[str, Any],
        entry_id: Optional[str] = None
    ) -> str:
        """
        Add a knowledge entry to semantic memory.
        
        Args:
            content: Text content to store
            metadata: Associated metadata (type, topic, outcome, etc.)
            entry_id: Optional custom ID (auto-generated if not provided)
        
        Returns:
            Entry ID
        """
        # Generate ID if not provided
        if entry_id is None:
            timestamp = datetime.now().strftime("%Y%m%d_%H%M%S_%f")
            entry_id = f"entry_{timestamp}"
        
        # Add timestamp to metadata
        metadata_with_timestamp = {
            **metadata,
            "timestamp": datetime.now().isoformat(),
            "content_length": len(content)
        }
        
        # Generate embedding
        embedding = embed_text(content)
        if embedding is None:
            raise RuntimeError("Failed to generate embedding")
        
        # Add to collection
        self.collection.add(
            ids=[entry_id],
            embeddings=[embedding[0]],  # Single embedding
            documents=[content],
            metadatas=[metadata_with_timestamp]
        )
        
        logger.debug(f"Added entry {entry_id} to semantic store")
        return entry_id
    
    def add_batch(
        self,
        entries: list[tuple[str, dict[str, Any]]]
    ) -> list[str]:
        """
        Add multiple entries in batch (more efficient).
        
        Args:
            entries: List of (content, metadata) tuples
        
        Returns:
            List of entry IDs
        """
        if not entries:
            return []
        
        # Generate IDs
        timestamp_base = datetime.now().strftime("%Y%m%d_%H%M%S")
        entry_ids = [f"entry_{timestamp_base}_{i:04d}" for i in range(len(entries))]
        
        # Prepare data
        contents = [content for content, _ in entries]
        metadatas = [
            {
                **meta,
                "timestamp": datetime.now().isoformat(),
                "content_length": len(content)
            }
            for content, meta in entries
        ]
        
        # Generate embeddings (batch)
        embeddings = embed_text(contents)
        if embeddings is None:
            raise RuntimeError("Failed to generate embeddings")
        
        # Add to collection
        self.collection.add(
            ids=entry_ids,
            embeddings=embeddings,
            documents=contents,
            metadatas=metadatas
        )
        
        logger.debug(f"Added {len(entries)} entries to semantic store")
        return entry_ids
    
    def search(
        self,
        query: str,
        top_k: int = 5,
        filter_metadata: Optional[dict[str, Any]] = None
    ) -> list[MemoryEntry]:
        """
        Search for similar knowledge entries.
        
        Args:
            query: Search query text
            top_k: Number of results to return
            filter_metadata: Optional metadata filters (e.g., {"type": "lesson"})
        
        Returns:
            List of matching MemoryEntry objects, sorted by relevance
        """
        # Generate query embedding
        query_embedding = embed_text(query)
        if query_embedding is None:
            logger.warning("Failed to generate query embedding")
            return []
        
        # Search collection
        results = self.collection.query(
            query_embeddings=[query_embedding[0]],
            n_results=top_k,
            where=filter_metadata
        )
        
        # Parse results
        entries = []
        if results['ids'] and results['ids'][0]:
            for i, entry_id in enumerate(results['ids'][0]):
                # ChromaDB returns distances, convert to similarity scores
                # For cosine distance: similarity = 1 - distance
                distance = results['distances'][0][i]
                score = 1.0 - distance
                
                entries.append(MemoryEntry(
                    id=entry_id,
                    content=results['documents'][0][i],
                    score=score,
                    metadata=results['metadatas'][0][i],
                    timestamp=results['metadatas'][0][i].get('timestamp', '')
                ))
        
        logger.debug(f"Search returned {len(entries)} results for query: {query[:50]}...")
        return entries
    
    def get_by_id(self, entry_id: str) -> Optional[MemoryEntry]:
        """
        Retrieve a specific entry by ID.
        
        Args:
            entry_id: Entry ID to retrieve
        
        Returns:
            MemoryEntry if found, None otherwise
        """
        results = self.collection.get(ids=[entry_id])
        
        if results['ids']:
            return MemoryEntry(
                id=results['ids'][0],
                content=results['documents'][0],
                score=1.0,  # Exact match
                metadata=results['metadatas'][0],
                timestamp=results['metadatas'][0].get('timestamp', '')
            )
        return None
    
    def delete(self, entry_id: str) -> bool:
        """
        Delete an entry by ID.
        
        Args:
            entry_id: Entry ID to delete
        
        Returns:
            True if deleted, False if not found
        """
        try:
            self.collection.delete(ids=[entry_id])
            logger.debug(f"Deleted entry {entry_id}")
            return True
        except Exception as e:
            logger.warning(f"Failed to delete entry {entry_id}: {e}")
            return False
    
    def count(self) -> int:
        """Get total number of entries in the store."""
        return self.collection.count()
    
    def get_stats(self) -> dict[str, Any]:
        """
        Get statistics about the semantic store.
        
        Returns:
            Dictionary with stats (count, dimension, etc.)
        """
        return {
            "total_entries": self.count(),
            "dimension": get_embedding_dimension(),
            "collection_name": self.collection_name,
            "storage_path": str(self.storage_path)
        }
    
    def reset(self):
        """Delete all entries (DANGEROUS - use with caution)."""
        self.client.delete_collection(self.collection_name)
        self.collection = self.client.create_collection(
            name=self.collection_name,
            metadata={
                "hnsw:space": "cosine",
                "dimension": get_embedding_dimension()
            }
        )
        logger.warning(f"Reset semantic store collection: {self.collection_name}")


# Convenience function for quick testing
def create_semantic_store(storage_path: Optional[str] = None) -> SemanticStore:
    """
    Create a semantic store with default settings.
    
    Args:
        storage_path: Optional custom path (defaults to /tmp/semantic_test)
    
    Returns:
        SemanticStore instance
    """
    if storage_path is None:
        storage_path = "/tmp/semantic_test"
    
    return SemanticStore(storage_path)


if __name__ == "__main__":
    # Example usage
    print("=" * 60)
    print("Semantic Store Example")
    print("=" * 60)
    print()
    
    # Check if embedding is available
    if not is_embedding_available():
        print("❌ Embedding not configured!")
        print("Set SOLOQUEUE_EMBEDDING_ENABLED=true and configure a provider")
        exit(1)
    
    # Create store
    store = create_semantic_store()
    
    # Add some knowledge
    print("Adding knowledge entries...")
    store.add_entry(
        content="JWT authentication requires a secret key stored in environment variables",
        metadata={"type": "lesson", "topic": "authentication", "outcome": "success"}
    )
    
    store.add_entry(
        content="Database connection pools should be limited to avoid resource exhaustion",
        metadata={"type": "lesson", "topic": "database", "outcome": "success"}
    )
    
    store.add_entry(
        content="Always validate user input to prevent SQL injection attacks",
        metadata={"type": "lesson", "topic": "security", "outcome": "success"}
    )
    
    print(f"✅ Added 3 entries. Total: {store.count()}")
    print()
    
    # Search
    print("Searching for 'how to secure authentication'...")
    results = store.search("how to secure authentication", top_k=2)
    
    for i, result in enumerate(results, 1):
        print(f"\n{i}. Score: {result.score:.3f}")
        print(f"   Content: {result.content}")
        print(f"   Topic: {result.metadata.get('topic')}")
    
    print()
    print("=" * 60)
    print("Stats:", store.get_stats())
