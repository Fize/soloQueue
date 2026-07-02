.PHONY: help build build-web build-desktop build-all build-go package-desktop clean

PLATFORM ?=
GOOS =
GOARCH =
PLATFORM_FLAGS =
ifeq ($(PLATFORM),mac)
  PLATFORM_FLAGS = --mac
  GOOS = darwin
else ifeq ($(PLATFORM),win)
  PLATFORM_FLAGS = --win
  GOOS = windows
  GOARCH = amd64
else ifeq ($(PLATFORM),linux)
  PLATFORM_FLAGS = --linux
  GOOS = linux
  GOARCH = amd64
endif

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

build-web: ## Build lightweight web portal (for Go embed)
	pnpm --prefix portal install
	pnpm --prefix portal build
	rm -rf internal/server/dist && cp -r portal/dist internal/server/dist
	cp -r skills internal/server/dist/skills

build-desktop: ## Build rich web UI (for Electron desktop client)
	pnpm --prefix desktop install
	pnpm --prefix desktop build

build: build-web ## Build Go binary with web portal embedded (Default binary build)
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o soloqueue ./cmd/soloqueue
	@if [ "$(GOOS)" = "windows" ] && [ -f soloqueue.exe ]; then mv soloqueue.exe soloqueue; fi

build-all: build build-desktop ## Full build: build Go binary (with portal) AND desktop web UI

build-go: ## Build Go binary only (assumes portal dist is already built)
	go build -o soloqueue ./cmd/soloqueue

package-desktop: build-all ## Package Electron desktop client binaries (specify platform via PLATFORM=mac|win|linux, defaults to current OS)
	pnpm --prefix desktop run package -- $(PLATFORM_FLAGS)

clean: ## Remove all build artifacts
	rm -rf soloqueue desktop/dist desktop/dist-desktop portal/dist internal/server/dist

