# AI Video Dubber - Go/Fyne

Functional clone of `AI-Video-Dubber-py`, reimplemented with **Go** for orchestration, **Fyne** for the graphical interface, and the same AI components used by the original project:

**Video -> audio -> Whisper -> translation -> dubbed video or selectable translated subtitles**

The application provides a dark desktop interface similar to the Python version, a full CLI, and standalone commands for each pipeline stage.

## Features

- Fyne graphical interface with video selection, output mode, API configuration, language selection, mode-specific progress, logs, and cancellation.
- Audio extraction and remuxing with FFmpeg.
- Local transcription with OpenAI Whisper.
- Translation through any OpenAI-compatible API (`/v1/models` and `/v1/chat/completions`).
- Automatic model detection when the **Model** field is empty.
- Subtitle mode that writes a translated `.srt` and an MP4 with a selectable subtitle track while preserving the original audio.
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

## First Run Downloads And Caches

The first run is intentionally heavier than later runs because the app prepares
the local speech stack:

- Python packages are installed into `.venv` in the project directory when a
  bundled Python runtime is not being used. Whisper pulls PyTorch and related
  packages, so budget several GB of disk and network transfer on a clean
  machine.
- The default Whisper model is `large-v3`. Its first transcription downloads
  the model into Whisper's cache, usually `~/.cache/whisper`, and requires about
  3 GB for the model file. Use `--whisper-model small` or `WHISPER_MODEL=small`
  for faster smoke tests on clean machines.
- Piper voice data is separate from Whisper. Voices are stored under the app's
  Piper voice cache, which defaults to the OS user cache directory plus
  `piper-voices` (for example `~/Library/Caches/piper-voices` on macOS). Each
  medium voice is typically tens of MB; the app verifies downloaded voice files
  against Piper's published size/checksum metadata before using them.
- First-run setup can take several minutes on a fast connection and much longer
  on slow networks or CPU-only machines. Later runs reuse `.venv`, the Whisper
  model cache, and the Piper voice cache unless those directories are removed.

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
5. Choose **Dub** or **Subtitle**.
6. Choose the language.
7. Click **Start Dubbing** or **Start Subtitling**.

The GUI always regenerates intermediate files, matching the Python project's interface behavior. The API key is not persisted in application preferences.

## CLI

The main executable opens the GUI when called without arguments and also provides subcommands:

```bash
./bin/ai-video-dubber dub --input video.mp4 --language pt-BR
./bin/ai-video-dubber subtitle --input video.mp4 --language pt-BR
./bin/ai-video-dubber subtitle --input video.mp4 --language pt-BR --burn-in
```

Default dubbing output:

```text
video.pt-BR.synced.mp4
```

Default subtitle output:

```text
video.pt-BR.srt
video.pt-BR.subtitled.mp4
```

With `subtitle --burn-in`, the sidecar SRT is still written and the default video output becomes:

```text
video.pt-BR.burned-in.mp4
```

Example with explicit API and model:

```bash
./bin/ai-video-dubber dub \
  --input video.mp4 \
  --language es \
  --api-base http://localhost:8000 \
  --api-key apikey \
  --model my-model \
  --batch-size 10 \
  --translation-timeout 10m \
  --force
```

`--batch-size` controls how many subtitle cues are translated per API request. `--translation-timeout` defaults to `120s`; increase it for slower local OpenAI-compatible models.

The headless binary accepts the same subcommands:

```bash
./bin/ai-video-dubber-cli dub --input video.mp4 --language fr
./bin/ai-video-dubber-cli subtitle --input video.mp4 --language fr
./bin/ai-video-dubber-cli subtitle --input video.mp4 --language fr --burn-in
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
  --api-base http://localhost:8000 \
  --batch-size 10 \
  --translation-timeout 10m

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

For `video.mp4` subtitled into `pt-BR`:

```text
video.mp3
video.srt
video.segments.txt
video.json
video.txt
video.pt-BR.srt
video.pt-BR.subtitled.mp4
```

For `video.mp4` subtitled into `pt-BR` with `--burn-in`:

```text
video.mp3
video.srt
video.segments.txt
video.json
video.txt
video.pt-BR.srt
video.pt-BR.burned-in.mp4
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
- `Contents/Resources/VERSIONS.txt` with the pinned runtime inputs used for the build
- Separate tarball for the headless CLI with the same runtime layout

```bash
make package-macos
```

Generated artifacts:

```text
dist/AI-Video-Dubber.app
dist/AI-Video-Dubber-cli-darwin-<arch>.tar.gz
```

By default, the script uses pinned inputs: Python standalone
`3.12.13+20260623`, FFmpeg/FFprobe `8.1.2`, `openai-whisper` `20250625`,
and `piper-tts` `1.4.2`. The same resolved values are written to
`VERSIONS.txt` inside the `.app` and CLI tarball. To inspect the configured
inputs without building, run:

```bash
./scripts/package-macos.sh versions
```

For internal sources, use:

```bash
PYTHON_STANDALONE_URL=https://.../cpython-3.12.13%2B20260623-aarch64-apple-darwin-install_only.tar.gz \
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
| `PYTHON_STANDALONE_RELEASE` / `PYTHON_STANDALONE_VERSION` | Pinned Python standalone release/version used to build the default URL |
| `PYTHON_STANDALONE_URL` | Exact Python standalone URL override |
| `FFMPEG_VERSION` / `FFPROBE_VERSION` | Pinned evermeet.cx binary versions used to build the default URLs |
| `FFMPEG_URL` / `FFPROBE_URL` | Alternative `.zip` URLs for the static binaries |
| `FFMPEG_BIN` / `FFPROBE_BIN` | Copy local binaries instead of downloading |
| `OPENAI_WHISPER_VERSION` / `PIPER_TTS_VERSION` | Pinned Python package versions |
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
- Dub mode replaces the main audio track and copies the video track. Subtitle mode preserves the original audio/video streams and adds a selectable subtitle track by default. With `--burn-in`, FFmpeg re-encodes the video with the subtitle text rendered into the pixels and copies the original audio. Additional tracks, chapters, and metadata are not preserved by default.
- Burned-in subtitles require an FFmpeg build with the `subtitles` filter, normally provided by libass.
- Synchronization prioritizes natural speech; when a segment still does not fit the window after bounded correction, the audio is trimmed with a short fade-out.

## License

MIT. See [`LICENSE`](LICENSE).
