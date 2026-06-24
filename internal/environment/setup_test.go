package environment

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
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

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
