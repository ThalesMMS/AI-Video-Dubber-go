package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/config"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/tts"
)

func TestParseCompleteRunConfigSetsSubtitleMode(t *testing.T) {
	projectDir := t.TempDir()
	output := filepath.Join(projectDir, "captioned.mp4")

	cfg, err := parseCompleteRunConfig("subtitle", []string{
		"--input", "video.mp4",
		"--output", output,
		"--language", "fr",
		"--api-base", "http://localhost:9000",
		"--api-key", "secret",
		"--model", "translator",
		"--whisper-model", "small",
		"--source-language", "en",
		"--python", "python3",
		"--venv", filepath.Join(projectDir, ".venv"),
		"--burn-in",
		"--force",
	}, config.ModeSubtitle)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != config.ModeSubtitle {
		t.Fatalf("Mode = %q, want %q", cfg.Mode, config.ModeSubtitle)
	}
	if cfg.InputPath != "video.mp4" || cfg.OutputPath != output || cfg.LanguageCode != "fr" {
		t.Fatalf("unexpected core config: %#v", cfg)
	}
	if cfg.APIBase != "http://localhost:9000" || cfg.APIKey != "secret" || cfg.Model != "translator" {
		t.Fatalf("unexpected translation config: %#v", cfg)
	}
	if cfg.WhisperModel != "small" || cfg.SourceLanguage != "en" || !cfg.Force {
		t.Fatalf("unexpected runtime config: %#v", cfg)
	}
	if !cfg.SubtitleBurnIn {
		t.Fatalf("SubtitleBurnIn = false, want true: %#v", cfg)
	}
}

func TestParseCompleteRunConfigRequiresInput(t *testing.T) {
	if _, err := parseCompleteRunConfig("subtitle", nil, config.ModeSubtitle); err == nil {
		t.Fatal("parseCompleteRunConfig accepted a missing input")
	}
}

func TestParseCompleteRunConfigKeepsDubTTSFlags(t *testing.T) {
	projectDir := t.TempDir()
	dataDir := filepath.Join(projectDir, "voices")

	cfg, err := parseCompleteRunConfig("dub", []string{
		"--input", "video.mp4",
		"--data-dir", dataDir,
		"--keep-temp",
	}, config.ModeDub)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VoiceDataDir != dataDir || !cfg.KeepTemp {
		t.Fatalf("unexpected TTS config: %#v", cfg)
	}
}

func TestRunSubtitleHelpReturnsSuccess(t *testing.T) {
	if code := Run([]string{"subtitle", "-h"}, t.TempDir()); code != 0 {
		t.Fatalf("Run(subtitle -h) exit code = %d, want 0", code)
	}
}

func TestRunSynthesizeHelpReturnsSuccess(t *testing.T) {
	if code := Run([]string{"synthesize", "-h"}, t.TempDir()); code != 0 {
		t.Fatalf("Run(synthesize -h) exit code = %d, want 0", code)
	}
}

func TestSynthesizeUsageGroupsAdvancedFlagsAndShowsExamples(t *testing.T) {
	set := newFlagSet("synthesize")
	addSynthesizeFlags(set, config.Defaults(), tts.Defaults())
	var builder strings.Builder

	printSynthesizeUsage(&builder, set)

	help := builder.String()
	for _, want := range []string{
		"Basic options:",
		"Runtime/cache options:",
		"Advanced voice controls:",
		"Advanced grouping and timing controls:",
		"--input string\n      translated .srt or .segments.txt file (required)",
		"--keep-temp\n      keep intermediate WAV files",
		"--length-scale 1.12 --sentence-silence 0.35",
		"--max-group-gap-ms 250 --max-group-duration-ms 4500",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("synthesize help missing %q:\n%s", want, help)
		}
	}
}
