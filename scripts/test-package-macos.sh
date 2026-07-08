#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

output="$(ARCH=arm64 "$ROOT/scripts/package-macos.sh" versions)"

require_line() {
  local expected="$1"
  if ! grep -Fq "$expected" <<<"$output"; then
    echo "package-macos versions output missing: $expected" >&2
    echo "$output" >&2
    exit 1
  fi
}

require_line "Python standalone URL: https://github.com/astral-sh/python-build-standalone/releases/download/20260623/cpython-3.12.13%2B20260623-aarch64-apple-darwin-install_only.tar.gz"
require_line "FFmpeg URL: https://evermeet.cx/ffmpeg/ffmpeg-8.1.2.zip"
require_line "FFprobe URL: https://evermeet.cx/ffmpeg/ffprobe-8.1.2.zip"
require_line "openai-whisper: 20250625"
require_line "piper-tts: 1.4.2"

if grep -Eq "releases/latest|getrelease" <<<"$output"; then
  echo "package-macos versions output still references rolling endpoints:" >&2
  echo "$output" >&2
  exit 1
fi

offline_output="$(ARCH=arm64 PIP_NO_INDEX=1 PIP_FIND_LINKS=/opt/ai-video-dubber/wheels "$ROOT/scripts/package-macos.sh" versions)"
if ! grep -Fq "pip no index: 1" <<<"$offline_output"; then
  echo "package-macos versions output missing offline pip mode:" >&2
  echo "$offline_output" >&2
  exit 1
fi
if ! grep -Fq "pip find links: /opt/ai-video-dubber/wheels" <<<"$offline_output"; then
  echo "package-macos versions output missing wheelhouse path:" >&2
  echo "$offline_output" >&2
  exit 1
fi
