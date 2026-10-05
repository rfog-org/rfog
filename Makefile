# rfog: build, test, cross-compile.
export CGO_ENABLED=0

BIN     := rfog
PKG     := ./cmd/rfog
DIST    := dist
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)
TARGETS := linux/386 linux/amd64 linux/arm linux/arm64 linux/riscv64 \
           darwin/amd64 darwin/arm64 freebsd/amd64 freebsd/arm64 \
           windows/amd64 windows/arm64

.PHONY: all build test vet lint cross package wasm web clean golden

all: vet test build

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN) $(PKG)

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
	  GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags="$(LDFLAGS)" \
	    -o $(DIST)/$(BIN)-$$os-$$arch$$ext $(PKG) || exit 1; \
	done

# Release archives from the cross builds: one per platform, each holding
# the binary (named rfog, or rfog.exe), LICENSE and README.md, as .tar.gz
# (.zip for Windows), plus SHA256SUMS. make VERSION=v0.1.0 package
package: cross
	@cd $(DIST) && for f in $(BIN)-*; do \
	  case "$$f" in *.tar.gz|*.zip) continue;; esac; \
	  plat=$${f#$(BIN)-}; plat=$${plat%.exe}; name=$(BIN)-$(VERSION)-$$plat; \
	  rm -rf "$$name" && mkdir "$$name"; \
	  cp ../LICENSE ../README.md "$$name"/; \
	  case "$$f" in \
	    *.exe) cp "$$f" "$$name/$(BIN).exe"; rm -f "$$name.zip"; zip -qr "$$name.zip" "$$name";; \
	    *) cp "$$f" "$$name/$(BIN)"; tar -czf "$$name.tar.gz" "$$name";; \
	  esac; \
	  rm -rf "$$name" "$$f"; echo "  $$name"; \
	done; \
	shasum -a 256 *.tar.gz *.zip > SHA256SUMS

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
