# Local ONNX embedding worker

The Go client lives in `internal/memory/engine/embedding/local`. Release archives
bundle `soloqueue-embedding` (`.exe` on Windows) next to the SoloQueue executable;
keep those two files together. The backend downloads and verifies the fixed model
and ONNX Runtime on first enable of local embeddings. Cached artifacts support
offline restart. An explicit `SOLOQUEUE_ONNX_WORKER` path can select a development
worker. No Python installation is required for normal local embedding use.

Source builds keep the worker optional:

```sh
rustup toolchain install 1.88.0 --profile minimal
make build-native       # worker only, copied next to ./soloqueue
make build-with-local   # both browser UIs, Go app and sibling worker
```

`make build` and `make build-go` retain their existing Rust-free behavior. For
distribution, archives include the project license and Rust dependency notices.
The legacy macOS Intel archive has no local worker because the pinned Runtime
does not publish a compatible package. Linux amd64/arm64 Docker images build a
native worker for each architecture; Linux arm64 local inference uses CPU.
The Linux amd64 release worker is built on Ubuntu 24.04 and needs a compatible
glibc/libstdc++ installation. Docker workers are built against Debian bookworm;
the standalone Linux archive does not promise compatibility with older distros.

The default model is official `intfloat/multilingual-e5-small`, revision
`614241f622f53c4eeff9890bdc4f31cfecc418b3`. Preparation downloads its official
FP32 ONNX graph and tokenizer; no model export, runtime download or network access
occurs in the worker. Files are checked against pinned SHA-256 or the immutable
Hugging Face revision's LFS/Git metadata. Model weights, runtimes and reference
fixtures must be staged **outside the repository**.

The worker pins `ort=2.0.0-rc.12` (API 24), `tokenizers=0.22.2`, and the complete
Rust graph in `Cargo.lock`. Runtime packages are pinned to ONNX Runtime 1.24.2.
`ort` default features are disabled: no automatic binary/model downloads.
Python reference dependencies are pinned in `scripts/requirements-reference.txt`.
The commands below are developer verification tools; Python 3.12 is used for
artifact preparation and reference comparison.

```sh
python3.12 -m venv /tmp/soloqueue-onnx-venv
/tmp/soloqueue-onnx-venv/bin/python -m pip install -r native/embedding/scripts/requirements-reference.txt
cargo build --locked --release --manifest-path native/embedding/Cargo.toml
python3.12 native/embedding/scripts/prepare.py --output /tmp/soloqueue-onnx --reference --runtime-flavor cpu
```

Use the absolute `modelDir` and `runtime` from preparation's JSON result:

```sh
/tmp/soloqueue-onnx-venv/bin/python native/embedding/scripts/verify.py \
  --worker "$PWD/native/embedding/target/release/soloqueue-embedding" \
  --model-dir /tmp/soloqueue-onnx/model --runtime /absolute/path/to/runtime-library \
  --device cpu --fixture-output /tmp/soloqueue-onnx/token-fixture.json
SOLOQUEUE_ONNX_MODEL_DIR=/tmp/soloqueue-onnx/model \
SOLOQUEUE_ONNX_TOKEN_FIXTURE=/tmp/soloqueue-onnx/token-fixture.json \
  cargo test --locked --manifest-path native/embedding/Cargo.toml tests::tokenizer_parity -- --ignored --exact
```

On Windows use the venv's `Scripts/python.exe` and the worker's `.exe` suffix.
Run the same verifier again with `--device auto`. It compares 22 real query and
document embeddings against Hugging Face AutoTokenizer/AutoModel on CPU (minimum
cosine 0.9999), effective token counts, truncation and finite unit vectors.
The separate artifact-dependent Rust test compares exact IDs, attention masks,
and type IDs. It is explicitly ignored in model-free unit runs and explicitly
required by native CI; it fails when its model or reference fixture is missing.

Input text is raw: the worker adds one `query: ` or `passage: ` prefix. Literal
prefixes already contained in user text remain part of that text. Full tokenizer
normalization/postprocessing are preserved; truncation/padding use 512 tokens,
`<pad>` from the tokenizer vocabulary, attention-mask mean pooling and L2
normalization. Inputs and `last_hidden_state` output are checked on model load.
Single-item inference keeps a batch's memory bounded while accepting up to 32
texts. Logs never include raw input.

## Device selection and supported artifacts

The default device is **auto**, with CPU fallback. `--device cpu` forces CPU.
`--runtime-flavor auto` prepares a provider-capable package where available:

| Native platform | CPU package | Auto package / selection |
|---|---|---|
| macOS arm64 | Official 1.24.2 | CoreML CPUAndGPU, then CPU |
| Linux x86_64 glibc | Official 1.24.2 | Official CUDA package, then CPU |
| Linux aarch64 glibc | Official 1.24.2 | Explicit provider-capable runtime required |
| Windows x86_64 | Official 1.24.2 | Official Microsoft DirectML NuGet + DirectML 1.15.4; CUDA attempted before DirectML when supplied runtime supports it |

Windows DirectML selects a DXGI hardware adapter, rejecting software adapters.
Its required DLL is placed next to the runtime and dependency lookup is set to
that directory. CUDA requires compatible NVIDIA driver, CUDA and cuDNN libraries;
the script does not install drivers or system CUDA packages. A runtime missing
its accelerated provider or dependencies falls back to CPU and reports a reason;
this does **not** mean the machine lacks GPU hardware. Explicit `--runtime` can
select another API-24-compatible provider-capable installation.

Acceleration is accepted only after successful warmup and profiling proves nodes
executed through the selected EP. `backend` describes the verified execution
provider, not a claim that all operations ran on GPU. CoreML CPUAndGPU allows
CoreML to schedule supported operations between CPU and GPU. A runtime inference
failure discards partial results and retries the **entire request** on a newly
created CPU session. There is no remote fallback.

The native CI gate runs macOS arm64 (`macos-15`), Linux x86_64, Windows x86_64.
It validates CPU parity and auto selection/fallback against real artifacts.
GPU hardware execution on hosted CI is conditional on actual hardware; a CPU
fallback is reported explicitly. macOS Intel has no official 1.24.2 archive and
is **not covered by this pinned package matrix**. Linux ARM64 package preparation
is supported but is not part of this three-runner gate.

## Protocol and lifecycle checks

`protocol.md` is the version 1 NDJSON wire contract. Stdout contains only protocol
frames; stderr is diagnostic. Unknown fields, invalid IDs/versions/purposes,
batches over 32, texts over 64 KiB and frames over 1 MiB are rejected. Malformed
JSON and oversized newline-terminated frames are recoverable. EOF and shutdown
exit cleanly. The Go client validates ready/results, dimensions, finite values,
unit norm and IDs. Its context-aware queue serializes requests; cancellation of
an active request kills and reaps the process. Queue cancellation preserves the
active request.

```sh
cargo test --locked --manifest-path native/embedding/Cargo.toml
cargo fmt --manifest-path native/embedding/Cargo.toml --check
cargo clippy --locked --manifest-path native/embedding/Cargo.toml --all-targets -- -D warnings
go test -race ./internal/memory/engine/embedding/local/...
python3.12 -m unittest discover -s native/embedding/scripts -v
```

Downloads, runtime extraction and reference fixtures affect only the chosen
external staging directory. Rollback removes that directory and these newly
introduced source paths; no production database/configuration migration occurs
in phase 1. Real native evidence is required before proceeding with those changes.
