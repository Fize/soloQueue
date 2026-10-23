FROM --platform=$BUILDPLATFORM node:24-alpine AS web-build

WORKDIR /src/web
RUN npm install --global pnpm@11.13.1
COPY web/package.json web/pnpm-lock.yaml ./
COPY web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM node:24-alpine AS status-build

WORKDIR /src/status-ui
RUN npm install --global pnpm@11.13.1
COPY status-ui/package.json status-ui/pnpm-lock.yaml ./
COPY status-ui/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY status-ui ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS go-build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
# Keep these inputs in sync with release.py's image fingerprint.
COPY cmd ./cmd
COPY internal ./internal
COPY --from=web-build /src/web/dist ./web/dist
COPY --from=status-build /src/status-ui/dist ./status-ui/dist
ARG TARGETOS
ARG TARGETARCH
ARG BUILDOS
ARG BUILDARCH
ARG VERSION=dev
RUN mkdir -p internal/assets/dist \
    && cp -r web/dist internal/assets/dist/web \
    && cp -r status-ui/dist internal/assets/dist/status \
    && CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/soloqueue ./cmd/soloqueue \
    && if [ "$TARGETOS/$TARGETARCH" = "$BUILDOS/$BUILDARCH" ]; then test "$(/out/soloqueue version)" = "$VERSION"; fi

FROM golang:1.25-bookworm AS go-runtime

# Native Rust is built for the image's target architecture (Buildx/QEMU for ARM64).
# The worker dynamically loads the verified Runtime downloaded on first enable.
FROM rust:1.88.0-bookworm AS native-build

WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends python3 \
    && rm -rf /var/lib/apt/lists/*
COPY native/embedding/Cargo.toml native/embedding/Cargo.lock ./native/embedding/
COPY native/embedding/src ./native/embedding/src
COPY native/decide/Cargo.toml native/decide/Cargo.lock ./native/decide/
COPY native/decide/src ./native/decide/src
COPY native/decide/vendor ./native/decide/vendor
COPY .github/scripts/release.py ./.github/scripts/release.py
RUN cargo build --locked --release --manifest-path native/embedding/Cargo.toml \
    && cargo build --locked --release --manifest-path native/decide/Cargo.toml \
    && cargo metadata --locked --format-version 1 --manifest-path native/embedding/Cargo.toml > /tmp/embedding-metadata.json \
    && cargo metadata --locked --format-version 1 --manifest-path native/decide/Cargo.toml > /tmp/decide-metadata.json \
    && mkdir -p /out \
    && cp native/embedding/target/release/soloqueue-embedding /out/soloqueue-embedding \
    && cp native/decide/target/release/soloqueue-decide /out/soloqueue-decide \
    && python3 .github/scripts/release.py licenses /tmp/embedding-metadata.json /tmp/decide-metadata.json /out/THIRD_PARTY_LICENSES.txt

FROM node:24-bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        build-essential \
        ca-certificates \
        curl \
        fd-find \
        file \
        git \
        jq \
        lsof \
        openssh-client \
        pkg-config \
        procps \
        python3 \
        python3-pip \
        python3-venv \
        ripgrep \
        unzip \
        wget \
        zip \
    && npm install --global pnpm@11.13.1 \
    && npm cache clean --force \
    && rm -rf /var/lib/apt/lists/*

COPY --from=go-runtime /usr/local/go /usr/local/go
COPY --from=go-build /out/soloqueue /usr/local/bin/soloqueue
COPY --from=native-build /out/soloqueue-embedding /usr/local/bin/soloqueue-embedding
COPY --from=native-build /out/soloqueue-decide /usr/local/bin/soloqueue-decide
COPY --from=native-build /out/THIRD_PARTY_LICENSES.txt /usr/local/share/licenses/soloqueue/THIRD_PARTY_LICENSES.txt
COPY LICENSE /usr/local/share/licenses/soloqueue/LICENSE
ENV HOME=/root \
    PATH=/usr/local/go/bin:$PATH
WORKDIR /root
EXPOSE 57689
ENTRYPOINT ["/usr/local/bin/soloqueue"]
CMD ["start", "--host", "0.0.0.0"]
