# Model Configuration System - Visual Diagrams

## 1. Configuration Resolution Flow

```
                    ┌─ DefaultModelByRole("expert") ─┐
                    │                                  │
                    ▼                                  ▼
        ┌──────────────────────────────────────────────────────┐
        │  Check DefaultModels.Expert in settings.toml          │
        │  Value: "deepseek:deepseek-v4-pro-max" (or empty)   │
        └──────────────────────────────────────────────────────┘
                                │
                    ┌───────────┴───────────┐
                    │ Config value found?   │
                    └───────────┬───────────┘
                        NO      │      YES
                        │       │       │
                        ▼       │       ▼
            ┌──────────────────┐│   ┌────────────────────┐
            │Check Fallback    ││   │Use explicit config │
            │value             ││   │"deepseek:v4-pro-max"
            └──────────────────┘│   └────────────────────┘
                    │           │           │
                NO  │    YES     │           │
                    ▼    │       │           │
            ┌──────────────────┐│           │
            │Use hardcoded     ││           │
            │default           ││           │
            │"deepseek:...pro-max"│        │
            └──────────────────┘│           │
                    │           │           │
                    └───┬───────┴───────────┘
                        │
                        ▼
            ┌──────────────────────────┐
            │ Parse "provider:model_id" │
            │ → "deepseek" + "v4-pro-max"
            └──────────────────────────┘
                        │
                        ▼
            ┌──────────────────────────────────┐
            │ ModelByProviderID(                │
            │   "deepseek", "v4-pro-max"      │
            │ )                                │
            └──────────────────────────────────┘
                        │
                        ▼
            ┌──────────────────────────────────┐
            │ Return LLMModel{                 │
            │   ID: "deepseek-v4-pro-max",    │
            │   APIModel: "deepseek-v4-pro",  │
            │   Thinking: {                    │
            │     Enabled: true,              │
            │     ReasoningEffort: "max"      │
            │   },                             │
            │   Generation: {...}              │
            │ }                                │
            └──────────────────────────────────┘
```

## 2. Role-to-Model Mapping

```
┌──────────────────────────────────────────────────────────────────────┐
│                     Four-Tier Model Hierarchy                        │
├──────────────────────────────────────────────────────────────────────┤
│                                                                       │
│  expert     deepseek-v4-pro-max                   🏆 Max Reasoning  │
│    │        └─ API: deepseek-v4-pro                  effort: max    │
│    │           Thinking: ✓ Enabled                  Time: ~10-30s   │
│    │           ReasoningEffort: "max" ────────────────────────→ Most│
│    │                                                            Capable
│    │
│  superior   deepseek-v4-pro                       🥈 Medium Reasoning
│    │        └─ API: deepseek-v4-pro                  effort: high   
│    │           Thinking: ✓ Enabled                  Time: ~5-15s    
│    │           ReasoningEffort: "high" ───────────────────────→ Balanced
│    │
│  universal  deepseek-v4-flash-thinking            ⚡ Fast + Thinking
│    │        └─ API: deepseek-v4-flash               effort: high   
│    │           Thinking: ✓ Enabled                  Time: ~2-5s    
│    │           ReasoningEffort: "high" ───────────────────────→ General
│    │
│  fast       deepseek-v4-flash                      🚀 No Thinking
│             └─ API: deepseek-v4-flash               No effort      
│                Thinking: ✗ Disabled                 Time: <2s      
│                ReasoningEffort: "" ────────────────────────→ Fastest
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘

Speed:     fast ◀─────────── universal ◀─────── superior ◀────── expert
Quality:   expert ─────────► superior ────────► universal ────────► fast
Cost:      expensive ◀────────────────────────────────────────────► cheap
```

## 3. Configuration File Structure

