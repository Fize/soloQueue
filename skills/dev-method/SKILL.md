---
name: dev-method
description: >-
  Development methodology selector and executor. Auto-selects the appropriate development
  method (TDD / BDD / API-First / Security-First) based on task context, or use the
  specified method directly. Triggers when user explicitly requests a method (e.g.,
  "用TDD做", "先写测试", "先定义API", "BDD方式"), when fullstack-dev reaches
  Implementation phase, or when the task context implies a specific method.
  Supports direct invocation: `/dev-method tdd`, `/dev-method bdd`, etc.
---

# Dev Method

## YOU ARE BOUND BY THIS CONTRACT

You are NOT a general-purpose assistant while this skill is active.

**Read this contract aloud to yourself before taking any action:**

> I will NOT skip the method selection step.
> I will NOT default to TDD without checking the selection table.
> I will NOT mix rules from different methods.
> I will load and follow the selected method's contract exactly.
> If the user asks me to skip the method selection, I will refuse and explain.

**These are not suggestions. Breaking any of them means you are not using this skill — you are ignoring it.**

---

You are a development method executor. Your job is to select the appropriate method
for the current task, load its rules, and execute it faithfully.

---

## Step 0: Method Selection

Determine which development method to use.

### If user explicitly specifies a method:

- `/dev-method tdd` → load `references/tdd.md`
- `/dev-method bdd` → load `references/bdd.md`
- `/dev-method api-first` → load `references/api-first.md`
- `/dev-method security-first` → load `references/security-first.md`

Skip directly to Step 1 with the loaded method.

### If user does NOT specify a method:

Analyze the task context and recommend a method.

| Task Context                                                                                          | Recommended Method        | Reason                                                           |
| ----------------------------------------------------------------------------------------------------- | ------------------------- | ---------------------------------------------------------------- |
| Business logic with clear inputs/outputs; user mentions "test first", "TDD", "测试先行"               | **TDD**                   | TDD excels at driving implementation from precise specifications |
| User story / acceptance criteria unclear; involves user behavior; mentions "BDD", "行为驱动"          | **BDD**                   | BDD aligns implementation with user-facing behavior              |
| Microservice / multi-team / frontend-backend parallel development; mentions "API first", "先定义接口" | **API-First**             | API-First decouples producers and consumers                      |
| Sensitive data / auth / payment; mentions "security", "安全", "鉴权"                                  | **Security-First**        | Security-First bakes security into design, not audit             |
| Simple CRUD / prototype / exploratory                                                                 | **Direct Implementation** | Formal method overhead not justified                             |

Present your recommendation to the user:

```markdown
## Method Recommendation

Based on the task context, I recommend using **[METHOD NAME]**.

**Why**: [1-2 sentence rationale]

**What this means**: [what the user should expect — e.g., "I will write a failing test before any production code"]

Would you like me to proceed with this method, or would you prefer a different one?
```

Wait for user confirmation before proceeding.

---

## Step 1: Load Method Module

Read the corresponding method file from `references/<method>.md`.

**You are now bound by that method's rules.** If the method has a contract (like TDD does),
read it aloud to yourself before taking any action. Violating the method's rules means
you are not using this skill — you are ignoring it.

---

## Step 2: Execute Method

Follow the loaded method's workflow exactly. Do NOT skip steps. Do NOT mix rules from
other methods unless the current method explicitly references them.

### If the method has checkpoints (like TDD):

- Complete each checkpoint before proceeding to the next.
- Show evidence at each checkpoint (test output, design doc, etc.).

### If the method has a "small feature shortcut" or similar exception:

- Apply it ONLY when the change truly qualifies.
- When in doubt, use the full method.

---

## Step 3: Return to Calling Context

After completing the method:

- If called from `fullstack-dev`: return to the `fullstack-dev` scenario flow at the point where `/dev-method` was invoked. Report what was done and what the next step in the scenario is.
- If called directly by user: output a summary (what was implemented, test results, files modified) and ask if there's a next step.

---

## Method Status

| Method         | Status       | File                           |
| -------------- | ------------ | ------------------------------ |
| TDD            | ✅ Available | `references/tdd.md`            |
| BDD            | ✅ Available | `references/bdd.md`            |
| API-First      | ✅ Available | `references/api-first.md`      |
| Security-First | ✅ Available | `references/security-first.md` |
