package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
