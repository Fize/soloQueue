# First useful task

> 中文：[第一个有用任务](../zh/getting-started/first-task.md)

I use a local project and a small engineering request in this walkthrough so I
can see routing, project scope, tool confirmation, and run history.

## 1. Register a project

I open Settings → Projects, add an existing repository using its absolute path,
and give it a short name. I keep the project inside a directory I am
comfortable allowing tools to inspect or modify.

## 2. Open a project session

I create a new chat, select the project, and ask for a bounded task with an
observable result, for example:

    Inspect the README and list the three commands needed to build and run this
    project. Do not modify files.

I keep this request read-only, so it gives me a good first check of the
workspace context.

## 3. Try a controlled change

After the read-only request succeeds, I ask for one small change and include
acceptance criteria:

    Add a short “Development” section to the README. Keep the existing
    structure, use the repository's package manager, and show the command that
    actually starts the service. Run the relevant documentation checks
    afterwards.

When a write or shell command requires my approval, I inspect the confirmation
before accepting it.

## 4. Observe the harness

During the run, I can see the selected task type and model, tool calls,
delegation, and the final result. For a larger task, I let the agent delegate a
bounded subtask and compare the child result with the parent summary.

## 5. Inspect the result

I check the project diff myself. If I run the task through a workflow, I open
Workflows → Runs to inspect node state, outputs, and audit events. I treat the
run record as evidence of what SoloQueue executed, not as a substitute for
reviewing the repository.
