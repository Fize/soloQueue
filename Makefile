.PHONY: build build-web build-go clean

build-web:
	cd web && pnpm build
	cp -r web/dist internal/server/dist

build: build-web
	go build -o soloqueue ./cmd/soloqueue

build-go:
	go build -o soloqueue ./cmd/soloqueue

clean:
	rm -rf soloqueue web/dist internal/server/dist
