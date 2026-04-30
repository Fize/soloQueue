# SoloQueue Model Configuration System - Research Summary

**Research Completion Date:** April 30, 2026  
**Scope:** Complete exploration of the SoloQueue model configuration and management system  
**Status:** ✅ Complete

## Executive Summary

This research document provides a comprehensive analysis of the SoloQueue model configuration system. The system implements a sophisticated **role-based model selection mechanism** that abstracts away LLM provider complexity and enables intelligent selection of models based on task type rather than infrastructure concerns.

The system is built on **four core roles** (expert, superior, universal, fast) that create a speed/quality continuum, with support for multiple LLM providers and advanced features like:
- DeepSeek V4 thinking modes with configurable reasoning effort
- Three-tier resolution priority (explicit config → fallback → hardcoded defaults)
- Hot-reload configuration without restart
- Clean abstraction between configuration IDs and actual API models

## Key Discoveries

### 1. Four-Tier Model Hierarchy

The system defines four role categories that form a natural speed/quality trade-off spectrum:

| Role | Model | Thinking | Effort | Speed | Use Case | System Role |
|------|-------|----------|--------|-------|----------|------------|
| **expert** | deepseek-v4-pro-max | ✓ | max | 10-30s | Critical decisions, security review | Decision-making |
| **superior** | deepseek-v4-pro | ✓ | high | 5-15s | Complex reasoning, architecture | Deliberation |
| **universal** | deepseek-v4-flash-thinking | ✓ | high | 2-5s | General analysis, code review | Multi-purpose |
| **fast** | deepseek-v4-flash | ✗ | - | <2s | Summaries, compression, direct | Response |

### 2. Three-Tier Resolution Priority

Every model query cascades through three levels of resolution:

```
Level 1: Explicit Configuration
  ↓ (if empty)
Level 2: Fallback Configuration  
  ↓ (if empty)
Level 3: Hardcoded Defaults (always available)
```

This design ensures:
- ✅ Users can override defaults via `~/.soloqueue/settings.toml`
- ✅ Teams can define organization-wide fallback models
- ✅ System never fails due to missing configuration
- ✅ Zero configuration needed for default behavior

### 3. APIModel Abstraction Layer

A clever abstraction separates configuration IDs from actual API models:

```
Config ID                  → API Model
────────────────────────────────────
deepseek-v4-flash         → deepseek-v4-flash (no thinking)
deepseek-v4-flash-thinking → deepseek-v4-flash (+ reasoning)
deepseek-v4-pro           → deepseek-v4-pro   (+ high reasoning)
deepseek-v4-pro-max       → deepseek-v4-pro   (+ max reasoning)
```

This enables:
- Multiple configuration entries mapping to same API model
- Different reasoning effort levels without different models
- Future provider switching without agent code changes

### 4. System Architecture Integration

Models are used strategically throughout the system:

- **L1 (Primary Agent):** "fast" model (user-facing, quick response needed)
- **L2 (Leaders/Supervisors):** "superior" or "universal" (thoughtful delegation)
- **L3 (Workers):** Task-specific models (optimal for each task type)
- **Compactor:** Hardcoded "fast" model (speed-critical context compression)

### 5. DeepSeek Thinking Modes

The system cleanly maps reasoning effort to DeepSeek's thinking capabilities:

```
no thinking           → thinking.enabled = false
high reasoning effort → thinking.reasoningEffort = "high"  (~20% more tokens)
max reasoning effort  → thinking.reasoningEffort = "max"   (~40% more tokens)
```

## Configuration System Details

### Settings Structure

Located in `~/.soloqueue/settings.toml`:

```toml
[DefaultModels]  # Optional - if missing, uses hardcoded defaults
expert    = "deepseek:deepseek-v4-pro-max"
superior  = "deepseek:deepseek-v4-pro"
universal = "deepseek:deepseek-v4-flash-thinking"
fast      = "deepseek:deepseek-v4-flash"
fallback  = ""  # Used if role not explicitly configured
```

### Hardcoded Defaults

Located in `internal/config/defaults.go`, the system defines four models:

```go
Models: []LLMModel{
    {ID: "deepseek-v4-flash", Thinking: {Enabled: false}},
    {ID: "deepseek-v4-flash-thinking", APIModel: "deepseek-v4-flash", 
     Thinking: {Enabled: true, ReasoningEffort: "high"}},
    {ID: "deepseek-v4-pro", 
     Thinking: {Enabled: true, ReasoningEffort: "high"}},
    {ID: "deepseek-v4-pro-max", APIModel: "deepseek-v4-pro", 
     Thinking: {Enabled: true, ReasoningEffort: "max"}},
}

DefaultModels: DefaultModelsConfig{
    Expert:    "deepseek:deepseek-v4-pro-max",
    Superior:  "deepseek:deepseek-v4-pro",
    Universal: "deepseek:deepseek-v4-flash-thinking",
    Fast:      "deepseek:deepseek-v4-flash",
}
```

