# Feature Specification: Web UI Enhancements for Agent Output & Interaction

**Feature Branch**: `001-web-ui-enhancements`
**Created**: 2026-02-13
**Status**: Draft
**Input**: User description: "当前项目有几个问题。1.写入动作的确认仍然在终端而不是在web端。2.agent的输出内容在web端是混合在一块输出，而不是区分agent区分工具调用区分思考内容，应该将这些内容区分，思考内容应该输出完成后折叠，不同agent应该区分不同的颜色。3.web端对话框中的字体太小了。3.现在agent在输出报告制品时仍然在根目录而不是它自己的memory记忆目录"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Write Action Confirmation in Web UI (Priority: P1)

As a SoloQueue user monitoring agent activity through the web interface, I want write‑action confirmations to appear directly in the web UI instead of only in the terminal, so that I can approve or reject file‑modification requests without switching contexts.

**Why this priority**: Write actions (file creation, modification, deletion) are security‑sensitive operations; placing the confirmation in the web UI centralizes control, improves user experience, and aligns with the project’s security‑first principle.

**Independent Test**: Can be fully tested by triggering a write‑capable agent (e.g., one that uses the `write_file` tool) and verifying that a confirmation dialog appears in the web UI, not just in the terminal logs.

**Acceptance Scenarios**:

1. **Given** an agent is about to perform a write operation (e.g., create a report file)
   **When** the agent requests user approval
   **Then** a clear confirmation dialog appears in the web UI showing the file path, operation type, and agent name, with Approve/Reject buttons.

2. **Given** a write‑action confirmation is displayed in the web UI
   **When** the user clicks “Approve”
   **Then** the agent proceeds with the write operation and the UI shows a success message.

3. **Given** a write‑action confirmation is displayed in the web UI
   **When** the user clicks “Reject”
   **Then** the agent cancels the write operation and the UI shows a cancellation message.

---

### User Story 2 - Differentiated Agent Output with Collapsible Sections (Priority: P1)

As a SoloQueue user observing multi‑agent collaboration, I want agent outputs in the web UI to be visually separated by agent, tool call, and thinking content, with thinking content collapsible after output and different agents colored distinctly, so that I can easily follow the collaboration flow and focus on relevant details.

**Why this priority**: Clear visual separation is essential for debugging and understanding complex agent interactions; collapsible thinking content reduces clutter while preserving auditability.

**Independent Test**: Can be tested by running a multi‑agent task (e.g., the investment‑analysis team) and verifying that the web UI shows separate visual blocks for each agent, tool calls, and thinking content, with appropriate color coding and collapse controls.

**Acceptance Scenarios**:

1. **Given** an agent generates output that includes thinking content, tool calls, and final results
   **When** the output is displayed in the web UI
   **Then** thinking content appears in a distinct, collapsible section that is collapsed by default; tool calls are shown in a separate block; and final results are presented in a persistent block.

2. **Given** multiple agents (leader, analyst, trader) are active in the same task
   **When** their outputs appear in the web UI
   **Then** each agent’s output block uses a unique, consistent color (e.g., leader‑blue, analyst‑green, trader‑orange) for easy visual identification.

3. **Given** a thinking‑content section is displayed
   **When** the user clicks a collapse/expand control
   **Then** the thinking content collapses/expands smoothly, preserving screen space while keeping the section header visible.

---

### User Story 3 - Improved Web Dialog Font Size (Priority: P2)

As a SoloQueue user spending extended time monitoring the web interface, I want dialog and message text to be larger and more readable, so that I can work comfortably without eye strain.

**Why this priority**: Readability directly affects user satisfaction and productivity; a too‑small font is an accessibility issue that can lead to errors or fatigue.

**Independent Test**: Can be tested by opening any web UI dialog (e.g., task submission, configuration panel) and verifying that the base font size meets minimum readability standards.

**Acceptance Scenarios**:

1. **Given** the user opens any dialog or message panel in the web UI
   **When** the content is displayed
   **Then** the base font size is at least 14px (or equivalent relative unit) for comfortable reading on standard desktop displays.

2. **Given** the user views agent output or logs in the main interface
   **When** the text is rendered
   **Then** monospace/code sections maintain clear distinction but are still legible at the increased base size.

---

### User Story 4 - Agent Artifacts Stored in Memory Directory (Priority: P3)

As a SoloQueue user reviewing agent‑generated artifacts (reports, analysis outputs), I want those artifacts to be stored in the agent’s own memory directory rather than the project root, so that the project structure remains clean and artifacts are logically organized with the agent’s other memory data.

**Why this priority**: Proper artifact organization supports the file‑driven architecture principle and makes it easier to locate, archive, or delete agent‑specific outputs.

**Independent Test**: Can be tested by triggering an agent that produces a report or other artifact and verifying that the file is saved under `.soloqueue/memory/<agent_id>/artifacts/` instead of the project root.

**Acceptance Scenarios**:

1. **Given** an agent creates a report or output artifact
   **When** the artifact is saved
   **Then** it is placed in the directory `.soloqueue/memory/<agent_id>/artifacts/` (creating the directory if needed).

2. **Given** multiple agents produce artifacts in the same task
   **When** the artifacts are saved
   **Then** each artifact is stored under its respective agent’s memory directory, preserving a clear ownership trail.

