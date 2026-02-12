"""
Tests for StateManager - Global state database.

Tests cover:
- Task queue operations (enqueue, claim, update)
- Concurrent task claiming
- Agent registry (register, heartbeat, crash detection)
- Coordination locks (acquire, release, expiry)
"""

import pytest
import time
from pathlib import Path
from datetime import datetime, timedelta
from multiprocessing import Pool

from soloqueue.core.state import StateManager


@pytest.fixture
def state_manager(tmp_path):
    """Create a fresh StateManager for each test."""
    return StateManager(str(tmp_path))


# === Phase 1: Core Infrastructure Tests ===

def test_db_initialization(tmp_path):
    """Verify database is created with correct schema."""
    sm = StateManager(str(tmp_path))
    
    # Check DB file exists
    db_path = tmp_path / ".soloqueue" / "state.db"
    assert db_path.exists()
    
    # Verify tables exist
    with sm._connect() as conn:
        cursor = conn.execute("""
            SELECT name FROM sqlite_master 
            WHERE type='table' 
            ORDER BY name
        """)
        tables = [row['name'] for row in cursor.fetchall()]
    
    assert 'tasks' in tables
    assert 'agents' in tables
    assert 'coordination_locks' in tables


def test_wal_mode_enabled(state_manager):
    """Verify WAL mode is enabled."""
    with state_manager._connect() as conn:
        cursor = conn.execute("PRAGMA journal_mode")
        mode = cursor.fetchone()[0]
    
    assert mode.lower() == 'wal'


# === Phase 2: Task Queue Tests ===

def test_enqueue_task(state_manager):
    """Basic task enqueue."""
    task_id = state_manager.enqueue_task(
        instruction="Test task",
        group_id="default",
        priority=5
    )
    
    assert task_id is not None
    
    # Verify task in DB
    task = state_manager.get_task(task_id)
    assert task['instruction'] == "Test task"
    assert task['status'] == 'pending'
    assert task['priority'] == 5


def test_claim_next_task(state_manager):
    """Agent claims next task from queue."""
    # Enqueue task
    task_id = state_manager.enqueue_task("Test task", "default")
    
    # Register agent
    state_manager.register_agent("agent_1", "default", [])
    
    # Claim task
    task = state_manager.claim_next_task("agent_1", "default")
    
    assert task is not None
    assert task['task_id'] == task_id
    assert task['status'] == 'pending'  # Status before update
    
    # Verify task is now running
    updated_task = state_manager.get_task(task_id)
    assert updated_task['status'] == 'running'
    assert updated_task['assigned_to'] == 'agent_1'


def test_claim_empty_queue(state_manager):
    """Claiming from empty queue returns None."""
    state_manager.register_agent("agent_1", "default", [])
    
    task = state_manager.claim_next_task("agent_1", "default")
    
    assert task is None


def test_priority_ordering(state_manager):
    """Tasks are claimed in priority order."""
    # Enqueue tasks with different priorities
    low_id = state_manager.enqueue_task("Low priority", "default", priority=1)
    high_id = state_manager.enqueue_task("High priority", "default", priority=10)
    med_id = state_manager.enqueue_task("Medium priority", "default", priority=5)
    
    state_manager.register_agent("agent_1", "default", [])
    
    # Should claim highest priority first
    task1 = state_manager.claim_next_task("agent_1", "default")
    assert task1['task_id'] == high_id
    
    # Then medium
    task2 = state_manager.claim_next_task("agent_1", "default")
    assert task2['task_id'] == med_id
    
    # Then low
    task3 = state_manager.claim_next_task("agent_1", "default")
    assert task3['task_id'] == low_id


def _claim_worker_helper(args):
    """Helper function for multiprocessing (must be at module level for pickle)."""
    tmp_path, agent_id = args
    sm_worker = StateManager(str(tmp_path))
    return sm_worker.claim_next_task(agent_id, "default")


def test_concurrent_claim(tmp_path):
    """Two agents cannot claim the same task."""
    sm = StateManager(str(tmp_path))
    
    # Enqueue single task
    task_id = sm.enqueue_task("Test task", "default")
    
    # Register agents
    sm.register_agent("agent_1", "default", [])
    sm.register_agent("agent_2", "default", [])
    
    # Try to claim simultaneously
    with Pool(2) as pool:
        results = pool.map(_claim_worker_helper, [
            (tmp_path, "agent_1"),
            (tmp_path, "agent_2")
        ])
    
    # Only one should succeed
    successful_claims = [r for r in results if r is not None]
    assert len(successful_claims) == 1
    assert successful_claims[0]['task_id'] == task_id


def test_update_task_status(state_manager):
    """Update task status to complete."""
    task_id = state_manager.enqueue_task("Test", "default")
    
    state_manager.update_task_status(task_id, 'complete', result_artifact_id='art_123')
    
    task = state_manager.get_task(task_id)
    assert task['status'] == 'complete'
    assert task['result_artifact_id'] == 'art_123'
    assert task['completed_at'] is not None


