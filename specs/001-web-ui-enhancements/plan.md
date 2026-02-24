# Implementation Plan: Web UI Enhancements for Agent Output & Interaction

**Branch**: `001-web-ui-enhancements` | **Date**: 2026-02-13 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/001-web-ui-enhancements/spec.md`

**Note**: This template is filled in by the `/speckit.plan` command. See `.specify/templates/commands/plan.md` for the execution workflow.

## Summary

Enhance the SoloQueue web interface to improve user experience, security, and observability of multi‑agent collaboration. Four key improvements:

1. **Write‑action confirmations in web UI** – Move security‑sensitive file‑write approvals from terminal to the web interface, displaying file path, operation type, and agent name with Approve/Reject buttons.
2. **Differentiated agent output** – Separate thinking content, tool calls, and final results into distinct visual blocks; make thinking content collapsible (collapsed by default with a preview snippet) and assign unique colors per agent (configurable via `color` field).
3. **Improved font size** – Increase base font size to at least 14px across all dialogs and message panels for better readability.
4. **Organized artifact storage** – Store agent‑generated artifacts (reports, output files) in `.soloqueue/memory/<agent_id>/artifacts/` instead of the project root, with automatic directory creation and duplicate‑filename handling.

**Technical approach**: Extend the existing FastAPI web server and frontend templates (HTML/JavaScript) to support real‑time confirmation dialogs, structured output rendering, and updated artifact‑storage logic. Agent configuration schema gains an optional `color` field (CSS color value). No database changes required; all enhancements operate within the existing file‑driven architecture.

## Technical Context

**Language/Version**: Python 3.11+ (as per constitution)
**Primary Dependencies**: FastAPI (web server), Pydantic (configuration validation), Loguru (structured logging), Ruff (formatting/linting), SQLite (optional ephemeral state)
**Storage**: File‑based (core architecture); SQLite permitted for transient caches. Artifacts stored under `.soloqueue/memory/<agent_id>/artifacts/`.
**Testing**: pytest (unit/integration/contract tests), test‑first discipline required.
**Target Platform**: Linux server (web UI accessible via browser; CLI remains).
**Project Type**: Web application (enhancements to existing FastAPI‑based web interface).
**Performance Goals**: Web UI responsive under 500ms (constitution); page‑load time increase ≤10% (spec assumption); write‑action confirmation dialog appears within 2 seconds (SC‑001).
**Constraints**: Must preserve backward compatibility (terminal logging remains); security sandboxing and user‑approval gates enforced; all file paths validated/sanitized; `REQUIRE_APPROVAL=true` respected.
**Scale/Scope**: Single‑deployment internal tool; typical concurrent users <10; 10+ agent threads supported (constitution).

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

**✅ All constitution principles satisfied.** No violations detected.

| Principle | Compliance |
|-----------|------------|
| **I. File‑Driven Architecture** | Feature enhances web UI without introducing databases; artifact storage remains file‑based. |
| **II. Recursive Agent Delegation** | No changes to agent delegation logic; UI only displays output. |
| **III. Unix Philosophy & Compatibility** | Web UI is optional; CLI remains intact; agent config compatibility preserved (adds optional `color` field). |
| **IV. Security & Sandboxing** | Write‑action confirmations moved to web UI (enhances security); user‑approval gates respected; path validation required. |
| **V. Testing & Observability** | All UI changes must be covered by tests (unit/integration); structured logging remains. |

**Gates passed** – Proceed to Phase 0 research.

## Project Structure

### Documentation (this feature)

```text
specs/001-web-ui-enhancements/
├── plan.md              # This file (/speckit.plan command output)
├── research.md          # Phase 0 output (/speckit.plan command)
├── data-model.md        # Phase 1 output (/speckit.plan command)
├── quickstart.md        # Phase 1 output (/speckit.plan command)
├── contracts/           # Phase 1 output (/speckit.plan command)
└── tasks.md             # Phase 2 output (/speckit.tasks command - NOT created by /speckit.plan)
```

### Source Code (repository root)

```text
config/
├── agents/              # Agent definition files (add optional `color` field)
└── groups/

src/soloqueue/
├── web/                 # FastAPI app, templates, static files
│   ├── app.py          # WebSocket endpoints for real‑time confirmations
│   ├── templates/      # HTML templates (output rendering)
│   └── static/         # JavaScript/CSS for collapsible sections, color coding
├── core/                # Core infrastructure
│   ├── loaders/        # Agent configuration loader (parse `color` field)
│   ├── memory/         # Artifact storage logic
│   └── tools/          # Write‑action approval handling
└── orchestration/      # Agent output streaming
    └── state.py        # Task state updates for UI

tests/
├── unit/               # Unit tests for new UI components
├── integration/        # Integration tests for web UI flows
└── contract/          # Contract tests for API endpoints

.soloqueue/memory/      # Runtime artifact storage (created automatically)
└── <agent_id>/
    └── artifacts/      # Agent‑generated files (new location)
```

**Structure Decision**: Single‑project structure (existing SoloQueue codebase). The feature modifies:
- `config/agents/` – Add optional `color` field to agent definitions.
- `src/soloqueue/web/` – Extend FastAPI app with WebSocket confirmations, update templates/static for differentiated output.
- `src/soloqueue/core/` – Enhance configuration loader and artifact‑storage logic.
- `src/soloqueue/orchestration/` – Stream agent output with metadata for UI.
- `tests/` – Add unit/integration/contract tests for new UI behaviors.
- `.soloqueue/memory/<agent_id>/artifacts/` – New artifact storage location.

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**
