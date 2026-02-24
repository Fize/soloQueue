---
description: "Task list for Web UI Enhancements feature implementation"
---

# Tasks: Web UI Enhancements for Agent Output & Interaction

**Input**: Design documents from `/specs/001-web-ui-enhancements/`
**Prerequisites**: plan.md (required), spec.md (required for user stories), data-model.md, contracts/, quickstart.md

**Tests**: Tests are OPTIONAL - included based on quickstart.md testing checklist and constitution requirement V (Testing & Observability).

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- **Single project**: `src/`, `tests/` at repository root (existing SoloQueue codebase)
- **Paths**: Based on project structure in plan.md

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure for this feature

- [x] T001 Create feature branch and verify existing SoloQueue codebase structure
- [x] T002 [P] Review existing web UI components in `src/soloqueue/web/` to understand current implementation
- [x] T003 [P] Review existing agent configuration loader in `src/soloqueue/core/loaders/schema.py` and `src/soloqueue/core/loaders/agent_loader.py`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [x] T004 Update AgentSchema to include optional `color` field in `src/soloqueue/core/loaders/schema.py`
- [x] T005 [P] Create deterministic color palette utility in `src/soloqueue/web/utils/colors.py`
- [x] T006 [P] Add CSS custom property for base font size in `src/soloqueue/web/static/css/style.css`
- [x] T007 Extend WebSocket message format schema in `src/soloqueue/web/websocket/schemas.py` (create directory if needed)
- [x] T008 Create WebUIApproval backend class in `src/soloqueue/core/security/webui_approval.py`
- [x] T009 Integrate WebUIApproval with approval manager in `src/soloqueue/core/security/approval.py`

**Checkpoint**: Foundation ready - user story implementation can now begin in parallel

---

## Phase 3: User Story 1 - Write Action Confirmation in Web UI (Priority: P1) 🎯 MVP

**Goal**: Move security-sensitive file-write approvals from terminal to web UI with confirmation dialogs

**Independent Test**: Trigger a write-capable agent and verify confirmation dialog appears in web UI (not just terminal)

### Tests for User Story 1

> **NOTE: Write these tests FIRST, ensure they FAIL before implementation**

- [x] T010 [P] [US1] Unit test for write-action WebSocket message parsing in `tests/web/test_websocket_write_action.py`
- [x] T011 [P] [US1] Integration test for write-action confirmation flow in `tests/integration/test_write_action_flow.py`
- [x] T012 [P] [US1] Contract test for WebSocket endpoint in `tests/contract/test_websocket_write_action.py` (create contract directory)

### Implementation for User Story 1

- [x] T013 [P] [US1] Create WebSocket endpoint `/ws/write-action` in `src/soloqueue/web/app.py`
- [x] T014 [US1] Implement write-action request handler in `src/soloqueue/core/security/webui_approval.py`
- [x] T015 [US1] Add WebSocket message handling for write-action responses in `src/soloqueue/web/websocket/handlers.py` (or extend app.py)
- [x] T016 [P] [US1] Create confirmation dialog UI component in `src/soloqueue/web/templates/components/confirmation_dialog.html`
- [ ] T017 [P] [US1] Add JavaScript for dialog interaction in `src/soloqueue/web/static/js/confirmation_dialog.js`
- [ ] T018 [US1] Integrate fallback to terminal logging when web UI not connected in `src/soloqueue/core/security/webui_approval.py`
- [ ] T019 [US1] Update tool execution to use web UI confirmation in `src/soloqueue/core/primitives/file_io.py` (write_file function)

**Checkpoint**: At this point, User Story 1 should be fully functional and testable independently

---

## Phase 4: User Story 2 - Differentiated Agent Output with Collapsible Sections (Priority: P1)

**Goal**: Separate agent output into visual blocks by agent, tool call, and thinking content with collapsible sections and color coding

**Independent Test**: Run multi-agent task and verify web UI shows separate visual blocks with color coding and collapse controls

### Tests for User Story 2

- [ ] T020 [P] [US2] Unit test for agent color assignment and color field loading in `tests/web/test_colors.py`
- [ ] T021 [P] [US2] Integration test for differentiated output rendering in `tests/integration/test_differentiated_output.py`
- [ ] T022 [P] [US2] Frontend test for collapsible sections in `tests/frontend/test_collapsible_sections.js` (if frontend testing setup exists)

