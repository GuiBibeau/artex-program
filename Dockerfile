# syntax=docker/dockerfile:1
#
# Multi-stage build: `docker compose up -d --build` (or `docker build .`) produces
# the complete image from source. No prebuilt binary in the build context is needed,
# so a fresh `git clone` builds — dist/ is git-ignored and is no longer required.
#
#   frontend (web) -> Go binary with embedded UI (build) -> runtime image
#
# The Go binary is produced by build.sh with the same flags the release workflow's
# binaries job uses, so the image binary and the published release binary come from
# one source.

# ---------------------------------------------------------------- frontend ---
# Pinned to the Node 22 line the CI/release workflows use. On the build platform
# only; the frontend static export is architecture-independent.
FROM --platform=$BUILDPLATFORM node:25-bookworm-slim@sha256:81db02c4b671288a03915da9534dbd54f96d0e7c24d80ccc54f5b36b2e684370 AS web
ENV NEXT_TELEMETRY_DISABLED=1
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --ignore-scripts
COPY web/ ./
RUN npm run build:static

# ------------------------------------------------------------- Go binary ---
# Pinned to the exact Go release go.mod requires. The official image sets
# GOTOOLCHAIN=local, so a floating 1.26 tag that lags go.mod fails the build;
# bump this together with the go line in go.mod. The binary is CGO-free, so this
# stage runs on the build platform and cross-compiles to $TARGETARCH without QEMU.
FROM --platform=$BUILDPLATFORM golang:1.26.11-bookworm@sha256:dc40a13b61304647ed7e5d581d246b86413dfc29d63c6d018310feb93cd98fc0 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Reuse the frontend built above; build.sh skips its own frontend step.
COPY --from=web /src/web/out ./server/webui/dist
ARG ARTEX_VERSION=dev
ARG TARGETOS
ARG TARGETARCH
# Same flags as the release binaries job (ARTEX_COMPRESS=0, -trimpath,
# -X main.version), driven through build.sh so there is one build path.
RUN ARTEX_SKIP_FRONTEND=1 \
    ARTEX_SKIP_NPM_CI=1 \
    ARTEX_COMPRESS=0 \
    ARTEX_OUTPUT=/out/artex \
    ARTEX_BUILD_VERSION="${ARTEX_VERSION}" \
    bash ./build.sh --target "${TARGETOS:-linux}/${TARGETARCH:-amd64}"

