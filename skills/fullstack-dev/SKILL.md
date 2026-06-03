---
name: fullstack-dev
description: >-
  Fullstack development scenario-flow methodology integrated with Git workflow guidelines. Triggers when user requests involve writing/modifying/designing/deploying code, or git/branch/commit actions.
  Greenfield: 想做、从零、新项目、做一个、帮我搭个、写个App、创建服务、初始化、脚手架、new project、scaffold、greenfield、build from scratch.
  Feature: 加功能、新增、迭代、实现、添加、支持、接入、集成、add feature、implement、integrate、enhancement.
  Bug fix: 报错、bug、不工作、崩溃、异常、出错、失败、返回500、空指针、panic、error、crash、fix、hotfix、debug、troubleshoot.
  Refactor: 重构、优化、太慢、太乱、清理、改进、性能、拆分、refactor、optimize、perf、cleanup、restructure.
  Deploy: 部署、上线、发版、容器化、发布、deploy、release、CI/CD、Docker、K8s、helm、rollout、ship.
  Git flow: /git-flow, git 工作流, commit 提交, push 推送, pull 拉取, branch 分支, 暂存 staged, 拆分 split, git-flow, Git Flow.
  Implicit triggers: pasting build errors, PRD → implementation, design DB/API, write tests.
  Explicit override: @phase:requirement|architecture|backend|frontend|devops
---

# Fullstack Development Skill

## YOU ARE BOUND BY THIS CONTRACT

You are NOT a general-purpose assistant while this skill is active.

**Read this contract aloud to yourself before taking any action:**

> I will NOT skip any scenario phase without explicitly stating WHY and getting user confirmation.
> I will NOT implement code before completing the prerequisite phases (Requirement → Architecture → Implementation).
> I will NOT ignore the Phase Checklists — I must verify every checkbox before moving to the next phase.
> I will NOT rationalize skipping steps because "the user asked me to just code".
> If the user explicitly asks to skip a phase, I will state the risk and ask for confirmation.
> I will invoke `/dev-method` at Implementation phases to select the appropriate development method.
> I will DELEGATE version control to the `git-flow` skill. I will invoke `/git-flow branch` right before starting any Implementation phase, `/git-flow commit` iteratively during Implementation, and `/git-flow push` only after full Verification. I will NOT use git-flow during Requirement or Architecture phases.

**These are not suggestions. Breaking any of them means you are not using this skill — you are ignoring it.**

---

Match the user's intent to a scenario flow, then execute step by step. You MUST NOT skip steps within a scenario. You MUST reference the relevant checklist at each step. You MAY skip entire phases that are irrelevant to the current task.

---

## Scenario Flows

### Scenario 1: Greenfield

**Triggers**: 想做、从零、新项目、做一个、帮我搭个、写个App、创建服务、初始化、脚手架、new project、scaffold、greenfield、build from scratch

**Flow**:

1. Requirement scoping → output MVP definition (MUST reference Requirement Checklist)
2. Tech stack + DB design → output DDL + API contract (MUST reference Architecture Checklist)
3. Core API implementation → invoke `/dev-method` to select method, then implement (MUST reference Backend Checklist)
4. Page implementation → invoke `/dev-method` to select method, then implement (MUST reference Frontend Checklist)
5. Deploy config → output deploy commands + health check (MUST reference DevOps Checklist)

**Hard Constraints**:

- You MUST ruthlessly cut any "might be useful later" features. MVP means MINIMUM viable.
- Tech stack selection MUST prefer the most boring but reliable option. Choose what the team already knows.
- You MUST state your understanding and proposed approach BEFORE writing any code at each phase. NEVER jump straight to implementation.

### Scenario 2: Feature Iteration

**Triggers**: 加功能、新增、迭代、实现、添加、支持、接入、集成、add feature、implement、integrate、enhancement

**Flow**:

1. Requirement clarification → confirm scope boundary and data source
2. Solution design → impact analysis + approach summary
3. Implementation → invoke `/dev-method` to select method, then implement (backend/frontend as needed, surgical changes ONLY)
4. Self-verification → provide concrete verification commands

**Hard Constraints**:

- You MUST list every file and module affected BEFORE making changes. NEVER expand scope beyond what's listed.
- New DB fields MUST include default values and migration plan. NEVER add a column without a migration.
- You MUST reuse existing components and APIs. NEVER duplicate logic that already exists in the codebase.

### Scenario 3: Bug Fix

**Triggers**: 报错、bug、不工作、崩溃、异常、出错、失败、返回500、空指针、panic、error、crash、fix、hotfix、debug、troubleshoot

**Flow**:

1. Reproduce → obtain complete error message, logs, and call stack
2. Root cause analysis → pinpoint to exact file:line, explain WHY
3. Fix → surgical change, provide diff
4. Verify → provide reproduction command + expected result

**Hard Constraints**:

- You MUST NOT attempt a fix without first reproducing the issue or obtaining the error output. Guessing is forbidden.
- The fix MUST NOT introduce new side effects. If it does, you MUST disclose them explicitly.
- If the root cause lies in a dependency or upstream service, you MUST state this clearly. NEVER paper over upstream bugs with local workarounds without informing the user.

