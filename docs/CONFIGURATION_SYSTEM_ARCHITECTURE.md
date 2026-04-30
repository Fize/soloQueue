# Configuration System Architecture

## Overview

The configuration system in `internal/config` is the runtime control plane for SoloQueue. It owns:

- the global settings schema
- default values
- layered file loading and merge semantics
- hot-reload watching
- convenience lookups for providers, models, and tool config conversion

Everything else in the application consumes resolved configuration through this package rather than reading TOML files directly.

## Core Schema

The full configuration schema is declared in [internal/config/schema.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/schema.go#L10) as `Settings`.

Top-level sections are:

- `Session`
- `Log`
- `Tools`
- `Providers`
- `Models`
- `Embedding`
- `DefaultModels`

This schema is intentionally broad enough to cover both infrastructure concerns and LLM/model concerns.

### ToolsConfig

`ToolsConfig` defines the application-facing version of tool limits and policies ([internal/config/schema.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/schema.go#L44)).

It does not directly use `time.Duration`; instead it stores timeouts in milliseconds and plain numeric limits. That keeps the serialized file format easy to author.

### Provider/Model Split

Model configuration is intentionally normalized into two lists:

- `Providers`: credentials, base URL, retry, timeout
- `Models`: semantic model IDs, context windows, generation settings, thinking configuration

This separation is important because the runtime can vary models independently from transport/provider details.

### DefaultModels

`DefaultModelsConfig` ([internal/config/schema.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/schema.go#L160)) maps logical roles such as `expert`, `superior`, `universal`, and `fast` to `provider:model` references.

This is the bridge between role-based routing and concrete model records.

## Default Layer

`DefaultSettings()` in [internal/config/defaults.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/defaults.go#L3) provides the hardcoded baseline.

It includes:

- session and logging defaults
- safe-ish tool limits
- default DeepSeek provider and model catalog
- role-based default model mappings

Architecturally, this means the application can boot with no user config file at all. User files are overrides, not mandatory manifests.

## Loader Architecture

The center of the package is the generic `Loader[T]` in [internal/config/loader.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/loader.go#L17).

It is a reusable layered configuration loader with:

- typed snapshots
- concurrent-safe reads
- atomic updates and saves
- on-change callbacks
- optional file watching

### Layering Model

The loader applies configuration in this order:

`defaults -> paths[0] -> paths[1] -> ...`

For the global config service, those paths are:

- `settings.toml`
- `settings.local.toml`

as wired by `config.New(...)` in [internal/config/service.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/service.go#L16).

This gives the system a predictable override stack:

- compiled defaults
- shared user config
- local machine override

### Snapshot Semantics

`Loader.Get()` returns the current typed snapshot. The loader guards updates with internal locks, but consumers receive values, not mutable shared pointers.

This is a strong architectural choice because it reduces the chance of accidental shared mutable config state leaking across subsystems.

### Save And Set

`Set(fn)` updates the in-memory config, writes back to the primary config path, and rolls back on persistence failure ([internal/config/loader.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/loader.go#L141)).

`saveTo(...)` uses a temp-file-plus-rename pattern for atomic persistence ([internal/config/loader.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/loader.go#L346)).

So the loader is not read-only infrastructure. It also defines the writeback contract.

## Merge Semantics

File merging is handled by `MergeTOML(...)` in [internal/config/merge_toml.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/merge_toml.go#L9).

The semantics are explicit:

- object fields merge recursively
- arrays replace wholesale
- omitted fields preserve previous values
- scalar values override directly

This is one of the most important architectural contracts in the config system because all layering depends on it.

Consequences:

- users can partially override nested objects without repeating the full object
- replacing provider/model arrays requires supplying the complete replacement list

## Hot Reload Design

`Loader.Watch()` in [internal/config/loader.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/loader.go#L190) creates an `fsnotify` watcher on parent directories, not only target files.

That choice allows the loader to react to:

- writes to existing files
- creation of config files that did not exist at startup
- rename-based editor save flows

The watch path is debounced for 200ms through `scheduleReload()` ([internal/config/loader.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/loader.go#L305)).

Architecturally, debounce is part of the contract because text editors often trigger multiple fsnotify events per save.

## GlobalService As Application Facade

`GlobalService` in [internal/config/service.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/service.go#L9) embeds `Loader[Settings]` and adds domain-specific convenience queries.

It provides:

- default provider lookup
- provider/model lookup by ID
- role-based model resolution through `DefaultModelByRole(...)`
- embedding defaults

This keeps higher-level runtime code simple. `cmd/soloqueue/main.go` uses `GlobalService` rather than manually searching arrays.

### DefaultModelByRole

`DefaultModelByRole(...)` in [internal/config/service.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/service.go#L92) resolves models in three steps:

1. role-specific setting
2. fallback setting
3. hardcoded role default

Then it parses `provider:model` and returns the corresponding `LLMModel`.

This function is a core bridge between config and runtime routing.

## Conversion To Runtime-Specific Config

The config package also owns conversion from user-facing settings schema into runtime-specific tool config.

`ToolsConfig.ToToolsConfig(...)` in [internal/config/tools_convert.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/tools_convert.go#L9) transforms:

- integer timeout fields into `time.Duration`
- zero values into runtime defaults
- allowed directories plus runtime-injected sandboxes into `tools.Config`

This conversion layer is useful because it prevents the `tools` package from depending on TOML/file-format concerns.

## Runtime Integration

Application startup uses the config system through `config.Init(workDir)` in [internal/config/service.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/service.go#L169).

`Init` does three things:

1. create a `GlobalService`
2. load layered settings
3. start file watching and write an initial file if missing

The CLI entrypoint then consumes the resolved config in [cmd/soloqueue/main.go](/Users/xiaobaitu/github.com/soloQueue/cmd/soloqueue/main.go#L155):

- build the DeepSeek client from provider config
- derive `tools.Config` from `settings.Tools`
- resolve default model IDs for factories and routers

This makes the config package the canonical source of runtime wiring inputs.

## Code Layout Summary

The config package is organized by responsibility:

- schema: `schema.go`
- defaults: `defaults.go`
- generic loader and watch logic: `loader.go`
- layered merge semantics: `merge_toml.go`
- application facade: `service.go`
- runtime conversion helpers: `tools_convert.go`
- tests: `config_test.go`

## Architectural Strengths

- Typed generic loader avoids ad hoc config parsing across the codebase.
- Layered merge model is explicit and testable.
- Global defaults allow zero-config bootstrapping.
- `GlobalService` provides a stable facade for runtime consumers.
- Runtime conversion helpers isolate serialized schema from execution structs.

## Architectural Tradeoffs

- Arrays replace wholesale, which is simple but can be surprising for large provider/model catalogs.
- The loader supports logging injection but remains mostly infrastructure-oriented; richer config-diff reporting is not built in.
- Role-based default model resolution is hardcoded to a fixed role vocabulary.
- Config validation is distributed: schema shape is here, but some semantic validation happens later in factory/model resolver code.

## Files To Read First

- [internal/config/schema.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/schema.go)
- [internal/config/defaults.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/defaults.go)
- [internal/config/loader.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/loader.go)
- [internal/config/merge_toml.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/merge_toml.go)
- [internal/config/service.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/service.go)
- [internal/config/tools_convert.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/tools_convert.go)
