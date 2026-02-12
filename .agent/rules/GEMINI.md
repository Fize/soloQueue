# AGENT.md - Project Governance & AI Rules (Production)

**Vision:** SoloQueue is a production-ready, multi-agent swarm system designed to enable the "One-Person Company." Similar to OpenClaw but architected as a decentralized swarm, it empowers a single user to orchestrate complex software development and operational tasks through autonomous agent collaboration.

This file defines the strict protocols, coding standards, and architectural constraints that ALL AI Agents (including Gemini/Claude) must follow when contributing to **SoloQueue**.

---

## 1. Core Philosophy

1.  **One-Person Company:** The system must maximize leverage for a single operator. Automation is not just a feature; it is the primary directive.
2.  **Swarm Intelligence:** Agents are specialized, autonomous units that collaborate. Avoid monolithic logic; favor distributed, role-based orchestration.
3.  **Production First:** Code is not a prototype. It must be robust, error-tolerant, and observable. "It works on my machine" is not acceptable.
4.  **Unix Philosophy:** Functions should do one thing and do it well. Everything is a file.
5.  **No-DB First:** Avoid SQLite/Postgres unless absolutely necessary for concurrency or huge datasets. Use JSONL/Filesystem for state.
6.  **Security by Design:** All file operations MUST go through `WorkspaceManager`. All dangerous shell commands MUST pass `ApprovalManager`.
7.  **Configuration as Code:** All agent behaviors and skills must be defined in Markdown/YAML files, not hardcoded in Python.

---

## 2. Coding Standards (Python)

### 2.1 Style Guide
*   **Version:** Python 3.11+
*   **Typing:** Strict type hints are **MANDATORY**. Use `typing.TypedDict`, `typing.Protocol`, `typing.Optional`.
*   **Imports:** Clean imports ONLY. No unused imports allowed. Use `ruff` to auto-remove them.
*   **Deprecation:** Do NOT use deprecated library features (e.g., Pydantic v1 style `class Config`). Use modern equivalents.
*   **Docstrings:** Google Style Docstrings for all public functions/classes.
*   **Formatter:** Follow `ruff` / `black` defaults.

### 2.2 Project Structure
*   **Source:** `src/soloqueue/`
*   **Tests:** `tests/` (Mirror source structure)
*   **Configs:** `config/` (NO Python files here, only YAML/MD)

### 2.3 Error Handling
*   **Never fail silently.** Catch specific exceptions and re-raise custom exceptions (e.g., `PrimitiveError`).
*   **Structured Logging:** Use `loguru` with context binding (`agent_id`, `trace_id`). Do NOT use `print()`.

---

## 3. Architecture Constraints

### 3.1 Layered Dependency Rule (Strict)
*   **Layer 3 (Interface)** CAN import **Layer 2 (Orchestration)**.
*   **Layer 2 (Orchestration)** CAN import **Layer 1 (Core/Infra)**.
*   **Layer 1 (Core)** MUST NOT import from Layer 2 or 3.
*   *Violation of this rule will cause circular dependencies and spaghetti code.*

### 3.2 Part 1: Infrastructure (The Nexus)
*   **Primitives:** MUST encompass all IO/OS interactions.
*   **Loaders:** MUST use `pydantic` for schema validation of YAML/MD files.
*   **Workspace:** ALL file paths must be resolved via `WorkspaceManager.resolve_path()`.

### 3.3 Part 2: Orchestration
*   **Communication:** Via LangGraph State or standard Python method calls.
*   **No Global State:** Avoid mutable global variables. Pass state explicitly.

---

## 4. Operational Protocols

1.  **Test First:** Create a test file (e.g., `tests/core/test_workspace.py`) BEFORE or WITH implementation.
2.  **Verify:** Always run the test after writing code.
3.  **Incremental:** Implement one module (e.g., `logging`) at a time. Do not scaffold empty files.
4.  **Doc Update:** If implementation diverges from `doc/*.md`, UPDATE THE DOC immediately.
5.  **User Confirmation:** Do not implement any code changes until the user confirms the design meets requirements.

---

## 5. Artifact Management

*   **Agent Definitions:** `config/agents/*.md` (Markdown + YAML Frontmatter).
*   **Skill Definitions:** `skills/*/SKILL.md`.
*   **Logs:** `logs/*.jsonl` (Git ignored).

---

**CRITICAL:** Before generating any code, review this file to ensure compliance.
