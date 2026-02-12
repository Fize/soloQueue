"""
StateManager - Global state database for multi-agent orchestration.

Provides:
- Persistent task queue with priority scheduling
- Agent lifecycle tracking and crash detection
- Coordination primitives (distributed locks)
"""

from pathlib import Path
from typing import Any
import sqlite3
import json
from datetime import datetime, timedelta
import uuid

from soloqueue.core.logger import logger


class StateManager:
    """
    Global state database manager for orchestration.
    
    Responsibilities:
    - Task queue operations (enqueue, dequeue, status updates)
    - Agent registry management
    - Coordination primitives (locks, heartbeats)
    """
    
    def __init__(self, workspace_root: str):
        self.workspace_root = Path(workspace_root)
        self.db_path = self.workspace_root / ".soloqueue" / "state.db"
        self.db_path.parent.mkdir(parents=True, exist_ok=True)
        self._init_db()
        
        logger.info(f"StateManager initialized: {self.db_path}")
    
    def _connect(self) -> sqlite3.Connection:
        """Create connection with WAL mode enabled."""
        conn = sqlite3.connect(str(self.db_path))
        conn.row_factory = sqlite3.Row
        conn.execute("PRAGMA journal_mode=WAL")
        conn.execute("PRAGMA foreign_keys=ON")
        return conn
    
    def _init_db(self):
        """Initialize database schema with tables and indexes."""
        with self._connect() as conn:
            # Tasks table
            conn.execute("""
                CREATE TABLE IF NOT EXISTS tasks (
                    task_id TEXT PRIMARY KEY,
                    group_id TEXT NOT NULL,
                    parent_task_id TEXT,
                    assigned_to TEXT,
                    status TEXT NOT NULL,
                    priority INTEGER DEFAULT 5,
                    
                    instruction TEXT NOT NULL,
                    context_artifact_id TEXT,
                    
                    created_at TEXT NOT NULL,
                    started_at TEXT,
                    completed_at TEXT,
                    deadline TEXT,
                    
                    result_type TEXT,
                    result_artifact_id TEXT,
                    error_msg TEXT,
                    
                    tags TEXT,
                    dependencies TEXT,
                    
                    FOREIGN KEY (parent_task_id) REFERENCES tasks(task_id)
                )
            """)
            
            # Agents table
            conn.execute("""
                CREATE TABLE IF NOT EXISTS agents (
                    agent_id TEXT PRIMARY KEY,
                    group_id TEXT NOT NULL,
                    status TEXT NOT NULL,
                    current_task_id TEXT,
                    
                    registered_at TEXT NOT NULL,
                    last_heartbeat TEXT NOT NULL,
                    last_active TEXT,
                    
                    capabilities TEXT,
                    max_concurrent_tasks INTEGER DEFAULT 1
                )
            """)
            
            # Coordination locks table
            conn.execute("""
                CREATE TABLE IF NOT EXISTS coordination_locks (
                    lock_name TEXT PRIMARY KEY,
                    held_by TEXT NOT NULL,
                    acquired_at TEXT NOT NULL,
                    expires_at TEXT NOT NULL,
                    
                    FOREIGN KEY (held_by) REFERENCES agents(agent_id)
                )
            """)
            
            # Task indexes
            conn.execute("CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status)")
            conn.execute("CREATE INDEX IF NOT EXISTS idx_tasks_group_status ON tasks(group_id, status)")
            conn.execute("CREATE INDEX IF NOT EXISTS idx_tasks_assigned ON tasks(assigned_to)")
            conn.execute("CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority DESC, created_at ASC)")
            
            # Agent indexes
            conn.execute("CREATE INDEX IF NOT EXISTS idx_agents_group ON agents(group_id)")
            conn.execute("CREATE INDEX IF NOT EXISTS idx_agents_status ON agents(status)")
            
            # Lock indexes
            conn.execute("CREATE INDEX IF NOT EXISTS idx_locks_expires ON coordination_locks(expires_at)")
            
            conn.commit()
        
        logger.debug("Database schema initialized")
    
    # === Task Queue Operations ===
    
    def enqueue_task(
        self,
        instruction: str,
        group_id: str,
        priority: int = 5,
        parent_task_id: str | None = None,
        context_artifact_id: str | None = None,
        tags: list[str] | None = None,
        dependencies: list[str] | None = None
    ) -> str:
        """
        Add a new task to the queue.
        
        Args:
            instruction: Task description
            group_id: Target agent group
            priority: 0-10 (higher = more urgent)
            parent_task_id: Parent task ID if this is a subtask
            context_artifact_id: Optional reference to context artifact
            tags: Optional task tags
            dependencies: Task IDs that must complete first
        
        Returns:
            task_id (UUID)
        """
        task_id = str(uuid.uuid4())
        
        with self._connect() as conn:
            conn.execute("""
                INSERT INTO tasks (
                    task_id, group_id, parent_task_id, status, priority,
                    instruction, context_artifact_id, created_at, tags, dependencies
                ) VALUES (?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?)
            """, (
                task_id, group_id, parent_task_id, priority,
                instruction, context_artifact_id,
                datetime.now().isoformat(),
                json.dumps(tags or []),
                json.dumps(dependencies or [])
            ))
            conn.commit()
        
        logger.info(f"Task enqueued: {task_id} (group={group_id}, priority={priority})")
        return task_id
    
    def claim_next_task(
        self,
        agent_id: str,
        group_id: str,
        capabilities: list[str] | None = None
    ) -> dict[str, Any] | None:
        """
        Atomically claim the highest-priority pending task.
        
        Uses BEGIN IMMEDIATE + row locking to prevent double-assignment.
        
        Args:
            agent_id: Claiming agent's ID
            group_id: Agent's group
            capabilities: Agent's capabilities for task filtering
        
        Returns:
            Task dict or None if queue is empty
        """
        with self._connect() as conn:
            conn.execute("BEGIN IMMEDIATE")
            
            try:
                # Find highest-priority pending task with no unmet dependencies
                cursor = conn.execute("""
                    SELECT * FROM tasks
                    WHERE group_id = ? 
                      AND status = 'pending'
                      AND (dependencies IS NULL OR dependencies = '[]')
                    ORDER BY priority DESC, created_at ASC
                    LIMIT 1
                """, (group_id,))
                
                row = cursor.fetchone()
                if not row:
                    conn.rollback()
                    return None
                
                task_id = row['task_id']
                
                # Atomically claim it
                conn.execute("""
                    UPDATE tasks 
                    SET status = 'running', 
                        assigned_to = ?, 
                        started_at = ?
                    WHERE task_id = ?
                """, (agent_id, datetime.now().isoformat(), task_id))
                
                conn.commit()
                
                logger.info(f"Task claimed: {task_id} by {agent_id}")
                return dict(row)
                
            except Exception as e:
                conn.rollback()
                logger.error(f"Failed to claim task: {e}")
                raise
    
    def update_task_status(
        self,
        task_id: str,
        status: str,
        result_artifact_id: str | None = None,
        error_msg: str | None = None
    ):
        """
        Update task status and result.
        
        Args:
            task_id: Task ID
            status: New status (running|complete|failed|blocked)
            result_artifact_id: Optional link to result artifact
            error_msg: Error message if failed
        """
        with self._connect() as conn:
            conn.execute("""
                UPDATE tasks
                SET status = ?,
                    completed_at = ?,
                    result_artifact_id = ?,
                    error_msg = ?
                WHERE task_id = ?
            """, (
                status,
                datetime.now().isoformat() if status in ['complete', 'failed'] else None,
                result_artifact_id,
                error_msg,
                task_id
            ))
            conn.commit()
        
        logger.info(f"Task status updated: {task_id} -> {status}")
    
    def get_task(self, task_id: str) -> dict[str, Any] | None:
        """Retrieve task details."""
        with self._connect() as conn:
            cursor = conn.execute("SELECT * FROM tasks WHERE task_id = ?", (task_id,))
            row = cursor.fetchone()
            return dict(row) if row else None
    
    def list_tasks(
        self,
        group_id: str | None = None,
        status: str | None = None,
        assigned_to: str | None = None,
        limit: int = 100
    ) -> list[dict[str, Any]]:
        """
        Query tasks with filters.
        
        Args:
            group_id: Filter by group
            status: Filter by status
            assigned_to: Filter by assigned agent
            limit: Max results
        
        Returns:
            List of task dicts
        """
        with self._connect() as conn:
            query = "SELECT * FROM tasks WHERE 1=1"
            params: list[Any] = []
            
            if group_id:
                query += " AND group_id = ?"
                params.append(group_id)
            if status:
                query += " AND status = ?"
                params.append(status)
            if assigned_to:
                query += " AND assigned_to = ?"
                params.append(assigned_to)
            
            query += " ORDER BY created_at DESC LIMIT ?"
            params.append(limit)
            
            cursor = conn.execute(query, params)
            return [dict(row) for row in cursor.fetchall()]
    
    # === Agent Registry ===
    
    def register_agent(
        self,
        agent_id: str,
        group_id: str,
        capabilities: list[str]
    ):
        """Register agent in global registry."""
        with self._connect() as conn:
            conn.execute("""
                INSERT OR REPLACE INTO agents (
                    agent_id, group_id, status, registered_at, last_heartbeat, capabilities
                ) VALUES (?, ?, 'idle', ?, ?, ?)
            """, (
                agent_id, group_id,
                datetime.now().isoformat(),
                datetime.now().isoformat(),
                json.dumps(capabilities)
            ))
            conn.commit()
        
        logger.info(f"Agent registered: {agent_id} (group={group_id})")
    
    def update_heartbeat(self, agent_id: str):
        """Update agent last_heartbeat timestamp."""
        with self._connect() as conn:
            conn.execute("""
                UPDATE agents
                SET last_heartbeat = ?
                WHERE agent_id = ?
            """, (datetime.now().isoformat(), agent_id))
            conn.commit()
    
    def mark_agent_busy(self, agent_id: str, task_id: str):
        """Mark agent as busy with task."""
        with self._connect() as conn:
            conn.execute("""
                UPDATE agents
                SET status = 'busy', 
                    current_task_id = ?,
                    last_active = ?
                WHERE agent_id = ?
            """, (task_id, datetime.now().isoformat(), agent_id))
            conn.commit()
        
        logger.debug(f"Agent {agent_id} marked busy with task {task_id}")
    
    def mark_agent_idle(self, agent_id: str):
        """Mark agent as idle."""
        with self._connect() as conn:
            conn.execute("""
                UPDATE agents
                SET status = 'idle',
                    current_task_id = NULL,
                    last_active = ?
                WHERE agent_id = ?
            """, (datetime.now().isoformat(), agent_id))
            conn.commit()
        
        logger.debug(f"Agent {agent_id} marked idle")
    
    def get_agent_status(self, agent_id: str) -> dict[str, Any] | None:
        """Get agent details."""
        with self._connect() as conn:
            cursor = conn.execute("SELECT * FROM agents WHERE agent_id = ?", (agent_id,))
            row = cursor.fetchone()
            return dict(row) if row else None
    
    def detect_crashed_agents(self, timeout_seconds: int = 300) -> list[str]:
        """
        Detect agents with stale heartbeats.
        
        Args:
            timeout_seconds: Heartbeat timeout (default: 5 minutes)
        
        Returns:
            List of crashed agent IDs
        """
        cutoff = (datetime.now() - timedelta(seconds=timeout_seconds)).isoformat()
        
        with self._connect() as conn:
            cursor = conn.execute("""
                SELECT agent_id FROM agents
                WHERE status != 'crashed'
                  AND last_heartbeat < ?
            """, (cutoff,))
            
            crashed = [row['agent_id'] for row in cursor.fetchall()]
            
            # Mark as crashed
            if crashed:
                placeholders = ','.join('?' * len(crashed))
                conn.execute(f"""
                    UPDATE agents
                    SET status = 'crashed'
                    WHERE agent_id IN ({placeholders})
                """, crashed)
                conn.commit()
            
            if crashed:
                logger.warning(f"Detected crashed agents: {crashed}")
            
            return crashed
    
    # === Coordination Primitives ===
    
    def acquire_lock(
        self,
        lock_name: str,
        agent_id: str,
        ttl_seconds: int = 30
    ) -> bool:
        """
        Try to acquire a named lock.
        
        Args:
            lock_name: Unique lock identifier
            agent_id: Requesting agent
            ttl_seconds: Lock expiry time
        
        Returns:
            True if acquired, False if already held
        """
        expires_at = (datetime.now() + timedelta(seconds=ttl_seconds)).isoformat()
        
        with self._connect() as conn:
            try:
                conn.execute("""
                    INSERT INTO coordination_locks (lock_name, held_by, acquired_at, expires_at)
                    VALUES (?, ?, ?, ?)
                """, (lock_name, agent_id, datetime.now().isoformat(), expires_at))
                conn.commit()
                
                logger.debug(f"Lock acquired: {lock_name} by {agent_id}")
                return True
                
            except sqlite3.IntegrityError:
                # Lock already exists, check if expired
                cursor = conn.execute("""
                    SELECT expires_at, held_by FROM coordination_locks
                    WHERE lock_name = ?
                """, (lock_name,))
                row = cursor.fetchone()
                
                if row and row['expires_at'] < datetime.now().isoformat():
                    # Expired, steal it
                    conn.execute("""
                        UPDATE coordination_locks
                        SET held_by = ?, acquired_at = ?, expires_at = ?
                        WHERE lock_name = ?
                    """, (agent_id, datetime.now().isoformat(), expires_at, lock_name))
                    conn.commit()
                    
                    logger.debug(f"Lock stolen (expired): {lock_name} by {agent_id} from {row['held_by']}")
                    return True
                
                logger.debug(f"Lock acquisition failed: {lock_name} held by {row['held_by'] if row else 'unknown'}")
                return False
    
    def release_lock(self, lock_name: str, agent_id: str):
        """Release a held lock."""
        with self._connect() as conn:
            conn.execute("""
                DELETE FROM coordination_locks
                WHERE lock_name = ? AND held_by = ?
            """, (lock_name, agent_id))
            conn.commit()
        
        logger.debug(f"Lock released: {lock_name} by {agent_id}")
    
    def cleanup_expired_locks(self):
        """Background job: remove expired locks."""
        with self._connect() as conn:
            cursor = conn.execute("""
                DELETE FROM coordination_locks
                WHERE expires_at < ?
                RETURNING lock_name
            """, (datetime.now().isoformat(),))
            
            expired = [row['lock_name'] for row in cursor.fetchall()]
            conn.commit()
        
        if expired:
            logger.info(f"Cleaned up {len(expired)} expired locks")
