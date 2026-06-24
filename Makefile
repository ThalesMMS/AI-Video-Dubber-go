GO ?= go
BIN_DIR ?= bin
DIST_DIR ?= dist
APP := $(BIN_DIR)/ai-video-dubber
CLI := $(BIN_DIR)/ai-video-dubber-cli

.PHONY: all deps fmt fmt-check test vet check build build-cli run run-cli package package-macos package-cli clean

all: check build

deps:
	$(GO) mod download

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

fmt-check:
	@files="$$(gofmt -l $$(find . -name '*.go' -not -path './vendor/*'))"; \
	if [ -n "$$files" ]; then echo "Files without gofmt:"; echo "$$files"; exit 1; fi

test:
	$(GO) test -tags ci ./...

vet:
	$(GO) vet -tags ci ./...

check: fmt-check test vet

build: deps
	mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -o $(APP) ./cmd/ai-video-dubber
	$(GO) build -trimpath -o $(CLI) ./cmd/ai-video-dubber-cli

build-cli: deps
	mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -o $(CLI) ./cmd/ai-video-dubber-cli

run:
	$(GO) run ./cmd/ai-video-dubber

run-cli:
	$(GO) run ./cmd/ai-video-dubber-cli help

package:
	@command -v fyne >/dev/null || { echo "Install the tool: go install fyne.io/tools/cmd/fyne@latest"; exit 1; }
	fyne package

package-macos:
	./scripts/package-macos.sh all

package-cli:
	./scripts/package-macos.sh cli

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)
