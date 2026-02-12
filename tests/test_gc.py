"""
Test suite for GarbageCollector.
"""

import pytest
import tempfile
import shutil
import time
from pathlib import Path
from datetime import datetime, timedelta
from soloqueue.core.memory.artifact_store import ArtifactStore
from soloqueue.core.memory.gc import GarbageCollector

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

@pytest.fixture
def gc(temp_workspace, artifact_store):
    """Create a GarbageCollector instance (depends on artifact_store to ensure DB exists)."""
    return GarbageCollector(temp_workspace, retention_days=0)  # 0 days for testing

def test_phase1_metadata_pruning(artifact_store, gc):
    """Test that Phase 1 deletes expired ephemeral artifacts."""
    # Create ephemeral and persistent artifacts
    artifact_store.save_artifact("Ephemeral Log", "Debug Log", tags=["sys:ephemeral"])
    artifact_store.save_artifact("Persistent Code", "Report", tags=["user:persistent"])
    
    # Wait a moment to ensure created_at is in the past
    time.sleep(0.1)
    
    # Run GC (retention=0 means delete immediately)
    stats = gc.run_once(skip_orphan_scan=True)
    
    # Verify ephemeral was deleted
    assert stats["phase1_deleted"] == 1
    
    # Verify persistent still exists
    remaining = artifact_store.list_artifacts()
    assert len(remaining) == 1
    assert "user:persistent" in remaining[0]["tags"]

def test_phase2_orphan_scanning(temp_workspace, artifact_store, gc):
    """Test that Phase 2 removes orphaned blobs."""
    # Create an artifact
    content = "Test content for orphan"
    artifact_id = artifact_store.save_artifact(content, "Test")
    
    # Get the blob path
    artifact = artifact_store.get_artifact(artifact_id)
    blob_path = Path(temp_workspace) / artifact["metadata"]["path"]
    
    # Manually delete metadata (simulating orphan scenario)
    artifact_store.delete_artifact(artifact_id)
    
    # Blob should still exist
    assert blob_path.exists()
    
    # Run Phase 2
    stats = gc.run_once(skip_orphan_scan=False)
    
    # Blob should be deleted
    assert stats["phase2_deleted"] == 1
    assert not blob_path.exists()

def test_process_locking(gc):
    """Test that fcntl prevents concurrent GC execution."""
    # This is a simplified test - full concurrency testing requires multiprocessing
    
    # First run should succeed
    stats1 = gc.run_once(skip_orphan_scan=True)
    assert stats1["skipped"] is False
    
    # Immediate second run should succeed (lock was released)
    stats2 = gc.run_once(skip_orphan_scan=True)
    assert stats2["skipped"] is False

def test_cooldown_mechanism(gc):
    """Test that GC respects cooldown period."""
    # First run
    gc.run_once(skip_orphan_scan=True)
    
    # Should not run again immediately (cooldown = 24h by default)
    assert gc.should_run(cooldown_hours=24) is False
    
    # But with 0 cooldown, should run
    assert gc.should_run(cooldown_hours=0) is True

def test_retention_window(artifact_store, temp_workspace):
    """Test that retention window is respected."""
    gc_strict = GarbageCollector(temp_workspace, retention_days=7)
    
    # Create ephemeral artifact
    artifact_store.save_artifact("Recent Log", "Log", tags=["sys:ephemeral"])
    
    # Run GC with 7-day retention
    stats = gc_strict.run_once(skip_orphan_scan=True)
    
    # Should NOT delete (artifact is fresh)
    assert stats["phase1_deleted"] == 0
