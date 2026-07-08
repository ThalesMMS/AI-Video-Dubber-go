package transcription

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/executil"
)

func TestCuesFromWhisperResultFiltersLikelyNoSpeechHallucinations(t *testing.T) {
	result := whisperResult{Segments: []whisperSegment{
		{
			Start:        0,
			End:          6.6,
			Text:         " Oh yeah, we shall go one and yeah, one and yeah",
			NoSpeechProb: 0.59576416015625,
			AvgLogProb:   -0.9968271255493164,
			Compression:  1.8043478260869565,
		},
	}}

	cues := cuesFromWhisperResult(result)
	if len(cues) != 0 {
		t.Fatalf("cues = %#v, want no cues for likely no-speech hallucination", cues)
	}
}

func TestCuesFromWhisperResultKeepsConfidentSpeech(t *testing.T) {
	result := whisperResult{Segments: []whisperSegment{
		{
			Start:        1.25,
			End:          3.5,
			Text:         " Ultrasound uses sound waves.",
			NoSpeechProb: 0.08,
			AvgLogProb:   -0.21,
			Compression:  1.2,
		},
	}}

	cues := cuesFromWhisperResult(result)
	if len(cues) != 1 {
		t.Fatalf("len(cues) = %d, want 1", len(cues))
	}
	if cues[0].Text != "Ultrasound uses sound waves." {
		t.Fatalf("cue text = %q", cues[0].Text)
	}
}

func TestRunLogsWhisperModelLoadBeforePythonStarts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	t.Cleanup(ShutdownWorkers)
	python, _ := writeFakeWhisperPython(t, dir)
	var logs []string
	outputs := OutputPaths{
		SRT:      filepath.Join(dir, "out.srt"),
		Segments: filepath.Join(dir, "out.segments.txt"),
		JSON:     filepath.Join(dir, "out.json"),
		Text:     filepath.Join(dir, "out.txt"),
	}

	err := Run(context.Background(), executil.Runner{Log: func(line string) { logs = append(logs, line) }}, python, "input.mp3", "large-v3", "en", outputs)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "Loading Whisper model large-v3") || !strings.Contains(joined, "first use may download") {
		t.Fatalf("logs missing first-run model loading hint:\n%s", joined)
	}
}

func TestRunReusesWhisperWorkerAcrossCalls(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	t.Cleanup(ShutdownWorkers)
	python, starts := writeFakeWhisperPython(t, dir)

	for index := 0; index < 2; index++ {
		outputs := OutputPaths{
			SRT:      filepath.Join(dir, fmt.Sprintf("out-%d.srt", index)),
			Segments: filepath.Join(dir, fmt.Sprintf("out-%d.segments.txt", index)),
			JSON:     filepath.Join(dir, fmt.Sprintf("out-%d.json", index)),
			Text:     filepath.Join(dir, fmt.Sprintf("out-%d.txt", index)),
		}
		if err := Run(context.Background(), executil.Runner{}, python, fmt.Sprintf("input-%d.mp3", index), "large-v3", "en", outputs); err != nil {
			t.Fatal(err)
		}
	}

	data, err := os.ReadFile(starts)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(data), "start\n"); count != 1 {
		t.Fatalf("python starts = %d, want one long-lived worker; starts log:\n%s", count, string(data))
	}
}

func writeFakeWhisperPython(t *testing.T, dir string) (string, string) {
	t.Helper()
	starts := filepath.Join(dir, "whisper-starts")
	t.Setenv("WHISPER_STARTS", starts)
	python := filepath.Join(dir, "python")
	if err := os.WriteFile(python, []byte(`#!/bin/sh
printf 'start\n' >> "$WHISPER_STARTS"

write_result() {
  output="$1"
  cat > "$output" <<'JSON'
{"text":"Hello","segments":[{"start":0,"end":1,"text":"Hello","no_speech_prob":0.01,"avg_logprob":-0.1,"compression_ratio":1.0}]}
JSON
}

if [ "$1" = "-u" ]; then
  printf '{"ready":true}\n'
  while IFS= read -r line; do
    output=$(printf '%s\n' "$line" | sed -n 's/.*"output_json":"\([^"]*\)".*/\1/p')
    if [ -z "$output" ]; then
      printf '{"error":"missing output_json"}\n'
      continue
    fi
    write_result "$output"
    printf '{"ok":true}\n'
  done
  exit 0
fi

output=""
for arg do
  output="$arg"
done
write_result "$output"
`), 0o755); err != nil {
		t.Fatal(err)
	}
	return python, starts
}
