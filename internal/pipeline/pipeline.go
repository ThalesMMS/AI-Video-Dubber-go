// Package pipeline orchestrates the complete video dubbing workflow.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/audio"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/config"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/environment"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/executil"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/language"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/transcription"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/translation"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/tts"
)

// Step identifies one visible stage by ordinal in the selected run mode.
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

var subtitleStepLabels = []string{
	"Setup environment",
	"Extract audio",
	"Transcribe (Whisper)",
	"Translate subtitles",
	"Create subtitled video",
}

var subtitleBurnInStepLabels = []string{
	"Setup environment",
	"Extract audio",
	"Transcribe (Whisper)",
	"Translate subtitles",
	"Create burned-in video",
}

// StepLabelsForMode returns the visible progress labels for a complete run mode.
func StepLabelsForMode(mode config.Mode) []string {
	return StepLabelsForModeOptions(mode, false)
}

// StepLabelsForModeOptions returns visible progress labels for a run mode and output style.
func StepLabelsForModeOptions(mode config.Mode, subtitleBurnIn bool) []string {
	parsedMode, err := config.ParseMode(string(mode))
	if err != nil {
		parsedMode = config.ModeDub
	}
	if parsedMode == config.ModeSubtitle {
		if subtitleBurnIn {
			return append([]string(nil), subtitleBurnInStepLabels...)
		}
		return append([]string(nil), subtitleStepLabels...)
	}
	return append([]string(nil), StepLabels[:]...)
}

// Observer receives thread-safe logical events; GUI implementations marshal
// updates onto their UI thread.
type Observer interface {
	OnLog(string)
	OnStep(Step, State)
}

// Result contains all deterministic output paths.
type Result struct {
	Paths       audio.Paths
	OutputVideo string
	SubtitleSRT string
}

// Pipeline is reusable by the GUI and CLI.
type Pipeline struct {
	ProjectDir string
	Observer   Observer
	stepLabels []string
	observerMu *sync.Mutex
}

// Run executes the complete local pipeline.
func (p Pipeline) Run(ctx context.Context, rawConfig config.Config) (Result, error) {
	cfg := rawConfig.Normalize(p.ProjectDir)
	p.stepLabels = StepLabelsForModeOptions(cfg.Mode, cfg.SubtitleBurnIn)
	p.observerMu = &sync.Mutex{}
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
	paths, err := audio.BuildPathsForModeOptions(cfg.InputPath, lang.Code, cfg.OutputPath, cfg.Mode, cfg.SubtitleBurnIn)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(paths.FinalVideo), 0o755); err != nil {
		return Result{}, fmt.Errorf("create output directory: %w", err)
	}

	runner := executil.Runner{Log: p.log, Tools: cfg.ToolPaths(), Env: cfg.RuntimeEnv()}
	p.log("")
	if cfg.Mode == config.ModeSubtitle {
		p.log("Subtitling Pipeline")
	} else {
		p.log("Dubbing Pipeline")
	}
	p.log("  Input:    " + paths.Input)
	p.log("  Language: " + lang.TranslationName)
	if cfg.Mode == config.ModeDub {
		p.log("  Voice:    " + lang.Voice)
	}
	p.log("  Output:   " + paths.FinalVideo)
	p.log("")

	fail := func(step Step, err error) (Result, error) {
		p.step(step, StateError)
		if errors.Is(err, context.Canceled) {
			return resultForPaths(paths), err
		}
		return resultForPaths(paths), fmt.Errorf("%s: %w", p.stepLabel(step), err)
	}
	failStarted := func(step Step, err error) (Result, error) {
		if errors.Is(err, context.Canceled) {
			return resultForPaths(paths), err
		}
		return resultForPaths(paths), fmt.Errorf("%s: %w", p.stepLabel(step), err)
	}
	translateWillRun, err := shouldRun(cfg.Force, paths.TranslatedSRT)
	if err != nil {
		return fail(StepTranslate, err)
	}
	translationModel := cfg.Model
	if translateWillRun {
		p.log("Checking translation API connectivity...")
		client := translation.Client{APIBase: cfg.APIBase, APIKey: cfg.APIKey, Model: translationModel, RequestTimeout: cfg.TranslationTimeout, BatchParallelism: cfg.TranslationParallelism, Log: p.log}
		model, err := client.Preflight(ctx)
		if err != nil {
			return fail(StepTranslate, fmt.Errorf("translation API preflight: %w", err))
		}
		translationModel = model
	}

	runStep, err := shouldRun(cfg.Force, paths.ExtractedAudio)
	if err != nil {
		return fail(StepExtract, err)
	}
	pythonExe, failedStep, err := p.runSetupAndExtract(ctx, runner, cfg, lang, paths, runStep)
	if err != nil {
		return failStarted(failedStep, err)
	}

	current := StepTranscribe
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
	if translateWillRun {
		client := translation.Client{APIBase: cfg.APIBase, APIKey: cfg.APIKey, Model: translationModel, RequestTimeout: cfg.TranslationTimeout, Log: p.log}
		if err := client.TranslateFile(ctx, paths.TranscriptSRT, paths.TranslatedSRT, lang.TranslationName, cfg.TranslationBatchSize); err != nil {
			return fail(current, err)
		}
	} else {
		p.log("Skipped: translated subtitles already exist. Use --force to regenerate them.")
	}
	p.finish(current)

	if cfg.Mode == config.ModeSubtitle {
		current = StepSynthesize
		p.begin(current)
		runStep, err = shouldRun(cfg.Force, paths.FinalVideo)
		if err != nil {
			return fail(current, err)
		}
		if runStep {
			if cfg.SubtitleBurnIn {
				err = audio.BurnInSubtitles(ctx, runner, paths.Input, paths.TranslatedSRT, paths.FinalVideo)
			} else {
				err = audio.EmbedSubtitles(ctx, runner, paths.Input, paths.TranslatedSRT, paths.FinalVideo)
			}
			if err != nil {
				return fail(current, err)
			}
		} else {
			p.log("Skipped: final video already exists. Use --force to regenerate it.")
		}
		p.finish(current)

		p.log("")
		p.log("Pipeline complete!")
		p.log("  Output: " + paths.FinalVideo)
		p.log("  Subtitles: " + paths.TranslatedSRT)
		return resultForPaths(paths), nil
	}

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
		options.Parallelism = cfg.TTSParallelism
		if err := tts.Synthesize(ctx, runner, pythonExe, paths.TranslatedSRT, paths.SyncedAudio, lang.Voice, cfg.VoiceDataDir, options); err != nil {
			return fail(current, err)
		}
	} else {
		p.log("Skipped: synchronized audio already exists. Use --force to regenerate it.")
	}
	p.finish(current)

	current = StepMerge
	p.begin(current)
	runStep, err = shouldRun(cfg.Force, paths.FinalVideo)
	if err != nil {
		return fail(current, err)
	}
	if runStep {
		if err := audio.MergeVideoAudio(ctx, runner, paths.Input, paths.SyncedAudio, paths.FinalVideo); err != nil {
			return fail(current, err)
		}
	} else {
		p.log("Skipped: final video already exists. Use --force to regenerate it.")
	}
	p.finish(current)

	p.log("")
	p.log("Pipeline complete!")
	p.log("  Output: " + paths.FinalVideo)
	return resultForPaths(paths), nil
}

