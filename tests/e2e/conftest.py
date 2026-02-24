"""
Pytest fixtures for SoloQueue end-to-end tests.

Provides fixtures for:
- FastAPI test server
- WebSocket connections
- Playwright browser contexts
- Test data cleanup
"""

import asyncio
import os
import pytest
import threading
import time
from pathlib import Path
from typing import Generator, AsyncGenerator

from soloqueue.web.app import app
from soloqueue.core.registry import Registry
from soloqueue.core.config import settings
from soloqueue.core.workspace import workspace
import uvicorn


@pytest.fixture(scope="session")
def event_loop():
    """Create an event loop for async fixtures."""
    loop = asyncio.get_event_loop_policy().new_event_loop()
    yield loop
    loop.close()


@pytest.fixture(scope="session")
def test_workspace(tmp_path_factory) -> Path:
    """Create a temporary workspace for testing."""
    workspace_path = tmp_path_factory.mktemp("test_workspace")

    # Configure settings for testing
    settings.REQUIRE_APPROVAL = False  # Disable approval for faster tests

    # Initialize workspace
    workspace.root = workspace_path

    return workspace_path


@pytest.fixture(scope="session")
def test_server_port() -> int:
    """Return the test server port."""
    return 8123


@pytest.fixture(scope="session")
def test_server_url(test_server_port: int) -> str:
    """Return the test server URL."""
    return f"http://localhost:{test_server_port}"


@pytest.fixture(scope="session")
def test_server(test_server_port: int, test_workspace: Path):
    """Start a test FastAPI server in a separate thread."""
    from soloqueue.core.registry import Registry
    from soloqueue.core.config import settings

    # Configure settings
    settings.REQUIRE_APPROVAL = False

    # Start server in a thread
    config = uvicorn.Config(app, host="127.0.0.1", port=test_server_port, log_level="warning")
    server = uvicorn.Server(config)

    thread = threading.Thread(target=server.run, daemon=True)
    thread.start()

    # Wait for server to start
    import time
    import socket
    max_wait = 10
    start_time = time.time()
    while time.time() - start_time < max_wait:
        try:
            with socket.create_connection(("127.0.0.1", test_server_port), timeout=1):
                break
        except (ConnectionRefusedError, socket.timeout):
            time.sleep(0.1)
    else:
        raise RuntimeError(f"Test server failed to start on port {test_server_port}")

    yield server

    # Cleanup
    server.should_exit = True
    thread.join(timeout=5)


@pytest.fixture
async def websocket_connection(test_server_url: str):
    """Create a WebSocket connection for testing."""
    import websockets

    ws_url = test_server_url.replace("http", "ws") + "/ws/chat"
    async with websockets.connect(ws_url) as websocket:
        yield websocket


@pytest.fixture
async def write_action_websocket(test_server_url: str):
    """Create a WebSocket connection for write-action confirmations."""
    import websockets

    ws_url = test_server_url.replace("http", "ws") + "/ws/write-action"
    async with websockets.connect(ws_url) as websocket:
        yield websocket


@pytest.fixture(scope="session")
def playwright_browser(playwright):
    """Create a Playwright browser instance."""
    # Allow using system chromium via environment variable
    executable_path = os.environ.get("PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH")
    launch_args = {"headless": True}
    if executable_path:
        launch_args["executable_path"] = executable_path
    browser = playwright.chromium.launch(**launch_args)
    yield browser
    browser.close()


@pytest.fixture
async def browser_page(playwright_browser):
    """Create a new browser page for testing."""
    context = await playwright_browser.new_context()
    page = await context.new_page()
    yield page
    await context.close()


@pytest.fixture
def artifact_dir(test_workspace: Path) -> Path:
    """Get the artifact directory path."""
    artifact_path = test_workspace / ".soloqueue" / "memory" / "investment__leader" / "artifacts"
    artifact_path.mkdir(parents=True, exist_ok=True)
    return artifact_path