### Query Service

Core function in `internal/config/service.go`:

```go
func (s *GlobalService) DefaultModelByRole(role string) *LLMModel {
    settings := s.Get()
    
    // 1. Explicit role configuration
    ref := roleField(settings.DefaultModels, role)
    
    // 2. Fallback configuration
    if ref == "" {
        ref = settings.DefaultModels.Fallback
    }
    
    // 3. Hardcoded default
    if ref == "" {
        ref = roleDefault(role)
    }
    
    // Parse "provider:id" and lookup
    return s.ModelByProviderID(providerID, modelID)
}
```

## Technical Implementation

### Configuration Type Hierarchy

```go
type Settings struct {
    Providers     []LLMProvider
    Models        []LLMModel
    DefaultModels DefaultModelsConfig
}

type LLMModel struct {
    ID            string          // Local config ID
    ProviderID    string          // Provider reference
    APIModel      string          // Actual API model (optional override)
    Name          string          // Display name
    ContextWindow int             // Context size in tokens
    Generation    GenerationParams // Temperature, MaxTokens
    Thinking      ThinkingConfig  // Reasoning mode config
}

type ThinkingConfig struct {
    Enabled         bool   // true to enable thinking mode
    ReasoningEffort string // "high", "max", or ""
}

type DefaultModelsConfig struct {
    Expert    string  // "provider:model_id" format
    Superior  string
    Universal string
    Fast      string
    Fallback  string
}
```

### API Integration

Model configuration flows into DeepSeek API requests:

```go
// In internal/llm/deepseek/wire.go
type WireRequest struct {
    Model               string       `json:"model"`
    ReasoningEffort     *string      `json:"reasoning_effort,omitempty"`
    MaxCompletionTokens int          `json:"max_completion_tokens"`
    Temperature         float64      `json:"temperature"`
}

func buildWireRequest(req Request) WireRequest {
    if req.ReasoningEffort != "" {
        out.ReasoningEffort = &req.ReasoningEffort
    }
    return out
}
```

### Hot-Reload Support

The configuration system supports runtime updates via `fsnotify`:

```go
cfg.Watch()  // File changes trigger OnChange callbacks
cfg.Get()    // Always returns latest settings
```

## Usage Examples

### Query Model by Role

```go
import "github.com/xiaobaitu/soloqueue/internal/config"

cfg, _ := config.Init(workDir)

// Get expert model for critical task
expertModel := cfg.DefaultModelByRole("expert")
// → deepseek-v4-pro-max with reasoning_effort: "max"

// Get fast model for quick response
fastModel := cfg.DefaultModelByRole("fast")
// → deepseek-v4-flash with no thinking mode
```

### Access Model Parameters

```go
model := cfg.DefaultModelByRole("universal")

// Generation parameters
temp := model.Generation.Temperature         // 0.0
maxTokens := model.Generation.MaxTokens     // 8192

// Thinking configuration
thinkingEnabled := model.Thinking.Enabled           // true
reasoningEffort := model.Thinking.ReasoningEffort   // "high"

// Context
ctxWindow := model.ContextWindow                     // 1048576
```

### Override Model Selection

```toml
# ~/.soloqueue/settings.toml
[DefaultModels]
# Use superior model for all expert tasks
expert = "deepseek:deepseek-v4-pro"

# Define organization-wide fallback
fallback = "deepseek:deepseek-v4-flash-thinking"
```

## Design Patterns

### 1. Role-Based vs Provider-Based Selection

**Pattern:** Ask "what role?" not "which provider?"

**Benefits:**
- Domain-driven: roles are business concepts
- Provider agnostic: can swap implementations
- Easier to reason about: fast/universal/expert are semantic
- Future-proof: supports multi-provider without agent changes

### 2. Three-Tier Resolution

**Pattern:** Explicit → Fallback → Default

**Benefits:**
- User control: can override defaults
- Team alignment: fallback enables shared policy
- Resilience: always has valid configuration
- Backward compatibility: works without config files

### 3. Abstraction with APIModel

**Pattern:** Separate configuration ID from API model

