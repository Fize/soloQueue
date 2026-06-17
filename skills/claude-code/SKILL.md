---
name: claude-code
description: Delegate complex coding tasks, refactoring, and codebase analysis to Anthropic's Claude Code CLI.
when_to_use: When you need to delegate code writing, bug fixing, test generation, codebase refactoring, or code analysis to Claude Code CLI.
allowed-tools: Bash
---

# Claude Code Integration Skill

This skill guides you in delegating complex software engineering and codebase manipulation tasks to the external **Claude Code CLI** (`claude`) tool.

## YOU ARE BOUND BY THIS CONTRACT

When using this skill to run Claude Code, you are delegating execution to a powerful sub-agent. Follow these instructions precisely to ensure clean execution, proper session management, and context sharing.

---

## Technical Specifications & CLI Options

- `-p, --print`: Runs Claude Code in non-interactive (headless) mode, prints the result, and exits. **This flag is mandatory for automated delegation.**
- `--permission-mode bypassPermissions`: Bypasses all interactive tool permission prompts, enabling full autonomy.
- `--session-id <uuid>`: Starts or continues a conversation using a specific UUID.

---

## Step-by-Step Execution Protocol

### Step 1: Directory & Context Verification

1. Ensure you run the `Bash` tool in the **project root directory** (which is your current working directory).
2. Claude Code executes in a separate process and **does not share your in-memory conversation history**. You must pass all target file paths (relative to project root), logs, and database schemas explicitly in the prompt.

### Step 2: Session ID Management (In-Memory / Task-Scoped Only)

Session IDs must **only** be reused within the context of the **same active task**. Do not share sessions across different tasks.

1. **Initial Run**: Start the first run of the task without a session ID:
   ```bash
   claude -p "$ARGUMENTS" --permission-mode bypassPermissions
   ```
2. **Capture Session ID**: Inspect the output of the command. Look for the printed session ID/UUID. Keep this ID in your memory/conversation history for the duration of this task.
3. **Follow-up Runs (Same Task Only)**: If you need to make consecutive runs or follow-up corrections _for the same task_, pass the captured session ID:
   ```bash
   claude -p "Now fix the failing test you just observed." --session-id <UUID> --permission-mode bypassPermissions
   ```
4. **Task Completion**: Once the overall task is finished, discard the session ID. Do not store it to any files.

### Step 3: Parse and Summarize Output

1. Extract the final answers, test outcomes, or file modifications from the CLI's stdout.
2. Summarize the changes and report them back to the calling context.

---

## Troubleshooting & Guardrails

- **Stuck CLI**: If execution hangs, it is likely waiting for user input. Ensure `--permission-mode bypassPermissions` is set.
- **Git State**: If Claude Code makes modifications, run `git status` or `git diff` after execution to review the changes.
