"""
ArtifactStore - SQLite-based Content-Addressable Storage for L4 Memory.

PRODUCTION SPECIFICATION:
- Backend: SQLite (WAL mode for concurrency)
- Deduplication: SHA256-based Content Addressable Storage (CAS)
- Crash Safety: Atomic transactions
- Concurrent reads during writes
"""

import sqlite3
import hashlib
import datetime
import json
import logging
from typing import Dict, Any, List, Optional
from pathlib import Path
from contextlib import contextmanager

logger = logging.getLogger(__name__)

class ArtifactStore:
    """
    Production-grade Artifact Store with SQLite backend.
    
    Features:
    - Content Addressable Storage (CAS) using SHA256
    - Automatic deduplication
    - Concurrent-safe (SQLite WAL mode)
    - Metadata indexed for fast queries
    """
    
    def __init__(self, workspace_root: str):
        self.workspace_root = Path(workspace_root)
        self.artifacts_dir = self.workspace_root / ".soloqueue" / "artifacts"
        self.blobs_dir = self.artifacts_dir / "blobs"
        self.db_path = self.workspace_root / ".soloqueue" / "artifacts.db"
        
        # Ensure directories exist
        self.blobs_dir.mkdir(parents=True, exist_ok=True)
        
        # Initialize database
        self._init_db()
    
    @contextmanager
    def _connect(self):
        """Context manager for database connections."""
        conn = sqlite3.connect(str(self.db_path))
        conn.row_factory = sqlite3.Row  # Enable column access by name
        
        # Enable WAL mode for concurrent access (Production requirement)
        conn.execute("PRAGMA journal_mode=WAL")
        
        try:
            yield conn
            conn.commit()
        except Exception as e:
            conn.rollback()
            raise e
        finally:
            conn.close()
    
    def _init_db(self):
        """Initialize database schema with WAL mode."""
        with self._connect() as conn:
            cursor = conn.cursor()
            
            # Enable WAL mode for concurrent reads during writes
            cursor.execute("PRAGMA journal_mode=WAL;")
            
            # Create artifacts table
            # Note: Using auto-increment ID allows multiple metadata entries for same content
            cursor.execute("""
                CREATE TABLE IF NOT EXISTS artifacts (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    content_hash TEXT NOT NULL,
                    group_id TEXT,
                    title TEXT,
                    tags TEXT,
                    author TEXT,
                    created_at TIMESTAMP,
                    path TEXT,
                    size INTEGER,
                    mime TEXT
                )
            """)
            
            # Create indexes for common queries
            cursor.execute("""
                CREATE INDEX IF NOT EXISTS idx_content_hash 
                ON artifacts(content_hash)
            """)
            cursor.execute("""
                CREATE INDEX IF NOT EXISTS idx_tags 
                ON artifacts(tags)
            """)
            cursor.execute("""
                CREATE INDEX IF NOT EXISTS idx_created 
                ON artifacts(created_at)
            """)
            
            logger.info("ArtifactStore database initialized with WAL mode")
    
    def _compute_hash(self, content: str) -> str:
        """Compute SHA256 hash of content."""
        return hashlib.sha256(content.encode('utf-8')).hexdigest()
    
    def _get_blob_path(self, content_hash: str, timestamp: datetime.datetime) -> Path:
        """
        Get blob path using Hybrid Date + CAS.
        Structure: blobs/YYYY/MM/DD/ab/cd/abcdef...
        """
        date_prefix = timestamp.strftime("%Y/%m/%d")
        prefix1 = content_hash[:2]
        prefix2 = content_hash[2:4]
        blob_dir = self.blobs_dir / date_prefix / prefix1 / prefix2
        blob_dir.mkdir(parents=True, exist_ok=True)
        return blob_dir / content_hash
    
    def save_artifact(
        self, 
        content: str, 
        title: str,
        author: str = "system", 
        group_id: str = "default",
        tags: List[str] = None, 
        artifact_type: str = "text"
    ) -> str:
        """
        Save artifact with content deduplication.
        
        Args:
            content: Content to store
            title: Human-readable title
            author: Author identifier
            group_id: Group scope
            tags: List of tags (e.g., ["sys:ephemeral"])
            artifact_type: MIME type or extension
        
        Returns:
            artifact_id: Auto-generated integer ID (as string)
        """
        # 1. Compute Content Hash
        content_hash = self._compute_hash(content)
        timestamp = datetime.datetime.now()
        
        # 2. Check if content already exists (Deduplication within same date)
        blob_path = self._get_blob_path(content_hash, timestamp)
        if not blob_path.exists():
            # Write new blob
            try:
                blob_path.write_text(content, encoding='utf-8')
                logger.debug(f"Wrote new blob: {content_hash[:8]}...")
            except Exception as e:
                logger.error(f"Failed to write blob {content_hash}: {e}")
                raise
        else:
            logger.debug(f"Blob already exists (deduped): {content_hash[:8]}...")
        
        # 3. Store Metadata (Always create new entry for tracking)
        relative_path = str(blob_path.relative_to(self.workspace_root))
        tags_json = json.dumps(tags or [])
        
        with self._connect() as conn:
            cursor = conn.cursor()
            cursor.execute("""
                INSERT INTO artifacts (
                    content_hash, group_id, title, tags, 
                    author, created_at, path, size, mime
                ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
            """, (
                content_hash,
                group_id,
                title,
                tags_json,
                author,
                timestamp.isoformat(),
                relative_path,
                len(content),
                artifact_type
            ))
            artifact_id = str(cursor.lastrowid)
        
        logger.info(f"Artifact saved: {artifact_id} (hash: {content_hash[:8]}..., title: {title})")
        return artifact_id
    
    def get_artifact(self, artifact_id: str) -> Optional[Dict[str, Any]]:
        """
        Retrieve artifact metadata and content.
        
        Args:
            artifact_id: SHA256 hash
        
        Returns:
            Dict with 'metadata' and 'content' keys, or None if not found
        """
        with self._connect() as conn:
            cursor = conn.cursor()
            cursor.execute("""
                SELECT * FROM artifacts WHERE id = ?
            """, (artifact_id,))
            row = cursor.fetchone()
        
        if not row:
            logger.warning(f"Artifact not found: {artifact_id}")
            return None
        
        # Load content
        blob_path = self.workspace_root / row["path"]
        try:
            content = blob_path.read_text(encoding='utf-8')
        except FileNotFoundError:
            # Orphan metadata (file deleted but DB not updated)
            logger.error(f"Orphan artifact: {artifact_id} (metadata exists but file missing)")
            return None
        except Exception as e:
            logger.error(f"Failed to read artifact {artifact_id}: {e}")
            return None
        
        metadata = dict(row)
        metadata["tags"] = json.loads(metadata["tags"])
        
        return {
            "metadata": metadata,
            "content": content
        }
    
    def list_artifacts(
        self, 
        group_id: Optional[str] = None, 
        tag: Optional[str] = None
    ) -> List[Dict[str, Any]]:
        """
        List artifacts with optional filters.
        
        Args:
            group_id: Filter by group
            tag: Filter by tag (checks if tag in tags list)
        
        Returns:
            List of metadata dictionaries
        """
        with self._connect() as conn:
            cursor = conn.cursor()
            
            query = "SELECT * FROM artifacts WHERE 1=1"
            params = []
            
            if group_id:
                query += " AND group_id = ?"
                params.append(group_id)
            
            if tag:
                # SQLite doesn't have native JSON contains, so use LIKE
                query += " AND tags LIKE ?"
                params.append(f'%"{tag}"%')
            
            cursor.execute(query, params)
            rows = cursor.fetchall()
        
        results = []
        for row in rows:
            metadata = dict(row)
            metadata["tags"] = json.loads(metadata["tags"])
            results.append(metadata)
        
        return results
    
    def delete_artifact(self, artifact_id: str) -> bool:
        """
        Delete artifact metadata.
        Note: Physical blob is NOT deleted (handled by GC Phase 2).
        
        Args:
            artifact_id: SHA256 hash
        
        Returns:
            True if deleted, False if not found
        """
        with self._connect() as conn:
            cursor = conn.cursor()
            cursor.execute("DELETE FROM artifacts WHERE id = ?", (artifact_id,))
            deleted = cursor.rowcount > 0
        
        if deleted:
            logger.info(f"Artifact metadata deleted: {artifact_id[:8]}...")
        return deleted
