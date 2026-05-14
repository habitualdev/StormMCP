#!/usr/bin/env bash
# Cross-compiles storm-mcp for macOS using Zig as the C cross-compiler.
# Called by `make build-darwin-arm64` / `make build-darwin-amd64` via WSL.
#
# Usage: wsl_build_darwin.sh [arm64|amd64]
set -euo pipefail

ARCH="${1:-arm64}"

# ── Zig setup ──────────────────────────────────────────────────────────────────
ZIG_VERSION="0.13.0"
ZIG_DIR="/tmp/zig-linux-x86_64-${ZIG_VERSION}"
ZIG_BIN="${ZIG_DIR}/zig"

if [[ ! -x "$ZIG_BIN" ]]; then
    echo "Downloading zig ${ZIG_VERSION} ..."
    rm -rf "$ZIG_DIR"
    curl -fsSL \
        "https://ziglang.org/download/${ZIG_VERSION}/zig-linux-x86_64-${ZIG_VERSION}.tar.xz" \
        | tar -xJ -C /tmp
    chmod +x "$ZIG_BIN"
    echo "  -> zig ready"
fi

# ── Repo + Go ─────────────────────────────────────────────────────────────────
export PATH="/usr/local/go/bin:$PATH"
REPO="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO"

# ── Architecture-specific settings ────────────────────────────────────────────
if [[ "$ARCH" == "arm64" ]]; then
    ZIG_TARGET="aarch64-macos.12.0.0"
    GOARCH="arm64"
    ORT_LIB="assets/onnxruntime_darwin_arm64.dylib"
elif [[ "$ARCH" == "amd64" ]]; then
    ZIG_TARGET="x86_64-macos.12.0.0"
    GOARCH="amd64"
    ORT_LIB="assets/onnxruntime_darwin_amd64.dylib"
else
    echo "Error: unknown arch '$ARCH'; expected arm64 or amd64" >&2
    exit 1
fi

OUT_BASE="storm-mcp-darwin-${ARCH}"
OUT_FULL="storm-mcp-full-darwin-${ARCH}"

# ── Preflight checks ──────────────────────────────────────────────────────────
if [[ ! -f "$ORT_LIB" ]]; then
    echo "Error: $ORT_LIB not found." >&2
    echo "Run: make assets/onnxruntime_darwin_${ARCH}.dylib" >&2
    exit 1
fi

# ── All build paths on Linux tmpfs to avoid Windows-mount issues ──────────────
ZIG_GLOBAL_CACHE="/tmp/zig-cache-global"
ZIG_LOCAL_CACHE="/tmp/zig-cache-local-${ARCH}"
GO_TMP="/tmp/go-tmp"
GO_CACHE="/tmp/go-cache"

# Wipe zig local cache to prevent stale/corrupt state causing AccessDenied.
rm -rf "$ZIG_LOCAL_CACHE"
mkdir -p "$ZIG_GLOBAL_CACHE" "$ZIG_LOCAL_CACHE" "$GO_TMP" "$GO_CACHE"

# ── CC wrapper script (avoids shell-splitting issues with env inheritance) ─────
ZIG_CC_WRAPPER="/tmp/zig-cc-${ARCH}.sh"
ZIG_CXX_WRAPPER="/tmp/zig-cxx-${ARCH}.sh"

cat > "$ZIG_CC_WRAPPER" << EOF
#!/bin/bash
export ZIG_GLOBAL_CACHE_DIR="${ZIG_GLOBAL_CACHE}"
export ZIG_LOCAL_CACHE_DIR="${ZIG_LOCAL_CACHE}"
exec "${ZIG_BIN}" cc -target ${ZIG_TARGET} "\$@"
EOF
chmod +x "$ZIG_CC_WRAPPER"

cat > "$ZIG_CXX_WRAPPER" << EOF
#!/bin/bash
export ZIG_GLOBAL_CACHE_DIR="${ZIG_GLOBAL_CACHE}"
export ZIG_LOCAL_CACHE_DIR="${ZIG_LOCAL_CACHE}"
exec "${ZIG_BIN}" c++ -target ${ZIG_TARGET} "\$@"
EOF
chmod +x "$ZIG_CXX_WRAPPER"

# ── Build via Linux tmpfs ─────────────────────────────────────────────────────
BUILD_DIR="/tmp/storm-mcp-darwin-build"
rm -rf "$BUILD_DIR"
mkdir -p "$BUILD_DIR"
echo "Copying source to Linux tmpfs ..."
rsync -a \
    --exclude='.git' \
    --exclude='bin/' \
    --exclude='dist/' \
    --exclude='.venv/' \
    --exclude='__pycache__/' \
    "$REPO/" "$BUILD_DIR/"

build_one() {
    local out_name="$1" extra_tags="${2:-}"
    echo "Building $out_name (zig target: ${ZIG_TARGET}) ..."
    local tag_flag=""
    [[ -n "$extra_tags" ]] && tag_flag="-tags $extra_tags"
    ( cd "$BUILD_DIR" && \
      CGO_ENABLED=1 \
      GOOS=darwin \
      GOARCH="$GOARCH" \
      GOTMPDIR="$GO_TMP" \
      GOCACHE="$GO_CACHE" \
      CC="$ZIG_CC_WRAPPER" \
      CXX="$ZIG_CXX_WRAPPER" \
      go build \
          -ldflags="-s -w" \
          $tag_flag \
          -o "/tmp/${out_name}" \
          . )
    mkdir -p "$REPO/bin"
    cp "/tmp/${out_name}" "$REPO/bin/${out_name}"
    rm -f "/tmp/${out_name}"
    echo "  -> bin/${out_name}  ($(du -sh "$REPO/bin/${out_name}" | cut -f1))"
}

build_one "$OUT_BASE"
build_one "$OUT_FULL" "embedquery"
