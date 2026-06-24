# AI Video Dubber - Go/Fyne

Functional clone of `AI-Video-Dubber-py`, reimplemented with **Go** for orchestration, **Fyne** for the graphical interface, and the same AI components used by the original project:

**Video -> audio -> Whisper -> translation -> synchronized Piper TTS -> dubbed video**

The application provides a dark desktop interface similar to the Python version, a full CLI, and standalone commands for each pipeline stage.

## Features

- Fyne graphical interface with video selection, API configuration, language selection, six-step progress, logs, and cancellation.
- Audio extraction and remuxing with FFmpeg.
- Local transcription with OpenAI Whisper.
- Translation through any OpenAI-compatible API (`/v1/models` and `/v1/chat/completions`).
- Automatic model detection when the **Model** field is empty.
- Local synthesis with Piper TTS and automatic download of the required voice.
- Subtitle grouping to improve prosody.
- `length_scale` tuning, bounded `atempo` correction, padding, and trimming per timing window.
- `.srt` and `.segments.txt` input for synthesis.
- Optional JSON report with timing parameters for each group.
- Reuse of CLI intermediate files and regeneration with `--force`.
- Cancellation that terminates the subprocess tree on Unix.
- Atomic writes for the main artifacts to reduce partially written files.
- Separate CLI executable without a graphical runtime dependency.

> The application is written in Go, but Whisper and Piper still run in an automatically managed Python environment. This preserves compatibility and quality from the reference project without maintaining custom Python scripts in the Go project.

## Requirements

- Go 1.23 or newer.
- Python 3.10 or newer.
- `ffmpeg` and `ffprobe` available in `PATH`.
- C compiler and native Fyne dependencies to build the GUI.
- An OpenAI-compatible API for translation.

On first run, the program creates `.venv`, upgrades the packaging tools, and installs `openai-whisper` and `piper-tts`. The selected Piper voice is also downloaded automatically.

### Linux (Debian/Ubuntu)

```bash
sudo apt update
sudo apt install -y golang python3 python3-venv ffmpeg gcc libgl1-mesa-dev xorg-dev
```

### macOS

```bash
xcode-select --install
brew install go python ffmpeg
```

To build the self-contained macOS bundle, system Python and FFmpeg are still
required only on the build machine. The resulting `.app` loads the embedded
runtime from `Contents/Resources`.

### Windows

Install Go, Python 3.10+, FFmpeg, and a Fyne-compatible C compiler. Confirm in PowerShell:

```powershell
go version
python --version
ffmpeg -version
ffprobe -version
```

## Quick Start

### Linux/macOS

```bash
./scripts/build.sh
./bin/ai-video-dubber
```

### Windows PowerShell

```powershell
.\scripts\build.ps1
.\bin\ai-video-dubber.exe
```

You can also run directly during development:

```bash
go run ./cmd/ai-video-dubber
```

## Graphical Interface

1. Select a video.
2. Enter the translation API endpoint.
3. Enter the key when required.
4. Leave **Model** empty to detect the first model exposed by the API.
5. Choose the language.
6. Click **Start Dubbing**.

The GUI always regenerates intermediate files, matching the Python project's interface behavior. The API key is not persisted in application preferences.

## CLI

The main executable opens the GUI when called without arguments and also provides subcommands:

```bash
./bin/ai-video-dubber dub --input video.mp4 --language pt-BR
```

Default output:

```text
video.pt-BR.synced.mp4
```

Example with explicit API and model:

```bash
./bin/ai-video-dubber dub \
  --input video.mp4 \
  --language es \
  --api-base http://localhost:8000 \
  --api-key apikey \
  --model my-model \
  --force
```

The headless binary accepts the same subcommands:

```bash
./bin/ai-video-dubber-cli dub --input video.mp4 --language fr
```

### Standalone Stages

```bash
# 1. Video -> MP3
./bin/ai-video-dubber-cli extract --input video.mp4

# 2. Audio -> SRT, segments, JSON, and text
./bin/ai-video-dubber-cli transcribe --input video.mp3 --model medium

# 3. SRT -> translated SRT
./bin/ai-video-dubber-cli translate \
  --input video.srt \
  --output video.pt-BR.srt \
  --language pt-BR \
  --api-base http://localhost:8000

# 4. SRT/segments -> synchronized audio
./bin/ai-video-dubber-cli synthesize \
  --input video.pt-BR.srt \
  --language pt-BR \
  --report-json video.pt-BR.tts-report.json

# 5. Video + new audio -> final video
./bin/ai-video-dubber-cli merge \
  --video video.mp4 \
  --audio video.pt-BR.synced.mp3
```

Use `-h` after any subcommand to see all options. The `synthesize` command exposes advanced grouping, Piper, and timing-correction controls.

## Default Languages And Voices

| Language | Code | Piper Voice |
|---|---:|---|
| Brazilian Portuguese | `pt-BR` | `pt_BR-faber-medium` |
| Spanish | `es` | `es_ES-davefx-medium` |
| French | `fr` | `fr_FR-siwis-medium` |
| German | `de` | `de_DE-thorsten-medium` |
| Italian | `it` | `it_IT-riccardo-x_low` |

In the `synthesize` subcommand, `--voice` replaces the default voice.

