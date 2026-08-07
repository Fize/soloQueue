# Task Routing & Classification

SoloQueue uses intelligent task classification to route user input to the appropriate processing model based on work nature (`general`, `engineering`, `research`).

## Task Types (Work-Nature Taxonomy)

| Task Type | Scope / Description | Typical Use Case |
| --------- | ------------------- | ---------------- |
| **`general`** | Q&A, writing, translation, summarizing | Conversation & general text tasks |
| **`engineering`** | Code, repositories, debugging, tests, deployment | Technical & software development work |
| **`research`** | Web search, current info, source verification | Information retrieval & research |

> Note: Routing classifies tasks by work nature, not by difficulty.

## How It Works

The classifier uses a **dual-channel** strategy:

1. **Local Fast Track** (always first): Pattern-based rules (zero latency)
   - Matches code blocks, stack traces, file paths, commands, search keywords
   - Supports Chinese and English

2. **LLM Fallback** (when uncertain): Lightweight classification call
   - 2-second timeout
   - Preserves previous task type on error or fallback

The result determines which model and thinking parameters to apply for the execution turn.

## Session Continuity

The session remembers the `PreviousTaskType` of prior interactions to maintain classification context across follow-up prompts.

## Architecture

```
User Prompt
    │
    ▼
Session.AskStream()
    │
    └─ Router.Route(ctx, input, history)
            │
            ▼
        Classifier.Classify()
            │
            ├─ Local FastTrack (pattern matching)
            │
            └─ LLM Fallback (if ambiguous)
```

## Related Files

| File | Purpose |
| ---- | ------- |
| `internal/tasktype/tasktype.go` | TaskType taxonomy definitions (`general`, `engineering`, `research`) |
| `internal/router/models.go` | ClassifyInput & ClassificationResult structs |
| `internal/router/fasttrack.go` | Pattern-based Local Classifier |
| `internal/router/llm_classifier.go` | LLM semantic classifier fallback |
| `internal/router/classifier.go` | Default classifier orchestrator |
| `internal/router/router.go` | Router: task classification → model parameter resolution |
| `internal/session/session.go` | Session level tracking & execution payload assembly |
