# Architecture

The current architecture overview moved to
[architecture/overview.md](architecture/overview.md). This file remains as a
stable link for older bookmarks.

The current product boundary is local-first: a Go server owns runtime state and
an Electron desktop client or embedded portal consumes its HTTP and WebSocket
interfaces. The repository-level [AGENTS.md](../AGENTS.md) is the tactical
maintainer reference for package paths and build commands.