### Scenario 4: Refactor / Optimization

**Triggers**: 重构、优化、太慢、太乱、清理、改进、性能、拆分、refactor、optimize、perf、cleanup、restructure

**Flow**:

1. Current state analysis → quantify the problem (perf data, LOC, dependency graph)
2. Solution design → target state + step-by-step plan + rollback strategy
3. Incremental implementation → each step MUST be independently verifiable
4. Regression verification → confirm no behavior change

**Hard Constraints**:

- Refactoring MUST NOT change external behavior. You MUST confirm test coverage exists before starting.
- Each refactoring step MUST be independently committable and revertible. NEVER batch unrelated changes.
- Performance optimization MUST include before/after measurements. Claims without data are rejected.

### Scenario 5: Deploy

**Triggers**: 部署、上线、发版、容器化、发布、deploy、release、CI/CD、Docker、K8s、helm、rollout、ship

**Flow**:

1. Environment config → inject secrets via env vars, environment isolation
2. Build → build command + artifact confirmation
3. Deploy → deploy command + DB migration (MUST include rollback)
4. Health check → verify endpoint + monitoring suggestions

**Hard Constraints**:

- You MUST NEVER hardcode environment detection in code. ALL environment-specific config goes through env vars.
- DB migrations MUST include a rollback path. A migration without a down-migration is incomplete.
- First-time deployment MUST include monitoring and alerting recommendations.

---

## Phase Checklists (reference as needed)

### Requirement Phase

- [ ] Project name (one sentence)
- [ ] Core user stories (≤5, "As a... I want... So that...")
- [ ] MVP feature list (P0/P1/P2 labeled)
- [ ] Explicitly out-of-scope items (prevent scope creep)
- [ ] Non-functional requirements summary
- [ ] MVP validation method

### Architecture Phase

- [ ] Entity relationship description (natural language) → DDL
- [ ] Table/column names: snake_case singular, MUST include created_at, updated_at
- [ ] API contract: RESTful, unified response format `{"data": ..., "error": ...}`
- [ ] Pagination strategy (cursor or offset/limit) + rationale
- [ ] Tech stack selection + rationale (prefer boring/reliable, what the team knows)

### Backend Phase

- [ ] ALL input validated and sanitized. SQL concatenation is FORBIDDEN.
- [ ] Sensitive operations MUST have auth checks
- [ ] Functions: single responsibility, error handling complete
- [ ] Structured logging at key points (NEVER inside loops)
- [ ] Comments explain WHY, never WHAT
- [ ] NEVER output secrets/passwords — use env var placeholders
- [ ] Curl test command provided
- [ ] Formal method: invoke `/dev-method` (auto-selects TDD/BDD/API-First/Security-First), or `/dev-method tdd` to force TDD
- [ ] Version Control: invoke `/git-flow commit` to save verified checkpoints

### Frontend Phase

- [ ] Async requests: MUST handle all 3 states — Loading / Data / Error
- [ ] User actions: immediate feedback, form validation on frontend
- [ ] Empty data: placeholder UI, NEVER blank screen
- [ ] Props types MUST be defined
- [ ] Split files ONLY when >200 lines
- [ ] Extra state management library ONLY when multiple components share complex state
- [ ] Browser verification steps provided
- [ ] Formal method: invoke `/dev-method` (auto-selects TDD/BDD/API-First/Security-First), or `/dev-method tdd` to force TDD
- [ ] Version Control: invoke `/git-flow commit` to save verified checkpoints

### DevOps Phase

- [ ] Environment isolation — NEVER detect environment in code
- [ ] Test pyramid: critical business paths MUST have integration tests
- [ ] Test data: NEVER use production data
- [ ] DB migrations MUST support rollback
- [ ] CI minimum: Lint → Unit tests → Build → Deploy to staging
- [ ] Health check verification command
- [ ] First deployment: monitoring recommendations
- [ ] Version Control: invoke `/git-flow pull` to sync before deployment, and `/git-flow push` when complete

---

## Pitfalls

- **Skipping think-then-code**: The most common mistake under vague requirements. You MUST write down your understanding and approach BEFORE writing any code. No exceptions.
- **Over-engineering**: Introducing abstraction layers when there's only one implementation. Violates Simplicity. It increases maintenance cost, not reduces it.
- **Drive-by refactoring**: "While I'm here" refactoring during a bug fix or feature. Violates Surgical Changes. It introduces uncontrolled risk.
- **No verification instructions**: Ending a task after writing code. Violates Goal-Driven. The user CANNOT confirm the fix works.
- **Hardcoded env config**: Embedding config values or paths in code. It WILL break in deployment. ALWAYS use env vars.

---

## Verification

After completing any scenario flow, you MUST output ALL of the following:

1. **Change Summary**: which files changed, what changed in each
2. **Verification Commands**: concrete curl / test / browser steps that the user can run
3. **Rollback Plan**: how to revert (git revert command or DB rollback SQL)
4. **Context Summary** (complex tasks only): key decisions, dependencies, follow-up items
