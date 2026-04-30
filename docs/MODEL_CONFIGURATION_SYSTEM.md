# SoloQueue Model Configuration System - Complete Analysis

## Overview

SoloQueue implements a sophisticated **multi-tier model configuration system** that enables dynamic selection of LLM models based on task roles. The system is built on four core role categories: **Expert**, **Superior**, **Universal**, and **Fast**, each with specific performance/speed trade-offs and reasoning capabilities.

---

## 1. Core Architecture

### 1.1 Role-Based Model Selection

The system defines four model roles, each optimized for different use cases:

```
┌─────────────────────────────────────────────────────────────┐
│        Role-Based Model Configuration System               │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  expert    → "deepseek:deepseek-v4-pro-max"               │
│  superior  → "deepseek:deepseek-v4-pro"                   │
│  universal → "deepseek:deepseek-v4-flash-thinking"        │
│  fast      → "deepseek:deepseek-v4-flash"                 │
│                                                             │
│  Format: "provider:model_id"                               │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### 1.2 Three-Tier Resolution Priority

When resolving a model for a role, SoloQueue uses a cascading priority system:

```
Priority 1: Explicit Role Configuration
            └─ if empty, fall through to Priority 2
                
Priority 2: Fallback Configuration  
            └─ if empty, fall through to Priority 3
                
Priority 3: Hardcoded Role Defaults
            └─ always defined in code
```

This allows:
- Users to override defaults via `settings.toml`
- Teams to define a universal fallback model
- Graceful degradation with hardcoded defaults

---

## 2. Model Definitions

### 2.1 DeepSeek V4 Flash (Fast Model)

**Configuration:**
```json
{
  "id": "deepseek-v4-flash",
  "providerID": "deepseek",
  "name": "DeepSeek V4 Flash",
  "contextWindow": 1048576,     // 1M tokens
  "enabled": true,
  "generation": {
    "temperature": 0.0,          // No randomness (deterministic)
    "maxTokens": 8192
  },
  "thinking": {
    "enabled": false             // No extended reasoning
  }
}
```

**Role Assignment:** `fast`  
**Speed Profile:** Fastest response  
**Use Cases:** Quick summaries, simple queries, context compression  
**Typical Use:** Compactor (context window compression engine)

---

### 2.2 DeepSeek V4 Flash (Thinking Mode)

**Configuration:**
```json
{
  "id": "deepseek-v4-flash-thinking",
  "providerID": "deepseek",
  "apiModel": "deepseek-v4-flash",  // Actual API model (abstraction)
  "name": "DeepSeek V4 Flash (Thinking)",
  "contextWindow": 1048576,
  "enabled": true,
  "generation": {
    "temperature": 0.0,
    "maxTokens": 8192
  },
  "thinking": {
    "enabled": true,
    "reasoningEffort": "high"    // Medium reasoning depth
  }
}
```

**Role Assignment:** `universal`  
**Speed Profile:** Balanced (fast with reasoning)  
**Use Cases:** General problem-solving, analysis, code review  
**Key Feature:** "Abstraction" layer via `apiModel` field allows mapping to same underlying model with different configurations

---

### 2.3 DeepSeek V4 Pro (Superior Model)

**Configuration:**
```json
{
  "id": "deepseek-v4-pro",
  "providerID": "deepseek",
  "name": "DeepSeek V4 Pro",
  "contextWindow": 1048576,
  "enabled": true,
  "generation": {
    "temperature": 0.0,
    "maxTokens": 8192
  },
  "thinking": {
    "enabled": true,
    "reasoningEffort": "high"    // Medium reasoning depth
  }
}
```

**Role Assignment:** `superior`  
**Speed Profile:** Medium (better quality than Flash)  
**Use Cases:** Complex reasoning, architectural decisions, design reviews  
**Trade-off:** Slower than Flash, more capable than Flash

---

### 2.4 DeepSeek V4 Pro (Max Reasoning - Expert)

**Configuration:**
```json
{
  "id": "deepseek-v4-pro-max",
  "providerID": "deepseek",
  "apiModel": "deepseek-v4-pro",  // Maps to same underlying model
  "name": "DeepSeek V4 Pro (Max Reasoning)",
  "contextWindow": 1048576,
  "enabled": true,
  "generation": {
    "temperature": 0.0,
    "maxTokens": 8192
  },
  "thinking": {
    "enabled": true,
    "reasoningEffort": "max"     // Maximum reasoning depth
  }
}
```

**Role Assignment:** `expert`  
**Speed Profile:** Slowest (but most thorough)  
**Use Cases:** Critical decisions, complex problem decomposition, code security review  
**Key Feature:** Uses same underlying API model as Pro but with `reasoning_effort: "max"`

---

## 3. Configuration System Components

### 3.1 Settings Structure (schema.go)

```go
type Settings struct {
    Session       SessionConfig
    Log           LogConfig
    Tools         ToolsConfig
    Providers     []LLMProvider          // List of LLM providers
    Models        []LLMModel             // List of available models
    Embedding     EmbeddingConfig
    DefaultModels DefaultModelsConfig    // Role → model mappings
}

