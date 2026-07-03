.PHONY: help build build-web build-desktop build-all build-all-win build-go build-go-win build-win package-desktop package-desktop-win clean

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
	pnpm --prefix portal approve-builds esbuild
	pnpm --prefix portal install
	pnpm --prefix portal build
	rm -rf internal/server/dist && cp -r portal/dist internal/server/dist
	cp -r skills internal/server/dist/skills

build-desktop: ## Build rich web UI (for Electron desktop client)
	pnpm --prefix desktop approve-builds
	pnpm --prefix desktop install
	pnpm --prefix desktop build

build: build-web ## Build Go binary with web portal embedded (Default binary build)
	GOOS=$(GOOS) GOARCH=$(GOARCH) go build -o soloqueue ./cmd/soloqueue
	@if [ "$(GOOS)" = "windows" ]; then mv soloqueue.exe soloqueue; fi

build-win: build-web ## Build Go binary for Windows with web portal embedded
	GOOS=windows GOARCH=amd64 go build -o soloqueue.exe ./cmd/soloqueue

build-all: build build-desktop ## Full build: Go binary (with portal) AND desktop web UI

build-all-win: build-win build-desktop ## Build all source artifacts for Windows
	@if ! dpkg -l wine32:i386 >/dev/null 2>&1; then \
		echo "ERROR: wine32 is required for Windows desktop build on Linux."; \
		echo "Install:"; \
		echo "  sudo dpkg --add-architecture i386 && sudo apt update \\"; \
		echo "  && sudo apt install wine wine32:i386"; \
		exit 1; \
	fi
	cd desktop && npx electron-builder build --config electron-builder.json --win

build-go: ## Build Go binary only (assumes portal dist is already built)
	go build -o soloqueue ./cmd/soloqueue

build-go-win: ## Build Go binary for Windows only (assumes portal dist is already built)
	GOOS=windows GOARCH=amd64 go build -o soloqueue.exe ./cmd/soloqueue

package-desktop: build-all ## Package Electron desktop client binaries (specify platform via PLATFORM=mac|win|linux, defaults to current OS)
	cd desktop && npx electron-builder build --config electron-builder.json $(PLATFORM_FLAGS)

package-desktop-win: build-all-win ## Alias: full Windows build including desktop installer

clean: ## Remove all build artifacts
	rm -rf soloqueue soloqueue.exe desktop/dist desktop/dist-desktop portal/dist internal/server/dist

