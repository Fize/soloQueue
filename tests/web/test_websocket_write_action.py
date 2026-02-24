"""
Unit tests for write-action WebSocket message parsing.
"""

import pytest
from datetime import datetime
from src.soloqueue.web.websocket.schemas import (
    WriteActionRequest,
    WriteActionResponse,
    AgentOutputEvent,
    parse_websocket_message,
)


class TestWriteActionRequest:
    """Tests for WriteActionRequest schema."""

    def test_create_request_defaults(self):
        """Test creating a request with minimal required fields."""
        request = WriteActionRequest(
            agent_id="analyst",
            file_path="/tmp/test.md",
            operation="create",
        )
        assert request.type == "write_action_request"
        assert request.agent_id == "analyst"
        assert request.file_path == "/tmp/test.md"
        assert request.operation == "create"
        assert isinstance(request.id, str)
        assert len(request.id) == 36  # UUID length
        assert isinstance(request.timestamp, datetime)

    def test_create_request_with_custom_id(self):
        """Test creating a request with custom ID."""
        request = WriteActionRequest(
            id="test-id-123",
            agent_id="analyst",
            file_path="/tmp/test.md",
            operation="update",
        )
        assert request.id == "test-id-123"
        assert request.operation == "update"

    def test_operation_validation(self):
        """Test that operation must be one of allowed values."""
        # Valid operations
        for op in ("create", "update", "delete"):
            request = WriteActionRequest(
                agent_id="analyst",
                file_path="/tmp/test.md",
                operation=op,
            )
            assert request.operation == op

        # Invalid operation should raise error
        with pytest.raises(ValueError):
            WriteActionRequest(
                agent_id="analyst",
                file_path="/tmp/test.md",
                operation="invalid",
            )

    def test_serialization(self):
        """Test that request can be serialized to dict."""
        request = WriteActionRequest(
            agent_id="analyst",
            file_path="/tmp/test.md",
            operation="delete",
        )
        data = request.model_dump()
        assert data["type"] == "write_action_request"
        assert data["agent_id"] == "analyst"
        assert data["file_path"] == "/tmp/test.md"
        assert data["operation"] == "delete"
        assert "id" in data
        assert "timestamp" in data


class TestWriteActionResponse:
    """Tests for WriteActionResponse schema."""

    def test_create_response(self):
        """Test creating a response."""
        response = WriteActionResponse(
            id="test-id-123",
            approved=True,
        )
        assert response.type == "write_action_response"
        assert response.id == "test-id-123"
        assert response.approved is True
        assert isinstance(response.timestamp, datetime)

    def test_approved_boolean(self):
        """Test that approved must be a boolean."""
        response = WriteActionResponse(id="test", approved=False)
        assert response.approved is False

    def test_serialization(self):
        """Test that response can be serialized to dict."""
        response = WriteActionResponse(
            id="test-id-123",
            approved=True,
        )
        data = response.model_dump()
        assert data["type"] == "write_action_response"
        assert data["id"] == "test-id-123"
        assert data["approved"] is True


class TestAgentOutputEvent:
    """Tests for AgentOutputEvent schema."""

    def test_create_event_minimal(self):
        """Test creating an event with minimal fields."""
        event = AgentOutputEvent(
            type="thinking",
            agent_id="analyst",
            content="Thinking about the problem...",
        )
        assert event.type == "thinking"
        assert event.agent_id == "analyst"
        assert event.content == "Thinking about the problem..."
        assert event.agent_color is None
        assert event.preview_snippet is None
        assert event.collapsible is False
        assert event.collapsed_by_default is False

    def test_create_event_with_metadata(self):
        """Test creating an event with UI metadata."""
        event = AgentOutputEvent(
            type="thinking",
            agent_id="analyst",
            content="Thinking about the problem...",
            agent_color="#10b981",
            preview_snippet="Thinking about...",
            collapsible=True,
            collapsed_by_default=True,
        )
        assert event.agent_color == "#10b981"
        assert event.preview_snippet == "Thinking about..."
        assert event.collapsible is True
        assert event.collapsed_by_default is True

    def test_type_validation(self):
        """Test that type must be one of allowed values."""
        for msg_type in ("thinking", "tool_call", "tool_result", "final_result"):
            event = AgentOutputEvent(
                type=msg_type,
                agent_id="analyst",
                content="test",
            )
            assert event.type == msg_type

        # Invalid type should raise error
        with pytest.raises(ValueError):
            AgentOutputEvent(
                type="invalid",
                agent_id="analyst",
                content="test",
            )


class TestParseWebSocketMessage:
    """Tests for parse_websocket_message function."""

    def test_parse_write_action_request(self):
        """Test parsing a write_action_request message."""
        data = {
            "type": "write_action_request",
            "id": "test-id-123",
            "agent_id": "analyst",
            "file_path": "/tmp/test.md",
            "operation": "create",
            "timestamp": "2026-02-13T15:30:00Z",
        }
        message = parse_websocket_message(data)
        assert isinstance(message, WriteActionRequest)
        assert message.id == "test-id-123"
        assert message.agent_id == "analyst"

    def test_parse_write_action_response(self):
        """Test parsing a write_action_response message."""
        data = {
            "type": "write_action_response",
            "id": "test-id-123",
            "approved": True,
            "timestamp": "2026-02-13T15:30:00Z",
        }
        message = parse_websocket_message(data)
        assert isinstance(message, WriteActionResponse)
        assert message.id == "test-id-123"
        assert message.approved is True

    def test_parse_agent_output_event(self):
        """Test parsing an AgentOutputEvent message."""
        data = {
            "type": "thinking",
            "agent_id": "analyst",
            "content": "Thinking...",
            "agent_color": "#10b981",
            "timestamp": "2026-02-13T15:30:00Z",
        }
        message = parse_websocket_message(data)
        assert isinstance(message, AgentOutputEvent)
        assert message.type == "thinking"
        assert message.agent_id == "analyst"
        assert message.agent_color == "#10b981"

    def test_parse_unknown_type(self):
        """Test parsing unknown message type raises error."""
        data = {"type": "unknown", "timestamp": "2026-02-13T15:30:00Z"}
        with pytest.raises(ValueError, match="Unknown WebSocket message type"):
            parse_websocket_message(data)