type DefaultModelsConfig struct {
    Expert    string  // e.g., "deepseek:deepseek-v4-pro-max"
    Superior  string  // e.g., "deepseek:deepseek-v4-pro"
    Universal string  // e.g., "deepseek:deepseek-v4-flash-thinking"
    Fast      string  // e.g., "deepseek:deepseek-v4-flash"
    Fallback  string  // Used when role-specific config is empty
}
```

### 3.2 Model Query Service (service.go)

Core function: `DefaultModelByRole(role string) *LLMModel`

```go
func (s *GlobalService) DefaultModelByRole(role string) *LLMModel {
    settings := s.Get()
    
    // 1. Try explicit role configuration
    ref := roleField(settings.DefaultModels, role)
    
    // 2. Fall back to Fallback field
    if ref == "" {
        ref = settings.DefaultModels.Fallback
    }
    
    // 3. Use hardcoded default
    if ref == "" {
        ref = roleDefault(role)
    }
    
    if ref == "" {
        return nil
    }
    
    // 4. Parse "provider:id" and lookup
    providerID, modelID, ok := parseProviderModelID(ref)
    if !ok {
        return nil
    }
    return s.ModelByProviderID(providerID, modelID)
}
```

### 3.3 Configuration Files

**Location:** `~/.soloqueue/`

#### settings.toml (User Configuration)

```toml
[Session]
ReplaySegments = 3

[Log]
Level = 'info'
Console = false
File = true

[Tools]
AllowedDirs = []
MaxFileSize = 1048576
# ... tool limits ...

[DefaultModels]
# Override defaults here:
# expert = "deepseek:deepseek-v4-pro-max"
# Or customize to use different providers
```

**Note:** `DefaultModels` section is optional. If omitted, system uses hardcoded defaults.

#### Defaults (Go Code)

Located in `internal/config/defaults.go`, provides fallback values for all settings.

---

## 4. How Models Are Used in the System

### 4.1 Agent Initialization

When creating an agent in `cmd/soloqueue/main.go`:

```go
// Resolve default model (for session factory)
defaultModel := cfg.DefaultModelByRole("fast")
if defaultModel == nil {
    return nil, errors.New("no default model configured (fast role)")
}

// Create LLM client
llmClient, err := deepseek.NewClient(deepseek.Config{
    BaseURL:  baseURL,
    APIKey:   apiKey,
    // ... other config ...
})

// Create agent factory with model resolver
factory := agent.NewDefaultFactory(
    registry,
    llmClient,
    toolsCfg,
    skillDir,
    logger,
    agent.WithModelResolver(modelResolver),  // ← enables model validation
)
```

### 4.2 Session Factory

Each session uses the "fast" model by default:

```go
defaultModel := cfg.DefaultModelByRole("fast")

// In session's agent.Definition:
def := agent.Definition{
    ID:              agentID,
    Kind:            agent.KindChat,
    ModelID:         effectiveModelID,      // From model config
    Temperature:     defaultModel.Generation.Temperature,
    MaxTokens:       defaultModel.Generation.MaxTokens,
    ReasoningEffort: defaultModel.Thinking.ReasoningEffort,
    ThinkingEnabled: defaultModel.Thinking.Enabled,
    ContextWindow:   defaultModel.ContextWindow,
}
```

### 4.3 Context Compression (Compactor)

Uses "fast" model for speed:

```go
compactorModel := cfg.DefaultModelByRole("fast")
if compactorModel == nil {
    compactorModel = defaultModel
}
compactorModelID := compactorModel.APIModel
if compactorModelID == "" {
    compactorModelID = compactorModel.ID
}