func resultForPaths(paths audio.Paths) Result {
	return Result{Paths: paths, OutputVideo: paths.FinalVideo, SubtitleSRT: paths.TranslatedSRT}
}

type startupStepResult struct {
	step      Step
	pythonExe string
	err       error
}

func (p Pipeline) runSetupAndExtract(ctx context.Context, runner executil.Runner, cfg config.Config, lang language.Language, paths audio.Paths, extractWillRun bool) (string, Step, error) {
	if !extractWillRun {
		p.begin(StepSetup)
		pythonExe, err := p.setupEnvironment(ctx, runner, cfg, lang)
		if err != nil {
			p.step(StepSetup, StateError)
			return "", StepSetup, err
		}
		p.finish(StepSetup)

		p.begin(StepExtract)
		p.log("Skipped: extracted audio already exists. Use --force to regenerate it.")
		p.finish(StepExtract)
		return pythonExe, StepSetup, nil
	}

	p.begin(StepSetup)
	p.begin(StepExtract)

	stepCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan startupStepResult, 2)
	go func() {
		pythonExe, err := p.setupEnvironment(stepCtx, runner, cfg, lang)
		results <- startupStepResult{step: StepSetup, pythonExe: pythonExe, err: err}
	}()
	go func() {
		err := audio.ExtractMP3(stepCtx, runner, paths.Input, paths.ExtractedAudio)
		results <- startupStepResult{step: StepExtract, err: err}
	}()

	var pythonExe string
	var firstStep Step
	var firstErr error
	for i := 0; i < 2; i++ {
		result := <-results
		if result.err != nil {
			p.step(result.step, StateError)
			if firstErr == nil {
				firstStep = result.step
				firstErr = result.err
				cancel()
			}
			continue
		}
		if result.pythonExe != "" {
			pythonExe = result.pythonExe
		}
		p.finish(result.step)
	}
	if firstErr != nil {
		return "", firstStep, firstErr
	}
	return pythonExe, StepSetup, nil
}

func (p Pipeline) setupEnvironment(ctx context.Context, runner executil.Runner, cfg config.Config, lang language.Language) (string, error) {
	if cfg.Mode == config.ModeDub {
		return environment.Setup(ctx, runner, cfg, lang.Voice)
	}
	pythonExe, err := environment.SetupWhisperRuntime(ctx, runner, cfg)
	if err != nil {
		return "", err
	}
	p.log("Environment ready.")
	return pythonExe, nil
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
	p.log(fmt.Sprintf("  Step %d/%d — %s", int(step)+1, len(p.progressLabels()), p.stepLabel(step)))
	p.log("═══════════════════════════════════════════════════════════════")
	p.step(step, StateRunning)
}

func (p Pipeline) finish(step Step) { p.step(step, StateDone) }

func (p Pipeline) log(line string) {
	if p.Observer != nil {
		if p.observerMu != nil {
			p.observerMu.Lock()
			defer p.observerMu.Unlock()
		}
		p.Observer.OnLog(line)
	}
}

func (p Pipeline) step(step Step, state State) {
	if p.Observer != nil {
		if p.observerMu != nil {
			p.observerMu.Lock()
			defer p.observerMu.Unlock()
		}
		p.Observer.OnStep(step, state)
	}
}

func (p Pipeline) progressLabels() []string {
	if len(p.stepLabels) == 0 {
		return StepLabels[:]
	}
	return p.stepLabels
}

func (p Pipeline) stepLabel(step Step) string {
	labels := p.progressLabels()
	if int(step) < 0 || int(step) >= len(labels) {
		return fmt.Sprintf("Step %d", int(step)+1)
	}
	return labels[step]
}
