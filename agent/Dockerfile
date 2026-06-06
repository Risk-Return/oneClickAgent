# IAgent Agent Container
# Multi-stage build: ubuntu:24.04 + toolchain (opencode, camoufox, runtimes, VNC stack)
# + dependency cache warm-up from agent/deps/ manifests.
#
# Image layout (spec 04-agent-container §10):
#   - Base: ubuntu:24.04, non-root user 'app'
#   - Bundled: opencode, camoufox, Node 20, Python 3.12, Go 1.22, Rust, Temurin 21 JDK
#   - VNC stack: Xvfb + x11vnc (on-demand, loopback-only, never host-published)
#   - HEALTHCHECK hitting /healthz
#   - Multi-arch: linux/amd64, linux/arm64
#   - Writable areas limited to /work (volume) + /tmp (tmpfs)

FROM ubuntu:24.04 AS base
ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    curl \
    fluxbox \
    git \
    gnupg \
    software-properties-common \
    wget \
    x11vnc \
    xvfb \
  && rm -rf /var/lib/apt/lists/*

# ---------- Node.js 20 LTS ----------
FROM base AS node
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
  && apt-get install -y --no-install-recommends nodejs \
  && rm -rf /var/lib/apt/lists/*

# ---------- Python 3.12 ----------
FROM node AS python
RUN add-apt-repository -y ppa:deadsnakes/ppa \
  && apt-get update \
  && apt-get install -y --no-install-recommends python3.12 python3.12-venv python3.12-dev \
  && rm -rf /var/lib/apt/lists/* \
  && update-alternatives --install /usr/bin/python3 python3 /usr/bin/python3.12 1 \
  && update-alternatives --install /usr/bin/python python /usr/bin/python3.12 1 \
  && PIP_BREAK_SYSTEM_PACKAGES=1 curl -sS https://bootstrap.pypa.io/get-pip.py | python3.12

# ---------- Go 1.22 ----------
FROM python AS golang
ENV GOLANG_VERSION=1.22.12
RUN arch=$(uname -m) \
  && case "$arch" in \
       x86_64)  go_arch="amd64" ;; \
       aarch64) go_arch="arm64" ;; \
       *)       echo "unsupported arch: $arch"; exit 1 ;; \
     esac \
  && curl -fsSL "https://go.dev/dl/go${GOLANG_VERSION}.linux-${go_arch}.tar.gz" -o /tmp/go.tar.gz \
  && tar -C /usr/local -xzf /tmp/go.tar.gz \
  && rm /tmp/go.tar.gz
ENV PATH="/usr/local/go/bin:$PATH"

# ---------- Rust (stable) ----------
FROM golang AS rust
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH="/root/.cargo/bin:$PATH"

# ---------- Java (Temurin 21 JDK) ----------
FROM rust AS java
RUN apt-get update \
  && apt-get install -y --no-install-recommends wget apt-transport-https \
  && wget -qO - https://packages.adoptium.net/artifactory/api/gpg/key/public | gpg --dearmor > /usr/share/keyrings/adoptium.gpg \
  && echo "deb [signed-by=/usr/share/keyrings/adoptium.gpg] https://packages.adoptium.net/artifactory/deb $(awk -F= '/VERSION_CODENAME/{print$2}' /etc/os-release) main" \
       > /etc/apt/sources.list.d/adoptium.list \
  && apt-get update \
  && apt-get install -y --no-install-recommends temurin-21-jdk \
  && ln -s /usr/lib/jvm/temurin-21-jdk-$(dpkg --print-architecture) /usr/lib/jvm/java-21-temurin \
  && rm -rf /var/lib/apt/lists/*
ENV JAVA_HOME=/usr/lib/jvm/java-21-temurin

# ---------- Dependency cache warm-up ----------
FROM java AS cache
WORKDIR /tmp/deps

COPY deps/ /tmp/deps/

RUN npm ci 2>/dev/null || true \
  && rm -rf node_modules

RUN pip3.12 install -r requirements.txt 2>/dev/null || true

RUN go mod download 2>/dev/null || true

RUN cargo fetch 2>/dev/null || true

RUN mvn dependency:go-offline 2>/dev/null || true

WORKDIR /tmp
RUN rm -rf /tmp/deps

# ---------- Bundled agent tools ----------
FROM cache AS tools

RUN npm install -g opencode-ai@latest 2>/dev/null || true
RUN npx --yes @askjo/camofox-browser 2>/dev/null || true

# ---------- Final image ----------
FROM tools AS final

RUN useradd --create-home --shell /bin/bash app \
  && mkdir -p /work /tmp/.X11-unix \
  && chown -R app:app /work /tmp/.X11-unix \
  && rm -rf /root/.cargo/registry/cache /root/.cargo/git \
  && apt-get clean \
  && rm -rf /var/lib/apt/lists/*

COPY --chown=app:app pyproject.toml /home/app/
COPY --chown=app:app iagent_agent/ /home/app/iagent_agent/
COPY --chown=app:app vnc/ /home/app/vnc/

RUN PIP_BREAK_SYSTEM_PACKAGES=1 python3.12 -m pip install --no-cache-dir -e /home/app/

ENV IAGENT_AGENT_PORT=8090
ENV IAGENT_WORK_DIR=/work
ENV IAGENT_BROWSER_PROFILE=/work/profile
ENV IAGENT_VNC_DISPLAY=:99
ENV IAGENT_VNC_PORT=5901
ENV IAGENT_VNC_ENABLED=true
ENV IAGENT_BROWSER_CMD=camoufox
ENV IAGENT_BRAIN=opencode

VOLUME ["/work"]
WORKDIR /home/app
EXPOSE 8090 5901

# Security hardening flags (applied at docker run time by the device manager):
#   --cap-drop=ALL --security-opt=no-new-privileges --pids-limit=256
# No host network; only container port 8090 is published.
USER app

HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=3 \
  CMD curl -sf http://localhost:8090/healthz || exit 1

ENTRYPOINT ["python3.12", "-m", "iagent_agent"]
