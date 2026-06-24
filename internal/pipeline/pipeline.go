// Package pipeline orchestrates the complete video dubbing workflow.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/audio"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/config"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/environment"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/executil"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/language"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/transcription"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/translation"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/tts"
)

// Step identifies one of the six GUI-visible stages.
type Step int

const (
	StepSetup Step = iota
	StepExtract
	StepTranscribe
	StepTranslate
	StepSynthesize
	StepMerge
	StepCount
)

// State represents a pipeline step state.
type State string

const (
	StatePending State = "pending"
	StateRunning State = "running"
	StateDone    State = "done"
	StateError   State = "error"
)

var StepLabels = [StepCount]string{
	"Setup environment",
	"Extract audio",
	"Transcribe (Whisper)",
	"Translate subtitles",
	"Generate dubbed audio",
	"Merge final video",
}

// Observer receives thread-safe logical events; GUI implementations marshal
// updates onto their UI thread.
type Observer interface {
	OnLog(string)
	OnStep(Step, State)
}

// Result contains all deterministic output paths.
type Result struct {
	Paths audio.Paths
}

// Pipeline is reusable by the GUI and CLI.
type Pipeline struct {
	ProjectDir string
	Observer   Observer
}

// Run executes the complete local pipeline.
func (p Pipeline) Run(ctx context.Context, rawConfig config.Config) (Result, error) {
	cfg := rawConfig.Normalize(p.ProjectDir)
	lang, err := language.ByCode(cfg.LanguageCode)
	if err != nil {
		return Result{}, err
	}
	inputInfo, err := os.Stat(cfg.InputPath)
	if err != nil {
		return Result{}, fmt.Errorf("input video: %w", err)
	}
	if !inputInfo.Mode().IsRegular() {
		return Result{}, fmt.Errorf("input is not a regular file: %s", cfg.InputPath)
	}
	paths, err := audio.BuildPaths(cfg.InputPath, lang.Code, cfg.OutputPath)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(paths.FinalVideo), 0o755); err != nil {
		return Result{}, fmt.Errorf("create output directory: %w", err)
	}

	runner := executil.Runner{Log: p.log, Tools: cfg.ToolPaths(), Env: cfg.RuntimeEnv()}
	p.log("")
	p.log("Dubbing Pipeline")
	p.log("  Input:    " + paths.Input)
	p.log("  Language: " + lang.TranslationName)
	p.log("  Voice:    " + lang.Voice)
	p.log("  Output:   " + paths.FinalVideo)
	p.log("")

	current := StepSetup
	fail := func(step Step, err error) (Result, error) {
		p.step(step, StateError)
		if errors.Is(err, context.Canceled) {
			return Result{Paths: paths}, err
		}
		return Result{Paths: paths}, fmt.Errorf("%s: %w", StepLabels[step], err)
	}

	p.begin(current)
	pythonExe, err := environment.Setup(ctx, runner, cfg, lang.Voice)
	if err != nil {
		return fail(current, err)
	}
	p.finish(current)

	current = StepExtract
	p.begin(current)
	runStep, err := shouldRun(cfg.Force, paths.ExtractedAudio)
	if err != nil {
		return fail(current, err)
	}
	if runStep {
		if err := audio.ExtractMP3(ctx, runner, paths.Input, paths.ExtractedAudio); err != nil {
			return fail(current, err)
		}
	} else {
		p.log("Skipped: extracted audio already exists. Use --force to regenerate it.")
	}
	p.finish(current)

	current = StepTranscribe
	p.begin(current)
	runStep, err = shouldRun(cfg.Force, paths.TranscriptSRT)
	if err != nil {
		return fail(current, err)
	}
	if runStep {
		err := transcription.Run(ctx, runner, pythonExe, paths.ExtractedAudio, cfg.WhisperModel, cfg.SourceLanguage, transcription.OutputPaths{
			SRT: paths.TranscriptSRT, Segments: paths.SegmentsTXT,
			JSON: paths.TranscriptJSON, Text: paths.TranscriptTXT,
		})
		if err != nil {
			return fail(current, err)
		}
	} else {
		p.log("Skipped: transcript already exists. Use --force to regenerate it.")
	}
	p.finish(current)

	current = StepTranslate
	p.begin(current)
	runStep, err = shouldRun(cfg.Force, paths.TranslatedSRT)
	if err != nil {
		return fail(current, err)
	}
	if runStep {
		client := translation.Client{APIBase: cfg.APIBase, APIKey: cfg.APIKey, Model: cfg.Model, Log: p.log}
		if err := client.TranslateFile(ctx, paths.TranscriptSRT, paths.TranslatedSRT, lang.TranslationName, cfg.TranslationBatchSize); err != nil {
			return fail(current, err)
		}
	} else {
		p.log("Skipped: translated subtitles already exist. Use --force to regenerate them.")
	}
	p.finish(current)

	current = StepSynthesize
	p.begin(current)
	runStep, err = shouldRun(cfg.Force, paths.SyncedAudio)
	if err != nil {
		return fail(current, err)
	}
	if runStep {
		options := tts.Defaults()
		options.LanguageCode = lang.Code
		options.KeepTemp = cfg.KeepTemp
		if err := tts.Synthesize(ctx, runner, pythonExe, paths.TranslatedSRT, paths.SyncedAudio, lang.Voice, cfg.VoiceDataDir, options); err != nil {
			return fail(current, err)
		}
	} else {
		p.log("Skipped: synchronized audio already exists. Use --force to regenerate it.")
	}
	p.finish(current)

	current = StepMerge
	p.begin(current)
	if err := audio.MergeVideoAudio(ctx, runner, paths.Input, paths.SyncedAudio, paths.FinalVideo); err != nil {
		return fail(current, err)
	}
	p.finish(current)

	p.log("")
	p.log("Pipeline complete!")
	p.log("  Output: " + paths.FinalVideo)
	return Result{Paths: paths}, nil
}

func shouldRun(force bool, path string) (bool, error) {
	if force {
		return true, nil
	}
	info, err := os.Stat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return false, fmt.Errorf("expected a regular output file at %s", path)
		}
		return false, nil
	}
	if os.IsNotExist(err) {
		return true, nil
	}
	return false, fmt.Errorf("inspect intermediate output %q: %w", path, err)
}

func (p Pipeline) begin(step Step) {
	p.log("")
	p.log("═══════════════════════════════════════════════════════════════")
	p.log(fmt.Sprintf("  Step %d/%d — %s", int(step)+1, int(StepCount), StepLabels[step]))
	p.log("═══════════════════════════════════════════════════════════════")
	p.step(step, StateRunning)
}

func (p Pipeline) finish(step Step) { p.step(step, StateDone) }

func (p Pipeline) log(line string) {
	if p.Observer != nil {
		p.Observer.OnLog(line)
	}
}

func (p Pipeline) step(step Step, state State) {
	if p.Observer != nil {
		p.Observer.OnStep(step, state)
	}
}
