// Package cli implements command-line access to the full pipeline and each stage.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/usererror"
)

// Run executes a CLI command and returns a process exit code.
func Run(args []string, projectDir string) int {
	defer transcription.ShutdownWorkers()
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
	case "subtitle":
		err = runSubtitle(ctx, commandArgs, projectDir)
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
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "cancelled")
		return 130
	}
	fmt.Fprintln(os.Stderr, "error:", usererror.Message(err))
	return 1
}

type consoleObserver struct{}

func (consoleObserver) OnLog(line string)                    { fmt.Fprintln(os.Stdout, line) }
func (consoleObserver) OnStep(pipeline.Step, pipeline.State) {}

func runDub(ctx context.Context, args []string, projectDir string) error {
	cfg, err := parseCompleteRunConfig("dub", args, config.ModeDub)
	if err != nil {
		return err
	}
	_, err = (pipeline.Pipeline{ProjectDir: projectDir, Observer: consoleObserver{}}).Run(ctx, cfg)
	return err
}

func runSubtitle(ctx context.Context, args []string, projectDir string) error {
	cfg, err := parseCompleteRunConfig("subtitle", args, config.ModeSubtitle)
	if err != nil {
		return err
	}
	_, err = (pipeline.Pipeline{ProjectDir: projectDir, Observer: consoleObserver{}}).Run(ctx, cfg)
	return err
}

func parseCompleteRunConfig(name string, args []string, mode config.Mode) (config.Config, error) {
	defaults := config.Defaults()
	set := newFlagSet(name)
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
	batch := set.Int("batch-size", defaults.TranslationBatchSize, "subtitles per translation request")
	translationTimeout := set.Duration("translation-timeout", defaults.TranslationTimeout, "translation API request timeout")
	force := set.Bool("force", false, "regenerate intermediate files")
	dataDir := defaults.VoiceDataDir
	keepTemp := false
	subtitleBurnIn := false
	var dataDirFlag *string
	var keepTempFlag *bool
	var burnInFlag *bool
	if mode == config.ModeDub {
		dataDirFlag = set.String("data-dir", envOr("DATA_DIR", defaults.VoiceDataDir), "Piper voice directory")
		keepTempFlag = set.Bool("keep-temp", false, "keep TTS intermediate WAV files")
	} else if mode == config.ModeSubtitle {
		burnInFlag = set.Bool("burn-in", false, "render subtitles into the video pixels instead of adding a selectable subtitle track")
	}
	if err := set.Parse(args); err != nil {
		return config.Config{}, err
	}
	if dataDirFlag != nil {
		dataDir = *dataDirFlag
	}
	if keepTempFlag != nil {
		keepTemp = *keepTempFlag
	}
	if burnInFlag != nil {
		subtitleBurnIn = *burnInFlag
	}
	if strings.TrimSpace(*input) == "" {
		return config.Config{}, fmt.Errorf("--input is required")
	}
	if err := translation.ValidateAPIBase(*apiBase); err != nil {
		return config.Config{}, err
	}
	cfg := defaults
	cfg.Mode = mode
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
	cfg.VoiceDataDir = dataDir
	cfg.SubtitleBurnIn = subtitleBurnIn
	cfg.TranslationBatchSize = *batch
	cfg.TranslationTimeout = *translationTimeout
	cfg.Force = *force
	cfg.KeepTemp = keepTemp
	return cfg, nil
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
	translationTimeout := set.Duration("translation-timeout", defaults.TranslationTimeout, "translation API request timeout")
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
	client := translation.Client{APIBase: *apiBase, APIKey: *apiKey, Model: *model, RequestTimeout: *translationTimeout, Log: func(line string) { fmt.Println(line) }}
	return client.TranslateFile(ctx, *input, *output, lang.TranslationName, *batch)
}

