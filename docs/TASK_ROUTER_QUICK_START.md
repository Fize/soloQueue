# Task Router Classifier - Quick Start Guide

## Overview

The Task Router Classifier (TRC) automatically routes user input to the most appropriate processing level, selecting the right LLM model and features based on task complexity.

## Classification Levels at a Glance

| Level | Model | Thinking | Use Case | Keywords |
|-------|-------|----------|----------|----------|
| **L0** | flash | None | Pure conversation, explanations | explain, how, what, design |
| **L1** | flash-thinking | High | Single file edits, quick fixes | fix, add, change (single file) |
| **L2** | pro | High | Multi-file features, refactoring | refactor, migrate, implement (2-5 files) |
| **L3** | pro-max | Max | Complex architecture, large scope | redesign, optimize, full refactor (5+ files) |

## Detection Mechanism

### 1. Fast-Track Rules (< 5ms)
- Count file paths in input
- Detect dangerous operations
- Extract keywords
- Calculate confidence score
- **Output**: Level + Confidence (0-100)

### 2. Semantic Classification (if confidence < 75%)
- Send to LLM for semantic understanding
- Cache result for future identical inputs
- **Output**: Level + Confidence

### 3. Safety Checks
- Block dangerous operations (rm -rf, dd, DROP TABLE, etc.)
- Require confirmation for risky operations
- Escalate if safety concerns detected

## Slash Commands (Manual Override)

Force a specific level:
```
/l0 explain how this authentication works
/l1 fix the bug on line 42 of auth.js  
/l2 add authentication across all services
/l3 refactor the entire auth system
```

Shorthand commands:
```
/read <file>              # Read and analyze file
/write <file> <code>      # Write new file
/refactor <scope>         # L2/L3 refactoring
/run <command>            # Execute shell command
/debug <description>      # L2/L3 debugging
/classify                 # Show classification of last input
/stats                    # Show router statistics
```

## Examples

### L0: Pure Conversation
```
Input: "Explain how closures work in JavaScript"
Detection: No files, "explain" keyword → L0 (95% confidence)
Model: flash, No thinking
Tokens: ~500 (low cost)
```

### L1: Single File
```
Input: "Fix the null pointer bug on line 42 of services/auth.go"
Detection: 1 file path, "fix" keyword → L1 (90% confidence)
Model: flash-thinking, High reasoning
Tokens: ~5,000 (low-medium cost)
```

### L2: Multi-File Feature
```
Input: "Add user authentication. Update auth.go, middleware.go, and create login.tsx"
Detection: 3 file paths, "add", "update" keywords → L2 (85% confidence)
Model: pro, High reasoning
Tokens: ~20,000 (medium cost)
```

### L3: Complex Refactoring
```
Input: "Refactor the entire error handling system across all services"
Detection: No specific paths, "refactor entire" keyword → LLM classification → L3 (90%)
Model: pro-max, Max reasoning
Tokens: ~50,000+ (high cost, justified by complexity)
```

## Performance Targets

| Metric | Target |
|--------|--------|
| Fast-track classification | < 5ms |
| LLM classification | < 1s |
| Total routing decision | < 100ms (p95) |
| Cache hit rate | > 60% |
| Classification accuracy | > 85% |

## Configuration

In `settings.toml`:
```toml
[router]
enabled = true
fastTrackThreshold = 75.0      # Confidence needed to skip LLM
semanticThreshold = 60.0       # Min confidence for any classification
useSemanticClassifier = true   # Use LLM for ambiguous cases
classificationCache = true     # Cache recent classifications
cacheTTL = 3600               # Cache duration in seconds
blockDangerousOps = true      # Block rm -rf, DROP TABLE, etc.
confirmDangerousOps = true    # Ask before dangerous operations
```

## Architecture Impact

### Minimal Changes
- **New package**: `internal/router/`
- **Enhanced**: `internal/server/server.go` (routing call)
- **Enhanced**: `cmd/soloqueue/main.go` (session factory pool)
- **No breaking changes** to existing APIs

### Backward Compatible
- Router is optional (can disable in config)
- Existing sessions work unchanged
- Default model used if router disabled

## Implementation Phases

1. **Week 1**: Fast-track classifier + safety checks
2. **Week 2**: LLM semantic classifier + caching
3. **Week 3**: HTTP server + session factory integration
4. **Week 4**: Slash commands support
5. **Week 5**: Monitoring + optimization

## Key Files

| File | Purpose |
|------|---------|
| `docs/TASK_ROUTER_DESIGN.md` | Full design specification |
| `internal/router/router.go` | Main Router interface |
| `internal/router/classifier.go` | Fast-track rules |
| `internal/router/semantic_classifier.go` | LLM-based classification |
| `internal/router/safety.go` | Safety checks |
| `internal/router/types.go` | Types and constants |

## Testing

Run tests:
```bash
go test ./internal/router/...
```

Benchmarks:
```bash
go test -bench=Classify ./internal/router/...
```

## FAQ

**Q: Will this add latency?**  
A: No. Fast-track (< 5ms) handles 80%+ of cases. LLM only used for ambiguous inputs, cached for future hits.

**Q: Can users override the classification?**  
A: Yes! Slash commands (/l0, /l1, /l2, /l3) force specific levels.

**Q: Is it backward compatible?**  
A: Yes. Router is optional and doesn't break existing code.

**Q: How accurate is the classification?**  
A: Target > 85%. Fast-track + LLM semantic validation ensures high accuracy.

**Q: What if LLM classification fails?**  
A: Falls back to L1 (flash-thinking) for safety.

---

For complete details, see `TASK_ROUTER_DESIGN.md`
