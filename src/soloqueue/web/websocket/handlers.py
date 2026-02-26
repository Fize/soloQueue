"""
WebSocket connection handlers for write-action confirmations.

Manages WebSocket connections and provides functions for sending
write-action requests to connected web UI clients.
"""

from typing import Optional
from fastapi import WebSocket

from soloqueue.core.logger import logger
from soloqueue.web.websocket.schemas import WriteActionRequest


# Global connection state
_active_connections: set[WebSocket] = set()
_write_action_websocket: Optional[WebSocket] = None


def get_write_action_websocket() -> Optional[WebSocket]:
    """Get the primary write-action WebSocket connection."""
    return _write_action_websocket


def set_write_action_websocket(websocket: Optional[WebSocket]) -> None:
    """Set or clear the primary write-action WebSocket connection."""
    global _write_action_websocket
    _write_action_websocket = websocket


def add_connection(websocket: WebSocket) -> None:
    """Add a WebSocket connection to the active set."""
    _active_connections.add(websocket)


def remove_connection(websocket: WebSocket) -> None:
    """Remove a WebSocket connection from the active set."""
    if websocket in _active_connections:
        _active_connections.remove(websocket)
    if _write_action_websocket == websocket:
        set_write_action_websocket(None)


def get_active_connection_count() -> int:
    """Return the number of active WebSocket connections."""
    return len(_active_connections)


async def send_write_action_request(request: WriteActionRequest) -> bool:
    """Send a write-action request to the connected web UI client.

    Args:
        request: WriteActionRequest instance

    Returns:
        True if sent successfully, False if no client connected.
    """
    websocket = get_write_action_websocket()
    if websocket is None:
        return False

    try:
        await websocket.send_json(request.model_dump())
        return True
    except Exception as e:
        # Connection may be broken; clear the reference
        logger.error(f"Failed to send write-action request: {e}")
        set_write_action_websocket(None)
        return False