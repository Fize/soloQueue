FROM debian:bookworm

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
    clang-format \
    gdb \
    valgrind \
    libssl-dev \
    zlib1g-dev \
    libffi-dev \
    && rm -rf /var/lib/apt/lists/*

# Node.js 22.x & pnpm
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && \
    apt-get install -y nodejs && \
    npm install -g pnpm

# Go 1.25.0
RUN curl -fsSL https://go.dev/dl/go1.25.0.linux-amd64.tar.gz | \
    tar -C /usr/local -xz
ENV PATH=$PATH:/usr/local/go/bin

# Rust (cargo)
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH=$PATH:/root/.cargo/bin

# Python 3, pip, and uv
RUN curl -fsSL https://astral.sh/uv/install.sh | sh
ENV PATH=$PATH:/root/.local/bin

RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 \
    python3-pip \
    default-jdk \
    && rm -rf /var/lib/apt/lists/*

CMD ["tail", "-f", "/dev/null"]
