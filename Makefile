# rfog: build, test, cross-compile.
export CGO_ENABLED=0

BIN     := rfog
PKG     := ./cmd/rfog
DIST    := dist
TARGETS := linux/386 linux/amd64 linux/arm linux/arm64 linux/riscv64 \
           darwin/amd64 darwin/arm64 freebsd/amd64 windows/amd64

.PHONY: all build test vet lint cross wasm web clean golden

all: vet test build

build:
	go build -trimpath -ldflags="-s -w" -o $(BIN) $(PKG)

test:
	go test ./...

vet:
	go vet ./...

lint: vet
	@command -v staticcheck >/dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed; skipped"

# Regenerate golden replays after an intentional rules change.
golden:
	go test ./engine -run TestGoldenReplays -update

# Static binaries for every supported platform.
cross:
	@mkdir -p $(DIST)
	@for t in $(TARGETS); do \
	  os=$${t%/*}; arch=$${t#*/}; ext=""; \
	  [ "$$os" = "windows" ] && ext=".exe"; \
	  echo "  $$os/$$arch"; \
	  GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags="-s -w" \
	    -o $(DIST)/$(BIN)-$$os-$$arch$$ext $(PKG) || exit 1; \
	done

# The engine must compile for the browser too.
wasm:
	GOOS=js GOARCH=wasm go build ./engine/... ./data/...

# The engine for the graphical web client (web/index.html). Rebuild after
# any engine, data or webplay change, then rebuild the server that embeds it.
web:
	GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o web/engine.wasm ./cmd/wasm
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" web/wasm_exec.js

clean:
	rm -rf $(BIN) $(DIST)
