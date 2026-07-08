# Architecture

## Overview

The project separates interface, orchestration, and external integrations. The GUI and CLI build a `config.Config` and delegate execution to `pipeline.Pipeline`. The pipeline does not depend on Fyne and can be tested or run in headless environments.

```text
GUI / CLI
    │
    ▼
pipeline.Pipeline
    ├── environment  → Python/venv, Whisper, Piper, and voices
    ├── audio        → FFmpeg, ffprobe, and PCM/WAV handling
    ├── transcription→ local Whisper
    ├── translation  → OpenAI-compatible API
    └── tts          → Piper, grouping, and timing adjustment for dub mode
```

## Main Decisions

### Go As Orchestrator

Processes, files, networking, cancellation, and interface state are controlled in Go. Whisper and Piper stay in the Python runtime because their official distributions and models already follow that ecosystem. This boundary reduces duplicated code and keeps compatibility with the Python project.

### Observable Pipeline

`pipeline.Observer` receives logs and state changes. The CLI writes to the terminal; the GUI schedules updates on the Fyne thread. The core remains decoupled from presentation.

### Complete Run Modes

`config.ModeDub` keeps the original six-stage dubbing flow: setup, audio extraction, transcription, translation, Piper synthesis, and final video/audio merge. `config.ModeSubtitle` shares the setup/extract/transcribe/translate stages, skips Piper voice preparation and TTS, and uses FFmpeg to copy the original video/audio streams into an MP4 with the translated SRT as a selectable `mov_text` subtitle track. When `Config.SubtitleBurnIn` is true, the final stage instead renders the SRT through FFmpeg's `subtitles` filter, re-encodes the video with libx264, and copies the original audio.

### Deterministic Artifacts

`audio.BuildPathsForModeOptions` centralizes intermediate file names. Dub mode keeps `video.<lang>.synced.mp4`; subtitle mode creates `video.<lang>.srt` plus `video.<lang>.subtitled.mp4`, or `video.<lang>.burned-in.mp4` for burned-in subtitle output. This keeps compatibility with the reference project and lets the CLI resume shared stages.

### Tolerant Translation

The client accepts endpoints with or without `/v1`, automatically detects the model, and asks for one JSON translation array per batch so cue order survives sentence fragments. It still accepts legacy numbered responses and preserves the original when a line is missing from the response. SRT writes are atomic.

### TTS Synchronization

1. Nearby cues are grouped to improve prosody.
2. Text is normalized by language.
3. Multiple Piper `length_scale` values are tested.
4. The attempt closest to the timing window is selected.
5. A small `atempo` correction is applied when needed.
6. The segment is padded or trimmed to occupy the exact timing window.
7. Silence and segments are concatenated as mono PCM16 before final encoding.

### Cancellation

On Unix, subprocesses start in their own group; canceling the context terminates the group. This avoids leaving FFmpeg, pip, Whisper, or Piper running after GUI cancellation.

### Portability

- Paths and file replacement handle Windows differences.
- The headless CLI does not import Fyne.
- The `ci` tag uses Fyne's software driver for tests without a graphical server.
- The native GUI uses the platform default driver.

## Extension Points

- New languages: add an entry in `internal/language/language.go`.
- New translator: implement another client and inject it into the pipeline.
- New subtitle formats: extend `internal/srt`.
- Multiple-track preservation: change the mapping policy in `audio.MergeVideoAudio`.
- Whisper acceleration: adapt the script embedded in `internal/transcription` or create another backend.
