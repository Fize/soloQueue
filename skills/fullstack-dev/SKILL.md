---
name: fullstack-dev
description: >-
  Fullstack development scenario-flow methodology. Routes to the correct scenario
  based on user intent and executes phase-by-phase with mandatory checklists.
when_to_use: >-
  When user requests involve writing/modifying/designing/deploying code.
  Greenfield, feature iteration, bug fix, refactor, or deploy scenarios.
triggers:
  - 想做
  - 从零
  - 新项目
  - 做一个
  - 帮我搭个
  - 写个App
  - 创建服务
  - 初始化
  - 脚手架
  - new project
  - scaffold
  - greenfield
  - build from scratch
  - 加功能
  - 新增
  - 迭代
  - 实现
  - 添加
  - 支持
  - 接入
  - 集成
  - add feature
  - implement
  - integrate
  - enhancement
  - 报错
  - bug
  - 不工作
  - 崩溃
  - 异常
  - 出错
  - 失败
  - 返回500
  - 空指针
  - panic
  - error
  - crash
  - fix
  - hotfix
  - debug
  - troubleshoot
  - 重构
  - 优化
  - 太慢
  - 太乱
  - 清理
  - 改进
  - 性能
  - 拆分
  - refactor
  - optimize
  - perf
  - cleanup
  - restructure
  - 部署
  - 上线
  - 发版
  - 容器化
  - 发布
  - deploy
  - release
  - CI/CD
  - Docker
  - K8s
  - helm
  - rollout
  - ship
  - /git-flow
  - git 工作流
  - commit 提交
  - push 推送
  - pull 拉取
  - branch 分支
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

<delegates-to>
- `/dev-method` → selects TDD / BDD / API-First / Security-First / Direct Implementation
- `/git-flow` → branch, commit, push, pull operations
</delegates-to>

**These are not suggestions. Breaking any of them means you are not using this skill — you are ignoring it.**

---

Match the user's intent to a scenario flow, then execute step by step. You MUST NOT skip steps within a scenario. You MUST reference the relevant checklist at each step. You MAY skip entire phases that are irrelevant to the current task.

---

## Scenario Flows

### Scenario 1: Greenfield

**Triggers**: 想做、从零、新项目、做一个、帮我搭个、写个App、创建服务、初始化、脚手架、new project、scaffold、greenfield、build from scratch

**Flow**:

1. Requirement scoping → output MVP definition (MUST reference [Requirement Checklist](references/checklists.md#requirement-phase))
2. Tech stack + DB design → output DDL + API contract (MUST reference [Architecture Checklist](references/checklists.md#architecture-phase))
3. Core API implementation → invoke `/dev-method` to select method, then implement (MUST reference [Backend Checklist](references/checklists.md#backend-phase))
4. Page implementation → invoke `/dev-method` to select method, then implement (MUST reference [Frontend Checklist](references/checklists.md#frontend-phase))
5. Deploy config → output deploy commands + health check (MUST reference [DevOps Checklist](references/checklists.md#devops-phase))

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

## Phase Checklists

→ See [references/checklists.md](references/checklists.md) for all phase checklists (Requirement, Architecture, Backend, Frontend, DevOps).

---

## Pitfalls

→ See [references/pitfalls.md](references/pitfalls.md) for common anti-patterns and the verification protocol.

---

## Error Recovery

### Scenario Switch Protocol

If the user's intent changes mid-flow (e.g., Feature Iteration → Bug Fix):

1. **Acknowledge the switch**: "I see the situation has changed from [current scenario] to [new scenario]."
2. **Save checkpoint**: Note the current phase and what was completed.
3. **Reset to new scenario**: Start the new scenario flow from Step 1.
4. **Do NOT carry over assumptions** from the previous scenario.

### Dev-Method Delegation Failure

If `/dev-method` fails to load (e.g., method file missing or corrupted):

1. **Fallback to Direct Implementation**: Use the [Direct Implementation](../dev-method/references/direct-implementation.md) rules as a minimal safety net.
2. **Inform the user**: "The selected method file could not be loaded. Falling back to Direct Implementation with basic checks."
3. **Continue the current scenario flow** — do NOT restart.

### Git-Flow Delegation Failure

If `/git-flow` is unavailable during Implementation:

1. **Continue implementation** without git operations.
2. **Queue git operations**: Note which commits should have been created.
3. **Inform the user**: "Git operations are unavailable. I will proceed with implementation and batch git operations when available."
