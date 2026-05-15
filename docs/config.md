# Configuration System

**Location**: `internal/config/`

The config system is the runtime control plane. It owns the global settings schema, layered file loading, merge semantics, and hot-reload watching.

## Quick Start

Config file: `~/.soloqueue/settings.toml`

```toml
[session]
timeline_max_file_mb = 50

[tools]
max_file_size = 10485760

[providers]
  [providers.deepseek]
  base_url = "https://api.deepseek.com"
  api_key_env = "DEEPSEEK_API_KEY"
  timeout_ms = 600000
  [providers.deepseek.retry]
  max_retries = 3
  initial_delay_ms = 1000
  max_delay_ms = 30000
  backoff_multiplier = 2.0

[models]
  [models."deepseek-v4-flash"]
  context_window = 65536
  [models."deepseek-v4-flash".generation]
  max_tokens = 8192
  [models."deepseek-v4-flash".thinking]
  enabled = false

  [models."deepseek-v4-pro"]
  context_window = 131072
  [models."deepseek-v4-pro".generation]
  max_tokens = 16384
  [models."deepseek-v4-pro".thinking]
  enabled = true
  reasoning_effort = "high"

[default_models]
  expert = "deepseek:deepseek-v4-pro-max"
  superior = "deepseek:deepseek-v4-pro"
  universal = "deepseek:deepseek-v4-flash-thinking"
  fast = "deepseek:deepseek-v4-flash"
```

## Layered Loading

Config files are loaded in order (lowest to highest priority):

1. **Compiled defaults** (`internal/config/defaults.go`)
2. **`settings.toml`** (shared user config)
3. **`settings.local.toml`** (local machine override)

**Merge semantics:**
- Object fields: merge recursively
- Arrays: replace wholesale
- Omitted fields: preserve previous values

## Hot Reload

- Uses `fsnotify` to watch config files
- Debounced 200ms (handles editor save flows)
- Reacts to: writes, file creation, rename-based saves

## Key Features

### 1. Type-Safe Access

`Loader.Get()` returns typed snapshot (not mutable pointer).

```go
cfg := loader.Get()
model := cfg.Models["deepseek-v4-pro"]
```

### 2. Atomic Saves

`Set(fn)` updates in-memory config, writes to file, rolls back on failure.

Uses temp-file-plus-rename pattern.

### 3. Default Model Resolution

`DefaultModelByRole(role)` resolves models in 3 steps:
1. Role-specific setting
2. Fallback setting
3. Hardcoded role default

### 4. Runtime Conversion

`ToolsConfig.ToToolsConfig()` converts:
- Integer timeouts → `time.Duration`
- Zero values → runtime defaults
- Allowed dirs → `tools.Config`

## File Layout

```
internal/config/
├── schema.go          # Settings struct
├── defaults.go        # Hardcoded defaults
├── loader.go          # Generic loader + watch
├── merge_toml.go     # Merge semantics
├── service.go         # GlobalService facade
└── tools_convert.go   # Runtime conversion
```

## Architecture Strengths

- Typed generic loader avoids ad hoc parsing
- Layered merge model is explicit and testable
- Global defaults allow zero-config bootstrapping
- `GlobalService` provides stable facade for consumers

## Files to Read First

- `internal/config/schema.go` - Settings schema
- `internal/config/loader.go` - Loader and watch logic
- `internal/config/service.go` - GlobalService facade