## Environment Configuration

| Variable | Purpose | Default |
|---|---|---|
| `LLM_API_BASE` | OpenAI-compatible endpoint | `http://localhost:8000` |
| `LLM_API_KEY` | API key | `apikey` |
| `LLM_MODEL` | Translation model | auto-detection |
| `WHISPER_MODEL` | Whisper model | `large-v3` |
| `PYTHON_BIN` | System Python or embedded Python override | embedded Python, `python3`, or `python` |
| `VENV_DIR` | Managed virtual environment | empty in the bundle; `<project>/.venv` during development |
| `DATA_DIR` | Piper voice cache | user cache |
| `AI_VIDEO_DUBBER_HOME` | Application base directory | automatically detected |

Example:

```bash
LLM_API_BASE=http://localhost:1234 \
LLM_API_KEY=local \
LLM_MODEL=qwen \
WHISPER_MODEL=medium \
./bin/ai-video-dubber-cli dub --input video.mp4 --language pt-BR
```

## Generated Files

For `video.mp4` dubbed into `pt-BR`:

```text
video.mp3
video.srt
video.segments.txt
video.json
video.txt
video.pt-BR.srt
video.pt-BR.synced.mp3
video.pt-BR.synced.mp4
```

The CLI skips existing intermediate files unless `--force` is used. The final video is always remuxed.

## Development

```bash
# Download dependencies
make deps

# Formatting, tests, and vet in headless mode
make check

# Build GUI and CLI
make build

# Build only the CLI, including on servers without a desktop
make build-cli
```

### Self-Contained macOS Packaging

The `scripts/package-macos.sh` script assembles a `.app` bundle with:

- `Contents/MacOS/ai-video-dubber`
- `Contents/Resources/python/bin/python3` with `openai-whisper` and `piper-tts`
- `Contents/Resources/ffmpeg/ffmpeg` and `ffprobe`
- Separate tarball for the headless CLI with the same runtime layout

```bash
make package-macos
```

Generated artifacts:

```text
dist/AI-Video-Dubber.app
dist/AI-Video-Dubber-cli-darwin-<arch>.tar.gz
```

By default, the script discovers the latest
`astral-sh/python-build-standalone` release for Python 3.12 and downloads
FFmpeg/FFprobe through the `evermeet.cx` API. For reproducible builds or
internal sources, use:

```bash
PYTHON_STANDALONE_URL=https://.../cpython-3.12.x+release-aarch64-apple-darwin-install_only.tar.gz \
FFMPEG_BIN=/path/to/ffmpeg \
FFPROBE_BIN=/path/to/ffprobe \
make package-macos
```

For strict arm64 distribution, prefer setting `FFMPEG_BIN` and `FFPROBE_BIN`
with native static binaries for the target architecture.

Useful variables:

| Variable | Purpose |
|---|---|
| `ARCH` | `arm64` or `x86_64`; default: local architecture |
| `PYTHON_VERSION_PREFIX` | Python asset prefix; default: `cpython-3.12` |
| `PYTHON_STANDALONE_URL` | Exact Python standalone URL, skipping GitHub discovery |
| `FFMPEG_URL` / `FFPROBE_URL` | Alternative `.zip` URLs for the static binaries |
| `FFMPEG_BIN` / `FFPROBE_BIN` | Copy local binaries instead of downloading |
| `CODESIGN_IDENTITY` | Identity for hardened-runtime signing |
| `NOTARYTOOL_PROFILE` | `notarytool` profile for notarization |

You can also generate only the self-contained CLI tarball:

```bash
make package-cli
```

GUI tests use the `ci` tag, which selects Fyne's software driver and does not require OpenGL/X11:

```bash
go test -tags ci ./...
go vet -tags ci ./...
```

Native GUI builds still require the platform's graphical dependencies.

## Structure

```text
.
├── assets/                       # embedded icon
├── cmd/
│   ├── ai-video-dubber/          # GUI + CLI
│   └── ai-video-dubber-cli/      # headless CLI
├── internal/
│   ├── audio/                    # FFmpeg, ffprobe, WAV, and paths
│   ├── cli/                      # subcommands
│   ├── config/                   # configuration and defaults
│   ├── environment/              # venv and Python dependencies
│   ├── executil/                 # subprocesses, logs, and cancellation
│   ├── gui/                      # Fyne interface and theme
│   ├── language/                 # languages and voices
│   ├── pipeline/                 # six-stage orchestration
│   ├── srt/                      # SRT and segments
│   ├── transcription/            # Whisper integration
│   ├── translation/              # OpenAI-compatible client
│   └── tts/                      # Piper and timing synchronization
├── scripts/                      # build, run, and validation
├── FyneApp.toml
├── Makefile
└── go.mod
```

See also [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## Practical Limitations

- `large-v3` requires substantial memory and can be slow without a GPU. For tests, use `--whisper-model small` or `medium`.
- Translation sends subtitle text to the configured endpoint; transcription and TTS remain local.
- The pipeline replaces the main audio track and copies the video track. Additional tracks, chapters, and metadata are not preserved by default.
- Synchronization prioritizes natural speech; when a segment still does not fit the window after bounded correction, the audio is trimmed with a short fade-out.

## License

MIT. See [`LICENSE`](LICENSE).
