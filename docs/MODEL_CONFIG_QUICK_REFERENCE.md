# Model Configuration - Quick Reference

## Four Role Tiers

| Role | Model | Speed | Thinking | API Effort | Use Case |
|------|-------|-------|----------|-----------|----------|
| **fast** | deepseek-v4-flash | ⚡ Fastest | ❌ No | - | Quick responses, compression |
| **universal** | deepseek-v4-flash-thinking | ⚡⚡ Fast | ✅ Yes | high | General tasks, analysis |
| **superior** | deepseek-v4-pro | ⚡⚡⚡ Med | ✅ Yes | high | Complex reasoning, decisions |
| **expert** | deepseek-v4-pro-max | 🐢 Slow | ✅ Yes | max | Critical decisions, security |

## Resolution Priority

```
1. Explicit configuration in settings.toml
2. Fallback configuration in settings.toml
3. Hardcoded defaults (always works)
```

## Configuration File

**Location:** `~/.soloqueue/settings.toml`

```toml
# Override defaults (optional)
[DefaultModels]
expert = "deepseek:deepseek-v4-pro-max"
superior = "deepseek:deepseek-v4-pro"
universal = "deepseek:deepseek-v4-flash-thinking"
fast = "deepseek:deepseek-v4-flash"
fallback = "deepseek:deepseek-v4-flash"  # Used if role not configured
```

## Key Code Points

### Query Model by Role

```go
model := cfg.DefaultModelByRole("fast")
```

### Access Model Parameters

```go
model.Generation.Temperature      // 0.0
model.Generation.MaxTokens        // 8192
model.Thinking.Enabled            // true/false
model.Thinking.ReasoningEffort     // "high" or "max"
model.ContextWindow               // 1048576 (1M tokens)
```

### Map to API

```go
// APIModel field (if set) overrides ID
apiModel := model.APIModel
if apiModel == "" {
    apiModel = model.ID
}
// Send to API: {"model": apiModel, "reasoning_effort": reasoningEffort}
```

## Default Models (Hardcoded Fallback)

Located in `internal/config/defaults.go`:

```go
"expert":    "deepseek:deepseek-v4-pro-max"
"superior":  "deepseek:deepseek-v4-pro"
"universal": "deepseek:deepseek-v4-flash-thinking"
"fast":      "deepseek:deepseek-v4-flash"
```

## Common Tasks

### Get Expert Model for Critical Task

```go
expertModel := cfg.DefaultModelByRole("expert")
// Uses max reasoning, deep analysis
```

### Get Fast Model for Compression

```go
fastModel := cfg.DefaultModelByRole("fast")
// Direct inference, no thinking mode
```

### Override via Environment

Set environment variable:
```bash
export DEEPSEEK_API_KEY="sk-..."
```

### Add New Provider

1. Add to `Providers` array in defaults.go
2. Add models to `Models` array
3. Reference in DefaultModels as `"provider:model_id"`

## Architecture Integration

- **L1 (Primary Agent):** Uses "fast" model by default
- **L2 (Leaders):** Can use "superior" or "universal"
- **L3 (Workers):** Configurable per task
- **Compactor:** Hardcoded to "fast" model

## Testing Models

```bash
# Run config tests
go test ./internal/config -v

# Check model resolution
go run cmd/soloqueue/main.go version
```

## Troubleshooting

**Error: "no default model configured"**
- Check `~/.soloqueue/settings.toml` exists
- Verify `DefaultModels` section if present
- Check DEEPSEEK_API_KEY environment variable

**Wrong model being used**
- Check explicit configuration in settings.toml
- Verify provider and model ID format: "provider:id"
- Check models are enabled in configuration

**Thinking mode not working**
- Verify model has `Thinking.Enabled = true`
- Check `ReasoningEffort` is "high" or "max"
- Ensure model is not flash model without thinking

## File Locations

```
~/.soloqueue/settings.toml          ← User configuration
internal/config/defaults.go          ← Hardcoded defaults
internal/config/schema.go            ← Type definitions
internal/config/service.go           ← Query service
cmd/soloqueue/main.go                ← Usage examples
```

## Think Token Economy

- **No thinking:** Standard tokens only
- **reasoning_effort: "high":** +20% to reasoning tokens
- **reasoning_effort: "max":** +40% to reasoning tokens

Total completion = regular tokens + reasoning tokens (if applicable)
