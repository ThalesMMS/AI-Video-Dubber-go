#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

require_contains() {
  local file="$1"
  local expected="$2"
  if ! grep -Fq "$expected" "$file"; then
    echo "$file is missing prerequisite check fragment: $expected" >&2
    exit 1
  fi
}

require_contains scripts/build.sh "go was not found in PATH"
require_contains scripts/run.sh "go was not found in PATH"
require_contains scripts/build.ps1 "Get-Command go"
require_contains scripts/build.ps1 "go was not found in PATH"
require_contains scripts/run.ps1 "Get-Command go"
require_contains scripts/run.ps1 "go was not found in PATH"
