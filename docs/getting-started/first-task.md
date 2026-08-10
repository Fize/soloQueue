# First useful task

This walkthrough uses a local project and a small engineering request so that
the routing, project scope, tool confirmation, and run history are visible.

## 1. Register a project

Open Settings → Projects, add an existing repository using its absolute path,
and give it a short name. Keep the project inside a directory you are
comfortable allowing tools to inspect or modify.

## 2. Open a project session

Create a new chat and select the project. Ask for a bounded task with an
observable result, for example:

    Inspect the README and list the three commands needed to build and run this
    project. Do not modify files.

The request should remain read-only, so it is a good first check of the
workspace context.

## 3. Try a controlled change

After the read-only request succeeds, ask for one small change and include
acceptance criteria:

    Add a short “Development” section to the README. Keep the existing
    structure, use the repository's package manager, and show the command that
    actually starts the service. Run the relevant documentation checks
    afterwards.

When a write or shell command requires approval, inspect the confirmation
before accepting it.

## 4. Observe the harness

During the run, the UI can show the selected task type and model, tool calls,
delegation, and the final result. For a larger task, let the agent delegate a
bounded subtask and compare the child result with the parent summary.

## 5. Inspect the result

Check the project diff yourself. If the task was run through a workflow, open
Workflows → Runs to inspect node state, outputs, and audit events. Treat the
run record as evidence of what SoloQueue executed, not as a substitute for
reviewing the repository.