llmCompactor := compactor.NewLLMCompactor(
    compactor.NewAgentChatClient(llmClient),
    compactorModelID,
)
```

---

## 5. LLM API Integration

### 5.1 DeepSeek Wire Format

Model configuration flows into DeepSeek API requests:

```go
// In internal/llm/deepseek/wire.go
type WireRequest struct {
    Model             string       `json:"model"`
    ReasoningEffort   *string      `json:"reasoning_effort,omitempty"`  // "high", "max", or nil
    MaxCompletionTokens int        `json:"max_completion_tokens"`
    Temperature       float64      `json:"temperature"`
    // ... other fields ...
}

func buildWireRequest(req Request) WireRequest {
    out := WireRequest{
        Model:       req.ModelID,
        Temperature: req.Temperature,
    }
    
    if req.ReasoningEffort != "" {
        out.ReasoningEffort = &req.ReasoningEffort
    }
    
    if req.ThinkingEnabled {
        // Use thinking mode with specified reasoning_effort
    }
    
    return out
}
```

### 5.2 API Request Flow

```
Configuration Layer
    ↓
DefaultModelByRole("role")
    ↓
LLMModel{ID, APIModel, Thinking{Enabled, ReasoningEffort}}
    ↓
Agent Definition
    ↓
LLM Request
    ↓
DeepSeek Wire Format
    ↓
DeepSeek API
    ↓
Response (with reasoning tokens if applicable)
```

---

## 6. Hardcoded Role Defaults

Located in `service.go`:

```go
func roleDefault(role string) string {
    defaults := map[string]string{
        "expert":    "deepseek:deepseek-v4-pro-max",
        "superior":  "deepseek:deepseek-v4-pro",
        "universal": "deepseek:deepseek-v4-flash-thinking",
        "fast":      "deepseek:deepseek-v4-flash",
    }
    return defaults[role]
}
```

These serve as the ultimate fallback when:
- No role-specific configuration is provided
- No fallback model is configured
- The system needs a guarantee of a valid model

---

## 7. Model Selection Strategy in Multi-Level Architecture

Based on the exploration findings, the multi-level agent system uses different models strategically:

### Level 1 (L1 - Primary)
- **Role:** Single interactive agent
- **Model:** "fast" (default)
- **Reason:** Direct user interaction, quick response needed

### Level 2 (L2 - Leaders/Supervisors)
- **Role:** Coordinate and delegate to L3 workers
- **Model:** Could use "universal" or "superior" depending on configuration
- **Reason:** Need thoughtful delegation decisions

### Level 3 (L3 - Workers)
- **Role:** Execute specific tasks
- **Model:** Configurable per task type
- **Reason:** Task-specific optimization

### Context Compression (Compactor)
- **Role:** Compress context window when hitting waterlines
- **Model:** "fast" (hardcoded preference)
- **Reason:** Speed-critical, doesn't need reasoning

---

## 8. Provider-Model Mapping

The system uses a **provider:model_id** naming scheme to support multiple LLM providers:

```
Current Setup:
┌──────────────┬───────────────────────────────────┐
│ Provider     │ Models                            │
├──────────────┼───────────────────────────────────┤
│ deepseek     │ deepseek-v4-flash                │
│              │ deepseek-v4-flash-thinking       │
│              │ deepseek-v4-pro                  │
│              │ deepseek-v4-pro-max              │
├──────────────┼───────────────────────────────────┤
│ openai       │ (would be configurable)          │
│ kimi         │ (would be configurable)          │
│ gemini       │ (would be configurable)          │
└──────────────┴───────────────────────────────────┘
```

Each provider has:
- `ID`: Unique identifier (e.g., "deepseek")
- `Name`: Display name
- `BaseURL`: API endpoint
- `APIKeyEnv`: Environment variable for API key
- `Enabled`: Whether provider is available
- `IsDefault`: Primary provider to use
- `Retry`: Exponential backoff configuration

---

## 9. Configuration Hot-Reload

The configuration system supports runtime changes:

```go
type GlobalService struct {
    *Loader[Settings]  // Embeds Loader which supports:
                       // - Load()
                       // - Save()
                       // - Get()
                       // - Set()
                       // - OnChange()
                       // - Watch()
                       // - Close()
}