---

### Edge Cases

- What happens when two agents have similar or identical assigned colors? (System must guarantee distinct colors, e.g., via a deterministic palette rotation.)
- How does the system handle extremely long thinking content? (Collapsible section should show a reasonable preview when collapsed, e.g., first 200 characters visible, with “Show more” option to expand fully.)
- What if the artifact storage directory already contains a file with the same name? (Use timestamp‑based uniqueness or ask user; default to timestamp‑appended filename.)
- How does the UI behave when the user is offline and a write confirmation appears? (Confirmation should remain pending until connection is restored; show a “waiting for connection” status.)

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST display write‑action confirmation dialogs in the web UI, not only in the terminal.
- **FR-002**: System MUST separate agent output into distinct visual blocks for thinking content, tool calls, and final results.
- **FR-003**: System MUST make thinking‑content blocks collapsible/expandable via user‑clickable controls.
- **FR-004**: System MUST assign a unique, consistent color to each agent's output blocks, preferring a `color` field defined in the agent's configuration; if no color is specified, use a default color from a predefined palette.
- **FR-005**: System MUST increase the base font size of all dialogs and message panels to at least 14px (or equivalent relative unit).
- **FR-006**: System MUST store agent‑generated artifacts (reports, output files) in the directory `.soloqueue/memory/<agent_id>/artifacts/` instead of the project root.
- **FR-007**: System MUST create the artifact directory automatically if it does not exist.
- **FR-008**: System MUST handle duplicate artifact filenames by appending a timestamp or sequence number to avoid overwrites.
- **FR-009**: System MUST support an optional `color` field in agent configuration files (accepting CSS color values, e.g., hex codes or named colors).
- **FR-010**: System MUST NOT impose a timeout on write‑action confirmation dialogs; the agent must wait indefinitely for the user's explicit approval or rejection.
- **FR-011**: System MUST display thinking‑content sections collapsed (folded) by default when first shown, requiring the user to expand them to view the full content.
- **FR-012**: System MUST show a preview snippet (e.g., first 200 characters) of the thinking content even when the section is collapsed, providing context without requiring expansion.
- **FR-013**: System MUST display file path, operation type, and agent name in write‑action confirmation dialogs.

*No unclear requirements – all are testable with the assumptions documented below.*

### Key Entities *(include if feature involves data)*

- **Agent Output Stream**: A chronological sequence of messages from a single agent, containing thinking content, tool‑call requests/responses, and final results.
- **Artifact**: Any file produced by an agent as a result of its work (e.g., analysis report, generated code, data summary).
- **Agent Memory Directory**: A directory under `.soloqueue/memory/` uniquely named after the agent ID, containing the agent’s episodic memory, semantic memory, and artifact storage.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Users can approve/reject write actions entirely within the web UI within 2 seconds of the request appearing (no terminal interaction required).
- **SC-002**: 100% of agent output blocks in the web UI are visually distinguishable by agent role (via color) and content type (thinking vs. tool call vs. result) with zero ambiguity in a user test with 5 participants.
- **SC-003**: 90% of users in a usability test successfully collapse/expand thinking content on first attempt without guidance.
- **SC-004**: All web UI dialogs and message panels meet WCAG 2.1 Level AA contrast and size requirements for readability (verified by automated accessibility scan).
- **SC-005**: 100% of agent‑generated artifacts are stored under the correct agent memory directory, with zero artifacts mistakenly placed in the project root after 50 test task runs.
- **SC-006**: Task‑completion time for monitoring a multi‑agent collaboration decreases by at least 30% compared to the previous mixed‑output UI (measured via timed user scenarios).

---

## Clarifications

### Session 2026-02-13
- Q: What attribute or configuration field should be used to determine an agent's "type" for color assignment? → A: Agent config should include a `color` field; if not provided, use a default color from a predefined palette.
- Q: Should the confirmation dialog have a timeout after which the write operation is automatically rejected or cancelled? → A: No timeout – wait indefinitely for user response.
- Q: Should thinking‑content sections appear collapsed (folded) by default when first displayed, or expanded? → A: Collapsed by default (user must expand to see content).
- Q: Should the preview snippet be visible even when the thinking‑content section is collapsed, or should the collapsed state show only the header/control with no content visible? → A: Preview snippet (e.g., first 200 chars) visible when collapsed.
- Q: What specific information must the write‑action confirmation dialog display to the user? → A: File path + operation type + agent name.


## Assumptions & Notes

1. **Color Assignment**: Agent configuration files may include an optional `color` field (CSS color value); if omitted, the system auto‑assigns a distinct color from a predefined palette. Custom color mapping is now in scope via this configuration field.
2. **Font Size**: Base font size will be increased to 14px for all UI text; further user‑adjustable zoom is not required but may be added as a future enhancement.
3. **Artifact Directory Structure**: The path `.soloqueue/memory/<agent_id>/artifacts/` aligns with the existing memory‑architecture design; existing artifacts in the root will not be automatically moved.
4. **Backward Compatibility**: The write‑action confirmation in the web UI does not remove terminal logging; both channels remain active for auditing purposes.
5. **Performance Impact**: The UI enhancements must not increase page‑load time by more than 10% (measured on a representative task page).