func runSynthesize(ctx context.Context, args []string, projectDir string) error {
	defaults := config.Defaults()
	ttsDefaults := tts.Defaults()
	set := newFlagSet("synthesize")
	flags := addSynthesizeFlags(set, defaults, ttsDefaults)
	set.Usage = func() { printSynthesizeUsage(set.Output(), set) }
	if err := set.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*flags.input) == "" {
		return fmt.Errorf("--input is required")
	}
	if *flags.speaker < -1 {
		return fmt.Errorf("--speaker must be -1 or a non-negative integer")
	}
	if *flags.lengthScale < 0 || *flags.noiseScale < 0 || *flags.noiseW < 0 {
		return fmt.Errorf("--length-scale, --noise-scale, and --noise-w cannot be negative")
	}
	if *flags.sentenceSilence < 0 {
		return fmt.Errorf("--sentence-silence cannot be negative")
	}
	lang, err := language.ByCode(*flags.langCode)
	if err != nil {
		return err
	}
	voice := lang.Voice
	if strings.TrimSpace(*flags.voiceOverride) != "" {
		voice = strings.TrimSpace(*flags.voiceOverride)
	}
	if *flags.output == "" {
		*flags.output = timestampBase(*flags.input) + ".synced.mp3"
	}
	cfg := defaults
	cfg.PythonBin = *flags.python
	cfg.VenvDir = *flags.venv
	cfg.VoiceDataDir = *flags.dataDir
	cfg = cfg.Normalize(projectDir)
	runner := consoleRunner(cfg)
	pythonExe, err := environment.Setup(ctx, runner, cfg, voice)
	if err != nil {
		return err
	}
	options := ttsDefaults
	options.LanguageCode = lang.Code
	options.KeepTemp = *flags.keepTemp
	options.ReportPath = strings.TrimSpace(*flags.reportJSON)
	options.DisableTextNormalization = *flags.noNormalization
	options.SentenceSilence = *flags.sentenceSilence
	options.MinLengthScale = *flags.minLengthScale
	options.MaxLengthScale = *flags.maxLengthScale
	options.MaxAtempo = *flags.maxAtempo
	options.MaxGroupGap = time.Duration(*flags.maxGroupGapMS) * time.Millisecond
	options.MaxGroupDuration = time.Duration(*flags.maxGroupDurationMS) * time.Millisecond
	options.MaxGroupChars = *flags.maxGroupChars
	options.SentenceBreakGap = time.Duration(*flags.sentenceBreakGapMS) * time.Millisecond
	options.MinSentenceGroupDuration = time.Duration(*flags.minSentenceDurationMS) * time.Millisecond
	if *flags.speaker >= 0 {
		value := *flags.speaker
		options.Speaker = &value
	}
	if *flags.lengthScale > 0 {
		value := *flags.lengthScale
		options.LengthScale = &value
	}
	if *flags.noiseScale > 0 {
		value := *flags.noiseScale
		options.NoiseScale = &value
	}
	if *flags.noiseW > 0 {
		value := *flags.noiseW
		options.NoiseW = &value
	}
	return tts.Synthesize(ctx, runner, pythonExe, *flags.input, *flags.output, voice, cfg.VoiceDataDir, options)
}

type synthesizeFlags struct {
	input                 *string
	output                *string
	langCode              *string
	voiceOverride         *string
	python                *string
	venv                  *string
	dataDir               *string
	reportJSON            *string
	keepTemp              *bool
	noNormalization       *bool
	speaker               *int
	maxGroupGapMS         *int
	maxGroupDurationMS    *int
	maxGroupChars         *int
	sentenceBreakGapMS    *int
	minSentenceDurationMS *int
	sentenceSilence       *float64
	lengthScale           *float64
	noiseScale            *float64
	noiseW                *float64
	minLengthScale        *float64
	maxLengthScale        *float64
	maxAtempo             *float64
}

