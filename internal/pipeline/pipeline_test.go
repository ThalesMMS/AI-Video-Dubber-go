package pipeline

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/audio"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/config"
)

type recordingObserver struct {
	lines []string
}

func (o *recordingObserver) OnLog(line string) { o.lines = append(o.lines, line) }
func (*recordingObserver) OnStep(Step, State)  {}

func TestShouldRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output")
	if run, err := shouldRun(false, path); err != nil || !run {
		t.Fatalf("missing file: run=%v err=%v", run, err)
	}
	if err := os.WriteFile(path, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if run, err := shouldRun(false, path); err != nil || run {
		t.Fatalf("existing file: run=%v err=%v", run, err)
	}
	if run, err := shouldRun(true, path); err != nil || !run {
		t.Fatalf("force: run=%v err=%v", run, err)
	}
	directory := filepath.Join(dir, "directory")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := shouldRun(false, directory); err == nil {
		t.Fatal("directory output unexpectedly accepted")
	}
}

func TestRunRejectsUnsupportedLanguageBeforeLocalWork(t *testing.T) {
	_, err := (Pipeline{ProjectDir: t.TempDir()}).Run(context.Background(), config.Config{
		InputPath:    "video.mp4",
		LanguageCode: "xx",
		APIBase:      "http://localhost:8000",
	})

	if err == nil {
		t.Fatal("Run accepted unsupported language")
	}
	if !strings.Contains(err.Error(), "unsupported language") {
		t.Fatalf("error = %q, want unsupported language", err.Error())
	}
}

func TestRunRejectsMissingInputBeforeLocalWork(t *testing.T) {
	dir := t.TempDir()
	_, err := (Pipeline{ProjectDir: dir}).Run(context.Background(), config.Config{
		InputPath:    filepath.Join(dir, "missing.mp4"),
		LanguageCode: "pt-BR",
		APIBase:      "http://localhost:8000",
	})

	if err == nil {
		t.Fatal("Run accepted missing input")
	}
	if !strings.Contains(err.Error(), "input video") {
		t.Fatalf("error = %q, want input video", err.Error())
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".venv")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf(".venv stat error = %v, want not exist", statErr)
	}
}

func TestBeginUsesOneBasedSixStepProgress(t *testing.T) {
	observer := &recordingObserver{}
	p := Pipeline{Observer: observer}
	p.begin(StepSetup)
	p.begin(StepMerge)

	joined := strings.Join(observer.lines, "\n")
	if !strings.Contains(joined, "Step 1/6 — Setup environment") {
		t.Fatalf("setup progress line not found:\n%s", joined)
	}
	if !strings.Contains(joined, "Step 6/6 — Merge final video") {
		t.Fatalf("merge progress line not found:\n%s", joined)
	}
}

func TestStepLabelsForMode(t *testing.T) {
	dub := StepLabelsForMode(config.ModeDub)
	if len(dub) != 6 {
		t.Fatalf("dub labels len = %d, want 6", len(dub))
	}
	if dub[4] != "Generate dubbed audio" {
		t.Fatalf("dub fifth label = %q", dub[4])
	}

	subtitle := StepLabelsForMode(config.ModeSubtitle)
	if len(subtitle) != 5 {
		t.Fatalf("subtitle labels len = %d, want 5", len(subtitle))
	}
	if subtitle[4] != "Create subtitled video" {
		t.Fatalf("subtitle fifth label = %q", subtitle[4])
	}
	if strings.Join(subtitle, "\n") == strings.Join(dub, "\n") || strings.Contains(strings.Join(subtitle, "\n"), "Generate dubbed audio") {
		t.Fatalf("subtitle labels still include dubbing-only steps: %#v", subtitle)
	}

	burned := StepLabelsForModeOptions(config.ModeSubtitle, true)
	if len(burned) != 5 {
		t.Fatalf("burn-in labels len = %d, want 5", len(burned))
	}
	if burned[4] != "Create burned-in video" {
		t.Fatalf("burn-in fifth label = %q", burned[4])
	}
}

