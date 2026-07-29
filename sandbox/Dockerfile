FROM debian:bookworm

LABEL org.soloqueue.sandbox.schema="2"

# System, DevOps, C/C++ compilation, and Network debugging tools
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    wget \
    git \
    openssh-client \
    jq \
    unzip \
    zip \
    tar \
    gzip \
    bzip2 \
    xz-utils \
    zstd \
    vim-tiny \
    gh \
    ripgrep \
    fd-find \
    procps \
    strace \
    lsof \
    htop \
    tree \
    file \
    net-tools \
    iproute2 \
    dnsutils \
    iputils-ping \
    traceroute \
    socat \
    netcat-openbsd \
    tcpdump \
    build-essential \
    cmake \
    ninja-build \
    pkg-config \
    autoconf \
    automake \
    libtool \
    clang \
    clangd \
    clang-format \
    gdb \
    valgrind \
    libssl-dev \
    zlib1g-dev \
    libffi-dev \
    && rm -rf /var/lib/apt/lists/*

# Node.js 22.x, pnpm, and built-in language servers.
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && \
    apt-get install -y nodejs && \
    npm install -g \
      pnpm \
      typescript \
      typescript-language-server@5.3.0 \
      bash-language-server@5.6.0 \
      pyright \
      @vue/language-server \
      yaml-language-server

# Go toolchain and gopls. TARGETARCH keeps local image builds portable.
ARG TARGETARCH
RUN go_arch="${TARGETARCH:-$(dpkg --print-architecture)}" && \
    curl --fail --show-error --location \
      --retry 5 --retry-all-errors --retry-delay 2 \
      --output /tmp/go.tar.gz \
      "https://go.dev/dl/go1.25.0.linux-${go_arch}.tar.gz" && \
    tar -C /usr/local -xzf /tmp/go.tar.gz && \
    rm /tmp/go.tar.gz
ENV PATH=$PATH:/usr/local/go/bin
RUN GOBIN=/usr/local/bin go install golang.org/x/tools/gopls@v0.21.1

# Rust and RTK execute inside the sandbox; the host RTK binary is never reused.
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH=$PATH:/root/.cargo/bin
RUN cargo install --locked --root /usr/local --git https://github.com/rtk-ai/rtk --tag v0.43.0

# Python 3, pip, and uv
RUN curl -fsSL https://astral.sh/uv/install.sh | env UV_INSTALL_DIR=/usr/local/bin sh

RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 \
    python3-pip \
    && rm -rf /var/lib/apt/lists/*

CMD ["tail", "-f", "/dev/null"]
