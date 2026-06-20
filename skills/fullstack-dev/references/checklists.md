# Phase Checklists

> Referenced by `fullstack-dev` SKILL.md. Verify every checkbox before moving to the next phase.

---

## Requirement Phase

- [ ] Project name (one sentence)
- [ ] Core user stories (≤5, "As a... I want... So that...")
- [ ] MVP feature list (P0/P1/P2 labeled)
- [ ] Explicitly out-of-scope items (prevent scope creep)
- [ ] Non-functional requirements summary
- [ ] MVP validation method

---

## Architecture Phase

- [ ] Entity relationship description (natural language) → DDL
- [ ] Table/column names: snake_case singular, MUST include created_at, updated_at
- [ ] API contract: RESTful, unified response format `{"data": ..., "error": ...}`
- [ ] Pagination strategy (cursor or offset/limit) + rationale
- [ ] Tech stack selection + rationale (prefer boring/reliable, what the team knows)

---

## Backend Phase

- [ ] ALL input validated and sanitized. SQL concatenation is FORBIDDEN.
- [ ] Sensitive operations MUST have auth checks
- [ ] Functions: single responsibility, error handling complete
- [ ] Structured logging at key points (NEVER inside loops)
- [ ] Comments explain WHY, never WHAT
- [ ] NEVER output secrets/passwords — use env var placeholders
- [ ] Curl test command provided
- [ ] Formal method: invoke `/dev-method` (auto-selects TDD/BDD/API-First/Security-First), or `/dev-method tdd` to force TDD
- [ ] Version Control: invoke `/git-flow commit` to save verified checkpoints

---

## Frontend Phase

- [ ] Async requests: MUST handle all 3 states — Loading / Data / Error
- [ ] User actions: immediate feedback, form validation on frontend
- [ ] Empty data: placeholder UI, NEVER blank screen
- [ ] Props types MUST be defined
- [ ] Split files ONLY when >200 lines
- [ ] Extra state management library ONLY when multiple components share complex state
- [ ] Browser verification steps provided
- [ ] Formal method: invoke `/dev-method` (auto-selects TDD/BDD/API-First/Security-First), or `/dev-method tdd` to force TDD
- [ ] Version Control: invoke `/git-flow commit` to save verified checkpoints

---

## DevOps Phase

- [ ] Environment isolation — NEVER detect environment in code
- [ ] Test pyramid: critical business paths MUST have integration tests
- [ ] Test data: NEVER use production data
- [ ] DB migrations MUST support rollback
- [ ] CI minimum: Lint → Unit tests → Build → Deploy to staging
- [ ] Health check verification command
- [ ] First deployment: monitoring recommendations
- [ ] Version Control: invoke `/git-flow pull` to sync before deployment, and `/git-flow push` when complete
