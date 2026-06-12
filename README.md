# SoloQueue

**AI multi-agent collaboration platform** with hierarchical task routing, built with Go and React. Built for [DeepSeek](https://deepseek.com).

## Quick Start

```bash
git clone https://github.com/xiaobaitu/soloqueue.git
cd soloqueue

# Start server
go run ./cmd/soloqueue serve --port 8765

# Start web UI (separate terminal)
cd web && pnpm install && pnpm dev
```

Open `http://localhost:5173`.

## Build

```bash
make build        # pnpm build + copy dist + go build
make build-web    # web UI only
make build-go     # Go binary only
```

## Test

```bash
# Go
go test ./...

# Web UI
cd web && pnpm check && pnpm test
```

## Token Optimization with RTK (Recommended)

SoloQueue integrates with [RTK (Rust Token Killer)](https://github.com/rtk-ai/rtk) to optimize tool executions and compress command outputs (e.g. `git`, test runners, linters, directory structures), reducing LLM token consumption by 60%–90%.

### Installation

We highly recommend installing RTK for your operating system:

- **macOS (via Homebrew):**

  ```bash
  brew install rtk
  ```

- **Linux/macOS (via script):**

  ```bash
  curl -fsSL https://raw.githubusercontent.com/rtk-ai/rtk/master/install.sh | sh
  ```

- **Windows:**
  Follow the instructions in the [RTK README](https://github.com/rtk-ai/rtk#installation) or run SoloQueue in **WSL** with RTK installed.

### How it works

When SoloQueue starts up, it automatically detects if `rtk` is installed in the system's `PATH` and whether the system platform is supported (macOS/Linux). If so, SoloQueue will transparently route all command executions in the `Bash` tool through `rtk rewrite` to compress output before sending it to the LLM context. No extra configuration is needed.

## Memory Engine (BM25 + KG + Vector)

SoloQueue includes a hybrid memory engine with three pipelines. By default it runs BM25 + Knowledge Graph with zero dependencies. Enable ONNX for local vector search:

```bash
# Install ONNX Runtime (required for vector search)
brew install onnxruntime

# Rebuild after installing dependencies
make build-go
```

```toml
# ~/.soloqueue/settings.toml
[embedding]
enabled = true
provider = 'onnx'
```

The model (~1.1GB) must be downloaded manually before enabling ONNX:

```bash
pip install huggingface-hub
hf download intfloat/multilingual-e5-large \
  --local-dir ~/.soloqueue/models/intfloat/multilingual-e5-large/
```

If the model is not found at startup, a download instruction will be logged.

## License

MIT
