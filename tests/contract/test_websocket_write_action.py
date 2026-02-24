"""
Contract test for WebSocket write-action endpoint.

Validates that message formats conform to OpenAPI specification.
"""

import yaml
import json
import pytest
from pathlib import Path

from src.soloqueue.web.websocket.schemas import (
    WriteActionRequest,
    WriteActionResponse,
    AgentOutputEvent,
)


def load_openapi_spec():
    """Load the OpenAPI specification from contracts/openapi.yaml."""
    spec_path = Path(__file__).parent.parent.parent / "specs" / "001-web-ui-enhancements" / "contracts" / "openapi.yaml"
    with open(spec_path, "r") as f:
        return yaml.safe_load(f)


class TestWebSocketWriteActionContract:
    """Contract tests for WebSocket write-action endpoint."""

    @classmethod
    def setup_class(cls):
        """Load OpenAPI specification."""
        cls.openapi = load_openapi_spec()
        cls.schemas = cls.openapi.get("components", {}).get("schemas", {})

    def test_write_action_request_schema_matches_openapi(self):
        """Test that WriteActionRequest schema matches OpenAPI definition."""
        schema = self.schemas.get("WriteActionRequest")
        assert schema is not None, "WriteActionRequest schema not found in OpenAPI"

        # Check required fields
        required_fields = set(schema.get("required", []))
        expected_required = {"type", "id", "agent_id", "file_path", "operation", "timestamp"}
        assert required_fields == expected_required, f"Required fields mismatch: {required_fields}"

        # Check type enum
        type_spec = schema["properties"]["type"]
        assert type_spec.get("enum") == ["write_action_request"]

        # Check operation enum
        operation_spec = schema["properties"]["operation"]
        assert operation_spec.get("enum") == ["create", "update", "delete"]

        # Verify we can create a request that matches the schema
        request = WriteActionRequest(
            agent_id="test-agent",
            file_path="/tmp/test.md",
            operation="create",
        )
        data = request.model_dump()

        # Validate required fields present
        for field in required_fields:
            assert field in data, f"Missing required field: {field}"

        # Validate field types
        assert isinstance(data["agent_id"], str)
        assert isinstance(data["file_path"], str)
        assert data["operation"] in ("create", "update", "delete")
        assert data["type"] == "write_action_request"

    def test_write_action_response_schema_matches_openapi(self):
        """Test that WriteActionResponse schema matches OpenAPI definition."""
        schema = self.schemas.get("WriteActionResponse")
        assert schema is not None, "WriteActionResponse schema not found in OpenAPI"

        # Check required fields
        required_fields = set(schema.get("required", []))
        expected_required = {"type", "id", "approved", "timestamp"}
        assert required_fields == expected_required, f"Required fields mismatch: {required_fields}"

        # Check type enum
        type_spec = schema["properties"]["type"]
        assert type_spec.get("enum") == ["write_action_response"]

        # Verify we can create a response that matches the schema
        response = WriteActionResponse(
            id="test-id-123",
            approved=True,
        )
        data = response.model_dump()

        # Validate required fields present
        for field in required_fields:
            assert field in data, f"Missing required field: {field}"

        # Validate field types
        assert isinstance(data["id"], str)
        assert isinstance(data["approved"], bool)
        assert data["type"] == "write_action_response"

    def test_agent_output_event_schema_matches_openapi(self):
        """Test that AgentOutputEvent schema matches OpenAPI definition."""
        schema = self.schemas.get("AgentOutputEvent")
        assert schema is not None, "AgentOutputEvent schema not found in OpenAPI"

        # Check required fields
        required_fields = set(schema.get("required", []))
        expected_required = {"type", "agent_id", "content", "timestamp"}
        assert required_fields == expected_required, f"Required fields mismatch: {required_fields}"

        # Check type enum
        type_spec = schema["properties"]["type"]
        assert type_spec.get("enum") == ["thinking", "tool_call", "tool_result", "final_result"]

        # Verify we can create an event that matches the schema
        event = AgentOutputEvent(
            type="thinking",
            agent_id="test-agent",
            content="Thinking...",
        )
        data = event.model_dump()

        # Validate required fields present
        for field in required_fields:
            assert field in data, f"Missing required field: {field}"

        # Validate field types
        assert isinstance(data["agent_id"], str)
        assert isinstance(data["content"], str)
        assert data["type"] in ("thinking", "tool_call", "tool_result", "final_result")

        # Optional fields should be present (nullable)
        optional_fields = {"agent_color", "preview_snippet", "collapsible", "collapsed_by_default"}
        for field in optional_fields:
            assert field in schema["properties"], f"Missing optional field in schema: {field}"

    def test_openapi_paths_include_websocket_endpoint(self):
        """Test that OpenAPI includes documentation for WebSocket endpoint."""
        paths = self.openapi.get("paths", {})
        # The WebSocket endpoint is documented as a path with description
        ws_path = paths.get("/ws/write-action")
        assert ws_path is not None, "WebSocket endpoint /ws/write-action not documented in OpenAPI"
        assert "description" in ws_path, "Missing description for WebSocket endpoint"

    def test_artifact_schemas_exist(self):
        """Test that artifact-related schemas exist in OpenAPI."""
        required_schemas = {"Artifact", "ArtifactList", "Error"}
        for schema_name in required_schemas:
            assert schema_name in self.schemas, f"Missing schema: {schema_name}"

    def test_schema_consistency(self):
        """Test consistency between OpenAPI schemas and Pydantic models."""
        # WriteActionRequest
        request = WriteActionRequest(
            agent_id="test",
            file_path="/tmp/test.md",
            operation="create",
        )
        assert request.type == "write_action_request"

        # WriteActionResponse
        response = WriteActionResponse(id="test", approved=True)
        assert response.type == "write_action_response"

        # AgentOutputEvent
        event = AgentOutputEvent(
            type="thinking",
            agent_id="test",
            content="test",
        )
        assert event.type in ["thinking", "tool_call", "tool_result", "final_result"]