Vendored copy of `laya-rust` from https://github.com/NandhaKishorM/laya (package `laya-onnx`, v0.1.0).

- Upstream: NandhaKishorM/laya, branch `main`, snapshot 2026-10-22
  (source tarball sha256 ca5bff3e38d6f2ad8fdbd1e0968250a2c5255bd6e23f901addd52b6db9daa50d)
- License: Apache-2.0 (see LICENSE)
- Local changes: the `[workspace]` table is dropped (the upstream `samples/*` members are not
  vendored); `src/` is otherwise unmodified.

Reason for vendoring: `laya-onnx` is not published on crates.io and the upstream repository has no
root `Cargo.toml`, so it cannot be consumed as a versioned or git dependency.
