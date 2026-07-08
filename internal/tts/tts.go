// Package tts generates synchronized local narration with Piper TTS.
package tts

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/audio"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/executil"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/srt"
)

const privateReportFileMode os.FileMode = 0o600

var piperVoicesIndexURL = "https://huggingface.co/rhasspy/piper-voices/raw/main/voices.json"

const piperWorkerScript = `
import inspect
import json
import sys
import traceback
import wave

try:
    from piper import PiperVoice
    try:
        from piper import SynthesisConfig
    except Exception:
        SynthesisConfig = None
except Exception:
    from piper.voice import PiperVoice
    SynthesisConfig = None

model_path, config_path = sys.argv[1:3]
voice = PiperVoice.load(model_path, config_path)
print(json.dumps({"ready": True}), flush=True)

def synthesize_request(request):
    speaker = request.get("speaker")
    length_scale = request.get("length_scale")
    noise_scale = request.get("noise_scale")
    noise_w = request.get("noise_w")
    sentence_silence = request.get("sentence_silence", 0.0)
    with wave.open(request["output_path"], "wb") as wav_file:
        synthesize = getattr(voice, "synthesize", None)
        if synthesize is not None and "wav_file" in inspect.signature(synthesize).parameters:
            synthesize(
                request["text"],
                wav_file,
                speaker_id=speaker,
                length_scale=length_scale,
                noise_scale=noise_scale,
                noise_w=noise_w,
                sentence_silence=sentence_silence,
            )
            return

        synthesize_wav = getattr(voice, "synthesize_wav")
        syn_config = None
        if SynthesisConfig is not None:
            values = {
                "speaker_id": speaker,
                "length_scale": length_scale,
                "noise_scale": noise_scale,
                "noise_w_scale": noise_w,
                "sentence_silence": sentence_silence,
            }
            accepted = inspect.signature(SynthesisConfig).parameters
            kwargs = {key: value for key, value in values.items() if value is not None and key in accepted}
            syn_config = SynthesisConfig(**kwargs)

        if syn_config is not None and "syn_config" in inspect.signature(synthesize_wav).parameters:
            synthesize_wav(request["text"], wav_file, syn_config=syn_config)
        else:
            synthesize_wav(request["text"], wav_file)

for line in sys.stdin:
    try:
        synthesize_request(json.loads(line))
        print(json.dumps({"ok": True}), flush=True)
    except Exception as exc:
        print(json.dumps({"error": str(exc), "traceback": traceback.format_exc()}), flush=True)
`

var (
	sentenceEnd     = regexp.MustCompile(`[.!?…:][\]\)"'”’]*\s*$`)
	pauseEnd        = regexp.MustCompile(`[,;:!?….][\]\)"'”’]*\s*$`)
	spaces          = regexp.MustCompile(`\s+`)
	ellipsis        = regexp.MustCompile(`\.{3,}`)
	decimal         = regexp.MustCompile(`(\d)[,.](\d)`)
	spaceBeforePunc = regexp.MustCompile(`\s+([,.;:!?…])`)
	frontEnd        = regexp.MustCompile(`(?i)\bfront-end\b`)
)

// Options controls synthesis grouping and timing correction.
type Options struct {
	LanguageCode             string
	Speaker                  *int
	SentenceSilence          float64
	LengthScale              *float64
	NoiseScale               *float64
	NoiseW                   *float64
	MinLengthScale           float64
	MaxLengthScale           float64
	MaxAtempo                float64
	MaxGroupGap              time.Duration
	MaxGroupDuration         time.Duration
	MaxGroupChars            int
	SentenceBreakGap         time.Duration
	MinSentenceGroupDuration time.Duration
	DisableTextNormalization bool
	KeepTemp                 bool
	ReportPath               string
	Parallelism              int
}

// Defaults mirrors the quality-oriented settings of the Python implementation.
func Defaults() Options {
	return Options{
		SentenceSilence:          0.04,
		MinLengthScale:           0.72,
		MaxLengthScale:           1.10,
		MaxAtempo:                1.12,
		MaxGroupGap:              350 * time.Millisecond,
		MaxGroupDuration:         12 * time.Second,
		MaxGroupChars:            300,
		SentenceBreakGap:         180 * time.Millisecond,
		MinSentenceGroupDuration: 3200 * time.Millisecond,
		Parallelism:              runtime.NumCPU(),
	}
}

// Group is a sequence of nearby subtitle cues synthesized as one phrase.
type Group struct {
	ID    int
	Cues  []srt.Cue
	Text  string
	Start time.Duration
	End   time.Duration
}

