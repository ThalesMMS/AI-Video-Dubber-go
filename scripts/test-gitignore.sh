#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if ! git check-ignore -q .venv/pyvenv.cfg; then
  echo ".gitignore must exclude the default .venv directory" >&2
  exit 1
fi
