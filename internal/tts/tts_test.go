package tts

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/audio"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/executil"
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

func TestWriteReportUsesPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	err := writeReport(path, []GroupReport{{
		ID:   1,
		Text: "sensitive transcript",
	}})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("report permissions = %o, want 600", got)
	}
}

func TestEnsureVoiceRejectsChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	voice := "pt_BR-test-medium"
	model := filepath.Join(dir, voice+".onnx")
	config := filepath.Join(dir, voice+".onnx.json")
	modelData := []byte("tampered model")
	configData := []byte(`{"audio":{"sample_rate":22050}}`)
	if err := os.WriteFile(model, modelData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, configData, 0o600); err != nil {
		t.Fatal(err)
	}
	index := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, `{
			"pt_BR-test-medium": {
				"files": {
					"pt/pt_BR/test/medium/pt_BR-test-medium.onnx": {"size_bytes": %d, "md5_digest": "00000000000000000000000000000000"},
					"pt/pt_BR/test/medium/pt_BR-test-medium.onnx.json": {"size_bytes": %d, "md5_digest": "00000000000000000000000000000000"}
				}
			}
		}`, len(modelData), len(configData))
	}))
	defer index.Close()
	originalIndexURL := piperVoicesIndexURL
	piperVoicesIndexURL = index.URL
	defer func() { piperVoicesIndexURL = originalIndexURL }()

	_, _, err := ensureVoice(context.Background(), executil.Runner{}, "python", voice, dir)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("ensureVoice error = %v, want checksum mismatch", err)
	}
}

func TestPiperWorkerHandlesMultipleSynthesisRequests(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	python := filepath.Join(dir, "python")
	starts := filepath.Join(dir, "piper-starts")
	t.Setenv("PIPER_STARTS", starts)
	if err := os.WriteFile(python, []byte(`#!/bin/sh
printf 'start\n' >> "$PIPER_STARTS"
if [ "$1" = "-u" ]; then
  printf '{"ready":true}\n'
  while IFS= read -r line; do
    output=$(printf '%s\n' "$line" | sed -n 's/.*"output_path":"\([^"]*\)".*/\1/p')
    if [ -z "$output" ]; then
      printf '{"error":"missing output_path"}\n'
      continue
    fi
    printf wav > "$output"
    printf '{"ok":true}\n'
  done
  exit 0
fi
exit 1
`), 0o755); err != nil {
		t.Fatal(err)
	}

	worker, err := startPiperWorker(context.Background(), executil.Runner{}, python, "voice.onnx", "voice.onnx.json")
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()

	for index := 0; index < 2; index++ {
		output := filepath.Join(dir, fmt.Sprintf("out-%d.wav", index))
		if err := worker.synthesize(context.Background(), "hello", output, 1.0, 0.667, 0.8, Defaults()); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(output); err != nil {
			t.Fatal(err)
		}
	}

	data, err := os.ReadFile(starts)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(data), "start\n"); count != 1 {
		t.Fatalf("piper starts = %d, want one worker process; starts log:\n%s", count, string(data))
	}
}

func TestSynthesizeAttemptsReadsWAVDurationWithoutFFprobe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	template := filepath.Join(dir, "template.wav")
	if err := audio.WriteSilencePCM16Mono(template, (1500 * time.Millisecond).Nanoseconds(), 22050); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PIPER_TEMPLATE_WAV", template)
	python := filepath.Join(dir, "python")
	if err := os.WriteFile(python, []byte(`#!/bin/sh
if [ "$1" = "-u" ]; then
  printf '{"ready":true}\n'
  while IFS= read -r line; do
    output=$(printf '%s\n' "$line" | sed -n 's/.*"output_path":"\([^"]*\)".*/\1/p')
    cp "$PIPER_TEMPLATE_WAV" "$output"
    printf '{"ok":true}\n'
  done
  exit 0
fi
exit 1
`), 0o755); err != nil {
		t.Fatal(err)
	}
	ffprobe := filepath.Join(dir, "ffprobe")
	if err := os.WriteFile(ffprobe, []byte("#!/bin/sh\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	worker, err := startPiperWorker(context.Background(), executil.Runner{}, python, "voice.onnx", "voice.onnx.json")
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	options := Defaults()
	options.MinLengthScale = 1
	options.MaxLengthScale = 1
	group := Group{ID: 1, Text: "hello", Start: 0, End: 2 * time.Second}

	attempts, err := synthesizeAttempts(context.Background(), executil.Runner{Tools: map[string]string{"ffprobe": ffprobe}}, worker, group, dir, voiceConfig{SampleRate: 22050, LengthScale: 1, NoiseScale: 0.667, NoiseW: 0.8}, options)
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(attempts))
	}
	if attempts[0].Duration != 1500*time.Millisecond {
		t.Fatalf("duration = %s, want 1.5s", attempts[0].Duration)
	}
}