// Duration returns the target audio slot for the group.
func (g Group) Duration() time.Duration { return g.End - g.Start }

// GroupReport contains per-group timing diagnostics compatible with the
// Python implementation's optional JSON report.
type GroupReport struct {
	ID                int     `json:"gid"`
	CueIndices        []int   `json:"cue_indices"`
	StartSeconds      float64 `json:"start"`
	EndSeconds        float64 `json:"end"`
	TargetSeconds     float64 `json:"target_duration"`
	Text              string  `json:"text"`
	ChosenLengthScale float64 `json:"chosen_length_scale"`
	RawSeconds        float64 `json:"raw_duration"`
	SpeedupApplied    float64 `json:"speedup_applied"`
	TrimmedFallback   bool    `json:"trimmed_fallback"`
	SampleRate        int     `json:"sample_rate"`
}

type piperRequest struct {
	Text            string  `json:"text"`
	OutputPath      string  `json:"output_path"`
	Speaker         *int    `json:"speaker,omitempty"`
	LengthScale     float64 `json:"length_scale"`
	NoiseScale      float64 `json:"noise_scale"`
	NoiseW          float64 `json:"noise_w"`
	SentenceSilence float64 `json:"sentence_silence"`
}

type piperResponse struct {
	Ready bool   `json:"ready"`
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	Trace string `json:"traceback"`
}

// Prepare verifies Piper and downloads the selected voice if needed.
func Prepare(ctx context.Context, runner executil.Runner, pythonExe, voice, dataDir string) error {
	if err := ensurePiper(ctx, runner, pythonExe); err != nil {
		return err
	}
	_, _, err := ensureVoice(ctx, runner, pythonExe, voice, dataDir)
	return err
}

// Synthesize turns translated SRT or segments cues into one timeline-aligned
// audio file.
func Synthesize(
	ctx context.Context,
	runner executil.Runner,
	pythonExe, inputPath, outputPath, voice, dataDir string,
	options Options,
) error {
	options = normalizeOptions(options)
	runner = serializeRunnerLog(runner)
	cues, err := srt.ReadTimestampedFile(inputPath)
	if err != nil {
		return err
	}
	if !options.DisableTextNormalization {
		for index := range cues {
			cues[index].Text = normalizeText(cues[index].Text, options.LanguageCode)
		}
	}
	groups := GroupCues(cues, options)
	if len(groups) == 0 {
		return fmt.Errorf("no synthesis groups could be created")
	}
	runnerLog(runner, "Parsed %d cues → %d synthesis groups", len(cues), len(groups))
	runnerLog(runner, "Input: %s", inputPath)
	runnerLog(runner, "Output: %s", outputPath)
	runnerLog(runner, "Voice: %s", voice)

	if err := ensurePiper(ctx, runner, pythonExe); err != nil {
		return err
	}
	modelPath, configPath, err := ensureVoice(ctx, runner, pythonExe, voice, dataDir)
	if err != nil {
		return err
	}
	voiceDefaults, err := loadVoiceDefaults(configPath)
	if err != nil {
		return err
	}
	piperWorkers, err := startPiperWorkers(ctx, runner, pythonExe, modelPath, configPath, ttsParallelism(options, len(groups)))
	if err != nil {
		return fmt.Errorf("start Piper workers: %w", err)
	}
	defer closePiperWorkers(piperWorkers)

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create audio output directory: %w", err)
	}
	var workDir string
	if options.KeepTemp {
		workDir = outputPath + ".parts"
		if err := os.MkdirAll(workDir, 0o755); err != nil {
			return fmt.Errorf("create TTS parts directory: %w", err)
		}
	} else {
		workDir, err = os.MkdirTemp("", "ai-video-dubber-tts-*")
		if err != nil {
			return fmt.Errorf("create TTS work directory: %w", err)
		}
		defer os.RemoveAll(workDir)
	}

	groupResults, err := synthesizeGroups(ctx, runner, piperWorkers, groups, workDir, voiceDefaults, options)
	if err != nil {
		return err
	}

	parts := make([]string, 0, len(groups)*2)
	reports := make([]GroupReport, 0, len(groups))
	cursor := time.Duration(0)
	for _, result := range groupResults {
		if err := ctx.Err(); err != nil {
			return err
		}
		group := result.Group
		gap := group.Start - cursor
		if gap > time.Millisecond {
			silencePath := filepath.Join(workDir, fmt.Sprintf("%04d_gap.wav", group.ID))
			if err := audio.WriteSilencePCM16Mono(silencePath, gap.Nanoseconds(), voiceDefaults.SampleRate); err != nil {
				return err
			}
			parts = append(parts, silencePath)
		}
		parts = append(parts, result.FittedPath)
		cursor = group.End
		reports = append(reports, result.Report)
	}

	finalWAV := filepath.Join(workDir, "final_synced.wav")
	if err := audio.ConcatenatePCM16Mono(parts, finalWAV, voiceDefaults.SampleRate); err != nil {
		return err
	}
	if err := audio.TranscodeWAV(ctx, runner, finalWAV, outputPath); err != nil {
		return err
	}
	if strings.TrimSpace(options.ReportPath) != "" {
		if err := writeReport(options.ReportPath, reports); err != nil {
			return err
		}
		runnerLog(runner, "Report: %s", options.ReportPath)
	}
	runnerLog(runner, "Done: %s", outputPath)
	return nil
}

