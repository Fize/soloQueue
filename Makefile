.PHONY: help build build-web build-status build-assets build-go build-go-win build-win start web clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

build-web: ## Build the full browser Web Console
	cd web && pnpm install --frozen-lockfile && pnpm test && pnpm build
	rm -rf internal/assets/dist/web && cp -r web/dist internal/assets/dist/web

build-status: ## Build the read-only Status UI
	cd status-ui && pnpm install --frozen-lockfile && pnpm test && pnpm build
	rm -rf internal/assets/dist/status && cp -r status-ui/dist internal/assets/dist/status

build-assets: build-web build-status ## Build both browser bundles and copy built-in Skills
	rm -rf internal/assets/dist/skills && mkdir -p internal/assets/dist/skills
	rsync -a --exclude='.venv' --exclude='__pycache__' --exclude='*.pyc' skills/ internal/assets/dist/skills/

build-go: ## Build Go binary using already-built assets
	go build -ldflags="-s -w" -o soloqueue ./cmd/soloqueue

build: build-assets build-go ## Build browser assets and Go binary

build-win: build-assets ## Build Windows Go binary with embedded browser assets
	GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o soloqueue.exe ./cmd/soloqueue

build-go-win: build-go ## Compatibility alias for Windows-only Go build

start: build ## Build and start backend + both browser UIs on one port
	./soloqueue start

web: build-web ## Build and start standalone Web Console
	./soloqueue web

clean: ## Remove generated binaries and embedded frontend bundles
	rm -rf soloqueue soloqueue.exe web/dist status-ui/dist internal/assets/dist/web internal/assets/dist/status internal/assets/dist/skills
