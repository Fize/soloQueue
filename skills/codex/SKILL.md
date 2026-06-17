---
name: codex
description: Delegate tasks to Codex.
when_to_use: When you need to delegate code completion, explanation, refactoring, or editing tasks to Codex.
allowed-tools: Bash
---

# Codex Integration Skill

This skill guides you in delegating code completion, editing, and explanation tasks to the external **Codex CLI** (`codex`).

---

## Technical Specifications

### CLI Options Reference

- `codex "$ARGUMENTS"`: Invoke Codex directly with a prompt.
- `codex app-server --listen stdio://`: Runs Codex in LSP/ACP server mode (JSON-RPC interface).
- `codex --help`: Shows general command line help.

---

## Step-by-Step Execution Protocol

### Step 1: Context Preparation

Codex operates in a separate process. You must supply all necessary context in the prompt.

1. Identify target files and line ranges.
2. Compile a detailed prompt including the context of the files and what you want Codex to complete or edit.

### Step 2: Formulate the Command

Run the command via the `Bash` tool:

```bash
codex "Complete the implementation of the auth handler in internal/auth/login.go"
```

If you need to query helper options or verify the installation:

```bash
codex --help
```

### Step 3: Parse and Summarize Output

1. Extract the code output or edits.
2. Report the completed changes back to the calling context.
