package cli

import (
	"path/filepath"
	"testing"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/config"
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
