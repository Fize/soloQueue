.PHONY: help build build-web build-status build-assets build-go build-native build-decide build-with-local start web clean

RUST_TOOLCHAIN ?= 1.88.0
EXE_SUFFIX := $(if $(filter Windows_NT,$(OS)),.exe,)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

build-web: ## Build the full browser Web Console
	cd web && pnpm install --frozen-lockfile && pnpm test && pnpm build
	rm -rf internal/assets/dist/web && mkdir -p internal/assets/dist && cp -r web/dist internal/assets/dist/web

build-status: ## Build the read-only Status UI
	cd status-ui && pnpm install --frozen-lockfile && pnpm test && pnpm build
	rm -rf internal/assets/dist/status && mkdir -p internal/assets/dist && cp -r status-ui/dist internal/assets/dist/status

build-assets: build-web build-status ## Build both browser bundles

build-go: ## Build Go binary using already-built assets
	go build -ldflags="-s -w" -o soloqueue ./cmd/soloqueue

build: build-assets build-go ## Build browser assets and Go binary

build-native: ## Build the optional local ONNX worker (Rust 1.88.0 required)
	rustup run $(RUST_TOOLCHAIN) cargo build --locked --release --manifest-path native/embedding/Cargo.toml
	cp native/embedding/target/release/soloqueue-embedding$(EXE_SUFFIX) soloqueue-embedding$(EXE_SUFFIX)

build-decide: ## Build the optional local laya decision worker (Rust 1.88.0 required)
	rustup run $(RUST_TOOLCHAIN) cargo build --locked --release --manifest-path native/decide/Cargo.toml
	cp native/decide/target/release/soloqueue-decide$(EXE_SUFFIX) soloqueue-decide$(EXE_SUFFIX)

build-with-local: build build-native build-decide ## Build browser assets, Go binary and both sibling workers

start: build ## Build and start backend + both browser UIs on one port
	./soloqueue start

web: build-web ## Build and start standalone Web Console
	./soloqueue web

clean: ## Remove generated binaries and embedded frontend bundles
	rm -rf soloqueue soloqueue.exe soloqueue-embedding soloqueue-embedding.exe soloqueue-decide soloqueue-decide.exe web/dist status-ui/dist internal/assets/dist/web internal/assets/dist/status
