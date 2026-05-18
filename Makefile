.PHONY: help build build-web build-go clean build-sandbox push-sandbox

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

build-sandbox: ## Build Docker sandbox image
	docker build -t malzaharguo/soloqueue-sandbox sandbox/

push-sandbox: ## Push sandbox image to Docker Hub
	docker push malzaharguo/soloqueue-sandbox:latest

build-web: ## Build web UI (Vite + TypeScript + copy dist)
	cd web && pnpm build
	rm -rf internal/server/dist && cp -r web/dist internal/server/dist

build: build-web ## Full build: web + Go binary
	go build -o soloqueue ./cmd/soloqueue

build-go: ## Build Go binary only
	go build -o soloqueue ./cmd/soloqueue

clean: ## Remove all build artifacts
	rm -rf soloqueue web/dist internal/server/dist
