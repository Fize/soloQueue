---
name: qa-blueprint
description: >
  Testing strategy blueprint for any project. Determines what test types
  apply, recommends frameworks, inspects existing test infrastructure, sets
  up environments, and generates detailed interaction (E2E) test plans.
  Triggers when users ask about testing strategy, test setup, test
  environment, what tests to write, or how to test interactive features.
  Not for test execution — use dev-method for TDD/BDD, webapp-testing for
  browser E2E, Agent Browser for browser automation.
  Chinese triggers: "测试策略", "写测试", "测试环境", "搭建测试", "单元测试",
  "集成测试", "e2e", "测试框架", "覆盖率", "没有测试", "加测试", "测试数据",
  "mock", "测试计划", "交互测试", "怎么测", "测什么".
---

# QA-Blueprint: Strategy Before Execution

## YOU ARE BOUND BY THIS CONTRACT

You are NOT a general-purpose assistant while this skill is active.

**Read this contract aloud before taking any action:**

> I will NOT recommend a test framework before analyzing the project's language, platform, and existing dependencies.
> I will NOT suggest writing tests without first inspecting what test infrastructure already exists.
> I will NOT claim "no tests needed" without reviewing the project structure and risk profile.
> I will reference dev-method for TDD/BDD execution, webapp-testing for browser E2E, Agent Browser for browser automation — I will NOT duplicate their content.
> I will prioritize Docker-based containerized environments (Docker Compose / Testcontainers) when constructing test environments to ensure isolation, reproducibility, and local-CI parity.
> I will treat "no test environment" as a valid starting point, providing incremental adoption steps.
> For E2E/interaction testing, I will investigate specific features, elements, and behaviors before writing a test plan.

**These are not suggestions. Breaking any of them means you are not using this skill — you are ignoring it.**

---

## Outcome Contract

- Outcome: A concrete testing strategy with recommended test types, frameworks, environment setup steps, and (when applicable) a detailed interaction test plan.
- Done when: Test types are selected with rationale, frameworks are recommended (respecting existing deps), environment steps and gaps have action plans, and for interaction testing a feature-by-feature plan exists.
- Evidence: Project analysis (language, platform, package managers, existing test files, existing test config, CI config), environment inspection results.
- Output: Testing strategy document + executable setup commands + interaction test plan (if applicable).

## Mode Picker

| User intent | Mode | Load |
|-------------|------|------|
| "What tests should I write", "testing strategy", "测试策略" | Default: Full strategy | references/test-types.md |
| "Set up test environment", "搭建测试环境" | Environment setup | references/environment-inspection.md, then platform-specific env file |
| "No test environment exists", "没有测试" | No-environment fallback | references/no-environment.md |
| "Add tests to existing project", "加测试" | Incremental adoption | references/test-types.md |
| "Test data management", "测试数据", "fixtures" | Test data | references/test-data.md |
| "CI/CD integration", "automated tests" | CI integration | references/ci-integration.md |
| "E2E test plan", "交互测试", "UI test plan" | Interaction test plan | references/interaction-test-plan.md |

## Hard Rules

1. **Strategy before execution**: Recommend what and why before running any test command. This skill owns strategy; dev-method and webapp-testing own execution.
2. **Inspect before recommending**: Always run the environment inspection checklist (references/environment-inspection.md) before recommending frameworks or setup steps.
3. **Prefer existing frameworks**: Check package.json / go.mod / requirements.txt / Cargo.toml first. Never introduce a new framework when one is already in use.
4. **No test environment is a valid starting point**, not a failure. Provide incremental adoption steps.
5. **Reference, don't duplicate**: Point to dev-method for TDD/BDD execution, webapp-testing for browser E2E, Agent Browser for browser automation.
6. **Platform-appropriate defaults**: Don't recommend Playwright for a CLI tool. Don't recommend Jest for a Go project.
7. **Risk-proportional scope**: A simple CRUD app needs fewer test types than a payment system.
8. **Coverage is a signal, not a target**: Don't set arbitrary % targets. Calibrate thresholds to risk and test type.
9. **Test what can break**: Don't test framework internals, trivial getters/setters, or generated code.
10. **Interactive effects are first-class**: For UI tests, investigate and plan for interaction behaviors (validation feedback, loading states, error states, empty states, animations, keyboard navigation) — not just happy paths.
11. **Prioritize Docker for test environment construction**: When building or proposing test environments (databases, Redis, mock APIs, dependency services), prioritize Docker / Docker Compose or Testcontainers over local host installation to guarantee environment parity and isolation.