// GroupCues joins short neighboring cues to improve prosody and reduce
// phrase-boundary artifacts.
func GroupCues(cues []srt.Cue, options Options) []Group {
	options = normalizeOptions(options)
	if len(cues) == 0 {
		return nil
	}
	chunks := make([][]srt.Cue, 0)
	current := []srt.Cue{cues[0]}
	for _, cue := range cues[1:] {
		previous := current[len(current)-1]
		gap := cue.Start - previous.End
		if gap < 0 {
			gap = 0
		}
		prospectiveDuration := cue.End - current[0].Start
		prospectiveChars := utf8.RuneCountInString(cue.Text) + len(current)
		for _, item := range current {
			prospectiveChars += utf8.RuneCountInString(item.Text)
		}
		shouldBreak := gap > options.MaxGroupGap ||
			prospectiveDuration > options.MaxGroupDuration ||
			prospectiveChars > options.MaxGroupChars ||
			(endsSentence(previous.Text) && (gap >= options.SentenceBreakGap || prospectiveDuration >= options.MinSentenceGroupDuration))
		if shouldBreak {
			chunks = append(chunks, current)
			current = []srt.Cue{cue}
		} else {
			current = append(current, cue)
		}
	}
	chunks = append(chunks, current)

	groups := make([]Group, 0, len(chunks))
	for index, chunk := range chunks {
		var builder strings.Builder
		builder.WriteString(strings.TrimSpace(chunk[0].Text))
		for cueIndex := 1; cueIndex < len(chunk); cueIndex++ {
			previous := chunk[cueIndex-1]
			currentCue := chunk[cueIndex]
			gap := currentCue.Start - previous.End
			separator := " "
			if !endsPause(previous.Text) && gap >= 450*time.Millisecond {
				separator = ", "
			}
			builder.WriteString(separator)
			builder.WriteString(strings.TrimSpace(currentCue.Text))
		}
		groups = append(groups, Group{
			ID: index + 1, Cues: append([]srt.Cue(nil), chunk...),
			Text:  spaces.ReplaceAllString(strings.TrimSpace(builder.String()), " "),
			Start: chunk[0].Start, End: chunk[len(chunk)-1].End,
		})
	}
	return groups
}

type voiceConfig struct {
	SampleRate  int
	LengthScale float64
	NoiseScale  float64
	NoiseW      float64
}

type attempt struct {
	LengthScale float64
	Duration    time.Duration
	Path        string
}

func normalizeOptions(options Options) Options {
	defaults := Defaults()
	if options.SentenceSilence < 0 {
		options.SentenceSilence = defaults.SentenceSilence
	}
	if options.SentenceSilence == 0 {
		options.SentenceSilence = defaults.SentenceSilence
	}
	if options.MinLengthScale <= 0 {
		options.MinLengthScale = defaults.MinLengthScale
	}
	if options.MaxLengthScale <= 0 {
		options.MaxLengthScale = defaults.MaxLengthScale
	}
	if options.MinLengthScale > options.MaxLengthScale {
		options.MinLengthScale, options.MaxLengthScale = options.MaxLengthScale, options.MinLengthScale
	}
	if options.MaxAtempo <= 1 {
		options.MaxAtempo = defaults.MaxAtempo
	}
	if options.MaxGroupGap <= 0 {
		options.MaxGroupGap = defaults.MaxGroupGap
	}
	if options.MaxGroupDuration <= 0 {
		options.MaxGroupDuration = defaults.MaxGroupDuration
	}
	if options.MaxGroupChars <= 0 {
		options.MaxGroupChars = defaults.MaxGroupChars
	}
	if options.SentenceBreakGap <= 0 {
		options.SentenceBreakGap = defaults.SentenceBreakGap
	}
	if options.MinSentenceGroupDuration <= 0 {
		options.MinSentenceGroupDuration = defaults.MinSentenceGroupDuration
	}
	if options.Parallelism <= 0 {
		options.Parallelism = defaults.Parallelism
	}
	return options
}