func TestBeginUsesOneBasedFiveStepSubtitleProgress(t *testing.T) {
	observer := &recordingObserver{}
	p := Pipeline{Observer: observer, stepLabels: StepLabelsForMode(config.ModeSubtitle)}
	p.begin(StepSynthesize)

	joined := strings.Join(observer.lines, "\n")
	if !strings.Contains(joined, "Step 5/5 — Create subtitled video") {
		t.Fatalf("subtitle progress line not found:\n%s", joined)
	}
}

func TestRunPreflightsTranslationAPIBeforeLocalSetup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" {
			http.NotFound(writer, request)
			return
		}
		http.Error(writer, "bad key", http.StatusUnauthorized)
	}))
	defer server.Close()

	dir := t.TempDir()
	input := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(input, []byte("placeholder video"), 0o644); err != nil {
		t.Fatal(err)
	}
	observer := &recordingObserver{}
	cfg := config.Config{
		Mode:         config.ModeDub,
		InputPath:    input,
		APIBase:      server.URL,
		APIKey:       "wrong",
		LanguageCode: "pt-BR",
		Force:        true,
	}

	_, err := (Pipeline{ProjectDir: dir, Observer: observer}).Run(context.Background(), cfg)
	if err == nil {
		t.Fatal("Run unexpectedly succeeded")
	}
	if text := err.Error(); !strings.Contains(text, "Translate subtitles: translation API preflight") || !strings.Contains(text, "HTTP 401") {
		t.Fatalf("error = %q, want preflight HTTP failure", text)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".venv")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf(".venv stat error = %v, want not exist", statErr)
	}
	if joined := strings.Join(observer.lines, "\n"); !strings.Contains(joined, "Checking translation API connectivity") {
		t.Fatalf("preflight log missing:\n%s", joined)
	}
}

func TestRunSkipsExistingFinalSubtitleVideo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	python := filepath.Join(dir, "python")
	pythonScript := `#!/bin/sh
if [ "$1" = "-c" ]; then
  case "$2" in
    *version_info*) printf '3.11\n' ;;
  esac
fi
exit 0
`
	if err := os.WriteFile(python, []byte(pythonScript), 0o755); err != nil {
		t.Fatal(err)
	}
	venvDir := filepath.Join(dir, "venv")
	venvPython := config.VenvPython(venvDir)
	if err := os.MkdirAll(filepath.Dir(venvPython), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(venvPython, []byte(pythonScript), 0o755); err != nil {
		t.Fatal(err)
	}
	ffmpegCalled := filepath.Join(dir, "ffmpeg-called")
	t.Setenv("FFMPEG_CALLED", ffmpegCalled)
	ffmpeg := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte(`#!/bin/sh
printf called > "$FFMPEG_CALLED"
exit 1
`), 0o755); err != nil {
		t.Fatal(err)
	}
	ffprobe := filepath.Join(dir, "ffprobe")
	if err := os.WriteFile(ffprobe, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(dir, "video.mp4")
	if err := os.WriteFile(input, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := audio.BuildPathsForMode(input, "pt-BR", "", config.ModeSubtitle)
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string]string{
		paths.ExtractedAudio: "audio",
		paths.TranscriptSRT:  "1\n00:00:00,000 --> 00:00:01,000\nOne\n",
		paths.TranslatedSRT:  "1\n00:00:00,000 --> 00:00:01,000\nUm\n",
		paths.FinalVideo:     "existing final video",
	} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	observer := &recordingObserver{}
	_, err = (Pipeline{ProjectDir: dir, Observer: observer}).Run(context.Background(), config.Config{
		Mode:         config.ModeSubtitle,
		InputPath:    input,
		LanguageCode: "pt-BR",
		APIBase:      "http://127.0.0.1:1",
		PythonBin:    python,
		VenvDir:      venvDir,
		FFmpegBin:    ffmpeg,
		FFprobeBin:   ffprobe,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ffmpegCalled); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ffmpeg marker stat = %v, want not called", err)
	}
	if joined := strings.Join(observer.lines, "\n"); !strings.Contains(joined, "Skipped: final video already exists") {
		t.Fatalf("final skip log missing:\n%s", joined)
	}
}
