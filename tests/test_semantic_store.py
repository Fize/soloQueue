"""
Tests for SemanticStore - Semantic memory with vector database
"""

import pytest
import tempfile
import shutil
import os
from pathlib import Path
from unittest.mock import patch, MagicMock


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
         patch('soloqueue.core.memory.semantic_store.embed_text') as mock_embed:
        
        # Mock embed_text to return deterministic embeddings
        def mock_embed_fn(texts):
            if isinstance(texts, str):
                texts = [texts]
            # Return simple embeddings based on text hash
            return [[hash(text) % 100 / 100.0] * 1536 for text in texts]
        
        mock_embed.side_effect = mock_embed_fn
        yield mock_embed


def test_semantic_store_initialization(temp_storage, mock_embedding):
    """Test that SemanticStore initializes correctly."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    store = SemanticStore(temp_storage)
    
    assert store.storage_path == Path(temp_storage)
    assert store.collection_name == "knowledge_base"
    assert store.count() == 0


def test_semantic_store_requires_embedding():
    """Test that SemanticStore raises error if embedding not available."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    with patch('soloqueue.core.memory.semantic_store.is_embedding_available', return_value=False):
        with pytest.raises(RuntimeError, match="Semantic memory requires embedding"):
            SemanticStore("/tmp/test")


def test_add_entry(temp_storage, mock_embedding):
    """Test adding a single entry."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    store = SemanticStore(temp_storage)
    
    entry_id = store.add_entry(
        content="Test knowledge entry",
        metadata={"type": "test", "topic": "testing"}
    )
    
    assert entry_id is not None
    assert entry_id.startswith("entry_")
    assert store.count() == 1


def test_add_entry_with_custom_id(temp_storage, mock_embedding):
    """Test adding entry with custom ID."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    store = SemanticStore(temp_storage)
    
    custom_id = "custom_test_123"
    entry_id = store.add_entry(
        content="Test content",
        metadata={"type": "test"},
        entry_id=custom_id
    )
    
    assert entry_id == custom_id
    assert store.count() == 1


def test_add_batch(temp_storage, mock_embedding):
    """Test adding multiple entries in batch."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    store = SemanticStore(temp_storage)
    
    entries = [
        ("Entry 1", {"type": "test", "number": 1}),
        ("Entry 2", {"type": "test", "number": 2}),
        ("Entry 3", {"type": "test", "number": 3})
    ]
    
    entry_ids = store.add_batch(entries)
    
    assert len(entry_ids) == 3
    assert all(eid.startswith("entry_") for eid in entry_ids)
    assert store.count() == 3


def test_search(temp_storage, mock_embedding):
    """Test semantic search."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    store = SemanticStore(temp_storage)
    
    # Add entries
    store.add_entry(
        "JWT authentication requires secret keys",
        {"type": "lesson", "topic": "auth"}
    )
    store.add_entry(
        "Database connection pooling improves performance",
        {"type": "lesson", "topic": "database"}
    )
    
    # Search
    results = store.search("authentication security", top_k=2)
    
    assert len(results) <= 2
    assert all(hasattr(r, 'content') for r in results)
    assert all(hasattr(r, 'score') for r in results)
    assert all(hasattr(r, 'metadata') for r in results)
    
    # Scores should be between 0 and 1 (allow small floating point errors)
    for result in results:
        assert -0.01 <= result.score <= 1.01


def test_search_with_filter(temp_storage, mock_embedding):
    """Test search with metadata filtering."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    store = SemanticStore(temp_storage)
    
    # Add entries with different types
    store.add_entry("Auth lesson", {"type": "lesson", "topic": "auth"})
    store.add_entry("Auth error", {"type": "error", "topic": "auth"})
    store.add_entry("DB lesson", {"type": "lesson", "topic": "database"})
    
    # Search only for lessons
    results = store.search(
        "authentication",
        top_k=10,
        filter_metadata={"type": "lesson"}
    )
    
    # Should only return lesson entries
    assert all(r.metadata["type"] == "lesson" for r in results)


def test_get_by_id(temp_storage, mock_embedding):
    """Test retrieving entry by ID."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    store = SemanticStore(temp_storage)
    
    content = "Test content for retrieval"
    entry_id = store.add_entry(content, {"type": "test"})
    
    # Retrieve by ID
    entry = store.get_by_id(entry_id)
    
    assert entry is not None
    assert entry.id == entry_id
    assert entry.content == content
    assert entry.metadata["type"] == "test"
    assert entry.score == 1.0  # Exact match