**Benefits:**
- Configuration flexibility: map multiple IDs to same model
- Mode selection: use thinking modes without API model changes
- Provider abstraction: future multi-provider support
- Clean separation: config is not tied to API internals

## Current Configuration Status

**User Environment:** `~/.soloqueue/settings.toml`

- ✅ Hardcoded defaults in use (no [DefaultModels] section)
- ✅ Default provider: deepseek with API key from env
- ✅ All four models available and enabled
- ✅ Context window: 1M tokens across all models
- ✅ Temperature: 0.0 (deterministic) for all models
- ✅ Max tokens: 8192 for all models

**Active Models:**
- `expert`: deepseek-v4-pro-max → deepseek-v4-pro + max reasoning
- `superior`: deepseek-v4-pro → deepseek-v4-pro + high reasoning
- `universal`: deepseek-v4-flash-thinking → deepseek-v4-flash + high reasoning
- `fast`: deepseek-v4-flash → deepseek-v4-flash (no thinking)

## Files Involved

### Configuration
- `internal/config/schema.go` - Type definitions
- `internal/config/service.go` - Query service and resolution logic
- `internal/config/defaults.go` - Hardcoded defaults
- `~/.soloqueue/settings.toml` - User configuration

### Usage
- `cmd/soloqueue/main.go` - Agent initialization, model resolver
- `internal/llm/deepseek/wire.go` - API request building
- `internal/agent/factory.go` - Agent model assignment
- `internal/session/session.go` - Session model selection

### Testing
- `internal/config/config_test.go` - Model resolution tests

## Key Insights

### 1. Zero-Configuration Experience

Despite being sophisticated, the system is simple for end users:
- Works out-of-box without config files
- Sensible defaults (temperature 0, max tokens 8192)
- Environment variables for API keys
- Optional override for power users

### 2. Token Economy Awareness

The four-tier system maps directly to token costs:
- Flash models: minimal tokens, no reasoning
- Flash-thinking: slightly more tokens + reasoning
- Pro: more tokens + better reasoning
- Pro-max: most tokens + deepest reasoning

This enables intelligent trade-offs without exposing token economics to users.

### 3. Multi-Level Agent Coordination

The role-based system enables the multi-level architecture:
- L1 uses "fast" for user-facing speed
- L2 uses "superior"/"universal" for thoughtful delegation
- L3 uses task-specific models
- All coordinated via role-based lookup

### 4. Provider Abstraction Readiness

The `provider:model_id` format and APIModel abstraction prepare for:
- OpenAI integration (openai:gpt-4, openai:gpt-4o)
- Anthropic integration (anthropic:claude-opus)
- Local model support (ollama:llama2)
- Model switching without agent code changes

## Recommendations for Future Development

### 1. Provider Expansion
The infrastructure supports adding new LLM providers. Simply add to `Providers[]` and `Models[]` arrays in defaults.go and reference in DefaultModels.

### 2. Dynamic Model Selection
The role-based system could be extended with:
- Per-task role hints from agents
- Context-aware role selection
- Cost-optimized model selection (same quality, lower cost)

### 3. Reasoning Budget
Could implement token budgets:
```go
type ReasoningBudget struct {
    MaxReasoningTokens int  // "expert" → 20000, "fast" → 0
    CostPerToken       float64
}
```

### 4. Model Metrics
Track per-model performance:
- Average latency by role
- Token consumption patterns
- Error rates per model
- Cost per interaction

## References

**Related Documentation:**
- `docs/MODEL_CONFIG_QUICK_REFERENCE.md` - Quick lookup guide
- `docs/MODEL_CONFIG_DIAGRAMS.md` - Visual diagrams and flows
- `docs/MODEL_CONFIGURATION_SYSTEM.md` - Detailed analysis

**Key Code Files:**
- `internal/config/service.go` - Start here for resolution logic
- `internal/config/defaults.go` - Default model definitions
- `cmd/soloqueue/main.go` - Usage in practice

## Conclusion

SoloQueue's model configuration system is a **carefully designed abstraction** that successfully bridges the gap between:
- **Simplicity** for end users (zero config, sensible defaults)
- **Flexibility** for power users (role-based override)
- **Scalability** for the organization (fallback policies, multi-provider support)

The role-based approach (expert/superior/universal/fast) is semantically cleaner and more maintainable than provider-based selection, enabling the multi-level agent architecture to function coherently while maintaining individual agent autonomy.

---

**Document Version:** 1.0  
**Last Updated:** 2026-04-30  
**Next Review:** When adding new LLM providers or model roles
