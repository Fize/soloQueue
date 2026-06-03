---
name: git-flow
description: Git workflow 工作流 covering 分支管理 branch management, 规范化提交 normalized commits, 远程同步 remote sync, with 分阶段操作流 staged operation flows. Supports git commit 提交, git push 推送, git pull 拉取, git branch 分支, smart commit splitting 智能提交拆分, AI author tagging AI作者标记, safe branch operations 安全分支操作. Use for /git-flow, git 工作流, commit 提交, push 推送, pull 拉取, branch 分支, 暂存 staged, 拆分 split
---

# Git Flow Skill

## 🔴 CRITICAL - MANDATORY REQUIREMENTS (Cannot be ignored under any circumstances)

### ⚠️ AI AUTHOR TAGGING - ABSOLUTE REQUIREMENT

**🚨 THIS IS NON-NEGOTIABLE - YOU MUST FOLLOW THIS 🚨**

Every commit created using this skill **MUST** include the following line in the commit message:

```
Co-Authored-By: AI Agent
```

### Enforcement Rules

1. **Before creating ANY commit, you MUST**:
   - Add `Co-Authored-By: AI Agent` to the commit message
   - Verify the tag is present by reading the commit message back
   - **DO NOT SKIP THIS STEP UNDER ANY CIRCUMSTANCE**

2. **After creating ANY commit, you MUST**:
   - Run `git log -1 --pretty=full` to verify the commit
   - Check that `Co-Authored-By: AI Agent` is present
   - If not present, **AMEND THE COMMIT IMMEDIATELY**

3. **If you forget to add the tag**:
   - You have **FAILED** to follow this skill's requirements
   - You must **APOLOGIZE** to the user and **FIX IT IMMEDIATELY**
   - Use `git commit --amend` to add the missing tag

4. **Self-Check (MANDATORY)**:
   Before marking any task as completed, ask yourself:

   ```
   ✅ Did I add "Co-Authored-By: AI Agent" to EVERY commit?
   ✅ Did I verify the tag is present in `git log`?
   ✅ If I forgot, did I amend the commit to add it?
   ```

5. **Penalty for Non-Compliance**:
   If you create a commit without this tag, you have **NOT FULFILLED** your duty.
   This is not a suggestion - it is a **REQUIREMENT**.

---

## Quick Start

- `/git-flow commit`: Smart commit with auto-splitting
- `/git-flow push`: Safe push with auto-sync
- `/git-flow pull`: Safe pull with rebase
- `/git-flow branch <name>`: Create and switch to branch (safe mode)
- `/git-flow status`: Show current Git status with suggestions

---

## Core Features

### 1. Staged Operation Flows

Automatically select appropriate Git operation flow based on development stage:

| Stage          | Trigger Condition            | Operation Flow                                           |
| -------------- | ---------------------------- | -------------------------------------------------------- |
| **Initialize** | New feature/fix starts       | Sync remote → Create branch → Initial commit (if needed) |
| **Develop**    | Has staged changes           | Smart split → Generate commit → Verify                   |
| **Complete**   | Feature development complete | Rebase/Squash commits → Push to remote                   |
| **Maintain**   | Daily maintenance            | Pull --rebase → Resolve conflicts → Continue development |

### 2. Smart Commit Splitting

**Auto-split logic** (minimize user confirmation):

- Analyze file paths and modification intent
- Group by functional relevance (same directory/feature → likely same group)
- Cross-directory modifications (e.g., `src/` + `docs/`) → force split
- **Default behavior**: Auto-split and commit directly, only prompt if uncertain
- **Configurable**: User can set `GIT_FLOW_AUTO_SPLIT=true/false`

**Split Logic Example**:

```
Detected 5 file changes:
  Group 1 (feature A): src/auth/oauth2.py, tests/test_oauth2.py
  Group 2 (feature B): src/utils/token.py, tests/test_token.py
  Group 3 (docs): docs/auth.md

Auto-splitting into 3 commits...
  [1/3] feat(auth): add OAuth2 login support
  [2/3] feat(utils): add token utility functions
  [3/3] docs(auth): update auth module documentation

Committing...
✅ All commits created successfully
```

**If uncertain about splitting**:

```
Detected 4 files changed:
  Group 1: src/auth/oauth2.py, src/utils/token.py
  Group 2: tests/test_oauth2.py, tests/test_token.py

Suggested split (uncertain about grouping):
  1. feat(auth): add OAuth2 login and token utils
  2. test(auth): add tests for OAuth2 and token utils

Confirm? [Yes/Adjust/Cancel]
```

### 3. Safe Branch Operations

**Always sync remote first** before creating/switching branches:

1. `git fetch origin`
2. Check if local has uncommitted changes
   - If yes → auto-stash (if `GIT_FLOW_AUTO_STASH=true`) or prompt user
3. Check if local branch is behind remote
   - If behind → auto-pull or prompt user
4. Auto-detect if current branch complies with naming conventions

**Branch Naming Convention**:

- Format: `feat/xxx`, `fix/xxx`, `refactor/xxx`, `docs/xxx`, `test/xxx`, `chore/xxx`
- Auto-generate branch name from commit message or PRD content
- Validate branch name format before creation

**Example: Create Branch (Safe Mode)**:

```
User: /git-flow branch feat/user-profile
AI:  Syncing with remote...

     ✅ Remote sync complete
     ✅ Current branch: main (clean working tree)
     Creating branch...
     ✅ Branch created: feat/user-profile
     ✅ Switched to new branch
```

**If local has uncommitted changes**:

```
User: /git-flow branch feat/user-profile
AI:  Syncing with remote...

     ✅ Remote sync complete
     ⚠️  Uncommitted changes detected:
        M src/auth.py
        ?? new_file.py

     Options:
       1. Auto-stash changes, apply after switch
       2. Cancel and commit changes first

     Choose [1/2]:
```

### 4. Normalized Commits

#### 4.1 Commit Message Generation

- Auto-generate conventional commit messages based on diff content
- Format: `<type>(<scope>): <subject>`
- **Subject length limit**:
  - English: ≤ 50 characters
  - Chinese: ≤ 25 characters
- Body is optional, only added for complex changes (≤ 72 chars/line, max 3 lines)

#### 4.2 🔴 AI Author Tagging (MANDATORY - See TOP of this file)

**🚨 THIS IS NOT OPTIONAL - YOU MUST DO THIS 🚨**

- **EVERY** commit created using this skill **MUST** include AI author tag
- **Implementation**: Add `Co-Authored-By: AI Agent` in commit message body
- **Requirement**: Commit message body must be short (≤ 72 chars/line, max 3 lines)

**Step-by-Step (You MUST follow)**:

1. **Before commit**:

   ```
   Commit message format:
   <type>(<scope>): <subject>

   [optional short body - max 3 lines]

   Co-Authored-By: AI Agent
   ```

2. **Create commit**:

   ```bash
   git commit -m "<type>(<scope>): <subject>

   [optional body]

   Co-Authored-By: AI Agent"
   ```

3. **After commit - VERIFY**:

   ```bash
   git log -1 --pretty=full
   # CHECK: "Co-Authored-By: AI Agent" must be present
   ```

4. **If tag is missing - FIX IMMEDIATELY**:

   ```bash
   git commit --amend -m "<original message>

   Co-Authored-By: AI Agent"
   ```

**Example (Correct)**:

```
feat(auth): add OAuth2 login support

Co-Authored-By: AI Agent
```

**Example (With body)**:

```
feat(auth): add OAuth2 login support

Add OAuth2 login with Google and Facebook support.
Includes token refresh and user info fetching.

Co-Authored-By: AI Agent
```

**❌ WRONG - Missing AI Tag**:

```
feat(auth): add OAuth2 login support
# ❌ ERROR: Missing "Co-Authored-By: AI Agent"
```

#### 4.3 Commit Types

| Type       | Description             | Example                                                   |
| ---------- | ----------------------- | --------------------------------------------------------- |
| `feat`     | New feature             | `feat(auth): add OAuth2 login support`                    |
| `fix`      | Bug fix                 | `fix(login): fix null password bypass vulnerability`      |
| `refactor` | Refactoring             | `refactor(auth): extract auth logic into separate module` |
| `perf`     | Performance improvement | `perf(query): optimize user query SQL index`              |
| `docs`     | Documentation           | `docs(api): update REST API documentation`                |
| `test`     | Testing                 | `test(auth): add OAuth2 login test cases`                 |
| `chore`    | Build/tool changes      | `chore(deps): upgrade flask to 3.0`                       |
| `ci`       | CI configuration        | `ci(gitlab): add multi-stage pipeline`                    |

### 5. Remote Sync

#### 5.1 Safe Pull/Push

**Before pull**:

- Check if local has uncommitted changes
  - If yes → auto-stash (if configured) or warn user
- **Pull strategy**: Default to `git pull --rebase` (keep linear history)
- If divergence detected → prompt user to choose rebase/merge

**Before push**:

- Check if local is behind remote
  - If behind → auto-pull --rebase first, then push
  - If diverged → prompt user to choose rebase/merge

**Example: Push Code to Remote (Safe Mode)**:

```
User: /git-flow push
AI:  Checking status...

     ✅ Current branch: feat/oauth2-login
     ⚠️  Local is behind remote by 2 commits

     Auto-pulling with rebase...
     ✅ Rebase complete, no conflicts
     Pushing...
     ✅ Push successful: feat/oauth2-login → origin/feat/oauth2-login
```

#### 5.2 Branch Cleanup

- Auto-detect merged local branches
- Prompt before deleting (configurable to auto-delete with `GIT_FLOW_AUTO_CLEANUP=true`)

### 6. Conflict Resolution Assistance

- Detect merge/rebase conflicts
- List conflict files
- Provide resolution suggestions based on diff analysis
- **No agent calls**: Only provide suggestions, user resolves manually

**Example**:

```
Conflict detected during rebase:

Conflict files:
  - src/auth/oauth2.py
  - tests/test_oauth2.py

Suggestions:
  1. src/auth/oauth2.py:
     - Incoming changes add OAuth2 support
     - Current changes refactor auth logic
     - Suggestion: Keep both, OAuth2 should call refactored auth logic

  2. tests/test_oauth2.py:
     - Incoming changes add OAuth2 tests
     - Current changes update test fixtures
     - Suggestion: Merge both test cases and update fixtures

Resolve conflicts manually, then run `/git-flow continue` to continue rebase.
```

---

## Configuration

Environment variables (can be set in project's `.env` file or shell):

| Variable                     | Values                | Default  | Description                    |
| ---------------------------- | --------------------- | -------- | ------------------------------ |
| `GIT_FLOW_AUTO_SPLIT`        | `true`/`false`        | `true`   | Enable/disable auto-split      |
| `GIT_FLOW_PULL_STRATEGY`     | `rebase`/`merge`      | `rebase` | Pull strategy                  |
| `GIT_FLOW_AUTO_STASH`        | `true`/`false`        | `false`  | Auto-stash uncommitted changes |
| `GIT_FLOW_AUTO_CLEANUP`      | `true`/`false`        | `false`  | Auto-delete merged branches    |
| `GIT_FLOW_CONFIRM_THRESHOLD` | `high`/`medium`/`low` | `medium` | Confirmation threshold         |

---

## Workflow Examples

### Example 1: Start New Feature

```
User: /git-flow branch feat/user-profile
AI:  Syncing with remote...

     ✅ Remote sync complete
     ✅ Current branch: main (clean working tree)
     Creating branch...
     ✅ Branch created: feat/user-profile
     ✅ Switched to new branch

     You can now start developing. Use `/git-flow commit` to commit changes.
```

### Example 2: Commit Code During Development

```
User: /git-flow commit
AI:  Analyzing staged changes...

     Detected 3 files changed:
       Group 1: src/auth/oauth2.py, tests/test_oauth2.py
       Group 2: docs/auth.md

     Auto-splitting into 2 commits...
       [1/2] feat(auth): add OAuth2 login support
       [2/2] docs(auth): update auth module documentation

     Committing...

     [1/2] Creating commit: feat(auth): add OAuth2 login support
     ✅ Commit created: abc123
     ✅ AI author tag added and verified

     [2/2] Creating commit: docs(auth): update auth module documentation
     ✅ Commit created: def456
     ✅ AI author tag added and verified

     ✅ All commits created successfully
     ✅ Verification: All commits have "Co-Authored-By: AI Agent" tag
```

### Example 3: Push Code to Remote

```
User: /git-flow push
AI:  Checking status...

     ✅ Current branch: feat/oauth2-login
     ⚠️  Local is behind remote by 2 commits

     Auto-pulling with rebase...
     ✅ Rebase complete, no conflicts
     Pushing...
     ✅ Push successful: feat/oauth2-login → origin/feat/oauth2-login
```

### Example 4: Pull Latest Changes

```
User: /git-flow pull
AI:  Fetching from remote...

     ✅ Remote sync complete
     ✅ Local is behind by 3 commits

     Rebasing...
     ✅ Rebase complete, no conflicts

     ✅ Local branch updated successfully
```

---

## 🔴 FINAL VALIDATION CHECKLIST (MUST Complete Before Finishing)

**BEFORE you mark ANY task as completed, you MUST verify:**

- [ ] **EVERY commit created has `Co-Authored-By: AI Agent` tag**
- [ ] I have run `git log --pretty=full` to verify the tag is present
- [ ] If any commit is missing the tag, I have amended it
- [ ] I have NOT skipped adding the AI author tag under any circumstance

**If you cannot check ALL boxes above, YOU ARE NOT DONE.**

**This is not a suggestion. This is a REQUIREMENT.**

---

## References

- [Git Workflow Specification](./references/git-workflow.md): Detailed Git workflow specification document
