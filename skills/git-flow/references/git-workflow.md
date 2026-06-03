# Git Workflow Specification

This document defines the Git workflow standards for projects using the `git-flow` skill.

## 1. Commit Message Format

### Format

```
<type>(<scope>): <subject>

[optional body]

Co-Authored-By: AI Agent
```

### Rules

1. **Type**: Must be one of the predefined types (see section 2)
2. **Scope**: Optional, indicates the module/component affected
3. **Subject**:
   - English: ≤ 50 characters
   - Chinese: ≤ 25 characters
   - Imperative mood (add, fix, update, not added, fixed, updated)
   - No period at the end
4. **Body**:
   - Optional, only for complex changes
   - ≤ 72 characters per line
   - Max 3 lines
5. **AI Author Tag**: Required for commits generated using `git-flow` skill
   - Format: `Co-Authored-By: AI Agent`

### Examples

```
# Good examples
feat(auth): add OAuth2 login support

Co-Authored-By: AI Agent
```

```
fix(login): fix null password bypass vulnerability

Co-Authored-By: AI Agent
```

```
refactor(auth): extract auth logic into separate module

Co-Authored-By: AI Agent
```

```
# Bad examples
added OAuth2 login support
# ❌ Missing type(scope): prefix

feat: add OAuth2 login support for users who want to use third-party authentication with Google and Facebook
# ❌ Subject too long

feat(auth): add OAuth2 login support

This is a very long body that exceeds the 3 line limit and provides too much detail about the implementation that should be in the documentation or code comments.
# ❌ Body too long
```

## 2. Commit Types

| Type | Description | When to Use | Example |
|------|-------------|--------------|---------|
| `feat` | New feature | Adding new functionality | `feat(auth): add OAuth2 login support` |
| `fix` | Bug fix | Fixing a bug | `fix(login): fix null password bypass` |
| `refactor` | Refactoring | Code changes that neither fix bugs nor add features | `refactor(auth): extract auth logic` |
| `perf` | Performance improvement | Code changes that improve performance | `perf(query): optimize user query index` |
| `docs` | Documentation | Documentation only changes | `docs(api): update REST API docs` |
| `test` | Testing | Adding or updating tests | `test(auth): add OAuth2 test cases` |
| `chore` | Build/tool changes | Changes to build process, tools, dependencies | `chore(deps): upgrade flask to 3.0` |
| `ci` | CI configuration | Changes to CI configuration files | `ci(gitlab): add multi-stage pipeline` |

## 3. Commit Granularity

### Principles

1. **One commit per logical change**
   - Each commit should represent a single logical change
   - Don't mix unrelated changes in one commit

2. **Split different features into different commits**
   - If you add feature A and feature B, create separate commits
   - Makes it easier to revert or cherry-pick individual features

3. **Don't mix feat and fix in same commit**
   - If you add a feature and fix a bug, create separate commits
   - Exception: If the fix is directly related to the feature, they can be in one commit

4. **WIP commits**
   - Use `wip:` prefix for work-in-progress commits
   - Don't push WIP commits to remote
   - Squash WIP commits before merging

### Splitting Rules

| Scenario | Action | Example |
|----------|---------|---------|
| Different features | Split | `feat(auth): add OAuth2` + `feat(utils): add token utils` |
| Feature + tests | Can merge | `feat(auth): add OAuth2 login support` (includes tests) |
| Feature + docs | Split | `feat(auth): add OAuth2` + `docs(auth): update docs` |
| Cross-directory changes | Split | `src/` changes + `docs/` changes |
| Bug fix + refactor | Split | `fix(login): fix bug` + `refactor(login): cleanup` |

## 4. Subject Length Limit

### Rules

1. **English**: ≤ 50 characters
2. **Chinese**: ≤ 25 characters
3. If subject exceeds limit:
   - Try to shorten the subject
   - If can't shorten, use body to provide additional context (max 3 lines)

### Examples

```
# Good examples
feat(auth): add OAuth2 login support
# ✅ 38 characters (English)

fix(登录): 修复空密码绕过漏洞
# ✅ 12 characters (Chinese)
```

```
# Bad examples
feat(auth): add OAuth2 login support for users who want to use third-party authentication with Google and Facebook
# ❌ Too long (116 characters)

fix(登录模块): 修复用户在使用第三方登录时可能遇到的空指针异常问题
# ❌ Too long (32 characters)
```

## 5. Branch Naming Convention

### Format

