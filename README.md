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

## Skills

SoloQueue supports Claude Code–compatible skills: each skill is a `SKILL.md`
file with YAML frontmatter (`name`, `description`, `allowed-tools`, …),
installed under `~/.soloqueue/skills/<skill-id>/`. The agent lists every
available skill in the Skill tool description; when a task matches, the model
invokes the skill before using raw tools.

### How many skills can I install?

There is **no hard limit** — the listing always renders every visible skill in
full. The cost is tokens: the skill index is part of the system prompt on every
request.

- **Estimated cost**: the bundled catalog of 47 skills renders to roughly
  **6.4K characters ≈ 1.5K–2K tokens**, about **35 tokens per skill** on
  average. Actual size varies with description length, so treat these as
  estimates.
- **Best practice**: keep **≤ 50 installed skills**. Beyond that, every
  additional 10 skills adds roughly +350 tokens to every request and the model
  has a harder time picking the right skill.
- The listing is deterministic (invocation-count order, ID order for ties), so
  the system prompt stays stable for prompt caching.

Inspect never-invoked skills and weak descriptions with:

```bash
soloqueue skills report
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

## License

MIT