def test_list_tasks(state_manager):
    """Query tasks with filters."""
    # Create tasks
    state_manager.enqueue_task("Task 1", "group_a", priority=5)
    state_manager.enqueue_task("Task 2", "group_b", priority=3)
    state_manager.enqueue_task("Task 3", "group_a", priority=7)
    
    # Filter by group
    group_a_tasks = state_manager.list_tasks(group_id="group_a")
    assert len(group_a_tasks) == 2
    
    # Filter by status
    pending_tasks = state_manager.list_tasks(status="pending")
    assert len(pending_tasks) == 3
    
    # Limit results
    limited = state_manager.list_tasks(limit=2)
    assert len(limited) == 2


# === Phase 3: Agent Registry Tests ===

def test_register_agent(state_manager):
    """Register agent in global registry."""
    state_manager.register_agent("agent_1", "default", ["code", "research"])
    
    agent = state_manager.get_agent_status("agent_1")
    assert agent is not None
    assert agent['status'] == 'idle'
    assert agent['group_id'] == 'default'


def test_heartbeat_update(state_manager):
    """Update agent heartbeat."""
    state_manager.register_agent("agent_1", "default", [])
    
    initial = state_manager.get_agent_status("agent_1")
    initial_heartbeat = initial['last_heartbeat']
    
    time.sleep(0.1)
    
    state_manager.update_heartbeat("agent_1")
    
    updated = state_manager.get_agent_status("agent_1")
    assert updated['last_heartbeat'] > initial_heartbeat


def test_mark_agent_busy_idle(state_manager):
    """Mark agent as busy and idle."""
    state_manager.register_agent("agent_1", "default", [])
    
    # Mark busy
    state_manager.mark_agent_busy("agent_1", "task_123")
    agent = state_manager.get_agent_status("agent_1")
    assert agent['status'] == 'busy'
    assert agent['current_task_id'] == 'task_123'
    
    # Mark idle
    state_manager.mark_agent_idle("agent_1")
    agent = state_manager.get_agent_status("agent_1")
    assert agent['status'] == 'idle'
    assert agent['current_task_id'] is None


def test_crash_detection(state_manager):
    """Detect agents with stale heartbeats."""
    state_manager.register_agent("agent_1", "default", [])
    
    # Manually set old heartbeat
    with state_manager._connect() as conn:
        old_time = (datetime.now() - timedelta(minutes=10)).isoformat()
        conn.execute("""
            UPDATE agents SET last_heartbeat = ? WHERE agent_id = ?
        """, (old_time, "agent_1"))
        conn.commit()
    
    # Detect crash (5 minute timeout)
    crashed = state_manager.detect_crashed_agents(timeout_seconds=300)
    
    assert "agent_1" in crashed
    
    # Verify status updated
    agent = state_manager.get_agent_status("agent_1")
    assert agent['status'] == 'crashed'


# === Phase 4: Coordination Tests ===

def test_acquire_release_lock(state_manager):
    """Basic lock acquire and release."""
    state_manager.register_agent("agent_1", "default", [])
    
    # Acquire lock
    acquired = state_manager.acquire_lock("test_lock", "agent_1", ttl_seconds=30)
    assert acquired is True
    
    # Try to acquire again (should fail)
    acquired_again = state_manager.acquire_lock("test_lock", "agent_1", ttl_seconds=30)
    assert acquired_again is False
    
    # Release lock
    state_manager.release_lock("test_lock", "agent_1")
    
    # Now can acquire
    acquired_after_release = state_manager.acquire_lock("test_lock", "agent_1", ttl_seconds=30)
    assert acquired_after_release is True


def test_lock_contention(state_manager):
    """Two agents cannot hold same lock."""
    state_manager.register_agent("agent_1", "default", [])
    state_manager.register_agent("agent_2", "default", [])
    
    # Agent 1 acquires
    lock1 = state_manager.acquire_lock("resource", "agent_1")
    assert lock1 is True
    
    # Agent 2 tries (should fail)
    lock2 = state_manager.acquire_lock("resource", "agent_2")
    assert lock2 is False


def test_lock_expiry_steal(state_manager):
    """Expired locks can be stolen."""
    state_manager.register_agent("agent_1", "default", [])
    state_manager.register_agent("agent_2", "default", [])
    
    # Agent 1 acquires with short TTL
    state_manager.acquire_lock("resource", "agent_1", ttl_seconds=1)
    
    # Wait for expiry
    time.sleep(1.5)
    
    # Agent 2 can steal
    stolen = state_manager.acquire_lock("resource", "agent_2", ttl_seconds=30)
    assert stolen is True


def test_cleanup_expired_locks(state_manager):
    """Cleanup removes expired locks."""
    state_manager.register_agent("agent_1", "default", [])
    
    # Create expired lock manually
    with state_manager._connect() as conn:
        old_time = (datetime.now() - timedelta(minutes=1)).isoformat()
        conn.execute("""
            INSERT INTO coordination_locks (lock_name, held_by, acquired_at, expires_at)
            VALUES ('old_lock', 'agent_1', ?, ?)
        """, (old_time, old_time))
        conn.commit()
    
    # Cleanup
    state_manager.cleanup_expired_locks()
    
    # Verify lock removed
    with state_manager._connect() as conn:
        cursor = conn.execute("""
            SELECT * FROM coordination_locks WHERE lock_name = 'old_lock'
        """)
        assert cursor.fetchone() is None


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
