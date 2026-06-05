---
name: gemini-cli
description: Delegate complex coding tasks, refactoring, and codebase analysis to the Gemini CLI.
when_to_use: When you need to delegate code writing, bug fixing, test generation, codebase refactoring, or code analysis to Gemini CLI.
allowed-tools: Bash
---
# Gemini CLI Integration Skill

This skill guides you in delegating complex software engineering and codebase manipulation tasks to the external **Gemini CLI** (`gemini`).

## YOU ARE BOUND BY THIS CONTRACT

When using this skill to run Gemini CLI, you are delegating execution to a powerful sub-agent. Follow these instructions precisely to ensure clean execution, proper session management, and context sharing.

---

## Technical Specifications & CLI Options
- `-p, --prompt <string>`: Runs Gemini CLI in non-interactive (headless) mode with the given prompt. **This flag is mandatory for automated delegation.**
- `-y, --yolo` (or `--approval-mode yolo`): Auto-approves all tools (required for autonomous execution).
- `--skip-trust`: Bypasses the workspace trust dialog. **Always set this to true for automated runs.**
- `-r, --resume <session>`: Resumes a previous session by session ID, index, or `"latest"`.

---

## Step-by-Step Execution Protocol

### Step 1: Directory & Context Verification
1. Ensure you run the `Bash` tool in the **project root directory** (which is your current working directory).
2. Gemini CLI executes in a separate process and **does not share your in-memory conversation history**. You must pass all target file paths (relative to project root), logs, and database schemas explicitly in the prompt.

### Step 2: Session ID Management (In-Memory / Task-Scoped Only)
Session IDs must **only** be reused within the context of the **same active task**. Do not share sessions across different tasks.
1. **Initial Run**: Start the first run of the task without a session ID:
   ```bash
   gemini -p "$ARGUMENTS" --yolo --skip-trust
   ```
2. **Capture Session ID**: Inspect the output of the command. Look for the printed session ID. Keep this ID in your memory/conversation history for the duration of this task.
3. **Follow-up Runs (Same Task Only)**: If you need to make consecutive runs or follow-up corrections *for the same task*, pass the captured session ID:
   ```bash
   gemini -p "Now fix the failing test you just observed." --resume <session_id> --yolo --skip-trust
   ```
4. **Task Completion**: Once the overall task is finished, discard the session ID. Do not store it to any files.

### Step 3: Parse and Summarize Output
1. Extract the final answer and any diffs/changes made to the files from the stdout.
2. Summarize the changes and report them back to the calling context.

---

## Troubleshooting & Guardrails
- **Stuck CLI**: Ensure `--yolo` and `--skip-trust` are both set to prevent Gemini CLI from prompting for confirmation.
- **Git State**: If changes are made to files, verify them using `git status` or `git diff`.