func ttsParallelism(options Options, groupCount int) int {
	parallelism := options.Parallelism
	if parallelism <= 0 {
		parallelism = runtime.NumCPU()
	}
	if parallelism < 1 {
		parallelism = 1
	}
	if groupCount > 0 && parallelism > groupCount {
		parallelism = groupCount
	}
	return parallelism
}

func serializeRunnerLog(runner executil.Runner) executil.Runner {
	if runner.Log == nil {
		return runner
	}
	log := runner.Log
	var mu sync.Mutex
	runner.Log = func(line string) {
		mu.Lock()
		defer mu.Unlock()
		log(line)
	}
	return runner
}

func normalizeText(text, languageCode string) string {
	text = strings.NewReplacer("“", `"`, "”", `"`, "’", "'").Replace(text)
	text = ellipsis.ReplaceAllString(text, "…")
	text = frontEnd.ReplaceAllStringFunc(text, func(value string) string {
		if strings.HasPrefix(value, "F") {
			return "Front end"
		}
		return "front end"
	})
	if strings.EqualFold(languageCode, "pt-BR") || strings.EqualFold(languageCode, "pt_BR") {
		text = strings.ReplaceAll(text, "&", " e ")
		text = decimal.ReplaceAllString(text, "$1 vírgula $2")
	}
	text = spaceBeforePunc.ReplaceAllString(text, "$1")
	return strings.TrimSpace(spaces.ReplaceAllString(text, " "))
}

func endsSentence(text string) bool { return sentenceEnd.MatchString(strings.TrimSpace(text)) }
func endsPause(text string) bool    { return pauseEnd.MatchString(strings.TrimSpace(text)) }

func ensurePiper(ctx context.Context, runner executil.Runner, pythonExe string) error {
	if strings.TrimSpace(pythonExe) == "" {
		return fmt.Errorf("Python executable is empty")
	}
	if _, err := runner.Output(ctx, pythonExe, []string{"-m", "piper", "--help"}, executil.Options{}); err != nil {
		return fmt.Errorf("piper-tts is unavailable in the virtual environment: %w", err)
	}
	return nil
}

func ensureVoice(ctx context.Context, runner executil.Runner, pythonExe, voice, dataDir string) (string, string, error) {
	if strings.TrimSpace(voice) == "" {
		return "", "", fmt.Errorf("Piper voice name is empty")
	}
	if strings.TrimSpace(dataDir) == "" {
		return "", "", fmt.Errorf("Piper voice directory is empty")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create Piper voice directory: %w", err)
	}
	model, config := locateVoiceFiles(dataDir, voice)
	if model != "" && config != "" {
		if err := verifyVoiceFiles(ctx, voice, model, config); err != nil {
			return "", "", err
		}
		return model, config, nil
	}
	runnerLog(runner, "Downloading TTS voice: %s...", voice)
	if err := runner.Run(ctx, pythonExe, []string{"-m", "piper.download_voices", voice, "--data-dir", dataDir}, executil.Options{}); err != nil {
		return "", "", fmt.Errorf("download Piper voice %q: %w", voice, err)
	}
	model, config = locateVoiceFiles(dataDir, voice)
	if model == "" || config == "" {
		return "", "", fmt.Errorf("voice download completed, but %s.onnx and its config were not found in %s", voice, dataDir)
	}
	if err := verifyVoiceFiles(ctx, voice, model, config); err != nil {
		return "", "", err
	}
	return model, config, nil
}

type piperVoiceIndexEntry struct {
	Files   map[string]piperVoiceFile `json:"files"`
	Aliases []string                  `json:"aliases"`
}

type piperVoiceFile struct {
	SizeBytes int64  `json:"size_bytes"`
	MD5Digest string `json:"md5_digest"`
}

func verifyVoiceFiles(ctx context.Context, voice, modelPath, configPath string) error {
	index, err := fetchPiperVoiceIndex(ctx)
	if err != nil {
		return fmt.Errorf("verify Piper voice %q: %w", voice, err)
	}
	entry, ok := lookupPiperVoice(index, voice)
	if !ok {
		return fmt.Errorf("verify Piper voice %q: voice metadata was not found in Piper voices index", voice)
	}
	for _, path := range []string{modelPath, configPath} {
		expected, ok := lookupPiperVoiceFile(entry, filepath.Base(path))
		if !ok {
			return fmt.Errorf("verify Piper voice %q: %s was not found in Piper voices index", voice, filepath.Base(path))
		}
		if err := verifyFileChecksum(path, expected); err != nil {
			return fmt.Errorf("verify Piper voice %q: %w", voice, err)
		}
	}
	return nil
}