// Watch for changes in settings files
cfg.Watch()  // Triggered on settings.toml or settings.local.toml change
```

Changes are watched via `fsnotify`, allowing model configuration updates without restart.

---

## 10. Current Configuration in User Environment

From `~/.soloqueue/settings.toml`:

```toml
[Session]
ReplaySegments = 3
TimelineMaxFileMB = 50
TimelineMaxFiles = 5

[Log]
Level = 'info'
Console = false
File = true

[Tools]
AllowedDirs = []
# ... (all tool limits as per defaults)

[Embedding]
Enabled = false
# ... (embedding config)

# DefaultModels section NOT present - uses hardcoded defaults
```

Default models in use:
- **expert:** deepseek:deepseek-v4-pro-max (reasoning_effort: "max")
- **superior:** deepseek:deepseek-v4-pro (reasoning_effort: "high")
- **universal:** deepseek:deepseek-v4-flash-thinking (reasoning_effort: "high", on flash model)
- **fast:** deepseek:deepseek-v4-flash (no thinking mode)

---

## 11. Key Design Insights

### 11.1 APIModel Abstraction

The `APIModel` field allows configuration abstraction:

```go
type LLMModel struct {
    ID       string  // Local config ID (e.g., "deepseek-v4-pro-max")
    APIModel string  // Actual API model name (e.g., "deepseek-v4-pro")
    // ...
}
```

This enables:
- Multiple config entries mapping to same underlying model
- Thinking mode configurations that actually call the same API model
- Future provider-specific model name mappings

### 11.2 Zero-Configuration Experience

Despite complexity, users experience zero configuration:
1. Hardcoded defaults work out-of-box
2. Optional override via settings.toml for power users
3. Environment variables provide API key configuration
4. Sensible defaults (temperature: 0, maxTokens: 8192)

### 11.3 Role-Based vs Provider-Based

Rather than asking "which LLM provider should I use?", the system asks "what role is this task?" This is more maintainable because:
- Role is domain knowledge (fast, expert, etc.)
- Provider is implementation detail
- Easy to swap providers without changing agent code

### 11.4 Reasoning Effort Levels

Three tiers map to DeepSeek's `reasoning_effort` parameter:

```
No thinking    → thinking.enabled = false
High effort    → thinking.reasoningEffort = "high"     (~20% more tokens)
Max effort     → thinking.reasoningEffort = "max"      (~40% more tokens)
```

This creates natural speed/quality trade-offs without exposing API details to agents.

---

## 12. Testing Evidence

From `config_test.go`:

```go
expert := svc.DefaultModelByRole("expert")
// Expects: ID = "deepseek-v4-pro-max", ReasoningEffort = "max"

superior := svc.DefaultModelByRole("superior")
// Expects: ID = "deepseek-v4-pro", ReasoningEffort = "high"

universal := svc.DefaultModelByRole("universal")
// Expects: ID = "deepseek-v4-flash-thinking", ReasoningEffort = "high"

fast := svc.DefaultModelByRole("fast")
// Expects: ID = "deepseek-v4-flash", Thinking disabled
```

These tests validate the hardcoded defaults work correctly.

---

## Summary

The SoloQueue model configuration system is a **carefully designed abstraction layer** that:

1. **Abstracts LLM diversity** behind role-based selection
2. **Provides three-tier resolution** (explicit → fallback → hardcoded)
3. **Maps reasoning modes** to DeepSeek's thinking capabilities
4. **Supports multiple providers** via extensible provider:model_id scheme
5. **Enables runtime reconfiguration** via hot-reload
6. **Maintains backward compatibility** through sensible defaults

This design allows different parts of the system to optimize model selection independently (agent tasks vs. context compression) while maintaining a consistent, predictable configuration experience for end users.
