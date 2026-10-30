FROM --platform=$BUILDPLATFORM node:26-alpine AS web-build

WORKDIR /src/web
RUN npm install --global pnpm@12.16.1
COPY web/package.json web/pnpm-lock.yaml ./
COPY web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM node:26-alpine AS status-build

WORKDIR /src/status-ui
RUN npm install --global pnpm@12.16.1
COPY status-ui/package.json status-ui/pnpm-lock.yaml ./
COPY status-ui/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY status-ui ./
RUN pnpm build

FROM golang:1.27-bookworm AS go-build

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
    && CGO_ENABLED=1 GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/soloqueue ./cmd/soloqueue \
    && if [ "$TARGETOS/$TARGETARCH" = "$BUILDOS/$BUILDARCH" ]; then test "$(/out/soloqueue version)" = "$VERSION"; fi

FROM golang:1.27-bookworm AS go-runtime

FROM python:3.15-slim-bookworm AS python-runtime

FROM node:26-bookworm-slim

# Docker CLI only, no daemon: reach a daemon outside this container through
# DOCKER_HOST or a mounted /var/run/docker.sock.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc \
    && chmod a+r /etc/apt/keyrings/docker.asc \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian $(. /etc/os-release && echo "$VERSION_CODENAME") stable" > /etc/apt/sources.list.d/docker.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        bat \
        build-essential \
        ca-certificates \
        curl \
        dnsutils \
        docker-buildx-plugin \
        docker-ce-cli \
        docker-compose-plugin \
        fd-find \
        file \
        fzf \
        gh \
        git \
        git-lfs \
        htop \
        iproute2 \
        iputils-ping \
        jq \
        less \
        libffi-dev \
        libsqlite3-dev \
        libssl-dev \
        lsof \
        man-db \
        net-tools \
        netcat-openbsd \
        openssh-client \
        pkg-config \
        procps \
        python3 \
        python3-dev \
        python3-pip \
        python3-venv \
        ripgrep \
        rsync \
        shellcheck \
        sqlite3 \
        tmux \
        tree \
        unzip \
        vim \
        wget \
        zip \
        zlib1g-dev \
    && npm install --global npm@12.2.1 \
    && npm install --global pnpm@12.16.1 \
    && npm cache clean --force \
    && curl -fsSL "https://github.com/mikefarah/yq/releases/download/v4.54.1/yq_linux_$(dpkg --print-architecture)" -o /usr/local/bin/yq \
    && chmod +x /usr/local/bin/yq \
    && curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs -o /tmp/rustup-init.sh \
    && CARGO_HOME=/root/.cargo RUSTUP_HOME=/root/.rustup sh /tmp/rustup-init.sh -y --profile minimal --component clippy --component rustfmt --no-modify-path \
    && rm -f /tmp/rustup-init.sh \
    && rm -f /usr/lib/python3*/EXTERNALLY-MANAGED \
    && ln -sf /usr/bin/fdfind /usr/local/bin/fd \
    && ln -sf /usr/bin/batcat /usr/local/bin/bat \
    && git config --system user.name "soloqueue" \
    && git config --system user.email "soloqueue@localhost" \
    && rm -rf /var/lib/apt/lists/* /root/.cargo/registry /root/.rustup/downloads /root/.rustup/tmp

COPY --from=python-runtime /usr/local /usr/local
COPY --from=go-runtime /usr/local/go /usr/local/go
COPY --from=go-build /out/soloqueue /usr/local/bin/soloqueue
COPY LICENSE /usr/local/share/licenses/soloqueue/LICENSE
COPY internal/infra/tokenizer/LICENSE /usr/local/share/licenses/soloqueue/TOKENIZER_LICENSE
COPY internal/infra/localonnx/LAYA_LICENSE /usr/local/share/licenses/soloqueue/LAYA_LICENSE
COPY internal/infra/localonnx/LAYA_NOTICE.md /usr/local/share/licenses/soloqueue/LAYA_NOTICE
ENV HOME=/root \
    PATH=/root/.cargo/bin:/usr/local/go/bin:$PATH
WORKDIR /root
EXPOSE 57689
ENTRYPOINT ["/usr/local/bin/soloqueue"]
CMD ["start", "--host", "0.0.0.0"]