func addSynthesizeFlags(set *flag.FlagSet, defaults config.Config, ttsDefaults tts.Options) synthesizeFlags {
	return synthesizeFlags{
		input:                 set.String("input", "", "translated .srt or .segments.txt file (required)"),
		output:                set.String("output", "", "synchronized audio (.mp3, .m4a, .aac, or .wav)"),
		langCode:              set.String("language", defaults.LanguageCode, "target language"),
		voiceOverride:         set.String("voice", "", "override Piper voice"),
		python:                set.String("python", defaults.PythonBin, "system Python executable"),
		venv:                  set.String("venv", os.Getenv("VENV_DIR"), "Python virtual environment directory"),
		dataDir:               set.String("data-dir", envOr("DATA_DIR", defaults.VoiceDataDir), "Piper voice directory"),
		keepTemp:              set.Bool("keep-temp", false, "keep intermediate WAV files"),
		reportJSON:            set.String("report-json", "", "write per-group timing diagnostics as JSON"),
		noNormalization:       set.Bool("no-text-normalization", false, "disable TTS-oriented text normalization"),
		speaker:               set.Int("speaker", -1, "Piper speaker ID for multi-speaker voices"),
		sentenceSilence:       set.Float64("sentence-silence", ttsDefaults.SentenceSilence, "Piper sentence silence in seconds"),
		lengthScale:           set.Float64("length-scale", 0, "override the voice's base length scale"),
		noiseScale:            set.Float64("noise-scale", 0, "override the voice's noise scale"),
		noiseW:                set.Float64("noise-w", 0, "override the voice's noise width"),
		minLengthScale:        set.Float64("min-length-scale", ttsDefaults.MinLengthScale, "minimum tested Piper length scale"),
		maxLengthScale:        set.Float64("max-length-scale", ttsDefaults.MaxLengthScale, "maximum tested Piper length scale"),
		maxAtempo:             set.Float64("max-atempo", ttsDefaults.MaxAtempo, "maximum ffmpeg tempo correction before trimming"),
		maxGroupGapMS:         set.Int("max-group-gap-ms", int(ttsDefaults.MaxGroupGap/time.Millisecond), "maximum gap inside one TTS group"),
		maxGroupDurationMS:    set.Int("max-group-duration-ms", int(ttsDefaults.MaxGroupDuration/time.Millisecond), "maximum TTS group duration"),
		maxGroupChars:         set.Int("max-group-chars", ttsDefaults.MaxGroupChars, "maximum characters in one TTS group"),
		sentenceBreakGapMS:    set.Int("sentence-break-gap-ms", int(ttsDefaults.SentenceBreakGap/time.Millisecond), "gap that ends a sentence group"),
		minSentenceDurationMS: set.Int("min-sentence-duration-ms", int(ttsDefaults.MinSentenceGroupDuration/time.Millisecond), "duration that ends a completed sentence group"),
	}
}

func printSynthesizeUsage(writer io.Writer, set *flag.FlagSet) {
	fmt.Fprintln(writer, `Usage:
  ai-video-dubber synthesize --input FILE.srt|FILE.segments.txt [options]

Basic options:`)
	printFlagGroup(writer, set, []string{"input", "output", "language", "voice", "report-json"})
	fmt.Fprintln(writer, `
Runtime/cache options:`)
	printFlagGroup(writer, set, []string{"python", "venv", "data-dir", "keep-temp"})
	fmt.Fprintln(writer, `
Advanced voice controls:`)
	printFlagGroup(writer, set, []string{"sentence-silence", "length-scale", "noise-scale", "noise-w", "speaker", "no-text-normalization"})
	fmt.Fprintln(writer, `
Advanced grouping and timing controls:`)
	printFlagGroup(writer, set, []string{"min-length-scale", "max-length-scale", "max-atempo", "max-group-gap-ms", "max-group-duration-ms", "max-group-chars", "sentence-break-gap-ms", "min-sentence-duration-ms"})
	fmt.Fprintln(writer, `
Examples:
  # Slower speech with a little more pause between sentences.
  ai-video-dubber synthesize --input video.pt-BR.srt --language pt-BR --length-scale 1.12 --sentence-silence 0.35

  # Keep separated subtitle cues from being merged into long TTS phrases.
  ai-video-dubber synthesize --input video.pt-BR.srt --max-group-gap-ms 250 --max-group-duration-ms 4500`)
}

func printFlagGroup(writer io.Writer, set *flag.FlagSet, names []string) {
	for _, name := range names {
		item := set.Lookup(name)
		if item == nil {
			continue
		}
		printFlagLine(writer, item)
	}
}

func printFlagLine(writer io.Writer, item *flag.Flag) {
	typeName, usage := flag.UnquoteUsage(item)
	if typeName == "" {
		fmt.Fprintf(writer, "  --%s\n      %s", item.Name, usage)
	} else {
		fmt.Fprintf(writer, "  --%s %s\n      %s", item.Name, typeName, usage)
	}
	if item.DefValue != "" && item.DefValue != "false" {
		fmt.Fprintf(writer, " (default %s)", item.DefValue)
	}
	fmt.Fprintln(writer)
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
  ai-video-dubber subtitle --input VIDEO [options]
  ai-video-dubber extract --input VIDEO [--output AUDIO]
  ai-video-dubber transcribe --input AUDIO [options]
  ai-video-dubber translate --input FILE.srt [options]
  ai-video-dubber synthesize --input FILE.srt|FILE.segments.txt [options]
  ai-video-dubber merge --video VIDEO --audio AUDIO [--output VIDEO]

Run a command with -h to list its options.`)
}
