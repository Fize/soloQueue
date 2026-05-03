# Task Routing & Classification

SoloQueue uses an intelligent task classification system to route user input to the appropriate processing level. The system balances latency, cost, and capability by selecting the right model for each task.

## Classification Levels

| Level | Name | Typical Scope | Model | Thinking |
|-------|------|--------------|-------|----------|
| **L0** | Conversation | Q&A, explanation, discussion | deepseek-v4-flash | disabled |
| **L1** | Simple Single File | Fix a bug, add a field, rename | deepseek-v4-flash-thinking | high |
| **L2** | Medium Multi-File | Refactor, implement feature, 2-5 files | deepseek-v4-pro | high |
| **L3** | Complex Refactoring | Architecture, rewrite, 5+ files | deepseek-v4-pro-max | max |

## How Classification Works

The classifier uses a **dual-channel** strategy:

1. **Fast Track** (always runs first): Pattern-based rules with zero latency. Matches keywords, file paths, slash commands, and escalation modifiers in both Chinese and English.

2. **LLM Fallback** (when Fast Track is uncertain): A lightweight call to deepseek-v4-flash for semantic understanding. Has a 4-second timeout; degrades gracefully to L1 on error.

The result with higher confidence wins.

## Hybrid Sticky Level (Session Context)

A key innovation: the classifier is **session-aware**. Without this, short follow-up messages within an ongoing complex task would be misclassified.

### The Problem

```
User: "重构整个认证模块，把所有 JWT 逻辑抽到独立 package"  → L3 ✓
User: "再次测试"                                           → L1 ✗ (wrong!)
```

"再次测试" in isolation looks like L1. But within a complex refactoring session, it means "re-run the tests for the refactoring we're doing" — it should inherit L3.

### The Solution: Hybrid Sticky Logic

The session remembers its **current task level**. When classifying a new message, the prior level is passed to the classifier:

| New Confidence | Prior Level | Result |
|---------------|-------------|--------|
| ≥ 85 (high) | any | Use new result (clear signal wins) |
| 50–84 (medium) | higher than new | Use prior (stay at higher level) |
| < 50 (low) | exists | Use prior (inherit session context) |
| any | none | Use new result (no prior to consider) |

### Examples

**Scenario 1: Complex task with short follow-up**
```
1. "重构整个认证模块"        → L3, confidence=92 (high)
   Session level: L3

2. "再次测试"                → Fast Track: L1, confidence=45 (low)
   Prior: L3, confidence < 50 → inherit L3 ✓
   Session level: L3
```

**Scenario 2: Genuine topic switch**
```
1. "重构认证模块" (many messages at L3)
   Session level: L3

2. "解释一下什么是 JWT"      → Fast Track: L0, confidence=88 (high)
   Prior: L3, confidence ≥ 85 → use L0 ✓
   Session level: L0
```

**Scenario 3: Medium confidence with higher prior**
```
1. "实现用户权限系统"        → L2, confidence=80
   Session level: L2

2. "加一个 role 字段"        → Fast Track: L1, confidence=65 (medium)
   Prior: L2, confidence 50-84, L2 > L1 → use L2 ✓
   Session level: L2
```

## Explicit Level Locking

Users can lock the session to a specific level with slash commands:

```
/l0    Lock to L0 (Conversation, fastest)
/l1    Lock to L1 (Simple single file)
/l2    Lock to L2 (Medium multi-file)
/l3    Lock to L3 (Complex refactoring, maximum capability)
/max   Same as /l3
/expert Same as /l3
/chat  Same as /l0
```

### Lock Behavior

- Once locked, **all subsequent messages** use the locked level — routing is bypassed entirely
- Lock applies until a **new lock command** changes it
- Locked level is displayed in the TUI header

### Lock Examples

```
User: "/l2 分析这个 bug"
  → Session locked to L2
  → All following messages use L2 model (deepseek-v4-pro)

User: "再看看日志"
  → No re-classification, stays L2 ✓

User: "你好"
  → Even greetings stay L2 when locked

User: "/l0"
  → Unlock to L0, back to normal routing
```

## Slash Commands (Non-Locking)

These slash commands influence classification for the **current message only** and don't lock the level:

```
/read <file>       Classify as file reading (L1-L2)
/write <file>      Classify as file writing (L1-L2)
/refactor <files>  Classify as refactoring (L2-L3)
/test <scope>      Classify as testing (L1-L2)
/debug <target>    Classify as debugging (L1-L2)
/fast              Force L0 with no thinking (one-shot)
```

## Escalation & De-escalation Keywords

Certain keywords in the prompt influence the level:

**Escalation** (bump level up by 1-2):
- "仔细想", "深入分析", "深度思考"
- "think carefully", "be thorough", "in depth"
- "彻底解决" (bumps by +2)

**De-escalation** (bump level down by 1):
- "简单", "快速", "随便"
- "just", "quick", "simple", "keep it simple"

## Architecture Overview

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
                        ├─ LLM Fallback (if FastTrack uncertain)
                        │   └─ Semantic classification
                        │
                        └─ applyHybridLogic(result, priorLevel)
                            └─ Sticky level decision
                                │
                                ▼
                            Final Level + Model Params
                                │
                                ▼
                            SetModelOverride on Agent
```

## Related Files

| File | Purpose |
|------|---------|
| `internal/router/models.go` | Level constants, ClassificationResult, config |
| `internal/router/fasttrack.go` | Pattern-based Fast Track classifier |
| `internal/router/llm_classifier.go` | LLM semantic fallback classifier |
| `internal/router/classifier.go` | Dual-channel classifier + hybrid logic |
| `internal/router/router.go` | Router: classification → model params |
| `internal/session/session.go` | Session: level lock, sticky state, routing call |
