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

func TestPipInstallArgsSupportsOfflineWheelhouse(t *testing.T) {
	t.Setenv("PIP_NO_INDEX", "1")
	t.Setenv("PIP_FIND_LINKS", "/opt/ai-video-dubber/wheels")

	args := strings.Join(pipInstallArgs("openai-whisper==20250625"), " ")
	if !strings.Contains(args, "--no-index") {
		t.Fatalf("pip args missing --no-index: %s", args)
	}
	if !strings.Contains(args, "--find-links /opt/ai-video-dubber/wheels") {
		t.Fatalf("pip args missing wheelhouse: %s", args)
	}
	if strings.Contains(args, "--index-url") {
		t.Fatalf("offline pip args should not include --index-url: %s", args)
	}
}

func TestPipInstallArgsUsesCustomIndexURL(t *testing.T) {
	t.Setenv("PIP_INDEX_URL", "https://packages.example/simple")

	args := strings.Join(pipInstallArgs("piper-tts==1.4.2"), " ")
	if !strings.Contains(args, "--index-url https://packages.example/simple") {
		t.Fatalf("pip args missing custom index URL: %s", args)
	}
}

func TestSetupRuntimeMissingExecutableIncludesInstallHint(t *testing.T) {
	cfg := config.Config{
		FFmpegBin:  "ai-video-dubber-missing-ffmpeg",
		FFprobeBin: "ffprobe",
		PythonBin:  "python3",
	}

	_, err := SetupRuntime(context.Background(), executil.Runner{}, cfg)
	if err == nil {
		t.Fatal("SetupRuntime succeeded, want missing executable error")
	}
	if !strings.Contains(err.Error(), "ai-video-dubber-missing-ffmpeg") {
		t.Fatalf("error missing executable name: %v", err)
	}
	if !strings.Contains(err.Error(), "Install prerequisites") {
		t.Fatalf("error missing install hint: %v", err)
	}
}

func TestSetupRuntimeRebuildsBrokenExistingVenvOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	venvDir := filepath.Join(project, ".venv")
	python := filepath.Join(dir, "python3")
	ffmpeg := filepath.Join(dir, "ffmpeg")
	ffprobe := filepath.Join(dir, "ffprobe")
	events := filepath.Join(dir, "events.log")

	writeExecutable(t, python, `#!/bin/sh
if [ "$1" = "-c" ] && echo "$2" | grep -q "version_info"; then
  printf '3.12\n'
  exit 0
fi
if [ "$1" = "-m" ] && [ "$2" = "venv" ]; then
  printf 'create-venv\n' >> "$CAPTURE_EVENTS"
  mkdir -p "$3/bin"
  cat > "$3/bin/python" <<'PY'
#!/bin/sh
if [ "$1" = "-c" ] && echo "$2" | grep -q "import whisper, piper"; then
  printf 'new-verify\n' >> "$CAPTURE_EVENTS"
  exit 1
fi
if [ "$1" = "-m" ] && [ "$2" = "pip" ]; then
  printf 'new-pip %s\n' "$*" >> "$CAPTURE_EVENTS"
  exit 0
fi
printf 'unexpected new venv python invocation: %s\n' "$*" >&2
exit 9
PY
  chmod 755 "$3/bin/python"
  exit 0
fi
printf 'unexpected python invocation: %s\n' "$*" >&2
exit 9
`)
	brokenPython := config.VenvPython(venvDir)
	writeExecutable(t, brokenPython, `#!/bin/sh
if [ "$1" = "-c" ] && echo "$2" | grep -q "import whisper, piper"; then
  printf 'broken-verify\n' >> "$CAPTURE_EVENTS"
  exit 1
fi
if [ "$1" = "-m" ] && [ "$2" = "pip" ]; then
  printf 'broken-pip\n' >> "$CAPTURE_EVENTS"
  exit 9
fi
printf 'unexpected broken venv python invocation: %s\n' "$*" >&2
exit 9
`)
	if err := os.WriteFile(filepath.Join(venvDir, "BROKEN"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, ffmpeg, "#!/bin/sh\nexit 0\n")
	writeExecutable(t, ffprobe, "#!/bin/sh\nexit 0\n")

	cfg := (config.Config{
		PythonBin:  python,
		VenvDir:    venvDir,
		FFmpegBin:  ffmpeg,
		FFprobeBin: ffprobe,
	}).Normalize(project)
	runner := executil.Runner{
		Tools: cfg.ToolPaths(),
		Env:   append(cfg.RuntimeEnv(), "CAPTURE_EVENTS="+events),
	}

	pythonExe, err := SetupRuntime(context.Background(), runner, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if pythonExe != config.VenvPython(venvDir) {
		t.Fatalf("pythonExe = %q, want %q", pythonExe, config.VenvPython(venvDir))
	}
	data, err := os.ReadFile(events)
	if err != nil {
		t.Fatal(err)
	}
	log := string(data)
	for _, want := range []string{"broken-verify", "create-venv", "new-pip"} {
		if !strings.Contains(log, want) {
			t.Fatalf("event log missing %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "broken-pip") {
		t.Fatalf("setup tried to install into the broken venv:\n%s", log)
	}
	if _, err := os.Stat(filepath.Join(venvDir, "BROKEN")); !os.IsNotExist(err) {
		t.Fatalf("stale venv marker stat = %v, want removed", err)
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
