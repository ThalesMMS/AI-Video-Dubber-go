#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

MODE="${1:-all}"
ARCH="${ARCH:-$(uname -m)}"
DIST_DIR="${DIST_DIR:-$ROOT/dist}"
CACHE_DIR="${PACKAGE_CACHE_DIR:-$ROOT/.cache/package-macos}"
BUILD_DIR="$DIST_DIR/package-macos-$ARCH"
APP_BASENAME="${APP_BASENAME:-AI-Video-Dubber}"
APP_DIR="$DIST_DIR/$APP_BASENAME.app"
CLI_NAME="${CLI_NAME:-AI-Video-Dubber-cli-darwin-$ARCH}"
CLI_DIR="$DIST_DIR/$CLI_NAME"
PYTHON_VERSION_PREFIX="${PYTHON_VERSION_PREFIX:-cpython-3.12}"
PYTHON_REPO_API="${PYTHON_REPO_API:-https://api.github.com/repos/astral-sh/python-build-standalone/releases/latest}"
FFMPEG_URL="${FFMPEG_URL:-https://evermeet.cx/ffmpeg/getrelease/zip}"
FFPROBE_URL="${FFPROBE_URL:-https://evermeet.cx/ffmpeg/getrelease/ffprobe/zip}"

case "$MODE" in
  all|app|cli) ;;
  *)
    echo "usage: $0 [all|app|cli]" >&2
    exit 2
    ;;
esac

case "$ARCH" in
  arm64|aarch64)
    GOARCH="arm64"
    PYTHON_TARGET="aarch64-apple-darwin"
    ;;
  x86_64|amd64)
    GOARCH="amd64"
    PYTHON_TARGET="x86_64-apple-darwin"
    ;;
  *)
    echo "Unsupported ARCH=$ARCH. Use arm64 or x86_64." >&2
    exit 2
    ;;
esac

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "$1 was not found in PATH" >&2
    exit 1
  }
}

host_python() {
  if command -v python3 >/dev/null 2>&1; then
    command -v python3
  elif [[ -x /usr/bin/python3 ]]; then
    printf '%s\n' /usr/bin/python3
  else
    echo "python3 is required to parse GitHub release metadata. Set PYTHON_STANDALONE_URL to skip discovery." >&2
    exit 1
  fi
}

toml_value() {
  local key="$1"
  awk -F= -v key="$key" '
    $1 ~ key {
      value=$2
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      gsub(/^"|"$/, "", value)
      print value
      exit
    }
  ' FyneApp.toml
}

download() {
  local url="$1"
  local output="$2"
  if [[ -s "$output" ]]; then
    return
  fi
  echo "Downloading $url" >&2
  curl -fL --retry 3 --retry-delay 2 -o "$output" "$url"
}

python_standalone_url() {
  if [[ -n "${PYTHON_STANDALONE_URL:-}" ]]; then
    printf '%s\n' "$PYTHON_STANDALONE_URL"
    return
  fi
  local metadata="$CACHE_DIR/python-build-standalone-latest.json"
  mkdir -p "$CACHE_DIR"
  download "$PYTHON_REPO_API" "$metadata"
  "$(host_python)" - "$metadata" "$PYTHON_VERSION_PREFIX" "$PYTHON_TARGET" <<'PY'
import json
import sys

metadata_path, version_prefix, target = sys.argv[1:4]
with open(metadata_path, "r", encoding="utf-8") as handle:
    release = json.load(handle)

matches = []
for asset in release.get("assets", []):
    name = asset.get("name", "")
    url = asset.get("browser_download_url", "")
    if (
        name.startswith(version_prefix)
        and target in name
        and name.endswith("-install_only.tar.gz")
        and url
    ):
        matches.append((name, url))

if not matches:
    raise SystemExit(
        f"No {version_prefix} install_only asset for {target} in {release.get('html_url', 'latest release')}"
    )

matches.sort()
print(matches[-1][1])
PY
}

