import sys
from pathlib import Path

from loguru import logger as _base_logger

from soloqueue.core.config import settings


def setup_logger():
    """
    Configure structured logging for SoloQueue.
    
    Two sinks:
    1. Console (stderr) — structured logs with timestamps
    2. File (JSONL) — machine-friendly structured logs (all AI interactions)
    
    Usage:
        logger.info("structured log")              # → stderr with timestamp
    """
    # Remove default handler
    _base_logger.remove()
    
    # 1. Console Handler (Human Friendly)
    _base_logger.add(
        sys.stderr,
        format="<green>{time:HH:mm:ss}</green> | <level>{level: <8}</level> | <cyan>{name}</cyan>:<cyan>{function}</cyan>:<cyan>{line}</cyan> - <level>{message}</level> | {extra}",
        level=settings.LOG_LEVEL,
        colorize=True,
    )
    
    # 2. File Handler (Machine Friendly / Structured)
    log_dir = Path("logs")
    log_dir.mkdir(exist_ok=True)
    
    _base_logger.add(
        log_dir / "soloqueue.jsonl",
        serialize=True,
        rotation="10 MB",
        retention="7 days",
        level="DEBUG",  # Always capture debug logs in file
    )

    return _base_logger

# Configure immediately on import
logger = setup_logger()
