# Local laya decision worker

Runs the `laya-multilingual` ONNX graph and answers a fixed three-way task classification
(`general` / `engineering` / `research`) over stdin/stdout NDJSON. It is the local alternative to
the LLM task classifier; the Go side lives in `internal/router/laya`.

## Layout requirements

The model directory must contain both `model.onnx` and **two names for the external data file**:

```
model.onnx
model.onnx.data      <- required by laya-rust's artifact check
model.onnx_data      <- the name recorded inside the ONNX graph; ONNX Runtime opens this one
rl_agent_config.json
tokenizer/tokenizer.json
tokenizer/tokenizer_config.json
encoder/config.json
```

The published `onnx-community/laya-multilingual-ONNX` export records `model.onnx_data`. Prefer
rewriting that reference to `model.onnx.data` when preparing the artifact (one `onnx` pass, then
re-pin the checksums) over shipping two names in production.

## ONNX Runtime

`ort 2.0.0-rc.13` rejects ONNX Runtime 1.24.2 with `BadVersion { version_str: "1.24.2" }`, so this
worker cannot share the embedding worker's runtime. It is pinned to **1.30.0**; pass its shared
library with `--runtime` (an absolute path to an existing file). On Windows the containing
directory is added to the DLL search path before the runtime is loaded.

## Build

```sh
cargo build --locked --release --manifest-path native/decide/Cargo.toml
# or, from the repository root:
make build-decide
```

`make build` and `make build-go` stay Rust-free.

`laya-onnx` is vendored under `vendor/laya-onnx` (Apache-2.0, see `NOTICE.md`): it is not published
on crates.io and the upstream repository has no root `Cargo.toml`, so it cannot be consumed as a
versioned or git dependency.

## Measured

macOS arm64, fp32 multilingual graph, 50 labelled prompts (`laya-labels50.jsonl` in the plan dir):

| | |
| --- | --- |
| accuracy | 43/50 = 0.860 (identical to the Python `ONNXAgent` on the same items) |
| latency | p50 90 ms, mean 96 ms |
| load | ~3.1 s |
| peak RSS | 1.12 GB |
| runtime | ONNX Runtime 1.30.0 |

## Protocol

See `protocol.md`. Two operations only: `decide` and `shutdown`.
