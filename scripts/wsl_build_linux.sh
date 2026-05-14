#!/usr/bin/env bash
# Builds Linux amd64 binaries and optionally packages them as dist/storm-mcp-linux.tar.gz.
# Called by `make build-linux-amd64` (build-only) and `make dist-linux` (full).
#
# Usage: wsl_build_linux.sh [build-only]
set -euo pipefail

BUILD_ONLY="${1:-}"

export PATH="/usr/local/go/bin:$PATH"
REPO="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO"

echo "Building Linux amd64 binaries ..."
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
  go build -ldflags="-s -w" -o bin/storm-mcp-linux-amd64 .
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
  go build -tags embedquery -ldflags="-s -w" -o bin/storm-mcp-full-linux-amd64 .
echo "  -> bin/storm-mcp-linux-amd64"

[[ "$BUILD_ONLY" == "build-only" ]] && exit 0

echo "Packaging ..."
STAGING=$(mktemp -d)
trap 'rm -rf "$STAGING"' EXIT

mkdir "$STAGING/storm-mcp-linux"
cp bin/storm-mcp-linux-amd64      "$STAGING/storm-mcp-linux/"
cp bin/storm-mcp-full-linux-amd64 "$STAGING/storm-mcp-linux/" 2>/dev/null || true
chmod +x "$STAGING/storm-mcp-linux/"*

mkdir -p dist
tar -czf dist/storm-mcp-linux.tar.gz -C "$STAGING" storm-mcp-linux/

echo "  -> dist/storm-mcp-linux.tar.gz  ($(du -sh dist/storm-mcp-linux.tar.gz | cut -f1))"