extract_python() {
  local python_dir="$BUILD_DIR/python"
  if [[ -x "$python_dir/bin/python3" ]]; then
    printf '%s\n' "$python_dir"
    return
  fi
  local url archive
  url="$(python_standalone_url)"
  archive="$CACHE_DIR/$(basename "${url%%\?*}")"
  mkdir -p "$CACHE_DIR" "$BUILD_DIR"
  download "$url" "$archive"
  rm -rf "$python_dir" "$BUILD_DIR/python-extract"
  mkdir -p "$BUILD_DIR/python-extract"
  tar -xzf "$archive" -C "$BUILD_DIR/python-extract"
  if [[ ! -x "$BUILD_DIR/python-extract/python/bin/python3" ]]; then
    echo "Python archive did not contain python/bin/python3" >&2
    exit 1
  fi
  mv "$BUILD_DIR/python-extract/python" "$python_dir"
  rm -rf "$BUILD_DIR/python-extract"
  printf '%s\n' "$python_dir"
}

install_python_packages() {
  local python_dir="$1"
  "$python_dir/bin/python3" -m pip install --upgrade pip wheel setuptools
  "$python_dir/bin/python3" -m pip install --upgrade openai-whisper piper-tts
  "$python_dir/bin/python3" -c 'import whisper, piper'
  "$python_dir/bin/python3" -m piper --help >/dev/null
}

copy_or_download_binary() {
  local name="$1"
  local local_path="$2"
  local url="$3"
  local output_dir="$4"
  local output="$output_dir/$name"
  mkdir -p "$output_dir"
  if [[ -n "$local_path" ]]; then
    cp "$local_path" "$output"
  else
    local archive="$CACHE_DIR/$name.zip"
    local extract_dir="$BUILD_DIR/$name-extract"
    download "$url" "$archive"
    rm -rf "$extract_dir"
    mkdir -p "$extract_dir"
    ditto -x -k "$archive" "$extract_dir"
    local found
    found="$(find "$extract_dir" -type f -name "$name" -perm -111 -print -quit)"
    if [[ -z "$found" ]]; then
      echo "Could not find executable $name in $archive" >&2
      exit 1
    fi
    cp "$found" "$output"
    rm -rf "$extract_dir"
  fi
  chmod 0755 "$output"
}

prepare_ffmpeg() {
  local ffmpeg_dir="$BUILD_DIR/ffmpeg"
  if [[ -x "$ffmpeg_dir/ffmpeg" && -x "$ffmpeg_dir/ffprobe" ]]; then
    printf '%s\n' "$ffmpeg_dir"
    return
  fi
  if [[ "$GOARCH" == "arm64" && -z "${FFMPEG_BIN:-}" && -z "${FFPROBE_BIN:-}" && "$FFMPEG_URL" == *evermeet.cx* ]]; then
    echo "Warning: default evermeet.cx FFmpeg downloads may be Intel-only; set FFMPEG_BIN and FFPROBE_BIN for native arm64 distribution." >&2
  fi
  copy_or_download_binary ffmpeg "${FFMPEG_BIN:-}" "$FFMPEG_URL" "$ffmpeg_dir"
  copy_or_download_binary ffprobe "${FFPROBE_BIN:-}" "$FFPROBE_URL" "$ffmpeg_dir"
  "$ffmpeg_dir/ffmpeg" -version >/dev/null
  "$ffmpeg_dir/ffprobe" -version >/dev/null
  printf '%s\n' "$ffmpeg_dir"
}

copy_runtime() {
  local resources_dir="$1"
  local python_dir="$2"
  local ffmpeg_dir="$3"
  rm -rf "$resources_dir/python" "$resources_dir/ffmpeg"
  mkdir -p "$resources_dir"
  ditto "$python_dir" "$resources_dir/python"
  ditto "$ffmpeg_dir" "$resources_dir/ffmpeg"
}