### Implementation for User Story 2

- [ ] T023 [P] [US2] Update agent output template to separate block types in `src/soloqueue/web/templates/chat.html` (or appropriate template)
- [ ] T024 [P] [US2] Add CSS classes for block types in `src/soloqueue/web/static/css/blocks.css`
- [ ] T025 [US2] Extend WebSocket event format with metadata (agent_color, block_type, collapsible, preview_snippet) in `src/soloqueue/web/websocket/schemas.py`
- [ ] T026 [US2] Implement collapsible thinking content with preview snippet in `src/soloqueue/web/templates/components/thinking_block.html`
- [ ] T027 [P] [US2] Add JavaScript for expand/collapse functionality in `src/soloqueue/web/static/js/collapsible_blocks.js`
- [ ] T028 [US2] Apply agent colors to output blocks using `agent_color` from configuration in `src/soloqueue/web/templates/components/agent_block.html`
- [ ] T029 [US2] Add persistence for collapsed/expanded state per session in `src/soloqueue/web/static/js/session_state.js`
- [ ] T030 [US2] Update agent output streaming to include metadata in `src/soloqueue/web/app.py` (websocket chat endpoint)

**Checkpoint**: At this point, User Stories 1 AND 2 should both work independently

---

## Phase 5: User Story 3 - Improved Web Dialog Font Size (Priority: P2)

**Goal**: Increase base font size to at least 14px across all dialogs and message panels for better readability

**Independent Test**: Open any web UI dialog and verify base font size meets minimum readability standards

### Tests for User Story 3

- [ ] T031 [P] [US3] Accessibility test for font size and contrast in `tests/accessibility/test_font_size.py`
- [ ] T032 [P] [US3] CSS validation test in `tests/frontend/test_css_properties.py`

### Implementation for User Story 3

- [ ] T033 [US3] Update CSS to use `--base-font-size: 14px` custom property throughout in `src/soloqueue/web/static/css/style.css`
- [ ] T034 [US3] Convert fixed font sizes to `rem` units in `src/soloqueue/web/static/css/style.css` and component CSS files
- [ ] T035 [US3] Ensure dialogs and message panels inherit base font size in `src/soloqueue/web/static/css/style.css` (add dialog-specific rules)
- [ ] T036 [US3] Test font size rendering across different browsers and viewports

**Checkpoint**: All user stories should now be independently functional

---

## Phase 6: User Story 4 - Agent Artifacts Stored in Memory Directory (Priority: P3)

**Goal**: Store agent-generated artifacts in `.soloqueue/memory/<agent_id>/artifacts/` instead of project root

**Independent Test**: Trigger agent that produces artifact and verify file saved in memory directory (not project root)

### Tests for User Story 4

- [ ] T037 [P] [US4] Unit test for artifact path resolution and duplicate handling in `tests/core/test_artifact_storage.py`
- [ ] T038 [P] [US4] Integration test for artifact storage flow in `tests/integration/test_artifact_storage.py`
- [ ] T039 [P] [US4] Contract test for artifact REST endpoints in `tests/contract/test_artifact_endpoints.py`

### Implementation for User Story 4

- [ ] T040 [P] [US4] Modify artifact-writing logic to resolve target path in `src/soloqueue/core/primitives/file_io.py` (write_file function)
- [ ] T041 [US4] Implement directory creation (`mkdir -p`) for artifact storage in `src/soloqueue/core/primitives/file_io.py`
- [ ] T042 [US4] Add duplicate filename handling with timestamp suffix in `src/soloqueue/core/primitives/file_io.py`
- [ ] T043 [US4] Record artifact metadata in agent's episodic memory (JSON sidecar) in `src/soloqueue/core/memory/artifact_store.py`
- [ ] T044 [US4] Implement REST endpoint `GET /api/agents/{agent_id}/artifacts` in `src/soloqueue/web/api/artifacts.py`
- [ ] T045 [US4] Implement REST endpoint `GET /api/agents/{agent_id}/artifacts/{filename}` in `src/soloqueue/web/api/artifacts.py`
- [ ] T046 [US4] Add path validation for sandboxed memory directory in `src/soloqueue/web/api/artifacts.py`
- [ ] T047 [US4] Create UI component for artifact listing in `src/soloqueue/web/templates/components/artifact_list.html`
- [ ] T048 [US4] Add artifact listing to web UI page (e.g., agent detail page) in `src/soloqueue/web/templates/agent_detail.html`

