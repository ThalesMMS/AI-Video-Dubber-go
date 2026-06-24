// Package cli implements command-line access to the full pipeline and each stage.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/audio"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/config"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/environment"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/executil"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/language"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/pipeline"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/transcription"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/translation"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/tts"
)

// Run executes a CLI command and returns a process exit code.
func Run(args []string, projectDir string) int {
	if len(args) == 0 {
		printUsage(os.Stdout)
		return 0
	}
	command := args[0]
	commandArgs := args[1:]
	if strings.HasPrefix(command, "-") {
		command = "dub"
		commandArgs = args
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var err error
	switch command {
	case "dub":
		err = runDub(ctx, commandArgs, projectDir)
	case "extract":
		err = runExtract(ctx, commandArgs, projectDir)
	case "transcribe":
		err = runTranscribe(ctx, commandArgs, projectDir)
	case "translate":
		err = runTranslate(ctx, commandArgs)
	case "synthesize":
		err = runSynthesize(ctx, commandArgs, projectDir)
	case "merge":
		err = runMerge(ctx, commandArgs, projectDir)
	case "help", "--help", "-h":
		printUsage(os.Stdout)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", command)
		printUsage(os.Stderr)
		return 2
	}
	if err == nil {
		return 0
	}
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "cancelled")
		return 130
	}
	fmt.Fprintln(os.Stderr, "error:", err)
	return 1
}

type consoleObserver struct{}

func (consoleObserver) OnLog(line string)                    { fmt.Fprintln(os.Stdout, line) }
func (consoleObserver) OnStep(pipeline.Step, pipeline.State) {}

func runDub(ctx context.Context, args []string, projectDir string) error {
	defaults := config.Defaults()
	set := newFlagSet("dub")
	input := set.String("input", "", "input video (required)")
	output := set.String("output", "", "final output video")
	lang := set.String("language", defaults.LanguageCode, "target language: pt-BR, es, fr, de, it")
	apiBase := set.String("api-base", envOr("LLM_API_BASE", defaults.APIBase), "OpenAI-compatible API base URL")
	apiKey := set.String("api-key", envOr("LLM_API_KEY", defaults.APIKey), "API key")
	model := set.String("model", os.Getenv("LLM_MODEL"), "LLM model (blank auto-detects)")
	whisperModel := set.String("whisper-model", envOr("WHISPER_MODEL", defaults.WhisperModel), "local Whisper model")
	sourceLanguage := set.String("source-language", defaults.SourceLanguage, "source audio language code")
	python := set.String("python", defaults.PythonBin, "system Python executable")
	venv := set.String("venv", os.Getenv("VENV_DIR"), "Python virtual environment directory")
	dataDir := set.String("data-dir", envOr("DATA_DIR", defaults.VoiceDataDir), "Piper voice directory")
	force := set.Bool("force", false, "regenerate intermediate files")
	keepTemp := set.Bool("keep-temp", false, "keep TTS intermediate WAV files")
	if err := set.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*input) == "" {
		return fmt.Errorf("--input is required")
	}
	cfg := defaults
	cfg.InputPath = *input
	cfg.OutputPath = *output
	cfg.LanguageCode = *lang
	cfg.APIBase = *apiBase
	cfg.APIKey = *apiKey
	cfg.Model = *model
	cfg.WhisperModel = *whisperModel
	cfg.SourceLanguage = *sourceLanguage
	cfg.PythonBin = *python
	cfg.VenvDir = *venv
	cfg.VoiceDataDir = *dataDir
	cfg.Force = *force
	cfg.KeepTemp = *keepTemp
	_, err := (pipeline.Pipeline{ProjectDir: projectDir, Observer: consoleObserver{}}).Run(ctx, cfg)
	return err
}

func runExtract(ctx context.Context, args []string, projectDir string) error {
	set := newFlagSet("extract")
	input := set.String("input", "", "input video (required)")
	output := set.String("output", "", "output MP3")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *input == "" {
		return fmt.Errorf("--input is required")
	}
	if *output == "" {
		*output = strings.TrimSuffix(*input, filepath.Ext(*input)) + ".mp3"
	}
	cfg := config.Defaults().Normalize(projectDir)
	return audio.ExtractMP3(ctx, consoleRunner(cfg), *input, *output)
}