write_info_plist() {
  local plist="$1"
  local app_name bundle_id version build
  app_name="${APP_DISPLAY_NAME:-$(toml_value Name)}"
  bundle_id="${BUNDLE_ID:-$(toml_value ID)}"
  version="${APP_VERSION:-$(toml_value Version)}"
  build="${APP_BUILD:-$(toml_value Build)}"
  app_name="${app_name:-AI Video Dubber}"
  bundle_id="${bundle_id:-io.github.ai-video-dubber}"
  version="${version:-1.0.0}"
  build="${build:-1}"
  cat >"$plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key>
  <string>en</string>
  <key>CFBundleDisplayName</key>
  <string>$app_name</string>
  <key>CFBundleExecutable</key>
  <string>ai-video-dubber</string>
  <key>CFBundleIconFile</key>
  <string>icon.png</string>
  <key>CFBundleIdentifier</key>
  <string>$bundle_id</string>
  <key>CFBundleInfoDictionaryVersion</key>
  <string>6.0</string>
  <key>CFBundleName</key>
  <string>$app_name</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>$version</string>
  <key>CFBundleVersion</key>
  <string>$build</string>
  <key>LSMinimumSystemVersion</key>
  <string>11.0</string>
  <key>NSHighResolutionCapable</key>
  <true/>
</dict>
</plist>
PLIST
}

build_binaries() {
  mkdir -p "$BUILD_DIR/bin"
  GOOS=darwin GOARCH="$GOARCH" CGO_ENABLED="${CGO_ENABLED:-1}" go build -trimpath -o "$BUILD_DIR/bin/ai-video-dubber" ./cmd/ai-video-dubber
  GOOS=darwin GOARCH="$GOARCH" CGO_ENABLED="${CGO_ENABLED:-1}" go build -trimpath -o "$BUILD_DIR/bin/ai-video-dubber-cli" ./cmd/ai-video-dubber-cli
}

build_app() {
  local python_dir="$1"
  local ffmpeg_dir="$2"
  rm -rf "$APP_DIR"
  mkdir -p "$APP_DIR/Contents/MacOS" "$APP_DIR/Contents/Resources"
  cp "$BUILD_DIR/bin/ai-video-dubber" "$APP_DIR/Contents/MacOS/ai-video-dubber"
  cp "$BUILD_DIR/bin/ai-video-dubber-cli" "$APP_DIR/Contents/MacOS/ai-video-dubber-cli"
  cp assets/icon.png "$APP_DIR/Contents/Resources/icon.png"
  write_info_plist "$APP_DIR/Contents/Info.plist"
  copy_runtime "$APP_DIR/Contents/Resources" "$python_dir" "$ffmpeg_dir"
  if [[ -n "${CODESIGN_IDENTITY:-}" ]]; then
    codesign --force --deep --options runtime --sign "$CODESIGN_IDENTITY" "$APP_DIR"
  elif [[ "${ADHOC_CODESIGN:-1}" != "0" ]] && command -v codesign >/dev/null 2>&1; then
    codesign --force --deep --sign - "$APP_DIR" || true
  fi
  if [[ -n "${NOTARYTOOL_PROFILE:-}" ]]; then
    xcrun notarytool submit "$APP_DIR" --keychain-profile "$NOTARYTOOL_PROFILE" --wait
  fi
  echo "App bundle: $APP_DIR"
}

build_cli_tarball() {
  local python_dir="$1"
  local ffmpeg_dir="$2"
  local tarball="$DIST_DIR/$CLI_NAME.tar.gz"
  rm -rf "$CLI_DIR"
  mkdir -p "$CLI_DIR"
  cp "$BUILD_DIR/bin/ai-video-dubber-cli" "$CLI_DIR/ai-video-dubber-cli"
  copy_runtime "$CLI_DIR" "$python_dir" "$ffmpeg_dir"
  rm -f "$tarball"
  tar -czf "$tarball" -C "$DIST_DIR" "$CLI_NAME"
  echo "CLI tarball: $tarball"
}

need curl
need ditto
need find
need go
need tar

mkdir -p "$DIST_DIR" "$CACHE_DIR" "$BUILD_DIR"
python_dir="$(extract_python)"
install_python_packages "$python_dir"
ffmpeg_dir="$(prepare_ffmpeg)"
build_binaries

case "$MODE" in
  all)
    build_app "$python_dir" "$ffmpeg_dir"
    build_cli_tarball "$python_dir" "$ffmpeg_dir"
    ;;
  app)
    build_app "$python_dir" "$ffmpeg_dir"
    ;;
  cli)
    build_cli_tarball "$python_dir" "$ffmpeg_dir"
    ;;
esac