def test_get_by_id_not_found(temp_storage, mock_embedding):
    """Test retrieving non-existent entry."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    store = SemanticStore(temp_storage)
    
    entry = store.get_by_id("nonexistent_id")
    assert entry is None


def test_delete(temp_storage, mock_embedding):
    """Test deleting an entry."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    store = SemanticStore(temp_storage)
    
    entry_id = store.add_entry("Test content", {"type": "test"})
    assert store.count() == 1
    
    # Delete
    deleted = store.delete(entry_id)
    assert deleted is True
    assert store.count() == 0


def test_delete_not_found(temp_storage, mock_embedding):
    """Test deleting non-existent entry."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    store = SemanticStore(temp_storage)
    
    # Should not raise error
    deleted = store.delete("nonexistent_id")
    # Behavior may vary, just ensure it doesn't crash
    assert isinstance(deleted, bool)


def test_count(temp_storage, mock_embedding):
    """Test counting entries."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    store = SemanticStore(temp_storage)
    
    assert store.count() == 0
    
    store.add_entry("Entry 1", {"type": "test"})
    assert store.count() == 1
    
    store.add_entry("Entry 2", {"type": "test"})
    assert store.count() == 2


def test_get_stats(temp_storage, mock_embedding):
    """Test getting store statistics."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    store = SemanticStore(temp_storage)
    store.add_entry("Test", {"type": "test"})
    
    stats = store.get_stats()
    
    assert "total_entries" in stats
    assert stats["total_entries"] == 1
    assert "dimension" in stats
    assert stats["dimension"] == 1536
    assert "collection_name" in stats
    assert "storage_path" in stats


def test_reset(temp_storage, mock_embedding):
    """Test resetting the store."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    store = SemanticStore(temp_storage)
    
    # Add some entries
    store.add_entry("Entry 1", {"type": "test"})
    store.add_entry("Entry 2", {"type": "test"})
    assert store.count() == 2
    
    # Reset
    store.reset()
    assert store.count() == 0


def test_metadata_timestamp_added(temp_storage, mock_embedding):
    """Test that timestamp is automatically added to metadata."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    store = SemanticStore(temp_storage)
    
    entry_id = store.add_entry("Test", {"type": "test"})
    entry = store.get_by_id(entry_id)
    
    assert "timestamp" in entry.metadata
    assert "content_length" in entry.metadata
    assert entry.metadata["content_length"] == 4  # len("Test")


def test_persistence(temp_storage, mock_embedding):
    """Test that data persists across store instances."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    # Create store and add entry
    store1 = SemanticStore(temp_storage)
    entry_id = store1.add_entry("Persistent content", {"type": "test"})
    assert store1.count() == 1
    
    # Create new store instance with same path
    store2 = SemanticStore(temp_storage)
    assert store2.count() == 1
    
    # Retrieve same entry
    entry = store2.get_by_id(entry_id)
    assert entry is not None
    assert entry.content == "Persistent content"


def test_empty_batch(temp_storage, mock_embedding):
    """Test adding empty batch."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    store = SemanticStore(temp_storage)
    entry_ids = store.add_batch([])
    
    assert entry_ids == []
    assert store.count() == 0


def test_search_empty_store(temp_storage, mock_embedding):
    """Test searching in empty store."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    store = SemanticStore(temp_storage)
    results = store.search("anything", top_k=5)
    
    assert results == []


@pytest.mark.parametrize("top_k", [1, 3, 10])
def test_search_respects_top_k(temp_storage, mock_embedding, top_k):
    """Test that search respects top_k parameter."""
    from soloqueue.core.memory.semantic_store import SemanticStore
    
    store = SemanticStore(temp_storage)
    
    # Add more entries than top_k
    for i in range(15):
        store.add_entry(f"Entry {i}", {"type": "test", "number": i})
    
    results = store.search("test", top_k=top_k)
    
    # Should return at most top_k results
    assert len(results) <= top_k


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