## Workflow

### Step 1: Discover Project

Before any strategy recommendation, collect project context:

1. **Language/Platform**: Read package manager files (package.json, go.mod, requirements.txt, pyproject.toml, Cargo.toml, Gemfile, CMakeLists.txt)
2. **Existing test files**: Glob for `*test*`, `*spec*`, `test_*`, `*_test.*`
3. **Existing test config**: Jest config, vitest.config, pytest.ini, pyproject.toml [tool.pytest]
4. **CI config**: `.github/workflows/*.yml`, `.gitlab-ci.yml`, Makefile test targets
5. **Project type**: Backend (API/microservice/CLI), Frontend (SPA/SSR), Mobile (iOS/Android/RN/Flutter), Desktop (Electron/Tauri/native), Library/SDK

### Step 2: Inspect Existing Test Environment

Load `references/environment-inspection.md`. Run the inspection checklist against the project. Check:
- Test frameworks already installed
- Test configuration files present
- Test directories exist
- Test commands defined in package.json/Makefile
- CI pipeline includes test steps
- Test data/fixtures/seeds present

Output a baseline assessment: What exists, what's missing, maturity level (none / minimal / partial / mature).

### Step 3: Determine Test Types

Load `references/test-types.md`. For each applicable category, determine whether it's needed with rationale.

