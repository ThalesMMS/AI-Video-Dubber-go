package environment

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/config"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/executil"
)

func TestSetupRuntimeUsesBundledPythonWithoutCreatingVenv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	resources := filepath.Join(dir, "AI-Video-Dubber.app", "Contents", "Resources")
	python := filepath.Join(resources, "python", "bin", "python3")
	ffmpeg := filepath.Join(resources, "ffmpeg", "ffmpeg")
	ffprobe := filepath.Join(resources, "ffmpeg", "ffprobe")

	writeExecutable(t, python, `#!/bin/sh
if [ "$1" = "-c" ]; then
  case "$2" in
    *version_info*) printf '3.12\n'; exit 0 ;;
    *"import whisper, piper"*) exit 0 ;;
  esac
fi
if [ "$1" = "-m" ] && [ "$2" = "piper" ] && [ "$3" = "--help" ]; then
  exit 0
fi
printf 'unexpected python invocation: %s\n' "$*" >&2
exit 9
`)
	writeExecutable(t, ffmpeg, "#!/bin/sh\nexit 0\n")
	writeExecutable(t, ffprobe, "#!/bin/sh\nexit 0\n")

	cfg := (config.Config{
		PythonBin:  python,
		FFmpegBin:  ffmpeg,
		FFprobeBin: ffprobe,
	}).Normalize(project)
	if cfg.VenvDir != "" {
		t.Fatalf("VenvDir = %q, want empty", cfg.VenvDir)
	}
	runner := executil.Runner{Tools: cfg.ToolPaths(), Env: cfg.RuntimeEnv()}

	pythonExe, err := SetupRuntime(context.Background(), runner, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if pythonExe != python {
		t.Fatalf("pythonExe = %q, want %q", pythonExe, python)
	}
	if _, err := os.Stat(filepath.Join(project, ".venv")); !os.IsNotExist(err) {
		t.Fatalf("venv stat error = %v, want not exist", err)
	}
}

func TestSetupWhisperRuntimeDoesNotRequirePiper(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	resources := filepath.Join(dir, "AI-Video-Dubber.app", "Contents", "Resources")
	python := filepath.Join(resources, "python", "bin", "python3")
	ffmpeg := filepath.Join(resources, "ffmpeg", "ffmpeg")
	ffprobe := filepath.Join(resources, "ffmpeg", "ffprobe")

	writeExecutable(t, python, `#!/bin/sh
if [ "$1" = "-c" ]; then
  case "$2" in
    *version_info*) printf '3.12\n'; exit 0 ;;
    *"import whisper"*) exit 0 ;;
    *"import whisper, piper"*) exit 9 ;;
  esac
fi
if [ "$1" = "-m" ] && [ "$2" = "piper" ]; then
  exit 9
fi
printf 'unexpected python invocation: %s\n' "$*" >&2
exit 9
`)
	writeExecutable(t, ffmpeg, "#!/bin/sh\nexit 0\n")
	writeExecutable(t, ffprobe, "#!/bin/sh\nexit 0\n")

	cfg := (config.Config{
		PythonBin:  python,
		FFmpegBin:  ffmpeg,
		FFprobeBin: ffprobe,
	}).Normalize(project)
	runner := executil.Runner{Tools: cfg.ToolPaths(), Env: cfg.RuntimeEnv()}

	pythonExe, err := SetupWhisperRuntime(context.Background(), runner, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if pythonExe != python {
		t.Fatalf("pythonExe = %q, want %q", pythonExe, python)
	}
}

func TestSetupRuntimePinsPipIndexURL(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	python := filepath.Join(dir, "python3")
	ffmpeg := filepath.Join(dir, "ffmpeg")
	ffprobe := filepath.Join(dir, "ffprobe")
	argsLog := filepath.Join(dir, "pip-args.log")

	writeExecutable(t, python, `#!/bin/sh
if [ "$1" = "-c" ] && echo "$2" | grep -q "version_info"; then
  printf '3.12\n'
  exit 0
fi
if [ "$1" = "-m" ] && [ "$2" = "venv" ]; then
  mkdir -p "$3/bin"
  cat > "$3/bin/python" <<'PY'
#!/bin/sh
if [ "$1" = "-c" ] && echo "$2" | grep -q "import whisper, piper"; then
  exit 1
fi
if [ "$1" = "-m" ] && [ "$2" = "pip" ]; then
  printf '%s\n' "$*" >> "$CAPTURE_PIP_ARGS"
  exit 0
fi
printf 'unexpected venv python invocation: %s\n' "$*" >&2
exit 9
PY
  chmod 755 "$3/bin/python"
  exit 0
fi
printf 'unexpected python invocation: %s\n' "$*" >&2
exit 9
`)
	writeExecutable(t, ffmpeg, "#!/bin/sh\nexit 0\n")
	writeExecutable(t, ffprobe, "#!/bin/sh\nexit 0\n")

	cfg := (config.Config{
		PythonBin:  python,
		FFmpegBin:  ffmpeg,
		FFprobeBin: ffprobe,
	}).Normalize(project)
	runner := executil.Runner{
		Tools: cfg.ToolPaths(),
		Env:   append(cfg.RuntimeEnv(), "CAPTURE_PIP_ARGS="+argsLog),
	}

	if _, err := SetupRuntime(context.Background(), runner, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("pip invocations = %d, want 2:\n%s", len(lines), string(data))
	}
	for _, line := range lines {
		if !strings.Contains(line, "--index-url https://pypi.org/simple") {
			t.Fatalf("pip invocation does not pin the index URL:\n%s", line)
		}
	}
	dependencyInstall := lines[1]
	for _, want := range []string{"openai-whisper==20250625", "piper-tts==1.4.2"} {
		if !strings.Contains(dependencyInstall, want) {
			t.Fatalf("dependency install does not pin %s:\n%s", want, dependencyInstall)
		}
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
