---
name: git-flow
description: >-
  Git workflow covering branch management, normalized commits, remote sync,
  with staged operation flows. Supports git commit, push, pull, branch,
  smart commit splitting, AI author tagging, safe branch operations.
when_to_use: >-
  When user needs git operations: commit, push, pull, branch management.
  Also invoked by fullstack-dev during Implementation and DevOps phases.
  Trigger phrases: /git-flow, git 工作流, commit, 提交, push, 推送, pull, 拉取, branch, 分支, 暂存, staged, 拆分, split
allowed-tools:
  - Bash(git:*)
---

# Git Flow Skill

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

#### 4.2 AI Author Tagging (MANDATORY)

**Every commit created using this skill MUST include `Co-Authored-By: AI Agent` in the commit message body.** This is non-negotiable.

**Step-by-Step**:

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

## FINAL VALIDATION CHECKLIST (MUST Complete Before Finishing)

**BEFORE you mark ANY task as completed, you MUST verify:**

- [ ] **EVERY commit created has `Co-Authored-By: AI Agent` tag** (see §4.2 for procedure)
- [ ] I have run `git log --pretty=full` to verify the tag is present
- [ ] If any commit is missing the tag, I have amended it

**If you cannot check ALL boxes above, YOU ARE NOT DONE.**

---

## References

- [Git Workflow Specification](./references/git-workflow.md): Detailed Git workflow specification document

---

## Error Recovery

### Merge/Rebase Conflict Cannot Be Resolved

If conflicts are too complex to resolve with suggestions alone:

1. **Abort the operation**:
   - During rebase: `git rebase --abort`
   - During merge: `git merge --abort`
2. **Inform the user**: "The conflicts are too complex for automated suggestions. I've aborted the [rebase/merge] to return to a clean state."
3. **Provide options**:
   - Resolve conflicts manually, then retry
   - Use `git mergetool` for visual conflict resolution
   - Create a new branch and cherry-pick specific commits

### Detached HEAD State

If git operations result in a detached HEAD:

1. **Do NOT panic** — no work is lost.
2. **Check state**: `git log --oneline -5` to see where you are.
3. **Recovery**:
   - If work was committed on detached HEAD: `git branch <name>` to save it, then `git checkout <branch>`
   - If work was NOT committed: `git stash`, then `git checkout <branch>`, then `git stash pop`

### Accidental Commit on Wrong Branch

1. **Do NOT push**.
2. **Recovery**:
   ```bash
   git log --oneline -3  # identify the commit to move
   git checkout <correct-branch>
   git cherry-pick <commit-hash>
   git checkout <wrong-branch>
   git reset --hard HEAD~1  # remove from wrong branch
   ```
3. **Verify**: `git log --oneline -3` on both branches.