```
~/.soloqueue/settings.toml
│
├─ [Session]
│  └─ ReplaySegments, TimelineMaxFileMB, TimelineMaxFiles
│
├─ [Log]
│  └─ Level, Console, File
│
├─ [Tools]
│  └─ AllowedDirs, MaxFileSize, MaxMatches, etc.
│
├─ [Embedding]
│  └─ Enabled, Providers[], Models[]
│
└─ [DefaultModels]  ← Optional: if empty, uses hardcoded defaults
   ├─ expert    = "deepseek:deepseek-v4-pro-max"
   ├─ superior  = "deepseek:deepseek-v4-pro"
   ├─ universal = "deepseek:deepseek-v4-flash-thinking"
   ├─ fast      = "deepseek:deepseek-v4-flash"
   └─ fallback  = "" (optional)


internal/config/defaults.go (Go Code - Hardcoded Fallback)
│
├─ Providers[]
│  └─ DeepSeek
│     ├─ ID: "deepseek"
│     ├─ BaseURL: "https://api.deepseek.com/v1"
│     ├─ APIKeyEnv: "DEEPSEEK_API_KEY"
│     └─ Retry: {MaxRetries: 3, ...}
│
├─ Models[]
│  ├─ deepseek-v4-flash
│  │  ├─ ContextWindow: 1048576
│  │  ├─ Generation: {Temperature: 0, MaxTokens: 8192}
│  │  └─ Thinking: {Enabled: false}
│  │
│  ├─ deepseek-v4-flash-thinking
│  │  ├─ APIModel: "deepseek-v4-flash"  ← Abstraction!
│  │  ├─ Generation: {Temperature: 0, MaxTokens: 8192}
│  │  └─ Thinking: {Enabled: true, ReasoningEffort: "high"}
│  │
│  ├─ deepseek-v4-pro
│  │  ├─ Generation: {Temperature: 0, MaxTokens: 8192}
│  │  └─ Thinking: {Enabled: true, ReasoningEffort: "high"}
│  │
│  └─ deepseek-v4-pro-max
│     ├─ APIModel: "deepseek-v4-pro"  ← Abstraction!
│     ├─ Generation: {Temperature: 0, MaxTokens: 8192}
│     └─ Thinking: {Enabled: true, ReasoningEffort: "max"}
│
└─ DefaultModels
   ├─ Expert: "deepseek:deepseek-v4-pro-max"
   ├─ Superior: "deepseek:deepseek-v4-pro"
   ├─ Universal: "deepseek:deepseek-v4-flash-thinking"
   ├─ Fast: "deepseek:deepseek-v4-flash"
   └─ Fallback: "" (empty = unused)
```

## 4. API Integration Pipeline

```
┌──────────────────────────────────────────────────────────────┐
│ User Request to Agent                                        │
└──────────────────┬───────────────────────────────────────────┘
                   │
                   ▼
┌──────────────────────────────────────────────────────────────┐
│ Session Factory                                              │
│ cfg.DefaultModelByRole("fast")                              │
│ → LLMModel { ID: ..., APIModel: ..., Thinking: ... }       │
└──────────────────┬───────────────────────────────────────────┘
                   │
                   ▼
┌──────────────────────────────────────────────────────────────┐
│ Agent Definition                                             │
│ {                                                            │
│   ModelID: "deepseek-v4-flash"                             │
│   Temperature: 0.0                                           │
│   MaxTokens: 8192                                            │
│   ThinkingEnabled: false                                     │
│   ReasoningEffort: ""                                        │
│   ContextWindow: 1048576                                     │
│ }                                                            │
└──────────────────┬───────────────────────────────────────────┘
                   │
                   ▼
┌──────────────────────────────────────────────────────────────┐
│ Build LLM Request (deepseek/wire.go)                        │
│ WireRequest {                                                │
│   Model: "deepseek-v4-flash"                               │
│   Temperature: 0.0                                          │
│   MaxCompletionTokens: 8192                                 │
│   ReasoningEffort: nil (ThinkingEnabled=false)             │
│ }                                                            │
└──────────────────┬───────────────────────────────────────────┘
                   │
                   ▼
┌──────────────────────────────────────────────────────────────┐
│ DeepSeek API                                                │
│ POST https://api.deepseek.com/v1/chat/completions          │
│ {                                                            │
│   "model": "deepseek-v4-flash",                            │
│   "temperature": 0.0,                                       │
│   "max_completion_tokens": 8192,                            │
│   "messages": [...]                                          │
│   // No reasoning_effort (not present or null)             │
│ }                                                            │
└──────────────────┬───────────────────────────────────────────┘
                   │
                   ▼
┌──────────────────────────────────────────────────────────────┐
│ API Response                                                │
│ {                                                            │
│   "choices": [{                                             │
│     "message": {                                             │
│       "content": "..."                                      │
│     }                                                        │
│   }],                                                        │
│   "usage": {                                                │
│     "prompt_tokens": 100,                                   │
│     "completion_tokens": 50,                                │
│     "total_tokens": 150                                     │
│     // No reasoning_tokens                                 │
│   }                                                          │
│ }                                                            │
└──────────────────┬───────────────────────────────────────────┘
                   │
                   ▼
┌──────────────────────────────────────────────────────────────┐
│ Context Window Management                                   │
│ • Update token count: +150 tokens                          │
│ • Check soft waterline (75%): if exceeded → async compress │
│ • Check hard waterline (100%): if exceeded → sync truncate │
└──────────────────────────────────────────────────────────────┘
```

## 5. Model Abstraction with APIModel

