# storm-mcp Makefile
#
# Prerequisites (for production builds with real ONNX Runtime):
#   - Go 1.22+
#   - A C compiler (gcc / clang) for the CGo ONNX Runtime bindings
#   - curl + tar
#
# Quick dev build (no C compiler needed, ONNX inference stubbed):
#   make build-stub
#
# Production builds (downloads ONNX Runtime shared library per platform):
#   make build                    # current host platform (Windows native)
#   make build-linux-amd64        # Linux amd64 — run from WSL
#   make build-darwin-amd64       # macOS amd64 — via WSL + Zig cross-compiler
#   make build-darwin-arm64       # macOS arm64 — via WSL + Zig cross-compiler
#   make build-all                # all three release platforms (from WSL)

BINARY_NAME   := storm-mcp
ORT_VERSION   := 1.22.0
ASSETS_DIR    := assets
BIN_DIR       := bin

# Asset source directories (Windows prep step only)
SYNSYN_DIR    := G:/SynSyn
SYNAPSE_DB    := G:/SynapseMCP/synapse_db
SYNAPSE_MODEL := G:/SynapseMCP/model

# ── ONNX Runtime download URLs ─────────────────────────────────────────────────

ORT_LINUX_AMD64_URL   := https://github.com/microsoft/onnxruntime/releases/download/v$(ORT_VERSION)/onnxruntime-linux-x64-$(ORT_VERSION).tgz
ORT_DARWIN_AMD64_URL  := https://github.com/microsoft/onnxruntime/releases/download/v$(ORT_VERSION)/onnxruntime-osx-x86_64-$(ORT_VERSION).tgz
ORT_DARWIN_ARM64_URL  := https://github.com/microsoft/onnxruntime/releases/download/v$(ORT_VERSION)/onnxruntime-osx-arm64-$(ORT_VERSION).tgz
ORT_WINDOWS_AMD64_URL := https://github.com/microsoft/onnxruntime/releases/download/v$(ORT_VERSION)/onnxruntime-win-x64-$(ORT_VERSION).zip

# ── Directory setup ────────────────────────────────────────────────────────────

$(ASSETS_DIR):
	mkdir -p $(ASSETS_DIR)

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

# ── Asset prep (Windows — copies from SynapseMCP / SynSyn) ───────────────────

.PHONY: prepare
prepare:
	@echo "Copying assets from SynapseMCP and SynSyn ..."
	mkdir -p synapse_db model corpus
	cp $(SYNAPSE_DB)/vectors.bin    synapse_db/
	cp $(SYNAPSE_DB)/metadata.json  synapse_db/
	cp $(SYNAPSE_MODEL)/all-MiniLM-L6-v2.onnx  model/
	cp $(SYNAPSE_MODEL)/tokenizer.json          model/
	cp $(SYNSYN_DIR)/models/binary_nozip.onnx   model/
	@python -c "\
import json; from pathlib import Path; \
lines = Path('$(SYNSYN_DIR)/corpus/chunks.jsonl').read_text(encoding='utf-8').splitlines(); \
records = [json.loads(l) for l in lines if l.strip()]; \
clean = [{'text': r['text'], 'file': r.get('file',''), \
          'chunk_type': r.get('chunk_type',''), 'source': r.get('source','')} \
         for r in records if r.get('label') == 'clean']; \
Path('corpus/clean_chunks.json').write_text(json.dumps(clean, ensure_ascii=False, indent=2), encoding='utf-8'); \
print(f'clean_chunks.json: {len(clean)} examples')"
	@echo "Assets ready."

# ── ORT library downloads ──────────────────────────────────────────────────────

$(ASSETS_DIR)/onnxruntime_windows_amd64.dll: | $(ASSETS_DIR)
	@echo "Downloading ONNX Runtime $(ORT_VERSION) for windows/amd64 ..."
	wsl bash -c "curl -fsSL '$(ORT_WINDOWS_AMD64_URL)' -o /tmp/ort-win.zip \
	  && unzip -p /tmp/ort-win.zip \
	       'onnxruntime-win-x64-$(ORT_VERSION)/lib/onnxruntime.dll' \
	       > '$(WSL_CURDIR)/$(ASSETS_DIR)/onnxruntime_windows_amd64.dll' \
	  && rm /tmp/ort-win.zip"
	@echo "  -> $@"

$(ASSETS_DIR)/onnxruntime_linux_amd64.so: | $(ASSETS_DIR)
	@echo "Downloading ONNX Runtime $(ORT_VERSION) for linux/amd64 ..."
	wsl bash -c "curl -fsSL '$(ORT_LINUX_AMD64_URL)' \
	  | tar -xz -O --wildcards '*/lib/libonnxruntime.so.$(ORT_VERSION)' \
	  > '$(WSL_CURDIR)/$(ASSETS_DIR)/onnxruntime_linux_amd64.so'"
	@echo "  -> $@"

