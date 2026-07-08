#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

unformatted="$(gofmt -l $(find . -name '*.go' -not -path './vendor/*'))"
if [[ -n "$unformatted" ]]; then
  echo "Files without gofmt:" >&2
  echo "$unformatted" >&2
  exit 1
fi

go test -tags ci ./...
go vet -tags ci ./...
bash scripts/test-package-macos.sh