# ------------------------------------------------------- Solana toolchain ---
# The tools the agents run through their Bash tool: the Agave release (solana,
# solana-keygen, spl-token, solana-test-validator, cargo-build-sbf and the SBF
# SDK), the Anchor CLI and Surfpool. Versions are pinned per tool, and every
# download is verified against a SHA-256 written down here. The digest and the
# artifact it describes are not taken from the same release at build time, so a
# swapped asset fails the build unless the diff that swaps it also changes the
# digest a reviewer reads. A version bump is two edits: the version ARG and the
# digest.
#
# linux/amd64 only: Agave, Anchor and Surfpool publish no Linux arm64 binaries
# (their arm64 assets are macOS builds). On an arm64 host, build and run the
# image as linux/amd64 (DOCKER_DEFAULT_PLATFORM=linux/amd64); the stage fails
# with a clear message instead of installing a different toolchain.
FROM --platform=$BUILDPLATFORM debian:bookworm-slim@sha256:734a72b3d6142a71993b4bc580ca01d8ca687f77a2c422d96a961d3376b179bc AS tools
ARG TARGETARCH
ARG AGAVE_VERSION=v3.1.10
ARG AGAVE_SHA256_AMD64=a7205ff29bcf0f7199740225ecae2b85a28ea9668892d5ec21bd9749882984a1
ARG ANCHOR_VERSION=1.1.2
ARG ANCHOR_SHA256_AMD64=fdea9979629e9416e5f5e5622ff6c11b8c691d1e559581ece368e903c0c980c1
ARG SURFPOOL_VERSION=v1.5.0
ARG SURFPOOL_SHA256_AMD64=5b20a3b46e60c4f819af7b4da5c3ea211f76041710617841cc23247d15887ddc
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates curl bzip2 \
    && rm -rf /var/lib/apt/lists/*
RUN set -eux; \
    [ "${TARGETARCH:-amd64}" = "amd64" ] || { echo "the Solana toolchain stage has no linux/arm64 binaries: build the image with --platform linux/amd64" >&2; exit 1; }; \
    mkdir -p /out /tmp/t; \
    verify() { asset="$1"; want="$2"; \
      [ -n "$want" ] || { echo "no pinned digest for $asset" >&2; exit 1; }; \
      got="$(sha256sum "/tmp/t/$asset" | cut -d' ' -f1)"; \
      [ "$want" = "$got" ] || { echo "checksum mismatch for $asset: want $want got $got" >&2; exit 1; }; }; \
    gh="https://github.com"; \
    curl -fsSL "$gh/anza-xyz/agave/releases/download/$AGAVE_VERSION/solana-release-x86_64-unknown-linux-gnu.tar.bz2" -o /tmp/t/agave.tar.bz2; \
    verify agave.tar.bz2 "$AGAVE_SHA256_AMD64"; \
    tar -xjf /tmp/t/agave.tar.bz2 -C /tmp/t; \
    for bin in solana solana-keygen spl-token solana-test-validator solana-stake-accounts solana-tokens cargo-build-sbf cargo-test-sbf; do \
      install -m 0755 "/tmp/t/solana-release/bin/$bin" /out/; done; \
    cp -a /tmp/t/solana-release/bin/platform-tools-sdk /out/; \
    curl -fsSL "$gh/coral-xyz/anchor/releases/download/v$ANCHOR_VERSION/anchor-${ANCHOR_VERSION}-x86_64-unknown-linux-gnu" -o /tmp/t/anchor; \
    verify anchor "$ANCHOR_SHA256_AMD64"; \
    install -m 0755 /tmp/t/anchor /out/anchor; \
    curl -fsSL "$gh/txtx/surfpool/releases/download/$SURFPOOL_VERSION/surfpool-linux-x64.tar.gz" -o /tmp/t/surfpool.tar.gz; \
    verify surfpool.tar.gz "$SURFPOOL_SHA256_AMD64"; \
    tar -xzf /tmp/t/surfpool.tar.gz -C /tmp/t; \
    install -m 0755 /tmp/t/surfpool /out/surfpool; \
    rm -rf /tmp/t

# ------------------------------------------------------- Rust toolchain ---
# For building programs (anchor build, anchor test, Trident): the official
# image's toolchain, copied whole below, so cargo, rustc and the pinned image
# agree. RUSTUP_HOME points at the copy (read-only use is enough); CARGO_HOME is
# left at the user's default so the registry cache lands in a writable home.
# The stage is the target platform, so the toolchain matches the image. rustc
# is not executed at build time: it does not run under QEMU user emulation (it
# segfaults) when an arm64 host builds the amd64 image. The pinned image digest
# is the guarantee for rustc; the runtime stage runs cargo and checks the file.
FROM rust:1.96.0-slim-bookworm@sha256:4732ca96fd086cb9be682050c3f0176288eebaac2b80aa2bcefccfaf198e1950 AS rust

# ---------------------------------------------------------------- runtime ---
# Debian trixie (glibc 2.41): the Anchor 1.1.2 release binary requires glibc 2.39,
# which bookworm (2.36) does not have. Every other binary here is built against an
# older glibc and runs on a newer one.
FROM python:3.12-slim-trixie@sha256:191083cdf6c7bda8c76158983163ead5d5cf7b77cf3033e805bb0f5296f51b4b
ARG TARGETARCH
# Shell basics for the agents, plus a C toolchain and the headers a program
# build needs (cargo-build-sbf, anchor build).
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates ripgrep curl wget vim git jq unzip xz-utils bzip2 \
      openssh-client sqlite3 xxd protobuf-compiler \
      build-essential pkg-config libssl-dev libudev-dev \
    && rm -rf /var/lib/apt/lists/*
# Node 22 from the official nodejs.org tarball: trixie's apt nodejs is 18,
# the Solana JS SDKs need >= 20, and 22 matches the CI and release workflows.
# Verified against a digest pinned here rather than against the fetched
# SHASUMS256.txt, for the reason given at the tools stage.
ARG NODE_VERSION=22.23.4
ARG NODE_SHA256_AMD64=1805bcd35e6815559bd2eb97db438d3fdb0eb3a5e6a977b7c1badca76337e9ce
ARG NODE_SHA256_ARM64=6287aa46b6c375d816e63ae66d024fbbfd70404825aabdfdf898a970871d2f4b
RUN set -eux; \
    case "${TARGETARCH:-amd64}" in \
      amd64) NODE_ARCH=x64; want="$NODE_SHA256_AMD64" ;; \
      arm64) NODE_ARCH=arm64; want="$NODE_SHA256_ARM64" ;; \
      *) echo "unsupported architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    base="https://nodejs.org/dist/v${NODE_VERSION}"; \
    asset="node-v${NODE_VERSION}-linux-${NODE_ARCH}.tar.xz"; \
    curl -fsSL "$base/$asset" -o "/tmp/$asset"; \
    [ -n "$want" ] || { echo "no pinned digest for $asset" >&2; exit 1; }; \
    got="$(sha256sum "/tmp/$asset" | cut -d' ' -f1)"; \
    if [ "$want" != "$got" ]; then echo "checksum mismatch for $asset: want $want got $got" >&2; exit 1; fi; \
    tar -xJf "/tmp/$asset" -C /usr/local --strip-components=1 --no-same-owner; \
    rm -f "/tmp/$asset"; \
    node --version; npm --version
# The Solana toolchain in the release layout (bin/ with platform-tools-sdk/
# beside cargo-build-sbf, which finds it relative to its own path), then the
# Rust toolchain.
COPY --from=tools /out/ /usr/local/bin/
COPY --from=rust /usr/local/cargo /usr/local/cargo
COPY --from=rust /usr/local/rustup /usr/local/rustup
# NO_DNA=1 makes the Solana CLIs non-interactive (no TUI, no prompts), which is
# what an agent driving them through Bash needs.
ENV RUSTUP_HOME=/usr/local/rustup \
    PATH="/usr/local/cargo/bin:${PATH}" \
    NO_DNA=1
RUN set -eux; \
    solana --version; anchor --version; surfpool --version; spl-token --version; \
    solana-test-validator --version; cargo-build-sbf --version; \
    cargo --version; test -x /usr/local/rustup/toolchains/*/bin/rustc

WORKDIR /app
# Binary compiled from source in the build stage above.
COPY --from=build /out/artex /app/artex
# Daemon startup script: relaunches the process after a crash and forwards SIGTERM
# to artex — docker stop only signals PID 1; without forwarding, artex never receives it,
# cannot shut down gracefully, and is hard-killed by SIGKILL after 10 seconds.
COPY start.sh /app/start.sh
RUN chmod +x /app/artex /app/start.sh && mkdir -p -m 0700 /app/keys \
    && chmod 0711 /root
# No setuid or setgid programs (su, passwd, mount, ...): an
# agent process runs as the executor and must not regain root through one, even
# when the container runs without docker-compose.yml's no-new-privileges.
RUN find / -xdev -perm /6000 -type f -exec chmod a-s {} +
COPY skills/ /app/skills/
# persistence points: data/ (traffic index, task files, transcripts) and keys/ (jwt.key, secret.key, via ARTEX_JWT_KEY_DIR)
VOLUME ["/app/data", "/app/keys"]
EXPOSE 8787 8788
ENTRYPOINT ["/app/start.sh"]
CMD ["-addr", ":8787", "-proxy", ":8788"]
