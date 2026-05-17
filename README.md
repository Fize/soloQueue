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

## License

MIT