func runTranscribe(ctx context.Context, args []string, projectDir string) error {
	defaults := config.Defaults()
	set := newFlagSet("transcribe")
	input := set.String("input", "", "input audio/video (required)")
	prefix := set.String("output-prefix", "", "output prefix without extension")
	model := set.String("model", envOr("WHISPER_MODEL", defaults.WhisperModel), "Whisper model")
	sourceLanguage := set.String("language", defaults.SourceLanguage, "source language code")
	python := set.String("python", defaults.PythonBin, "system Python executable")
	venv := set.String("venv", os.Getenv("VENV_DIR"), "Python virtual environment directory")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *input == "" {
		return fmt.Errorf("--input is required")
	}
	if *prefix == "" {
		*prefix = strings.TrimSuffix(*input, filepath.Ext(*input))
	}
	cfg := defaults
	cfg.PythonBin = *python
	cfg.VenvDir = *venv
	cfg = cfg.Normalize(projectDir)
	runner := consoleRunner(cfg)
	pythonExe, err := environment.SetupRuntime(ctx, runner, cfg)
	if err != nil {
		return err
	}
	return transcription.Run(ctx, runner, pythonExe, *input, *model, *sourceLanguage, transcription.OutputPaths{
		SRT: *prefix + ".srt", Segments: *prefix + ".segments.txt",
		JSON: *prefix + ".json", Text: *prefix + ".txt",
	})
}

func runTranslate(ctx context.Context, args []string) error {
	defaults := config.Defaults()
	set := newFlagSet("translate")
	input := set.String("input", "", "input SRT (required)")
	output := set.String("output", "", "translated SRT")
	langCode := set.String("language", defaults.LanguageCode, "target language")
	apiBase := set.String("api-base", envOr("LLM_API_BASE", defaults.APIBase), "OpenAI-compatible API base URL")
	apiKey := set.String("api-key", envOr("LLM_API_KEY", defaults.APIKey), "API key")
	model := set.String("model", os.Getenv("LLM_MODEL"), "LLM model (blank auto-detects)")
	batch := set.Int("batch-size", defaults.TranslationBatchSize, "subtitles per request")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *input == "" {
		return fmt.Errorf("--input is required")
	}
	lang, err := language.ByCode(*langCode)
	if err != nil {
		return err
	}
	if *output == "" {
		*output = strings.TrimSuffix(*input, filepath.Ext(*input)) + "." + lang.Code + ".srt"
	}
	client := translation.Client{APIBase: *apiBase, APIKey: *apiKey, Model: *model, Log: func(line string) { fmt.Println(line) }}
	return client.TranslateFile(ctx, *input, *output, lang.TranslationName, *batch)
}