```
<type>/<short-description>

or

<type>/<issue-id>-<short-description>
```

### Rules

1. **Type**: Must match commit type (`feat`, `fix`, `refactor`, `docs`, `test`, `chore`)
2. **Short description**:
   - Lowercase
   - Hyphen-separated words
   - ≤ 30 characters
3. **Issue ID**: Optional, include if tracking with issue tracker

### Examples

| Branch Name | Description |
|-------------|-------------|
| `feat/oauth2-login` | New feature: OAuth2 login |
| `fix/123-login-bypass` | Bug fix: Issue #123, login bypass |
| `refactor/auth-module` | Refactoring: Auth module |
| `docs/api-endpoints` | Documentation: API endpoints |
| `test/oauth2-cases` | Testing: OAuth2 test cases |
| `chore/upgrade-flask` | Chore: Upgrade Flask dependency |
| `ci/multi-stage-pipeline` | CI: Multi-stage pipeline |

### Invalid Examples

| Branch Name | Issue |
|-------------|-------|
| `feature/oauth2` | ❌ Use `feat/` not `feature/` |
| `Feat/oauth2` | ❌ Type must be lowercase |
| `feat/OAuth2_Login` | ❌ Use hyphens, not underscores or camelCase |
| `feat/very-long-branch-name-that-exceeds-thirty-characters` | ❌ Too long |

## 6. AI Author Tagging

### Rule

Commits generated using `git-flow` skill must add:

```
Co-Authored-By: AI Agent
```

### Requirements

1. **Placement**: Add after commit message body (or after subject if no body)
2. **Format**: Exactly `Co-Authored-By: AI Agent` (case-sensitive)
3. **Body length**: If body exists, must be ≤ 72 characters/line and max 3 lines

### Examples

```
# With only subject
feat(auth): add OAuth2 login support

Co-Authored-By: AI Agent
```

```
# With subject and body
feat(auth): add OAuth2 login support

Add OAuth2 login with Google and Facebook support.
Includes token refresh and user info fetching.

Co-Authored-By: AI Agent
```

## 7. Pull/Merge Request

### Title

- Follows same convention as commit message
- Format: `<type>(<scope>): <subject>`
- Subject length limit applies

### Description

Must include:

1. **Background**: Why is this change needed?
2. **Changes**: What changes were made?
3. **Test Plan**: How was this tested?

### Issue Linking

- Link issue ID using `Closes #123`, `Fixes #123`, or `Related to #123`
- Format depends on issue tracker (GitHub, GitLab, TAPD, etc.)

### Example

```markdown
## Title
feat(auth): add OAuth2 login support

## Description

### Background
Users need to login with their Google or Facebook accounts for better UX.

### Changes
- Add OAuth2 login endpoints
- Add token refresh logic
- Add user info fetching from provider
- Update login page UI

### Test Plan
- [x] Unit tests for OAuth2 logic
- [x] Integration tests with mock providers
- [x] Manual testing with Google OAuth2

Closes #123
```

## 8. Best Practices

### Do's

1. **Commit early, commit often**
   - Make small, focused commits
   - Easier to review and revert

2. **Write meaningful commit messages**
   - Clearly describe what and why
   - Not how (code shows how)

3. **Rebase before merge**
   - Keep commit history clean
   - Resolve conflicts locally

4. **Use draft PR/MR for work-in-progress**
   - Get early feedback
   - Don't block main branch

### Don'ts

1. **Don't commit sensitive information**
   - API keys, passwords, tokens
   - Use environment variables

2. **Don't commit generated files**
   - Build artifacts, dependencies
   - Use `.gitignore`

3. **Don't force push to main/master**
   - Protect main branch
   - Use protected branches feature

4. **Don't leave commented-out code**
   - Delete unused code
   - Use version control for history

## 9. Troubleshooting

### Common Issues

| Issue | Solution |
|-------|----------|
| Commit message too long | Shorten subject or move details to body (max 3 lines) |
| Merge conflicts | Use `/git-flow pull` to auto-rebase, resolve conflicts manually |
| Forgot to add AI tag | Amend commit: `git commit --amend -m "..." -m "Co-Authored-By: AI Agent"` |
| Wrong branch name | Rename: `git branch -m <new-name>`, update remote |
| Accidental push of WIP | Revert commit or force push to own branch (not main) |

### Getting Help

- Check this document: [git-workflow.md](./git-workflow.md)
- Use `/git-flow status` to check current Git status
- Ask for help in team chat