| Test Type | When to apply | Severity if missing |
|-----------|--------------|---------------------|
| Unit | Always | High (no safety net) |
| Integration | External dependencies (DB, API, message queue) | High (can't verify real behavior) |
| API Contract | Multi-service, consumer-provider | Medium (breaking changes undetected) |
| E2E | User-facing flows, critical paths | Medium (user-visible regressions) |
| Performance | Latency-sensitive, high-traffic | Low (until it matters) |
| Visual Regression | UI component library, design system | Low (until it matters) |
| Snapshot | UI components with stable output | Low (overlapping with unit + visual) |
| Accessibility | User-facing UI, compliance | Medium (legal risk) |
| Security | Auth, payment, sensitive data | Medium (security incident risk) |

### Step 4: Recommend Frameworks

Based on discovered project context plus inspection results. Prefer frameworks already in the project's dependencies.

| Platform | Unit | Integration | E2E | Contract | Performance |
|----------|------|-------------|-----|----------|-------------|
| Python | pytest | pytest + testcontainers | Playwright | schemathesis | locust |
| Go | testing + testify | testcontainers-go | Playwright | pact-go | vegeta |
| Node (Backend) | vitest / jest | vitest + testcontainers | Playwright | pact-js | k6 |
| Frontend | vitest + Testing Library | MSW + vitest | Playwright | pact-js | lighthouse |
| Rust | cargo test | testcontainers-rs | Playwright | pact-rust | criterion |
| Mobile | XCTest / JUnit | integration stubs | Detox / Maestro | - | - |
| Desktop | platform-native | Electron: spectron; Tauri: cargo test | Playwright (Electron) | - | - |

State: "Found {framework} in {dependency file}, recommending it."

### Step 5: Environment Setup Routing

When constructing the test environment, **always prioritize Docker-based containerization** (e.g., Docker Compose, Testcontainers, or Dockerized test runners) to ensure environment parity between local development and CI pipelines without host machine pollution.

Based on project type, load the appropriate environment reference:
- Backend → `references/environment-backend.md`
- Frontend → `references/environment-frontend.md`
- Mobile → `references/environment-mobile.md`
- Desktop → `references/environment-desktop.md`
- No existing test infra → `references/no-environment.md`

### Step 6: Interaction Test Plan (when applicable)

**Activate when**: User mentions E2E, 交互测试, UI testing, browser testing, 端到端测试, or recommended test types include E2E.

Load `references/interaction-test-plan.md`. This step produces:

1. **Feature inventory** — List every user-facing feature/page/flow
2. **Element registry** — For each feature, enumerate interactive elements (buttons, inputs, forms, dropdowns, modals, tooltips, drag targets)
3. **Behavior catalog** — For each element, document:
   - Happy path
   - Validation states (invalid input → error, field highlight)
   - Loading states (spinner, skeleton, disabled)
   - Empty states (no data → empty UI)
   - Error states (API failure → error display, retry)
   - Edge cases (max length, special characters, boundary values)
4. **Output**: A feature-by-feature test plan ready for webapp-testing or Agent Browser.

### Step 7: CI Integration Guidance

Load `references/ci-integration.md`. Output CI-ready commands.

### Step 8: What NOT to Test

Explicitly call out anti-patterns for the specific project:
- Framework internals (don't test React/Vue behavior — test your code)
- Trivial getters/setters
- Mock verification (test outcomes, not interactions)
- Test infrastructure itself
- Generated code (proto stubs, OpenAPI clients)
- Config files without logic

## Integration with Other Skills

### Pointing to dev-method (execution layer)
```
To write tests using TDD: /dev-method tdd
To write tests using BDD: /dev-method bdd
To define API contracts first: /dev-method api-first
To bake security into tests: /dev-method security-first
```

### Pointing to webapp-testing (tool layer)
```
For browser-based E2E execution with Playwright:
Invoke the webapp-testing skill with the interaction test plan from Step 6.
It provides server lifecycle management and reconnaissance-then-action patterns.
```

### Pointing to Agent Browser (tool layer)
```
For interactive browser automation (screenshot, snapshot, click, fill):
Invoke the Agent Browser skill with the specific elements/behaviors from the interaction test plan.
```

### Integration with fullstack-dev
```
- Backend Phase: invoke /qa-blueprint for strategy and env setup, then /dev-method for execution
- Frontend Phase: invoke /qa-blueprint for strategy and env setup, then /dev-method for execution
- Before E2E steps: use Step 6 interaction test plan
- DevOps Phase: reference ci-integration.md for CI patterns
```

## Output

### Standard Output (all modes)

```markdown
## QA Blueprint: {project name}

### Project Profile
- Language: {language}
- Platform: {backend|frontend|mobile|desktop|library}
- Test maturity: {none|minimal|partial|mature}
- Existing tests: {file count}
- CI: {platform or none}

### Recommended Test Types
| Type | Apply? | Framework | Rationale |
|------|--------|-----------|-----------|
| ...  | ...    | ...       | ...       |

### Environment Setup
```bash
# Commands to set up test environment
```

### Gap Analysis
- Missing: {test types or capabilities not yet implemented}
- Fallback plan: {no-environment strategy if applicable}

### Next Steps
1. Run: {setup command}
2. To write tests: invoke /dev-method (for TDD) or /dev-method bdd (for BDD)
3. For E2E tests: invoke webapp-testing skill with the interaction test plan
4. For browser automation: invoke Agent Browser skill
```

### Interaction Test Plan Output (when applicable)

```markdown
### Interaction Test Plan

#### Feature: {feature name}
| # | Element | Action | Expected Behavior | State Type |
|---|---------|--------|-------------------|------------|
| 1 | Email input | Leave empty, click Submit | Shows "Email is required", red border | Validation |
| 2 | Email input | Type "not-an-email" | Shows "Invalid email format" | Validation |
| 3 | Email input | Type "user@example.com" | No error, green indicator | Happy path |
| 4 | Submit button | Click during loading | Button disabled, shows spinner | Loading |
| 5 | Submit button | Click, API returns 500 | Shows error toast "Something went wrong" | Error |

#### Feature: {next feature}
...
```

## Gotchas

| What happened | Rule |
|---------------|------|
| Recommended Jest for a Go project | Always check package manager first; match framework to language |
| Suggested Playwright for a CLI tool | Playwright is for web UIs only; CLI tools need integration/unit tests |
| Set up complex Docker env for a simple script | Calibrate env complexity to project risk |
| Ignored existing pytest.ini while recommending new setup | Always inspect before recommending |
| Treated "no tests" as a failure | "No tests" is a starting point; provide incremental adoption path |
| Missed input validation behaviors in E2E plan | Investigate every form field for validation states — not just happy paths |
| Skipped empty/error/loading states in interaction plan | Every UI element has 3+ states: happy, empty, loading, error |
