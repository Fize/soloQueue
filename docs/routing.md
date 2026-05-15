# Task Routing & Classification

SoloQueue uses intelligent task classification to route user input to the appropriate processing level (L0-L3).

## Classification Levels

| Level | Name | Scope | Model | Thinking |
|-------|------|-------|-------|----------|
| **L0** | Conversation | Q&A, explanation | deepseek-v4-flash | disabled |
| **L1** | Simple | Single file changes | deepseek-v4-flash-thinking | high |
| **L2** | Medium | Multi-file features (2-5 files) | deepseek-v4-pro | high |
| **L3** | Complex | Architecture changes (5+ files) | deepseek-v4-pro-max | max |

## How It Works

The classifier uses a **dual-channel** strategy:

1. **Fast Track** (always first): Pattern-based rules (zero latency)
   - Matches keywords, file paths, slash commands
   - Supports Chinese and English

2. **LLM Fallback** (when uncertain): Lightweight call to deepseek-v4-flash
   - 4-second timeout
   - Degrades gracefully to L1 on error

The result with higher confidence wins.

## Hybrid Sticky Level

The classifier is **session-aware**. Without this, short follow-up messages would be misclassified.

**Problem:**
```
User: "Refactor the auth module"  → L3 ✓
User: "test again"                  → L1 ✗ (wrong!)
```

**Solution:** Session remembers its current task level.

| New Confidence | Prior Level | Result |
|---------------|-------------|--------|
| ≥ 85 (high) | any | Use new result |
| 50–84 (medium) | higher than new | Use prior (stay at higher level) |
| < 50 (low) | exists | Use prior (inherit context) |

## Explicit Level Locking

Users can lock the session to a specific level:

```
/l0    Lock to L0 (fastest)
/l1    Lock to L1 (simple)
/l2    Lock to L2 (medium)
/l3    Lock to L3 (complex, maximum capability)
/max   Same as /l3
/expert Same as /l3
/chat  Same as /l0
```

**Lock behavior:**
- Once locked, all subsequent messages use the locked level
- Lock applies until a new lock command changes it
- Locked level is displayed in the runtime status API

## Slash Commands (Non-Locking)

These influence classification for the **current message only**:

```
/read <file>       Classify as file reading (L1-L2)
/write <file>      Classify as file writing (L1-L2)
/refactor <files>  Classify as refactoring (L2-L3)
/test <scope>      Classify as testing (L1-L2)
/debug <target>    Classify as debugging (L1-L2)
/fast              Force L0 with no thinking (one-shot)
```

## Escalation & De-escalation

**Escalation** (bump level up):
- "think carefully", "be thorough", "in depth"
- "solve thoroughly" (bumps by +2)

**De-escalation** (bump level down):
- "just", "quick", "simple"

## Architecture

```
User Prompt
    │
    ▼
Session.AskStream()
    │
    ├─ Check for /l0-/l3 lock command
    │   └─ Found → set levelLocked=true, save level
    │
    ├─ levelLocked? ──yes──→ Use cached RouteResult, skip router
    │
    └─ Not locked → Router.Route(ctx, prompt, priorLevel)
                        │
                        ▼
                    Classifier.Classify()
                        │
                        ├─ FastTrack (always)
                        │   └─ Patterns, file paths, escalation
                        │
                        ├─ LLM Fallback (if uncertain)
                        │   └─ Semantic classification
                        │
                        └─ applyHybridLogic(result, priorLevel)
                            └─ Sticky level decision
```

## Related Files

| File | Purpose |
|------|---------|
| `internal/router/models.go` | Level constants, ClassificationResult |
| `internal/router/fasttrack.go` | Pattern-based Fast Track classifier |
| `internal/router/llm_classifier.go` | LLM semantic fallback |
| `internal/router/classifier.go` | Dual-channel classifier + hybrid logic |
| `internal/router/router.go` | Router: classification → model params |
| `internal/session/session.go` | Session: level lock, sticky state |
