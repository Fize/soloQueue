# Quick Start: Web UI Enhancements

This guide outlines the implementation steps for the Web UI enhancements feature.

## Prerequisites

- SoloQueue codebase (Python 3.11+, FastAPI, existing web UI)
- Understanding of SoloQueue architecture (file‑driven, agent delegation, memory directories)
- Familiarity with:
  - FastAPI WebSocket endpoints
  - Jinja2 templating and static assets (CSS/JavaScript)
  - Pydantic configuration loading

## Core Changes

### 1. Agent Configuration (`color` field)

**File**: `config/agents/*.md` (Markdown frontmatter)

Add an optional `color` field to agent configuration files. The field accepts CSS color values (hex, named, rgb). If omitted, the UI will assign a deterministic color from a predefined palette.

Example addition to frontmatter:

```yaml
---
name: analyst
description: Fundamental analysis specialist
group: investment
model: deepseek-reasoner
reasoning: true
color: "#10b981"  # Optional
tools: [web_search, write_file]
---
```

**Implementation**: Update `AgentSchema` in `src/soloqueue/core/loaders/agent.py` to include an optional `color` field (type `str | None`). The loader should preserve the field.

### 2. Write‑Action Confirmations (WebSocket)

**Endpoint**: New WebSocket endpoint `/ws/write‑action` (or extend existing `/ws/chat`).

**Flow**:
1. When an agent requests approval for a file write (via `write_file` tool), the system sends a `write_action_request` message over the WebSocket connection.
2. The web UI displays a dialog with file path, operation type, agent name, and Approve/Reject buttons.
3. User clicks either button; the UI sends a `write_action_response` message with the same `id` and `approved` boolean.
4. The agent proceeds or cancels the operation based on the response.

**Message formats** (JSON):
- Request: `{"type": "write_action_request", "id": "<uuid>", "agent_id": "<name>", "file_path": "...", "operation": "create|update|delete", "timestamp": "..."}`
- Response: `{"type": "write_action_response", "id": "<uuid>", "approved": true|false, "timestamp": "..."}`

**Implementation**:
- Create a new WebSocket route in `src/soloqueue/web/app.py` (or extend existing chat endpoint).
- Integrate with the tool‑approval system (`src/soloqueue/core/tools/approval.py`).
- Ensure fallback to terminal logging when web UI is not connected.

### 3. Differentiated Agent Output (Frontend)

**Frontend changes**:
- Modify the template that renders agent output (likely `chat.html` or a dedicated output panel) to separate thinking content, tool calls, and final results into distinct visual blocks.
- Assign each agent a unique color (from `agent.color` or deterministic palette) and apply it as a border/background to the agent’s output blocks.
- Make thinking‑content blocks collapsible using `<details>`/`<summary>` with custom JavaScript for preview snippets (first 200 characters) and smooth animations.
- Collapse thinking content by default; show preview snippet when collapsed.

**Implementation**:
- Update Jinja2 templates in `src/soloqueue/web/templates/` to include CSS classes for block types (`thinking‑block`, `tool‑call‑block`, `result‑block`).
- Add JavaScript in `src/soloqueue/web/static/js/` to handle expand/collapse, persist state per session.
- Extend the WebSocket event format (already used by `/ws/chat`) to include metadata: `agent_color`, `block_type`, `collapsible`, `preview_snippet`.

### 4. Font Size Increase

**File**: `src/soloqueue/web/static/css/main.css` (or equivalent)

Increase the base font size to 14px by setting a CSS custom property:

```css
:root {
  --base-font-size: 14px;
}
```

Use `rem` units throughout the UI where appropriate. Ensure dialogs and message panels inherit this size.

### 5. Artifact Storage

**Directory**: `.soloqueue/memory/<agent_id>/artifacts/`

**Implementation**:
- Modify the artifact‑writing logic (likely in `src/soloqueue/core/tools/write_file.py` or similar) to resolve the target path as `.soloqueue/memory/<agent_id>/artifacts/`.
- Create the directory if it does not exist (`mkdir -p`).
- If a file with the same name already exists, append a timestamp: `<basename>_YYYYMMDDTHHMMSS.<ext>`.
- Record artifact metadata (filename, stored_path, size, timestamp) in the agent’s episodic memory (JSON sidecar).
- Provide REST endpoints for listing and downloading artifacts (see API contracts).

### 6. REST Endpoints for Artifacts

Implement the endpoints defined in `contracts/openapi.yaml`:

- `GET /api/agents/{agent_id}/artifacts` – list artifacts
- `GET /api/agents/{agent_id}/artifacts/{filename}` – download artifact

These endpoints should validate that the requested agent exists and that the artifact path is within the sandboxed memory directory.

## Testing Checklist

- [ ] Unit tests for `color` field loading (AgentSchema)
- [ ] Unit tests for artifact path resolution and duplicate handling
- [ ] Integration tests for write‑action WebSocket flow (approve/reject)
- [ ] Integration tests for artifact REST endpoints
- [ ] Frontend tests for collapsible thinking content and color coding
- [ ] Accessibility test (WCAG 2.1 AA) for increased font size and color contrast

## Deployment Notes

- Backward compatible: terminal logging of write‑action requests remains unchanged.
- Existing agent configurations continue to work (missing `color` field falls back to palette).
- Existing artifacts in the project root are **not** automatically moved; new artifacts will be stored in the new location.