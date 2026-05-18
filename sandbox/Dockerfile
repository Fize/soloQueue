FROM debian:bookworm

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    wget \
    git \
    openssh-client \
    jq \
    unzip \
    zip \
    vim-tiny \
    gh \
    ripgrep \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - && \
    apt-get install -y nodejs && \
    npm install -g pnpm

RUN curl -fsSL https://go.dev/dl/go1.25.0.linux-amd64.tar.gz | \
    tar -C /usr/local -xz
ENV PATH=$PATH:/usr/local/go/bin

RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH=$PATH:/root/.cargo/bin

RUN curl -fsSL https://astral.sh/uv/install.sh | sh
ENV PATH=$PATH:/root/.local/bin

RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 \
    python3-pip \
    default-jdk \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

CMD ["tail", "-f", "/dev/null"]
