"""
Test suite for SQLite-based ArtifactStore.
"""

import pytest
import tempfile
import shutil
from pathlib import Path
from soloqueue.core.memory.artifact_store import ArtifactStore

@pytest.fixture
def temp_workspace():
    """Create a temporary workspace directory."""
    temp_dir = tempfile.mkdtemp()
    yield temp_dir
    shutil.rmtree(temp_dir)

@pytest.fixture
def artifact_store(temp_workspace):
    """Create an ArtifactStore instance."""
    return ArtifactStore(temp_workspace)

def test_save_and_retrieve_artifact(artifact_store):
    """Test basic save and retrieve operations."""
    content = "This is a test artifact."
    artifact_id = artifact_store.save_artifact(
        content=content,
        title="Test Artifact",
        author="test_user",
        tags=["test"]
    )
    
    # Verify ID is a string (auto-increment int)
    assert artifact_id.isdigit()
    
    # Retrieve
    result = artifact_store.get_artifact(artifact_id)
    assert result is not None
    assert result["content"] == content
    assert result["metadata"]["title"] == "Test Artifact"
    assert "test" in result["metadata"]["tags"]

def test_content_deduplication(artifact_store):
    """Test that identical content produces same hash and reuses blob."""
    content = "Identical content"
    
    id1 = artifact_store.save_artifact(content, "First", tags=["v1"])
    id2 = artifact_store.save_artifact(content, "Second", tags=["v2"])
    
    # Different IDs (different metadata entries)
    assert id1 != id2
    
    # But both point to same content hash
    art1 = artifact_store.get_artifact(id1)
    art2 = artifact_store.get_artifact(id2)
    assert art1["metadata"]["content_hash"] == art2["metadata"]["content_hash"]
    assert art1["metadata"]["title"] == "First"
    assert art2["metadata"]["title"] == "Second"


def test_ephemeral_tag_filtering(artifact_store):
    """Test filtering by sys:ephemeral tag."""
    artifact_store.save_artifact("Log 1", "Debug Log", tags=["sys:ephemeral"])
    artifact_store.save_artifact("Code 1", "Report", tags=["user:persistent"])
    
    ephemeral = artifact_store.list_artifacts(tag="sys:ephemeral")
    assert len(ephemeral) == 1
    assert "sys:ephemeral" in ephemeral[0]["tags"]

def test_group_scoping(artifact_store):
    """Test group-based isolation."""
    artifact_store.save_artifact("Data", "Group A", group_id="groupA")
    artifact_store.save_artifact("Data", "Group B", group_id="groupB")
    
    group_a_artifacts = artifact_store.list_artifacts(group_id="groupA")
    assert len(group_a_artifacts) == 1
    assert group_a_artifacts[0]["group_id"] == "groupA"

def test_delete_artifact(artifact_store):
    """Test metadata deletion."""
    artifact_id = artifact_store.save_artifact("Data", "To Delete")
    
    deleted = artifact_store.delete_artifact(artifact_id)
    assert deleted is True
    
    # Should not be retrievable
    result = artifact_store.get_artifact(artifact_id)
    assert result is None

def test_concurrent_reads(artifact_store):
    """Test that SQLite WAL mode allows concurrent operations."""
    # This is a basic test; real concurrency testing requires threading
    artifact_id = artifact_store.save_artifact("Concurrent", "Test")
    
    # Simulate multiple reads
    for _ in range(10):
        result = artifact_store.get_artifact(artifact_id)
        assert result is not None
