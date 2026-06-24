package tts

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/srt"
)

func TestGroupCuesSentenceAndGapBoundaries(t *testing.T) {
	cues := []srt.Cue{
		{Index: 1, Start: 0, End: time.Second, Text: "Hello"},
		{Index: 2, Start: 1100 * time.Millisecond, End: 2 * time.Second, Text: "world."},
		{Index: 3, Start: 2300 * time.Millisecond, End: 3 * time.Second, Text: "Next sentence"},
		{Index: 4, Start: 4 * time.Second, End: 5 * time.Second, Text: "Far away"},
	}
	groups := GroupCues(cues, Defaults())
	if len(groups) != 3 {
		t.Fatalf("len(groups) = %d, groups=%#v", len(groups), groups)
	}
	if groups[0].Text != "Hello world." || len(groups[0].Cues) != 2 {
		t.Fatalf("first group = %#v", groups[0])
	}
	if groups[1].Text != "Next sentence" || groups[2].Text != "Far away" {
		t.Fatalf("unexpected groups = %#v", groups)
	}
}

func TestGroupCuesCountsUnicodeRunes(t *testing.T) {
	options := Defaults()
	options.MaxGroupChars = 7
	cues := []srt.Cue{
		{Index: 1, Start: 0, End: time.Second, Text: "ação"},
		{Index: 2, Start: time.Second, End: 2 * time.Second, Text: "é"},
	}
	groups := GroupCues(cues, options)
	if len(groups) != 1 {
		t.Fatalf("Unicode text was counted as bytes; groups=%#v", groups)
	}
}

func TestNormalizeText(t *testing.T) {
	got := normalizeText("  Front-end & 3,14 ...  ", "pt-BR")
	if got != "Front end e 3 vírgula 14…" {
		t.Fatalf("normalizeText() = %q", got)
	}
}

func TestScaleCandidatesAndAttemptSelection(t *testing.T) {
	candidates := chooseScaleCandidates(1.0, time.Second, 0.8, 1.1)
	if len(candidates) < 3 {
		t.Fatalf("candidates = %#v", candidates)
	}
	for _, value := range candidates {
		if value < 0.8 || value > 1.1 {
			t.Fatalf("candidate %f outside bounds", value)
		}
	}
	attempts := []attempt{
		{LengthScale: 1, Duration: 1100 * time.Millisecond, Path: "a"},
		{LengthScale: .9, Duration: 980 * time.Millisecond, Path: "b"},
		{LengthScale: .8, Duration: 1500 * time.Millisecond, Path: "c"},
	}
	best, err := selectBestAttempt(attempts, time.Second, 1.12)
	if err != nil {
		t.Fatal(err)
	}
	if best.Path != "b" {
		t.Fatalf("best = %#v", best)
	}
}

func TestFitSpeed(t *testing.T) {
	speed, trimmed := fitSpeed(1050*time.Millisecond, time.Second, 1.12)
	if trimmed || math.Abs(speed-1.05) > 1e-9 {
		t.Fatalf("speed=%f trimmed=%v", speed, trimmed)
	}
	speed, trimmed = fitSpeed(1500*time.Millisecond, time.Second, 1.12)
	if !trimmed || math.Abs(speed-1.12) > 1e-9 {
		t.Fatalf("speed=%f trimmed=%v", speed, trimmed)
	}
}

func TestExcerptUsesRunes(t *testing.T) {
	got := excerpt("áéíóú abc", 6)
	if !strings.HasSuffix(got, "…") || len([]rune(got)) != 6 {
		t.Fatalf("excerpt = %q", got)
	}
}
