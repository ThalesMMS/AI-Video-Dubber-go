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
PYTHON_STANDALONE_RELEASE="${PYTHON_STANDALONE_RELEASE:-20260623}"
PYTHON_STANDALONE_VERSION="${PYTHON_STANDALONE_VERSION:-3.12.13+20260623}"
FFMPEG_VERSION="${FFMPEG_VERSION:-8.1.2}"
FFPROBE_VERSION="${FFPROBE_VERSION:-$FFMPEG_VERSION}"
FFMPEG_URL="${FFMPEG_URL:-https://evermeet.cx/ffmpeg/ffmpeg-$FFMPEG_VERSION.zip}"
FFPROBE_URL="${FFPROBE_URL:-https://evermeet.cx/ffmpeg/ffprobe-$FFPROBE_VERSION.zip}"
PIP_INDEX_URL="${PIP_INDEX_URL:-https://pypi.org/simple}"
OPENAI_WHISPER_VERSION="${OPENAI_WHISPER_VERSION:-20250625}"
PIPER_TTS_VERSION="${PIPER_TTS_VERSION:-1.4.2}"

case "$MODE" in
  all|app|cli|versions) ;;
  *)
    echo "usage: $0 [all|app|cli|versions]" >&2
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

PYTHON_STANDALONE_URL="${PYTHON_STANDALONE_URL:-https://github.com/astral-sh/python-build-standalone/releases/download/$PYTHON_STANDALONE_RELEASE/cpython-${PYTHON_STANDALONE_VERSION//+/%2B}-$PYTHON_TARGET-install_only.tar.gz}"

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "$1 was not found in PATH" >&2
    exit 1
  }
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
  printf '%s\n' "$PYTHON_STANDALONE_URL"
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
  "$python_dir/bin/python3" -m pip install --index-url "$PIP_INDEX_URL" --upgrade pip wheel setuptools
  "$python_dir/bin/python3" -m pip install --index-url "$PIP_INDEX_URL" --upgrade "openai-whisper==$OPENAI_WHISPER_VERSION" "piper-tts==$PIPER_TTS_VERSION"
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

versions_manifest() {
  cat <<EOF
AI Video Dubber macOS package inputs
Architecture: $ARCH
Go architecture: $GOARCH
Python standalone version: $PYTHON_STANDALONE_VERSION
Python standalone release: $PYTHON_STANDALONE_RELEASE
Python standalone URL: $PYTHON_STANDALONE_URL
FFmpeg version: $FFMPEG_VERSION
FFmpeg URL: ${FFMPEG_BIN:-$FFMPEG_URL}
FFprobe version: $FFPROBE_VERSION
FFprobe URL: ${FFPROBE_BIN:-$FFPROBE_URL}
pip index URL: $PIP_INDEX_URL
openai-whisper: $OPENAI_WHISPER_VERSION
piper-tts: $PIPER_TTS_VERSION
EOF
}

write_versions_manifest() {
  local output="$1"
  mkdir -p "$(dirname "$output")"
  versions_manifest >"$output"
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
  write_versions_manifest "$APP_DIR/Contents/Resources/VERSIONS.txt"
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
  write_versions_manifest "$CLI_DIR/VERSIONS.txt"
  rm -f "$tarball"
  tar -czf "$tarball" -C "$DIST_DIR" "$CLI_NAME"
  echo "CLI tarball: $tarball"
}

if [[ "$MODE" == "versions" ]]; then
  versions_manifest
  exit 0
fi

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
