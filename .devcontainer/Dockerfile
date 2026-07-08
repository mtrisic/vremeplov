# Vremeplov development environment: Go toolchain + Node (for
# GOOS=js GOARCH=wasm tests via Go's go_js_wasm_exec wrapper) + the
# X11/GL/ALSA/Wayland dev headers Ebiten needs to compile the desktop
# frontend for linux (frontends/desktop is the repo's only cgo user;
# the container compiles and tests it, running the window needs a
# host display).
#
# All development, building, and verification happens inside this
# container (see AGENTS.md). Hosts are assumed to have only Docker
# and VSCode.
FROM mcr.microsoft.com/devcontainers/go:1.26-trixie

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
        nodejs \
        curl \
        ca-certificates \
        libc6-dev \
        libgl1-mesa-dev \
        libxcursor-dev \
        libxi-dev \
        libxinerama-dev \
        libxrandr-dev \
        libxxf86vm-dev \
        libasound2-dev \
        libwayland-dev \
        libxkbcommon-dev \
        pkg-config \
        xvfb \
        xauth \
    && rm -rf /var/lib/apt/lists/*

# Ebiten initializes GLFW at package init on linux, so the desktop
# frontend's test binaries need a display even for pure-logic tests —
# tools/check-desktop.sh wraps them in xvfb-run.

# The workspace bind-mount is owned by the host user; let git (and go
# build's VCS stamping) trust it for whoever runs inside the container.
RUN git config --system --add safe.directory /workspaces/vremeplov

# Persistent shell history lives on a named volume mounted at /commandhistory
# (see devcontainer.json); post_create.sh points HISTFILE there.
RUN mkdir -p /commandhistory && chown vscode:vscode /commandhistory