func fetchPiperVoiceIndex(ctx context.Context) (map[string]piperVoiceIndexEntry, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, piperVoicesIndexURL, nil)
	if err != nil {
		return nil, err
	}
	client := http.Client{Timeout: 30 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch Piper voices index: HTTP %d", response.StatusCode)
	}
	var index map[string]piperVoiceIndexEntry
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&index); err != nil {
		return nil, fmt.Errorf("decode Piper voices index: %w", err)
	}
	return index, nil
}

func lookupPiperVoice(index map[string]piperVoiceIndexEntry, voice string) (piperVoiceIndexEntry, bool) {
	if entry, ok := index[voice]; ok {
		return entry, true
	}
	for _, entry := range index {
		for _, alias := range entry.Aliases {
			if alias == voice {
				return entry, true
			}
		}
	}
	return piperVoiceIndexEntry{}, false
}

func lookupPiperVoiceFile(entry piperVoiceIndexEntry, name string) (piperVoiceFile, bool) {
	for path, file := range entry.Files {
		if filepath.Base(path) == name {
			return file, true
		}
	}
	return piperVoiceFile{}, false
}

func verifyFileChecksum(path string, expected piperVoiceFile) error {
	if strings.TrimSpace(expected.MD5Digest) == "" || expected.SizeBytes <= 0 {
		return fmt.Errorf("%s has incomplete checksum metadata", filepath.Base(path))
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s for checksum: %w", filepath.Base(path), err)
	}
	defer file.Close()
	hash := md5.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return fmt.Errorf("hash %s: %w", filepath.Base(path), err)
	}
	if size != expected.SizeBytes {
		return fmt.Errorf("%s size mismatch: got %d bytes, want %d", filepath.Base(path), size, expected.SizeBytes)
	}
	digest := fmt.Sprintf("%x", hash.Sum(nil))
	if !strings.EqualFold(digest, expected.MD5Digest) {
		return fmt.Errorf("%s checksum mismatch: got %s, want %s", filepath.Base(path), digest, expected.MD5Digest)
	}
	return nil
}

func locateVoiceFiles(dataDir, voice string) (string, string) {
	modelName := voice + ".onnx"
	configName := voice + ".onnx.json"
	var model, config string
	_ = filepath.WalkDir(dataDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		switch entry.Name() {
		case modelName:
			if model == "" {
				model = path
			}
		case configName:
			if config == "" {
				config = path
			}
		}
		return nil
	})
	return model, config
}

