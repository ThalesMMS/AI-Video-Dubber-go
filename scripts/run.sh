#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

command -v go >/dev/null 2>&1 || { echo "go was not found in PATH" >&2; exit 1; }

exec go run ./cmd/ai-video-dubber "$@"
