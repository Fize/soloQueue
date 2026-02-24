# Research & Design Decisions

**Feature**: Web UI Enhancements for Agent Output & Interaction
**Date**: 2026-02-13
**Branch**: 001-web-ui-enhancements

## 1. Write‑Action Confirmation Delivery

**Decision**: Use WebSocket bidirectional communication for real‑time write‑action confirmations, falling back to HTTP long‑polling if WebSocket unavailable.

**Rationale**:
- WebSocket provides instant push notification to the UI when an agent requests approval.
- Existing SoloQueue web UI already uses WebSocket for task‑status updates; extending same channel maintains consistency.
- Fallback ensures usability in restrictive network environments (e.g., corporate proxies that block WebSocket).

**Alternatives considered**:
- **HTTP polling**: Would increase latency and server load; rejected because confirmations are time‑sensitive (user expects immediate dialog).
- **Server‑Sent Events (SSE)**: Simpler than WebSocket but unidirectional; cannot send user response (Approve/Reject) back on same channel. Requires separate POST endpoint, complicating architecture.

## 2. Agent Color Assignment

**Decision**: Use an optional `color` field in agent configuration (CSS color value). If omitted, assign a deterministic color from a predefined palette based on agent name hash.

**Rationale**:
- Allows users to customize colors for visual branding or accessibility.
- Deterministic fallback ensures consistent coloring across sessions (same agent → same color).
- Palette limited to 12 distinct, WCAG‑AA compliant colors to avoid ambiguity.

**Alternatives considered**:
- **Random assignment each session**: Causes confusion; rejected.
- **Color by agent role (leader, analyst, etc.)**: Too restrictive; roles are not always defined. Kept as optional semantic hint.

## 3. Collapsible Thinking‑Content UI

**Decision**: Implement collapsible sections using pure CSS (details/summary) with custom JavaScript for enhanced behavior (preview snippet, smooth transitions).

**Rationale**:
- Native `<details>`/`<summary>` provides accessibility and basic functionality without JavaScript.
- Custom JavaScript adds preview snippet (first 200 characters) when collapsed, smooth expand/collapse animations, and persistent state per session.
- Lightweight; does not introduce heavy frontend frameworks.

**Alternatives considered**:
- **React/Vue component**: Overkill for a single UI enhancement; would increase bundle size and diverge from existing Jinja2‑based templates.
- **jQuery**: Already used elsewhere in the codebase; acceptable but prefer vanilla JS to reduce dependency.

## 4. Artifact Storage Path Resolution

**Decision**: Store artifacts in `.soloqueue/memory/<agent_id>/artifacts/`, where `<agent_id>` is the agent’s unique `name` field.

**Rationale**:
- Aligns with existing memory‑architecture design (episodic/semantic memory stored under same directory).
- Keeps project root clean; groups all agent‑specific data together.
- Automatic directory creation (`mkdir -p`) ensures no‑fail writes.

**Alternatives considered**:
- **Project‑root subdirectory `artifacts/<agent_id>/`**: Would still clutter root; rejected.
- **SQLite blob storage**: Violates file‑driven architecture principle; rejected.

## 5. Font Size Increase Implementation

**Decision**: Increase base font size to 14px via CSS variable (`--base-font-size`) in the main stylesheet, affecting all dialogs and message panels.

**Rationale**:
- Single variable ensures consistency across all UI components.
- Relative units (`em`, `rem`) allow user browser zoom to work naturally.
- Meets WCAG 2.1 Level AA minimum size requirement (14px for standard text).

**Alternatives considered**:
- **Inline style updates per component**: Harder to maintain; rejected.
- **User‑configurable font‑size slider**: Out of scope for this feature; may be added later.

## 6. Duplicate Artifact Filename Handling

**Decision**: Append a timestamp (ISO 8601 basic format: `YYYYMMDDTHHMMSS`) to duplicate filenames, e.g., `report.md` → `report_20260213T142030.md`.

**Rationale**:
- Deterministic, sortable, human‑readable.
- Avoids overwriting existing artifacts without asking user (simpler than interactive resolution).
- Timestamp reflects creation time, useful for auditing.

**Alternatives considered**:
- **Sequential number (`report_2.md`)**: Less informative; rejected.
- **UUID suffix**: Unreadable; rejected.

## 7. Backward Compatibility

**Decision**: Terminal logging of write‑action requests remains unchanged; web UI confirmations are additive.

**Rationale**:
- Users who rely on terminal logs (e.g., scripting, auditing) are not affected.
- No breaking changes to existing APIs or CLI behavior.

**Alternatives considered**:
- **Replace terminal prompts with web‑only**: Would break existing workflows; rejected.

## Summary

All design decisions align with the SoloQueue constitution (file‑driven, security‑first, Unix‑philosophy) and leverage existing technology choices (FastAPI, WebSocket, Jinja2, vanilla JS). No new heavyweight dependencies introduced.