$(ASSETS_DIR)/onnxruntime_darwin_amd64.dylib: | $(ASSETS_DIR)
	@echo "Downloading ONNX Runtime $(ORT_VERSION) for darwin/amd64 ..."
	wsl bash -c "curl -fsSL '$(ORT_DARWIN_AMD64_URL)' \
	  | tar -xz -O --wildcards '*/lib/libonnxruntime.$(ORT_VERSION).dylib' \
	  > '$(WSL_CURDIR)/$(ASSETS_DIR)/onnxruntime_darwin_amd64.dylib'"
	@echo "  -> $@"

$(ASSETS_DIR)/onnxruntime_darwin_arm64.dylib: | $(ASSETS_DIR)
	@echo "Downloading ONNX Runtime $(ORT_VERSION) for darwin/arm64 ..."
	wsl bash -c "curl -fsSL '$(ORT_DARWIN_ARM64_URL)' \
	  | tar -xz -O --wildcards '*/lib/libonnxruntime.$(ORT_VERSION).dylib' \
	  > '$(WSL_CURDIR)/$(ASSETS_DIR)/onnxruntime_darwin_arm64.dylib'"
	@echo "  -> $@"

# ── Build targets ──────────────────────────────────────────────────────────────

# Dev build: no CGo, no ONNX Runtime — tests the MCP protocol layer only.
.PHONY: build-stub
build-stub: | $(BIN_DIR)
	powershell -NoProfile -Command "$$env:CGO_ENABLED=0; go build -tags noort -ldflags='-s -w' -o $(BIN_DIR)/$(BINARY_NAME)-stub.exe ."
	@echo "Built stub: $(BIN_DIR)/$(BINARY_NAME)-stub.exe"

# Windows amd64 — native (requires CGo + ORT DLL in assets/).
.PHONY: build
build: $(ASSETS_DIR)/onnxruntime_windows_amd64.dll | $(BIN_DIR)
	powershell -NoProfile -Command "$$env:CGO_ENABLED=1; go build -ldflags='-s -w' -o $(BIN_DIR)/$(BINARY_NAME).exe ."
	@echo "Built: $(BIN_DIR)/$(BINARY_NAME).exe"

# Windows amd64 with embedded CodeT5 query generator.
.PHONY: build-full
build-full: $(ASSETS_DIR)/onnxruntime_windows_amd64.dll | $(BIN_DIR)
	powershell -NoProfile -Command "$$env:CGO_ENABLED=1; go build -tags embedquery -ldflags='-s -w' -o $(BIN_DIR)/$(BINARY_NAME)-full.exe ."
	@echo "Built: $(BIN_DIR)/$(BINARY_NAME)-full.exe"

# Linux amd64 — cross-compiled via WSL (same pattern as darwin).
.PHONY: build-linux-amd64
build-linux-amd64: $(ASSETS_DIR)/onnxruntime_linux_amd64.so | $(BIN_DIR)
	@echo "Building Linux amd64 via WSL ..."
	wsl bash "$(WSL_CURDIR)/scripts/wsl_build_linux.sh" build-only
	@echo "Built: $(BIN_DIR)/$(BINARY_NAME)-linux-amd64"

# macOS builds via WSL + Zig cross-compiler (no macOS host needed).
# Zig 0.13.0 is downloaded automatically on first use.

.PHONY: build-darwin-arm64
build-darwin-arm64: $(ASSETS_DIR)/onnxruntime_darwin_arm64.dylib | $(BIN_DIR)
	@echo "Building macOS arm64 via WSL + zig ..."
	wsl bash "$(WSL_CURDIR)/scripts/wsl_build_darwin.sh" arm64

.PHONY: build-darwin-amd64
build-darwin-amd64: $(ASSETS_DIR)/onnxruntime_darwin_amd64.dylib | $(BIN_DIR)
	@echo "Building macOS amd64 via WSL + zig ..."
	wsl bash "$(WSL_CURDIR)/scripts/wsl_build_darwin.sh" amd64

.PHONY: build-all
build-all: build-linux-amd64 build-darwin-arm64 build-darwin-amd64

# ── Distribution packages ──────────────────────────────────────────────────────

DIST_DIR := dist