func loadVoiceDefaults(path string) (voiceConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return voiceConfig{}, fmt.Errorf("read Piper voice config: %w", err)
	}
	var payload struct {
		Audio struct {
			SampleRate int `json:"sample_rate"`
		} `json:"audio"`
		Inference struct {
			LengthScale float64 `json:"length_scale"`
			NoiseScale  float64 `json:"noise_scale"`
			NoiseW      float64 `json:"noise_w"`
		} `json:"inference"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return voiceConfig{}, fmt.Errorf("decode Piper voice config: %w", err)
	}
	result := voiceConfig{
		SampleRate: payload.Audio.SampleRate, LengthScale: payload.Inference.LengthScale,
		NoiseScale: payload.Inference.NoiseScale, NoiseW: payload.Inference.NoiseW,
	}
	if result.SampleRate <= 0 {
		result.SampleRate = 22050
	}
	if result.LengthScale <= 0 {
		result.LengthScale = 1.0
	}
	if result.NoiseScale <= 0 {
		result.NoiseScale = 0.667
	}
	if result.NoiseW <= 0 {
		result.NoiseW = 0.8
	}
	return result, nil
}

type piperWorker struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	responses chan piperResponse
	done      chan struct{}
	waitMu    sync.Mutex
	waitErr   error
	log       executil.LogFunc
	closeOnce sync.Once
}

func startPiperWorker(ctx context.Context, runner executil.Runner, pythonExe, modelPath, configPath string) (*piperWorker, error) {
	cmd := exec.Command(pythonExe, "-u", "-c", piperWorkerScript, modelPath, configPath)
	executil.ConfigureProcess(cmd)
	if len(runner.Env) > 0 {
		cmd.Env = append(os.Environ(), runner.Env...)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	worker := &piperWorker{
		cmd:       cmd,
		stdin:     stdin,
		responses: make(chan piperResponse),
		done:      make(chan struct{}),
		log:       runner.Log,
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go worker.readResponses(stdout)
	go worker.readLogs(stderr)
	go worker.wait()

	for {
		select {
		case response, ok := <-worker.responses:
			if !ok {
				worker.Close()
				return nil, fmt.Errorf("Piper worker exited before reporting readiness")
			}
			if response.Error != "" {
				worker.Close()
				return nil, piperResponseError(response)
			}
			if response.Ready {
				return worker, nil
			}
		case <-worker.done:
			worker.Close()
			if err := worker.waitError(); err != nil {
				return nil, fmt.Errorf("Piper worker exited before readiness: %w", err)
			}
			return nil, fmt.Errorf("Piper worker exited before readiness")
		case <-ctx.Done():
			worker.Close()
			return nil, ctx.Err()
		}
	}
}

func startPiperWorkers(ctx context.Context, runner executil.Runner, pythonExe, modelPath, configPath string, count int) ([]*piperWorker, error) {
	if count < 1 {
		count = 1
	}
	workers := make([]*piperWorker, 0, count)
	for index := 0; index < count; index++ {
		worker, err := startPiperWorker(ctx, runner, pythonExe, modelPath, configPath)
		if err != nil {
			closePiperWorkers(workers)
			return nil, fmt.Errorf("worker %d/%d: %w", index+1, count, err)
		}
		workers = append(workers, worker)
	}
	return workers, nil
}

func closePiperWorkers(workers []*piperWorker) {
	for _, worker := range workers {
		if worker != nil {
			worker.Close()
		}
	}
}

func (w *piperWorker) wait() {
	err := w.cmd.Wait()
	w.waitMu.Lock()
	w.waitErr = err
	w.waitMu.Unlock()
	close(w.done)
}

func (w *piperWorker) waitError() error {
	w.waitMu.Lock()
	defer w.waitMu.Unlock()
	return w.waitErr
}

func (w *piperWorker) readResponses(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var response piperResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			if w.log != nil {
				w.log("Piper worker: " + line)
			}
			continue
		}
		w.responses <- response
	}
	if err := scanner.Err(); err != nil && w.log != nil {
		w.log("Piper worker stdout: " + err.Error())
	}
	close(w.responses)
}

func (w *piperWorker) readLogs(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && w.log != nil {
			w.log("Piper worker: " + line)
		}
	}
}

func (w *piperWorker) synthesize(ctx context.Context, text, outputPath string, lengthScale, noiseScale, noiseW float64, options Options) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	request := piperRequest{
		Text:            text,
		OutputPath:      outputPath,
		Speaker:         options.Speaker,
		LengthScale:     lengthScale,
		NoiseScale:      noiseScale,
		NoiseW:          noiseW,
		SentenceSilence: options.SentenceSilence,
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if _, err := w.stdin.Write(append(payload, '\n')); err != nil {
		return fmt.Errorf("send Piper request: %w", err)
	}
	for {
		select {
		case response, ok := <-w.responses:
			if !ok {
				return fmt.Errorf("Piper worker exited")
			}
			if response.Error != "" {
				return piperResponseError(response)
			}
			if response.OK {
				return nil
			}
		case <-w.done:
			if err := w.waitError(); err != nil {
				return fmt.Errorf("Piper worker exited: %w", err)
			}
			return fmt.Errorf("Piper worker exited")
		case <-ctx.Done():
			w.Close()
			return ctx.Err()
		}
	}
}

func (w *piperWorker) Close() {
	w.closeOnce.Do(func() {
		if w.stdin != nil {
			_ = w.stdin.Close()
		}
		executil.TerminateProcess(w.cmd)
		select {
		case <-w.done:
		case <-time.After(2 * time.Second):
		}
	})
}

func piperResponseError(response piperResponse) error {
	if strings.TrimSpace(response.Trace) != "" {
		return fmt.Errorf("%s\n%s", response.Error, strings.TrimSpace(response.Trace))
	}
	return fmt.Errorf("%s", response.Error)
}

type synthesisGroupJob struct {
	Index int
	Group Group
}

type synthesisGroupResult struct {
	Index      int
	Group      Group
	FittedPath string
	Report     GroupReport
	Err        error
}

func synthesizeGroups(ctx context.Context, runner executil.Runner, workers []*piperWorker, groups []Group, workDir string, defaults voiceConfig, options Options) ([]synthesisGroupResult, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	if len(workers) == 0 {
		return nil, fmt.Errorf("no Piper workers available")
	}
	runnerLog(runner, "Synthesizing %d group(s) with %d Piper worker(s).", len(groups), len(workers))

	stepCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan synthesisGroupJob)
	results := make(chan synthesisGroupResult, len(groups))
	var workerGroup sync.WaitGroup
	for _, worker := range workers {
		piper := worker
		workerGroup.Add(1)
		go func() {
			defer workerGroup.Done()
			for job := range jobs {
				if stepCtx.Err() != nil {
					return
				}
				result, err := synthesizeGroup(stepCtx, runner, piper, job.Group, len(groups), workDir, defaults, options)
				result.Index = job.Index
				result.Err = err
				results <- result
				if err != nil {
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index, group := range groups {
			select {
			case <-stepCtx.Done():
				return
			case jobs <- synthesisGroupJob{Index: index, Group: group}:
			}
		}
	}()
	go func() {
		workerGroup.Wait()
		close(results)
	}()

	ordered := make([]synthesisGroupResult, len(groups))
	var firstErr error
	for result := range results {
		if result.Err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("synthesize group %d: %w", result.Group.ID, result.Err)
				cancel()
			}
			continue
		}
		ordered[result.Index] = result
	}
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ordered, nil
}

func synthesizeGroup(ctx context.Context, runner executil.Runner, piper *piperWorker, group Group, groupCount int, workDir string, defaults voiceConfig, options Options) (synthesisGroupResult, error) {
	attempts, err := synthesizeAttempts(ctx, runner, piper, group, workDir, defaults, options)
	if err != nil {
		return synthesisGroupResult{Group: group}, err
	}
	best, err := selectBestAttempt(attempts, group.Duration(), options.MaxAtempo)
	if err != nil {
		return synthesisGroupResult{Group: group}, fmt.Errorf("select synthesis attempt: %w", err)
	}
	speedup, trimmed := fitSpeed(best.Duration, group.Duration(), options.MaxAtempo)
	fittedPath := filepath.Join(workDir, fmt.Sprintf("%04d_slot.wav", group.ID))
	if err := audio.FitPCMToSlot(ctx, runner, best.Path, fittedPath, group.Duration(), defaults.SampleRate, speedup, trimmed); err != nil {
		return synthesisGroupResult{Group: group}, err
	}

	cueIndices := make([]int, len(group.Cues))
	for i, cue := range group.Cues {
		cueIndices[i] = cue.Index
	}
	report := GroupReport{
		ID: group.ID, CueIndices: cueIndices,
		StartSeconds: group.Start.Seconds(), EndSeconds: group.End.Seconds(),
		TargetSeconds: group.Duration().Seconds(), Text: group.Text,
		ChosenLengthScale: best.LengthScale, RawSeconds: best.Duration.Seconds(),
		SpeedupApplied: speedup, TrimmedFallback: trimmed,
		SampleRate: defaults.SampleRate,
	}

	runnerLog(runner,
		"[%03d/%03d] %s → %s | slot=%5.2fs raw=%5.2fs scale=%.2f tempo=%.2f%s | %s",
		group.ID, groupCount, formatDuration(group.Start), formatDuration(group.End),
		group.Duration().Seconds(), best.Duration.Seconds(), best.LengthScale, speedup,
		map[bool]string{true: " TRIM", false: ""}[trimmed], excerpt(group.Text, 72),
	)
	return synthesisGroupResult{Group: group, FittedPath: fittedPath, Report: report}, nil
}

func synthesizeAttempts(ctx context.Context, runner executil.Runner, piper *piperWorker, group Group, workDir string, defaults voiceConfig, options Options) ([]attempt, error) {
	baseScale := defaults.LengthScale
	if options.LengthScale != nil {
		baseScale = *options.LengthScale
	}
	noiseScale := defaults.NoiseScale
	if options.NoiseScale != nil {
		noiseScale = *options.NoiseScale
	}
	noiseW := defaults.NoiseW
	if options.NoiseW != nil {
		noiseW = *options.NoiseW
	}
	candidates := chooseScaleCandidates(baseScale, group.Duration(), options.MinLengthScale, options.MaxLengthScale)
	attempts := make([]attempt, 0, len(candidates))
	for index, scale := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := filepath.Join(workDir, fmt.Sprintf("%04d_raw_%02d.wav", group.ID, index+1))
		if err := piper.synthesize(ctx, group.Text, path, scale, noiseScale, noiseW, options); err != nil {
			return nil, fmt.Errorf("Piper synthesis failed: %w", err)
		}
		duration, err := audio.WAVDuration(path)
		if err != nil {
			return nil, err
		}
		attempts = append(attempts, attempt{LengthScale: scale, Duration: duration, Path: path})
		runnerLog(runner, "[group %03d] try scale=%.3f raw=%.3fs target=%.3fs | %s", group.ID, scale, duration.Seconds(), group.Duration().Seconds(), excerpt(group.Text, 72))
	}
	if len(attempts) == 0 {
		return nil, fmt.Errorf("Piper produced no attempts for group %d", group.ID)
	}
	return attempts, nil
}

func chooseScaleCandidates(base float64, target time.Duration, minScale, maxScale float64) []float64 {
	raw := []float64{base, base * 0.94, base * 0.88, base * 0.82, base * 0.76, base * 1.06}
	if target < 2*time.Second {
		raw = append(raw, base*0.90, base*0.84)
	}
	seen := map[int]bool{}
	result := make([]float64, 0, len(raw))
	for _, value := range raw {
		value = math.Min(maxScale, math.Max(minScale, value))
		key := int(math.Round(value * 10000))
		if !seen[key] {
			seen[key] = true
			result = append(result, float64(key)/10000)
		}
	}
	return result
}

func selectBestAttempt(attempts []attempt, target time.Duration, maxAtempo float64) (attempt, error) {
	if len(attempts) == 0 {
		return attempt{}, fmt.Errorf("empty attempt list")
	}
	if target <= 0 {
		return attempt{}, fmt.Errorf("target duration must be positive")
	}
	viable := make([]attempt, 0, len(attempts))
	for _, item := range attempts {
		if item.Duration <= 0 {
			continue
		}
		if item.Duration.Seconds() <= target.Seconds()*maxAtempo {
			viable = append(viable, item)
		}
	}
	if len(viable) > 0 {
		sort.SliceStable(viable, func(i, j int) bool {
			deltaI := viable[i].Duration.Seconds() - target.Seconds()
			deltaJ := viable[j].Duration.Seconds() - target.Seconds()
			scoreI := math.Abs(deltaI)
			scoreJ := math.Abs(deltaJ)
			if deltaI > 0 {
				scoreI += 0.15
			}
			if deltaJ > 0 {
				scoreJ += 0.15
			}
			if math.Abs(scoreI-scoreJ) < 1e-9 {
				return viable[i].Duration < viable[j].Duration
			}
			return scoreI < scoreJ
		})
		return viable[0], nil
	}
	valid := append([]attempt(nil), attempts...)
	sort.SliceStable(valid, func(i, j int) bool { return valid[i].Duration < valid[j].Duration })
	if valid[0].Duration <= 0 {
		return attempt{}, fmt.Errorf("all attempts have invalid duration")
	}
	return valid[0], nil
}

func fitSpeed(raw, target time.Duration, maxAtempo float64) (float64, bool) {
	if raw <= 0 || target <= 0 {
		return 1.0, true
	}
	if raw <= target+time.Microsecond {
		return 1.0, false
	}
	exact := raw.Seconds() / target.Seconds()
	if exact <= maxAtempo {
		return exact, false
	}
	return maxAtempo, true
}

func writeReport(path string, reports []GroupReport) error {
	data, err := json.MarshalIndent(reports, "", "  ")
	if err != nil {
		return fmt.Errorf("encode TTS report: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".tts-report-*")
	if err != nil {
		return fmt.Errorf("create report temporary file: %w", err)
	}
	name := temp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write TTS report: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync TTS report: %w", err)
	}
	if err := temp.Chmod(privateReportFileMode); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set TTS report permissions: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close TTS report: %w", err)
	}
	if err := replaceFile(name, path); err != nil {
		return fmt.Errorf("replace TTS report: %w", err)
	}
	return nil
}

func replaceFile(source, destination string) error {
	if err := os.Rename(source, destination); err == nil {
		return nil
	} else if _, statErr := os.Stat(destination); statErr != nil {
		return err
	}
	if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(source, destination)
}

func formatDuration(value time.Duration) string {
	milliseconds := value.Round(time.Millisecond).Milliseconds()
	hours := milliseconds / 3_600_000
	milliseconds %= 3_600_000
	minutes := milliseconds / 60_000
	milliseconds %= 60_000
	seconds := milliseconds / 1_000
	milliseconds %= 1_000
	return fmt.Sprintf("%02d:%02d:%02d.%03d", hours, minutes, seconds, milliseconds)
}

func excerpt(text string, width int) string {
	text = spaces.ReplaceAllString(strings.TrimSpace(text), " ")
	if len([]rune(text)) <= width {
		return text
	}
	runes := []rune(text)
	return string(runes[:width-1]) + "…"
}

func runnerLog(runner executil.Runner, format string, args ...any) {
	if runner.Log != nil {
		runner.Log(fmt.Sprintf(format, args...))
	}
}
