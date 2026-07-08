package gui

import (
	"testing"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/pipeline"
)

func TestStepIndicatorShowsActivityWhileRunning(t *testing.T) {
	indicator := newStepIndicator(1, "Transcribe")
	if indicator.activity.Visible() {
		t.Fatal("activity indicator is visible before running")
	}

	indicator.setState(pipeline.StateRunning)
	if !indicator.activity.Visible() {
		t.Fatal("activity indicator is hidden while running")
	}

	indicator.setState(pipeline.StateDone)
	if indicator.activity.Visible() {
		t.Fatal("activity indicator stayed visible after completion")
	}
}
