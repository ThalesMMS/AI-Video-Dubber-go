package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
