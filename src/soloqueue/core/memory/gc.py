"""
GarbageCollector - Two-Phase Pruning for Ephemeral Artifacts.

PRODUCTION SPECIFICATION:
- Phase 1: Fast SQL metadata deletion
- Phase 2: Deep scan for orphaned blobs
- Process-safe: fcntl locking prevents concurrent GC
- Non-blocking: Skip if another GC is running
"""

import fcntl
import sqlite3
import logging
from datetime import datetime, timedelta
from pathlib import Path

logger = logging.getLogger(__name__)

class GarbageCollector:
    """
    Two-Phase Garbage Collection for Artifact Store.
    
    Phase 1: Metadata Pruning (Fast)
    - Delete expired ephemeral artifacts from SQLite
    
    Phase 2: Orphan Scanning (Deep Clean)
    - Remove physical blobs not referenced in database
    """
    
    def __init__(self, workspace_root: str, retention_days: int = 3):
        self.workspace_root = Path(workspace_root)
        self.retention_days = retention_days
        self.db_path = self.workspace_root / ".soloqueue" / "artifacts.db"
        self.blobs_dir = self.workspace_root / ".soloqueue" / "artifacts" / "blobs"
        self.lock_file_path = self.workspace_root / ".soloqueue" / ".gc.lock"
        self.state_file_path = self.workspace_root / ".soloqueue" / ".gc_state"
        
        # Ensure lock file exists
        self.lock_file_path.parent.mkdir(parents=True, exist_ok=True)
        self.lock_file_path.touch(exist_ok=True)
    
    def should_run(self, cooldown_hours: int = 24) -> bool:
        """
        Check if GC should run based on last execution time.
        
        Args:
            cooldown_hours: Minimum hours between GC runs
        
        Returns:
            True if enough time has passed since last run
        """
        if not self.state_file_path.exists():
            return True
        
        try:
            last_run_str = self.state_file_path.read_text().strip()
            last_run = datetime.fromisoformat(last_run_str)
            elapsed = datetime.now() - last_run
            
            return elapsed > timedelta(hours=cooldown_hours)
        except Exception as e:
            logger.warning(f"Failed to read GC state: {e}. Running GC anyway.")
            return True
    
    def run_once(self, skip_orphan_scan: bool = False) -> dict:
        """
        Execute garbage collection with process locking.
        
        Args:
            skip_orphan_scan: If True, only run Phase 1 (faster)
        
        Returns:
            Statistics dict: {phase1_deleted, phase2_deleted, skipped}
        """
        stats = {"phase1_deleted": 0, "phase2_deleted": 0, "skipped": False}
        
        # Open lock file
        lock_file = open(self.lock_file_path, 'a+')
        
        try:
            # Try to acquire non-blocking exclusive lock
            fcntl.flock(lock_file, fcntl.LOCK_EX | fcntl.LOCK_NB)
            
            logger.info("[GC] Lock acquired. Starting garbage collection...")
            
            # Phase 1: Metadata Pruning
            phase1_count = self._phase1_metadata_pruning()
            stats["phase1_deleted"] = phase1_count
            logger.info(f"[GC] Phase 1 complete: {phase1_count} metadata entries deleted")
            
            # Phase 2: Orphan Scanning (optional, slower)
            if not skip_orphan_scan:
                phase2_count = self._phase2_orphan_scanning()
                stats["phase2_deleted"] = phase2_count
                logger.info(f"[GC] Phase 2 complete: {phase2_count} orphan blobs deleted")
            
            # Update last run timestamp
            self._update_last_run()
            
        except BlockingIOError:
            logger.debug("[GC] Lock held by another process. Skipping.")
            stats["skipped"] = True
        except Exception as e:
            logger.error(f"[GC] Critical error: {e}")
            raise
        finally:
            # Release lock (also released automatically when file closes)
            try:
                fcntl.flock(lock_file, fcntl.LOCK_UN)
            except:
                pass
            lock_file.close()
        
        return stats
    
    def _phase1_metadata_pruning(self) -> int:
        """
        Phase 1: Fast SQL deletion of expired ephemeral artifacts.
        
        Returns:
            Number of metadata entries deleted
        """
        cutoff_date = datetime.now() - timedelta(days=self.retention_days)
        
        conn = sqlite3.connect(str(self.db_path))
        try:
            cursor = conn.cursor()
            
            # Delete artifacts with sys:ephemeral tag AND expired
            cursor.execute("""
                DELETE FROM artifacts 
                WHERE tags LIKE '%sys:ephemeral%' 
                AND created_at < ?
            """, (cutoff_date.isoformat(),))
            
            deleted_count = cursor.rowcount
            conn.commit()
            
            return deleted_count
        finally:
            conn.close()
    
    def _phase2_orphan_scanning(self) -> int:
        """
        Phase 2: Deep scan to delete physical blobs not in database.
        
        This catches orphans from:
        - Failed insertions (blob written, DB write failed)
        - External deletions
        
        Returns:
            Number of orphan files deleted
        """
        if not self.blobs_dir.exists():
            return 0
        
        # 1. Get all content_hash values from database
        conn = sqlite3.connect(str(self.db_path))
        try:
            cursor = conn.cursor()
            cursor.execute("SELECT DISTINCT content_hash FROM artifacts")
            valid_hashes = {row[0] for row in cursor.fetchall()}
        finally:
            conn.close()
        
        # 2. Scan all blob files
        deleted_count = 0
        for blob_file in self.blobs_dir.rglob("*"):
            if not blob_file.is_file():
                continue
            
            # Extract hash from filename (last component of path)
            file_hash = blob_file.name
            
            # If hash not in database, delete the file
            if file_hash not in valid_hashes:
                try:
                    blob_file.unlink()
                    deleted_count += 1
                    logger.debug(f"[GC] Deleted orphan blob: {file_hash[:8]}...")
                except Exception as e:
                    logger.warning(f"[GC] Failed to delete orphan {file_hash}: {e}")
        
        return deleted_count
    
    def _update_last_run(self):
        """Update the last run timestamp."""
        try:
            self.state_file_path.write_text(datetime.now().isoformat())
        except Exception as e:
            logger.warning(f"[GC] Failed to update state file: {e}")
    
    def archive_by_date(self, archive_days: int = 7) -> dict:
        """
        Archive old artifacts by date instead of deleting them.
        
        Moves artifacts older than `archive_days` to archive/YYYY-MM-DD/ directories.
        This implements the roadmap requirement for "按日归档".
        
        Args:
            archive_days: Days after which to archive (default: 7)
        
        Returns:
            Statistics dict: {archived_count, archive_dirs_created}
        """
        stats = {"archived_count": 0, "archive_dirs_created": 0}
        cutoff_date = datetime.now() - timedelta(days=archive_days)
        
        conn = sqlite3.connect(str(self.db_path))
        try:
            cursor = conn.cursor()
            
            # Find old non-ephemeral artifacts (we want to keep them, just archive)
            cursor.execute("""
                SELECT id, content_hash, created_at, title, author 
                FROM artifacts 
                WHERE created_at < ? 
                AND (tags NOT LIKE '%sys:ephemeral%' OR tags IS NULL)
            """, (cutoff_date.isoformat(),))
            
            old_artifacts = cursor.fetchall()
            
            for artifact_id, content_hash, created_at, title, author in old_artifacts:
                try:
                    # Parse creation date
                    created_date = datetime.fromisoformat(created_at)
                    date_str = created_date.strftime("%Y-%m-%d")
                    
                    # Create archive directory for this date
                    archive_dir = self.workspace_root / ".soloqueue" / "archive" / date_str
                    if not archive_dir.exists():
                        archive_dir.mkdir(parents=True, exist_ok=True)
                        stats["archive_dirs_created"] += 1
                        logger.info(f"[GC] Created archive directory: {date_str}")
                    
                    # Move blob file to archive
                    source_blob = self.blobs_dir / content_hash
                    if source_blob.exists():
                        # Archive with metadata in filename for easy identification
                        safe_title = "".join(c for c in title if c.isalnum() or c in (' ', '-', '_'))[:50]
                        archive_filename = f"{artifact_id}_{safe_title}_{content_hash[:8]}.blob"
                        dest_blob = archive_dir / archive_filename
                        
                        source_blob.rename(dest_blob)
                        logger.debug(f"[GC] Archived {artifact_id} to {date_str}/")
                    
                    # Update database record with archive location
                    cursor.execute("""
                        UPDATE artifacts 
                        SET tags = CASE 
                            WHEN tags IS NULL THEN 'sys:archived'
                            WHEN tags LIKE '%sys:archived%' THEN tags
                            ELSE tags || ',sys:archived'
                        END
                        WHERE id = ?
                    """, (artifact_id,))
                    
                    stats["archived_count"] += 1
                    
                except Exception as e:
                    logger.warning(f"[GC] Failed to archive artifact {artifact_id}: {e}")
                    continue
            
            conn.commit()
            logger.info(f"[GC] Archived {stats['archived_count']} artifacts to {stats['archive_dirs_created']} date directories")
            
        finally:
            conn.close()
        
        return stats