# Convert Windows path to WSL path using pure Make string functions (no shell).
# e.g. G:/StormMCP  or  G:\StormMCP  ->  /mnt/g/StormMCP
# Single line — continuation lines add whitespace into the expanded value.
_lc = $(subst A,a,$(subst B,b,$(subst C,c,$(subst D,d,$(subst E,e,$(subst F,f,$(subst G,g,$(subst H,h,$(subst I,i,$(subst J,j,$(subst K,k,$(subst L,l,$(subst M,m,$(subst N,n,$(subst O,o,$(subst P,p,$(subst Q,q,$(subst R,r,$(subst S,s,$(subst T,t,$(subst U,u,$(subst V,v,$(subst W,w,$(subst X,x,$(subst Y,y,$(subst Z,z,$(1)))))))))))))))))))))))))))
_CURDIR_FWD  := $(subst \,/,$(CURDIR))
_DRIVE       := $(firstword $(subst :, ,$(_CURDIR_FWD)))
_DRIVE_LC    := $(call _lc,$(_DRIVE))
_CURDIR_REST := $(patsubst $(_DRIVE):%,%,$(_CURDIR_FWD))
WSL_CURDIR   := /mnt/$(_DRIVE_LC)$(_CURDIR_REST)

.PHONY: dist-windows
dist-windows: build build-full
	@echo "Packaging Windows distribution ..."
	@mkdir -p $(DIST_DIR)
	@rm -rf .dist-staging-win
	@mkdir -p .dist-staging-win/storm-mcp-win
	cp $(BIN_DIR)/$(BINARY_NAME).exe      .dist-staging-win/storm-mcp-win/
	cp $(BIN_DIR)/$(BINARY_NAME)-full.exe .dist-staging-win/storm-mcp-win/ 2>/dev/null || true
	powershell -NoProfile -Command \
	  "Compress-Archive -Force -Path '.dist-staging-win\storm-mcp-win' \
	  -DestinationPath '$(DIST_DIR)\storm-mcp-win.zip'"
	@rm -rf .dist-staging-win
	@echo "  -> $(DIST_DIR)/storm-mcp-win.zip"

.PHONY: dist-linux
dist-linux:
	@echo "Building and packaging Linux distribution via WSL ..."
	@mkdir -p $(DIST_DIR)
	wsl bash "$(WSL_CURDIR)/scripts/wsl_build_linux.sh"

.PHONY: dist-darwin-arm64
dist-darwin-arm64: $(ASSETS_DIR)/onnxruntime_darwin_arm64.dylib
	@echo "Building and packaging macOS arm64 distribution via WSL ..."
	@mkdir -p $(DIST_DIR)
	wsl bash "$(WSL_CURDIR)/scripts/wsl_build_darwin.sh" arm64

.PHONY: dist-darwin-amd64
dist-darwin-amd64: $(ASSETS_DIR)/onnxruntime_darwin_amd64.dylib
	@echo "Building and packaging macOS amd64 distribution via WSL ..."
	@mkdir -p $(DIST_DIR)
	wsl bash "$(WSL_CURDIR)/scripts/wsl_build_darwin.sh" amd64

.PHONY: dist-darwin
dist-darwin: dist-darwin-arm64 dist-darwin-amd64

.PHONY: dist
dist: dist-windows dist-linux dist-darwin
	@echo "Distribution packages ready in $(DIST_DIR)/"
	@ls -lh $(DIST_DIR)/

# ── Docker convenience (Linux amd64 without WSL) ──────────────────────────────

.PHONY: docker-build-linux
docker-build-linux:
	docker run --rm \
	  -v "$(CURDIR):/workspace" \
	  -w /workspace \
	  golang:1.22-bookworm \
	  bash -c " \
	    apt-get update -qq && apt-get install -y -qq gcc curl tar && \
	    make build-linux-amd64 \
	  "

# ── Utility ────────────────────────────────────────────────────────────────────

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: vet
vet:
	go vet ./...

.PHONY: clean
clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)

.PHONY: help
help:
	@echo "Usage:"
	@echo "  make prepare             Copy assets from SynapseMCP and SynSyn"
	@echo "  make build-stub          Dev build (no CGo, stub ONNX)"
	@echo "  make build               Windows native build (needs C compiler + ORT)"
	@echo "  make build-full          Windows native build with embedded CodeT5"
	@echo "  make build-linux-amd64   Linux amd64 release (run from WSL)"
	@echo "  make build-darwin-amd64  macOS Intel release (via WSL + zig)"
	@echo "  make build-darwin-arm64  macOS ARM64 release (via WSL + zig)"
	@echo "  make build-all           All three release platforms (from WSL)"
	@echo "  make docker-build-linux  Linux amd64 via Docker (no WSL needed)"
	@echo "  make dist-windows        Package storm-mcp-win.zip"
	@echo "  make dist-linux          Package storm-mcp-linux.tar.gz  (via WSL)"
	@echo "  make dist-darwin-arm64   Package storm-mcp-darwin-arm64.tar.gz (via WSL + zig)"
	@echo "  make dist-darwin-amd64   Package storm-mcp-darwin-amd64.tar.gz (via WSL + zig)"
	@echo "  make dist                All four distribution packages"
	@echo "  make clean               Remove bin/, dist/"
