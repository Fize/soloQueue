# Architecture

> 中文：[架构](zh/architecture.md)

I moved the current architecture overview to
[architecture/overview.md](architecture/overview.md). I keep this file as a
stable link for older bookmarks.

I define the current product boundary as local-first: a Go server owns runtime
state and an Electron desktop client or embedded portal consumes its HTTP and
WebSocket interfaces. I use the repository-level [AGENTS.md](../AGENTS.md) as
the tactical maintainer reference for package paths and build commands.
