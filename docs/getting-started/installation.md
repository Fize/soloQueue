# Installation

SoloQueue is currently distributed as source. A local build produces both the
Go server and, when requested, the Electron desktop client.

## Prerequisites

- Go 1.25.8 or a compatible 1.25 release.
- Node.js and pnpm.
- Git.
- An API key for at least one enabled LLM provider.

The Makefile runs the required pnpm approve-builds and dependency install
steps for the portal and desktop packages.

## Build the browser portal and server

~~~bash
git clone https://github.com/Fize/soloQueue.git
cd soloQueue
make build
~~~

The command builds portal/, copies the result to
internal/server/dist/, copies bundled skills, and creates the soloqueue
binary at the repository root.

Start it with:

~~~bash
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue serve
~~~

The default listener is 127.0.0.1:57647. Continue with
[First run](first-run.md).

## Run the desktop development client

The desktop client and backend are separate development processes:

~~~bash
# Terminal 1
go run ./cmd/soloqueue serve --port 8765 --verbose

# Terminal 2
cd desktop
pnpm approve-builds
pnpm install
pnpm dev
~~~

The Vite server proxies /api and /ws to the backend on port 8765.
make build-desktop creates the production web assets, while
make package-desktop PLATFORM=mac (or win/linux) invokes Electron Builder.

## Build variants

| Command | Result |
| --- | --- |
| make build-web | Portal assets copied into the Go embed directory |
| make build-go | Go binary; assumes portal assets already exist |
| make build | Portal plus Go binary |
| make build-desktop | Desktop renderer assets |
| make build-all | Portal, Go binary, and desktop renderer |
| make package-desktop PLATFORM=mac | macOS desktop package |

macOS signing is a maintainer workflow documented in
[macOS signing](../macos-signing.md). It does not provide Apple notarization.

## Source-development note

go run ./cmd/soloqueue serve can start the backend without a fresh portal
build, but the embedded browser UI will be blank if internal/server/dist/ is
absent. Use make build-web first when you need the portal.
