.PHONY: help build build-web build-go clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

build-web: ## Build lightweight web portal (for Go embed)
	cd portal && pnpm build
	rm -rf internal/server/dist && cp -r portal/dist internal/server/dist
	cp -r skills internal/server/dist/skills

build-desktop: ## Build rich web UI (for Electron desktop client)
	cd desktop && pnpm build

build: build-web build-desktop ## Full build: web portal, desktop web, and Go binary
	go build -o soloqueue ./cmd/soloqueue

build-go: ## Build Go binary only
	go build -o soloqueue ./cmd/soloqueue

clean: ## Remove all build artifacts
	rm -rf soloqueue desktop/dist portal/dist internal/server/dist

