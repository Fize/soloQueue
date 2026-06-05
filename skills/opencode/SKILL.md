---
name: opencode
description: Delegate complex coding tasks, refactoring, and codebase analysis to the OpenCode CLI.
when_to_use: When you need to delegate code writing, bug fixing, test generation, codebase refactoring, or code analysis to OpenCode CLI.
allowed-tools: Bash
---
# OpenCode Integration Skill

This skill guides you in delegating complex software engineering and codebase manipulation tasks to the external **OpenCode CLI** (`opencode`).

## YOU ARE BOUND BY THIS CONTRACT

When using this skill to run OpenCode, you are delegating execution to a powerful sub-agent. Follow these instructions precisely to ensure clean execution, proper session management, and context sharing.

---

## Technical Specifications & CLI Options
- `opencode run [message..]`: Main command to execute OpenCode with a specific prompt.
- `--dangerously-skip-permissions`: Automatically approves all tool execution permissions (required for non-interactive execution).
- `-s, --session <session_id>`: Resumes a specific session by ID.

---

## Step-by-Step Execution Protocol

### Step 1: Directory & Context Verification
1. Ensure you run the `Bash` tool in the **project root directory** (which is your current working directory).
2. OpenCode executes in a separate process and **does not share your in-memory conversation history**. You must pass all target file paths (relative to project root), logs, and database schemas explicitly in the prompt.

### Step 2: Session ID Management (In-Memory / Task-Scoped Only)
Session IDs must **only** be reused within the context of the **same active task**. Do not share sessions across different tasks.
1. **Initial Run**: Start the first run of the task without a session ID:
   ```bash
   opencode run "$ARGUMENTS" --dangerously-skip-permissions
   ```
2. **Capture Session ID**: Inspect the output of the command. Look for the printed session ID. Keep this ID in your memory/conversation history for the duration of this task.
3. **Follow-up Runs (Same Task Only)**: If you need to make consecutive runs or follow-up corrections *for the same task*, pass the captured session ID:
   ```bash
   opencode run "Now fix the failing test you just observed." --session <session_id> --dangerously-skip-permissions
   ```
4. **Task Completion**: Once the overall task is finished, discard the session ID. Do not store it to any files.

### Step 3: Parse and Summarize Output
1. Extract the final answer and any diffs/changes made to the files from the stdout.
2. Summarize the changes and report them back to the calling context.

---

## Troubleshooting & Guardrails
- **Stuck CLI**: Ensure `--dangerously-skip-permissions` is set to prevent the agent from hanging on interactive prompts.
- **Git State**: OpenCode may modify files directly. Check `git status` or `git diff` after execution.