```
Configuration Layer                API Layer
───────────────────                ────────

Config ID                API Model       Actual API Call
──────────             ──────────       ──────────────

deepseek-v4-flash  ──→  deepseek-v4-flash  ──→  model: "deepseek-v4-flash"
                                                  thinking: (disabled)

deepseek-v4-flash-thinking  ──→  deepseek-v4-flash  ──→  model: "deepseek-v4-flash"
                 (APIModel)                           reasoning_effort: "high"

deepseek-v4-pro  ──→  deepseek-v4-pro  ──→  model: "deepseek-v4-pro"
                                             reasoning_effort: "high"

deepseek-v4-pro-max  ──→  deepseek-v4-pro  ──→  model: "deepseek-v4-pro"
          (APIModel)                             reasoning_effort: "max"


Benefits of Abstraction:
├─ Multiple config entries map to same API model
├─ Different thinking modes without different models
├─ Future: easy provider swapping (e.g., openai:gpt-4o → openai:gpt-4)
└─ Clean separation: config ID vs. API model
```

## 6. System Integration

```
┌──────────────────────────────────────────────────────────────┐
│                      SoloQueue System                        │
├──────────────────────────────────────────────────────────────┤
│                                                               │
│  User Interface (TUI / WebUI)                                │
│         │                                                     │
│         ▼                                                     │
│  L1 Primary Agent ────────────►  Uses: "fast" model          │
│    (Session)                     DefaultModelByRole("fast")  │
│         │                                                     │
│         │ (delegates)                                         │
│         ▼                                                     │
│  L2 Leader/Supervisor  ────────►  Uses: "superior" or       │
│    (Coordinates)                   "universal" model         │
│         │                                                     │
│         │ (spawns)                                            │
│         ▼                                                     │
│  L3 Worker Agents ─────────────►  Uses: task-specific model  │
│    (Execute tasks)                 Configurable per role     │
│                                                               │
│  Context Compression ──────────►  Uses: "fast" model         │
│    (Compactor)                     (always, for speed)       │
│                                                               │
│  Configuration Service                                        │
│    GlobalService.DefaultModelByRole(role)                    │
│    ├─ Check settings.toml                                    │
│    ├─ Check fallback config                                  │
│    └─ Use hardcoded defaults                                 │
│                                                               │
└──────────────────────────────────────────────────────────────┘
```

## 7. Token Flow with Reasoning

```
Model:deepseek-v4-pro with reasoning_effort: "max"
───────────────────────────────────────────────────

User prompt (e.g., "Analyze this code"):
  Tokens: 50 (prompt_tokens)
                            │
                            ▼
                   ┌────────────────┐
                   │ Model Thinking │
                   │ Phase (hidden) │
                   └────────────────┘
                       ~40% extra
                    reasoning tokens
                            │
                            ▼
                   ┌────────────────┐
                   │ Model Response │
                   │ (visible)      │
                   └────────────────┘
                     ~150 tokens
                            │
                            ▼
API Response:
  prompt_tokens: 50
  completion_tokens: 150 (includes ~60 reasoning tokens)
  total_tokens: 200
  
Context Window Impact: +200 tokens


Model: deepseek-v4-flash (no thinking)
──────────────────────────────────────

User prompt (e.g., "Quick summary"):
  Tokens: 30 (prompt_tokens)
                            │
                            ▼
                   ┌────────────────┐
                   │ Direct Inference│
                   │ (No thinking)  │
                   └────────────────┘
                    (no overhead)
                            │
                            ▼
API Response:
  prompt_tokens: 30
  completion_tokens: 40 (direct output)
  total_tokens: 70
  
Context Window Impact: +70 tokens
  
Saving: 200 vs 70 = 65% faster!
```

## 8. Configuration Precedence

```
                    ┌─────────────────┐
                    │ User's Request  │
                    │ agent.Ask(...)  │
                    └────────┬────────┘
                             │
                             ▼
                    ┌─────────────────────────────────┐
                    │ 1. Explicit Config              │
                    │    [DefaultModels] in           │
                    │    ~/.soloqueue/settings.toml   │
                    └────────┬────────────────────────┘
                             │ (empty?)
                             ▼
                    ┌─────────────────────────────────┐
                    │ 2. Fallback Config              │
                    │    DefaultModels.Fallback       │
                    │    in settings.toml             │
                    └────────┬────────────────────────┘
                             │ (empty?)
                             ▼
                    ┌─────────────────────────────────┐
                    │ 3. Hardcoded Defaults           │
                    │    roleDefault() in service.go  │
                    │    "Always available"           │
                    └────────┬────────────────────────┘
                             │
                             ▼
                    ┌─────────────────┐
                    │ Resolved Model  │
                    │ (guaranteed!)   │
                    └─────────────────┘

Priority: Explicit > Fallback > Hardcoded
Guarantee: Always resolves to valid model
```