**Checkpoint**: All four user stories should now be independently functional

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [ ] T049 [P] Documentation updates in `docs/web-ui-enhancements.md`
- [ ] T050 [P] Code cleanup and refactoring across modified files
- [ ] T051 [P] Performance optimization for WebSocket communication
- [ ] T052 [P] Additional unit tests for edge cases in `tests/`
- [ ] T053 [P] Security hardening for file path validation
- [ ] T054 Run quickstart.md validation to ensure all implementation steps completed
- [ ] T055 Verify backward compatibility (terminal logging still works)
- [ ] T056 Validate WCAG 2.1 Level AA accessibility compliance
- [ ] T057 Performance test: ensure page-load time increase ≤10%
- [ ] T058 Update CLAUDE.md with new technologies and patterns

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: Depends on Setup completion - BLOCKS all user stories
- **User Stories (Phase 3+)**: All depend on Foundational phase completion
  - User stories can then proceed in parallel (if staffed)
  - Or sequentially in priority order (P1 → P2 → P3)
- **Polish (Phase 7)**: Depends on all desired user stories being complete

### User Story Dependencies

- **User Story 1 (P1)**: Can start after Foundational (Phase 2) - Requires T008 (WebUIApproval), T009 (integration)
- **User Story 2 (P1)**: Can start after Foundational (Phase 2) - Requires T004 (color field), T005 (palette utility), T007 (WebSocket schemas)
- **User Story 3 (P2)**: Can start after Foundational (Phase 2) - Requires T006 (CSS base variable)
- **User Story 4 (P3)**: Can start after Foundational (Phase 2) - No dependencies on other stories

### Within Each User Story

- Tests (if included) MUST be written and FAIL before implementation
- Core infrastructure before UI components
- Backend implementation before frontend integration
- Story complete before moving to next priority

### Parallel Opportunities

- All Setup tasks marked [P] can run in parallel
- All Foundational tasks marked [P] can run in parallel (within Phase 2)
- Once Foundational phase completes, all user stories can start in parallel (if team capacity allows)
- All tests for a user story marked [P] can run in parallel
- Different user stories can be worked on in parallel by different team members

---

## Parallel Example: User Story 1

```bash
# Launch all tests for User Story 1 together:
Task: "Unit test for write-action WebSocket message parsing in tests/web/test_websocket_write_action.py"
Task: "Integration test for write-action confirmation flow in tests/integration/test_write_action_flow.py"
Task: "Contract test for WebSocket endpoint in tests/contract/test_websocket_write_action.py"

# Launch implementation tasks in parallel where possible:
Task: "Create WebSocket endpoint /ws/write-action in src/soloqueue/web/app.py"
Task: "Create confirmation dialog UI component in src/soloqueue/web/templates/components/confirmation_dialog.html"
Task: "Add JavaScript for dialog interaction in src/soloqueue/web/static/js/confirmation_dialog.js"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational (CRITICAL - blocks all stories)
3. Complete Phase 3: User Story 1
4. **STOP and VALIDATE**: Test User Story 1 independently
5. Deploy/demo if ready

### Incremental Delivery

1. Complete Setup + Foundational → Foundation ready
2. Add User Story 1 → Test independently → Deploy/Demo (MVP!)
3. Add User Story 2 → Test independently → Deploy/Demo
4. Add User Story 3 → Test independently → Deploy/Demo
5. Add User Story 4 → Test independently → Deploy/Demo
6. Each story adds value without breaking previous stories

### Parallel Team Strategy

With multiple developers:

1. Team completes Setup + Foundational together
2. Once Foundational is done:
   - Developer A: User Story 1 (write-action confirmations)
   - Developer B: User Story 2 (differentiated output)
   - Developer C: User Story 3 (font size) + User Story 4 (artifact storage)
3. Stories complete and integrate independently

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- Each user story should be independently completable and testable
- Verify tests fail before implementing
- Commit after each task or logical group
- Stop at any checkpoint to validate story independently
- Avoid: vague tasks, same file conflicts, cross-story dependencies that break independence
- Follow existing SoloQueue codebase patterns and conventions