func runSynthesize(ctx context.Context, args []string, projectDir string) error {
	defaults := config.Defaults()
	ttsDefaults := tts.Defaults()
	set := newFlagSet("synthesize")
	input := set.String("input", "", "translated .srt or .segments.txt file (required)")
	output := set.String("output", "", "synchronized audio (.mp3, .m4a, .aac, or .wav)")
	langCode := set.String("language", defaults.LanguageCode, "target language")
	voiceOverride := set.String("voice", "", "override Piper voice")
	python := set.String("python", defaults.PythonBin, "system Python executable")
	venv := set.String("venv", os.Getenv("VENV_DIR"), "Python virtual environment directory")
	dataDir := set.String("data-dir", envOr("DATA_DIR", defaults.VoiceDataDir), "Piper voice directory")
	keepTemp := set.Bool("keep-temp", false, "keep intermediate WAV files")
	reportJSON := set.String("report-json", "", "write per-group timing diagnostics as JSON")
	noNormalization := set.Bool("no-text-normalization", false, "disable TTS-oriented text normalization")
	speaker := set.Int("speaker", -1, "Piper speaker ID for multi-speaker voices")
	sentenceSilence := set.Float64("sentence-silence", ttsDefaults.SentenceSilence, "Piper sentence silence in seconds")
	lengthScale := set.Float64("length-scale", 0, "override the voice's base length scale")
	noiseScale := set.Float64("noise-scale", 0, "override the voice's noise scale")
	noiseW := set.Float64("noise-w", 0, "override the voice's noise width")
	minLengthScale := set.Float64("min-length-scale", ttsDefaults.MinLengthScale, "minimum tested Piper length scale")
	maxLengthScale := set.Float64("max-length-scale", ttsDefaults.MaxLengthScale, "maximum tested Piper length scale")
	maxAtempo := set.Float64("max-atempo", ttsDefaults.MaxAtempo, "maximum ffmpeg tempo correction before trimming")
	maxGroupGapMS := set.Int("max-group-gap-ms", int(ttsDefaults.MaxGroupGap/time.Millisecond), "maximum gap inside one TTS group")
	maxGroupDurationMS := set.Int("max-group-duration-ms", int(ttsDefaults.MaxGroupDuration/time.Millisecond), "maximum TTS group duration")
	maxGroupChars := set.Int("max-group-chars", ttsDefaults.MaxGroupChars, "maximum characters in one TTS group")
	sentenceBreakGapMS := set.Int("sentence-break-gap-ms", int(ttsDefaults.SentenceBreakGap/time.Millisecond), "gap that ends a sentence group")
	minSentenceDurationMS := set.Int("min-sentence-duration-ms", int(ttsDefaults.MinSentenceGroupDuration/time.Millisecond), "duration that ends a completed sentence group")
	if err := set.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*input) == "" {
		return fmt.Errorf("--input is required")
	}
	if *speaker < -1 {
		return fmt.Errorf("--speaker must be -1 or a non-negative integer")
	}
	if *lengthScale < 0 || *noiseScale < 0 || *noiseW < 0 {
		return fmt.Errorf("--length-scale, --noise-scale, and --noise-w cannot be negative")
	}
	if *sentenceSilence < 0 {
		return fmt.Errorf("--sentence-silence cannot be negative")
	}
	lang, err := language.ByCode(*langCode)
	if err != nil {
		return err
	}
	voice := lang.Voice
	if strings.TrimSpace(*voiceOverride) != "" {
		voice = strings.TrimSpace(*voiceOverride)
	}
	if *output == "" {
		*output = timestampBase(*input) + ".synced.mp3"
	}
	cfg := defaults
	cfg.PythonBin = *python
	cfg.VenvDir = *venv
	cfg.VoiceDataDir = *dataDir
	cfg = cfg.Normalize(projectDir)
	runner := consoleRunner(cfg)
	pythonExe, err := environment.Setup(ctx, runner, cfg, voice)
	if err != nil {
		return err
	}
	options := ttsDefaults
	options.LanguageCode = lang.Code
	options.KeepTemp = *keepTemp
	options.ReportPath = strings.TrimSpace(*reportJSON)
	options.DisableTextNormalization = *noNormalization
	options.SentenceSilence = *sentenceSilence
	options.MinLengthScale = *minLengthScale
	options.MaxLengthScale = *maxLengthScale
	options.MaxAtempo = *maxAtempo
	options.MaxGroupGap = time.Duration(*maxGroupGapMS) * time.Millisecond
	options.MaxGroupDuration = time.Duration(*maxGroupDurationMS) * time.Millisecond
	options.MaxGroupChars = *maxGroupChars
	options.SentenceBreakGap = time.Duration(*sentenceBreakGapMS) * time.Millisecond
	options.MinSentenceGroupDuration = time.Duration(*minSentenceDurationMS) * time.Millisecond
	if *speaker >= 0 {
		value := *speaker
		options.Speaker = &value
	}
	if *lengthScale > 0 {
		value := *lengthScale
		options.LengthScale = &value
	}
	if *noiseScale > 0 {
		value := *noiseScale
		options.NoiseScale = &value
	}
	if *noiseW > 0 {
		value := *noiseW
		options.NoiseW = &value
	}
	return tts.Synthesize(ctx, runner, pythonExe, *input, *output, voice, cfg.VoiceDataDir, options)
}

func timestampBase(path string) string {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".segments.txt") {
		return path[:len(path)-len(".segments.txt")]
	}
	return strings.TrimSuffix(path, filepath.Ext(path))
}

func runMerge(ctx context.Context, args []string, projectDir string) error {
	set := newFlagSet("merge")
	video := set.String("video", "", "source video (required)")
	audioPath := set.String("audio", "", "replacement audio (required)")
	output := set.String("output", "", "output video")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *video == "" || *audioPath == "" {
		return fmt.Errorf("--video and --audio are required")
	}
	if *output == "" {
		*output = strings.TrimSuffix(*video, filepath.Ext(*video)) + ".dubbed.mp4"
	}
	cfg := config.Defaults().Normalize(projectDir)
	return audio.MergeVideoAudio(ctx, consoleRunner(cfg), *video, *audioPath, *output)
}

func consoleRunner(cfg config.Config) executil.Runner {
	return executil.Runner{
		Log:   func(line string) { fmt.Println(line) },
		Tools: cfg.ToolPaths(),
		Env:   cfg.RuntimeEnv(),
	}
}

func newFlagSet(name string) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(os.Stderr)
	return set
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func printUsage(writer *os.File) {
	fmt.Fprintln(writer, `AI Video Dubber (Go/Fyne)

Usage:
  ai-video-dubber                         Open the graphical interface
  ai-video-dubber dub --input VIDEO [options]
  ai-video-dubber extract --input VIDEO [--output AUDIO]
  ai-video-dubber transcribe --input AUDIO [options]
  ai-video-dubber translate --input FILE.srt [options]
  ai-video-dubber synthesize --input FILE.srt|FILE.segments.txt [options]
  ai-video-dubber merge --video VIDEO --audio AUDIO [--output VIDEO]

Run a command with -h to list its options.`)
}
