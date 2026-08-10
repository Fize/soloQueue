# Installation

> 中文：[安装](../zh/getting-started/installation.md)

I currently distribute SoloQueue as source. When I build it locally, I produce
the Go server and, when needed, the Electron desktop client.

## Prerequisites

- Go 1.25.8 or a compatible 1.25 release.
- Node.js and pnpm.
- Git.
- An API key for at least one enabled LLM provider.

I use the Makefile to run the required pnpm approve-builds and dependency
install steps for the portal and desktop packages.

## Build the browser portal and server

~~~bash
git clone https://github.com/Fize/soloQueue.git
cd soloQueue
make build
~~~

I run this command to build portal/, copy the result to internal/server/dist/,
copy bundled skills, and create the soloqueue binary at the repository root.

I start it with:

~~~bash
export DEEPSEEK_API_KEY="your-api-key"
./soloqueue serve
~~~

I use 127.0.0.1:57647 by default. I continue with
[First run](first-run.md).

## Run the desktop development client

I run the desktop client and backend as separate development processes:

~~~bash
# Terminal 1
go run ./cmd/soloqueue serve --port 8765 --verbose

# Terminal 2
cd desktop
pnpm approve-builds
pnpm install
pnpm dev
~~~

I use the Vite server to proxy /api and /ws to my backend on port 8765.
I use make build-desktop to create the production web assets, while
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

I document macOS signing as a maintainer workflow in
[macOS signing](../macos-signing.md). I do not use it as Apple notarization.

## Source-development note

I can start the backend with go run ./cmd/soloqueue serve without a fresh portal
build, but my embedded browser UI will be blank if internal/server/dist/ is
absent. I run make build-web first when I need the portal.
