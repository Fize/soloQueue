"""
WebSocket message schemas for SoloQueue web UI enhancements.

Defines structured message formats for:
- Write-action confirmations (request/response)
- Agent output events with metadata
- Extended chat events with color and collapsible blocks
"""

import uuid
from datetime import datetime
from typing import Optional, Literal
from pydantic import BaseModel, Field


class BaseWebSocketMessage(BaseModel):
    """Base model for all WebSocket messages."""
    type: str
    timestamp: datetime = Field(default_factory=datetime.now)


class WriteActionRequest(BaseWebSocketMessage):
    """
    Request for user approval of a file write operation.

    Sent from server to client when an agent attempts a write operation.
    """
    type: Literal["write_action_request"] = "write_action_request"
    id: str = Field(default_factory=lambda: str(uuid.uuid4()))
    agent_id: str = Field(description="Agent requesting the write action")
    file_path: str = Field(description="Absolute or relative path (validated by sandbox)")
    operation: Literal["create", "update", "delete"] = Field(
        description="Type of file operation"
    )


class WriteActionResponse(BaseWebSocketMessage):
    """
    User response to a write-action request.

    Sent from client to server after user clicks Approve/Reject.
    """
    type: Literal["write_action_response"] = "write_action_response"
    id: str = Field(description="Matches the request ID")
    approved: bool = Field(description="Whether the user approved the operation")


class AgentOutputEvent(BaseWebSocketMessage):
    """
    Agent output with metadata for differentiated display.

    New message types:
    - stream: Agent streaming output (with stream_type: thinking/answer)
    - agent_status: Agent status notification (starting/completed/error)
    - tool_call: Tool invocation
    - tool_result: Tool execution result
    """
    type: Literal["stream", "agent_status", "tool_call", "tool_result", "error"] = Field(
        description="Type of output block"
    )
    agent_id: Optional[str] = Field(None, description="Agent that produced this output")
    content: Optional[str] = Field(None, description="The output text")

    # UI metadata
    agent_color: Optional[str] = Field(
        None,
        description="CSS color value for this agent (optional, from configuration)"
    )

    # stream type specific fields
    stream_type: Optional[Literal["thinking", "answer"]] = Field(
        None,
        description="For stream type: 'thinking' (reasoning) or 'answer' (actual response)"
    )

    # agent_status specific fields
    status: Optional[Literal["starting", "completed", "error"]] = Field(
        None,
        description="For agent_status type: current status of the agent"
    )
    message: Optional[str] = Field(
        None,
        description="Optional status message"
    )

    # tool specific fields
    tool_name: Optional[str] = Field(None, description="Name of the tool")
    tool_args: Optional[str] = Field(None, description="Tool arguments as string")

    # Legacy fields (kept for backward compatibility)
    preview_snippet: Optional[str] = Field(
        None,
        description="First 200 characters of content (for collapsed thinking blocks)"
    )
    collapsible: bool = Field(
        False,
        description="Whether this block can be collapsed"
    )
    collapsed_by_default: bool = Field(
        False,
        description="Whether this block should be collapsed when first displayed"
    )


# Union type for all possible WebSocket messages
WebSocketMessage = WriteActionRequest | WriteActionResponse | AgentOutputEvent


def parse_websocket_message(data: dict) -> WebSocketMessage:
    """
    Parse raw WebSocket message data into appropriate schema.

    Args:
        data: Dictionary parsed from JSON WebSocket message

    Returns:
        Parsed WebSocketMessage instance

    Raises:
        ValueError: If message type is unknown or validation fails
    """
    msg_type = data.get("type")

    if msg_type == "write_action_request":
        return WriteActionRequest(**data)
    elif msg_type == "write_action_response":
        return WriteActionResponse(**data)
    elif msg_type in ("stream", "agent_status", "tool_call", "tool_result", "error",
                      "thinking", "final_result"):  # Legacy support
        return AgentOutputEvent(**data)
    else:
        raise ValueError(f"Unknown WebSocket message type: {msg_type}")