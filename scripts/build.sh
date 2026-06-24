#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

command -v go >/dev/null 2>&1 || { echo "go was not found in PATH" >&2; exit 1; }
mkdir -p bin

go mod download
go build -trimpath -o bin/ai-video-dubber ./cmd/ai-video-dubber
go build -trimpath -o bin/ai-video-dubber-cli ./cmd/ai-video-dubber-cli

echo "Build complete:"
echo "  $ROOT/bin/ai-video-dubber"
echo "  $ROOT/bin/ai-video-dubber-cli"
