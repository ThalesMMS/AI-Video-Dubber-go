# Self-Contained Packaging Plan - AI-Video-Dubber-go

## Goal

Ship `AI-Video-Dubber-go` as a macOS `.app` that opens with a double-click and does not depend on Python, FFmpeg, environment variables, or a terminal. The bundle must contain the Go binary, the Fyne interface, the Python runtime with Whisper and Piper, and the `ffmpeg` and `ffprobe` binaries.

## Implementation Status

- Embedded runtime resolution is implemented in `internal/config`, with support for `.app` bundles and the headless CLI tarball.
- `executil.Runner` now injects embedded directories into `PATH` and redirects `ffmpeg` and `ffprobe` calls to the bundle binaries.
- The runtime creates a short cache link for `piper/espeak-ng-data` and sets `ESPEAK_DATA_PATH`, avoiding the `espeakbridge` failure caused by long paths inside the `.app`.
- `internal/environment` skips `.venv` creation when embedded Python is in use and validates that Whisper and Piper can be imported.
- `scripts/package-macos.sh` generates `dist/AI-Video-Dubber.app` and `dist/AI-Video-Dubber-cli-darwin-<arch>.tar.gz`.
- `make package-macos` runs the full packaging flow; `make package-cli` generates only the CLI tarball.
- The script pins its default Python standalone, FFmpeg/FFprobe, Whisper, and Piper inputs and writes them to `VERSIONS.txt` in each generated artifact.
- The script accepts `FFMPEG_BIN` and `FFPROBE_BIN` for native arm64 builds when the default downloaded binaries are not suitable.
- Distribution readiness depends on the clean-machine checklist below; do not ship a build until those checks pass for the target architecture.

## Clean-Machine Release Checklist

Run this on a fresh macOS account, VM, or machine that does not have Homebrew
Python or FFmpeg in `PATH`:

1. Build the `.app` and CLI tarball from a clean checkout.
2. Start the `.app` by double-clicking it, not from a terminal.
3. Confirm the GUI can start a run with `PATH` limited to system directories
   such as `/usr/bin:/bin:/usr/sbin:/sbin`.
4. Process a short sample video in subtitle mode and dub mode.
5. Verify the packaged CLI works from the tarball with the same sample video.
6. Inspect `VERSIONS.txt` in both artifacts and archive it with release notes.
7. Record bundle size, CLI tarball size, first-run setup time, Whisper model
   download time, Piper voice download time, and final cache sizes.
8. Confirm logs show bundled Python/FFmpeg resolution and no dependency on
   developer-machine paths.
9. Repeat after signing/notarization if those steps are enabled.

## Current State

- The application core is Go/Fyne.
- The CLI and GUI share the `pipeline` layer.
- The app does not embed Python scripts through `go:embed`; instead, it automatically creates a `.venv` on first run and installs `openai-whisper` and `piper-tts`.
- It depends on `ffmpeg` and `ffprobe` installed in `PATH`.
- The `PYTHON_BIN`, `VENV_DIR`, and `AI_VIDEO_DUBBER_HOME` environment variables control the runtime.
- There are two binaries: `ai-video-dubber` (GUI + CLI) and `ai-video-dubber-cli` (headless).
- The `fyne package` target exists in the `Makefile`, but it only generates the Go binary with metadata; it does not include embedded Python or FFmpeg.

## Architecture Decisions

### 1. Relocatable Python

Use **Python standalone** from the `indygreg/python-build-standalone` project as the base. This distribution is already compiled to be relocatable on macOS.

Packages to install into embedded Python:

- `openai-whisper` at the version pinned in `scripts/package-macos.sh`
- `piper-tts` at the version pinned in `scripts/package-macos.sh`
- Updated `wheel`, `setuptools`, and `pip`

Install directly into the relocatable Python, without an internal `venv`. This simplifies startup and avoids creating a virtual environment on first run.

### 2. Embedded FFmpeg

Download static `ffmpeg` and `ffprobe` builds for macOS, for example through `evermeet.cx` or by copying Homebrew binaries into the bundle. The binaries live in `Contents/Resources/ffmpeg/`.

### 3. `.app` Structure

```text
AI-Video-Dubber.app/
├── Contents/
│   ├── Info.plist
│   ├── MacOS/
│   │   └── ai-video-dubber
│   └── Resources/
│       ├── icon.png
│       ├── python/
│       │   ├── bin/python3
│       │   └── lib/python3.12/site-packages/
│       │       ├── whisper/
│       │       ├── piper/
│       │       └── ...
│       └── ffmpeg/
│           ├── ffmpeg
│           └── ffprobe
```

### 4. Runtime Resource Discovery

Add a Go helper that starts from `os.Executable()`, detects whether the binary is inside a `.app`, and resolves relative paths for:

- `Contents/Resources/python/bin/python3`
- `Contents/Resources/ffmpeg/ffmpeg`
- `Contents/Resources/ffmpeg/ffprobe`

Resolution order:

1. `PYTHON_BIN`, `VENV_DIR`, and `ffmpeg` in `PATH` (development mode).
2. Bundled resources in `Contents/Resources/`.
3. Fallback to the system `PATH`.

### 5. Headless CLI

The `ai-video-dubber-cli` binary can also be packaged as a self-contained executable, but the main focus is the GUI `.app`. For the CLI, generate a tarball with the Go binary, Python, and FFmpeg side by side, using the same resource-discovery logic.

### 6. Model Cache

Whisper models and Piper voices continue to download on first run and are stored in `~/Library/Caches/AI-Video-Dubber` or the equivalent cache directory. This keeps the bundle smaller and allows model updates without rebuilding the app.

## Required Changes

### Go Code

- `internal/config/config.go`
  - Add a helper to resolve embedded resource paths.
  - Preserve existing environment-variable reads.
  - Adjust `VenvDir` so it is not created when bundled Python is available.

- `internal/environment/setup.go`
  - Skip `.venv` creation when `pythonExe` is already the embedded Python.
  - Ensure `whisper` and `piper` can be imported from bundled Python.

- `internal/audio/ffmpeg.go`
  - Use embedded `ffmpeg` and `ffprobe` when detected.
  - Keep the `PATH` fallback.

- `internal/executil/runner.go`
  - Ensure subprocesses can see embedded Python and FFmpeg.

### Build And Scripts

- Create `scripts/package-macos.sh`.
  - Download Python standalone for the target architecture.
  - Install `openai-whisper` and `piper-tts`.
  - Download static `ffmpeg` and `ffprobe`.
  - Build `cmd/ai-video-dubber` and `cmd/ai-video-dubber-cli`.
  - Assemble the `.app` structure.
  - Generate `Info.plist`.
  - Copy `assets/icon.png`.
  - Optionally sign with `codesign` and notarize with `notarytool`.

- Update `Makefile`.
  - Add a `package-macos` target.
  - Add a `package-cli` target for the self-contained CLI tarball.

## Execution Phases

### Phase 1 - Code Analysis

- Map every place where `PYTHON_BIN`, `VENV_DIR`, `ffmpeg`, `ffprobe`, and `python` are used.
- Confirm exact Whisper and Piper dependencies.
- Verify whether the headless CLI indirectly depends on Fyne.

### Phase 2 - Relocatable Python Prototype

- Download `python-build-standalone` for macOS.
- Install `openai-whisper` and `piper-tts`.
- Manually test transcription and synthesis.
- Measure Python size after installation.

### Phase 3 - Relocatable FFmpeg Prototype

- Download static `ffmpeg` and `ffprobe` for macOS.
- Test audio extraction and remuxing with the binaries in an arbitrary directory.
- Confirm they do not depend on external dynamic libraries.

### Phase 4 - Go Resource Detection

- Implement embedded path resolution.
- Adjust `config.go`, `environment/setup.go`, and `audio/ffmpeg.go`.
- Add unit tests for these scenarios:
  - inside the `.app`
  - outside the `.app`
  - with environment variables set

### Phase 5 - Packaging Script

- Create `scripts/package-macos.sh`.
- Support arm64 and x86_64 architectures.
- Generate `Info.plist` with the appropriate bundle ID from `FyneApp.toml`.
- Optionally package the CLI in a separate tarball.

### Phase 6 - Tests

- Run the `.app` on a machine without Python or FFmpeg in `PATH`.
- Process a sample video end to end.
- Confirm all intermediate files are generated.
- Test subprocess cancellation.
- Test the packaged CLI.

### Phase 7 - Documentation

- Update `README.md` with `.app` build instructions.
- Record bundle size, first-run time, and notarization limitations.
- Document how to generate only the headless CLI.

## Expected Bundle Size

- Python standalone with Whisper and Piper: ~250-400 MB.
- Static FFmpeg: ~50-100 MB.
- Go binary: ~20-40 MB.
- Estimated total: ~350-550 MB.

## Risks And Limitations

- **Notarization**: without an Apple Developer ID, the user needs to allow the app in `Security & Privacy`.
- **First run**: it can still be slow because the Whisper model and Piper voice must be downloaded.
- **Size**: Whisper and models significantly increase the bundle if included.
- **Static FFmpeg**: it may lack support for some codecs; validate with sample videos.
- **Whisper**: the `large-v3` model may not fit on low-RAM machines; consider a smaller default model for the bundle.
- **Updates**: changes to Python packages or FFmpeg require a new build.

## Recommended Next Steps

1. Start with Phase 2, the relocatable Python prototype with Whisper and Piper.
2. In parallel, run Phase 3, the static FFmpeg prototype.
3. After both are validated, apply Phase 4, Go detection, and Phase 5, the packaging script.
