package audio

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/executil"
)

// ExtractMP3 extracts the first audio stream as high-quality VBR MP3.
func ExtractMP3(ctx context.Context, runner executil.Runner, videoPath, outputPath string) error {
	return atomicMediaOutput(outputPath, func(tempPath string) error {
		args := []string{
			"-hide_banner", "-nostdin", "-y",
			"-i", videoPath,
			"-vn", "-acodec", "libmp3lame", "-q:a", "2",
			tempPath,
		}
		if err := runner.Run(ctx, "ffmpeg", args, executil.Options{}); err != nil {
			return fmt.Errorf("extract audio: %w", err)
		}
		return nil
	})
}

// MergeVideoAudio replaces the video's audio with a synchronized track.
func MergeVideoAudio(ctx context.Context, runner executil.Runner, videoPath, audioPath, outputPath string) error {
	return atomicMediaOutput(outputPath, func(tempPath string) error {
		args := []string{
			"-hide_banner", "-nostdin", "-y",
			"-i", videoPath,
			"-i", audioPath,
			"-map", "0:v:0", "-map", "1:a:0",
			"-c:v", "copy",
			"-c:a", "aac", "-b:a", "192k",
			"-shortest",
			tempPath,
		}
		if err := runner.Run(ctx, "ffmpeg", args, executil.Options{}); err != nil {
			return fmt.Errorf("merge video and dubbed audio: %w", err)
		}
		return nil
	})
}

// EmbedSubtitles copies the original video/audio streams and adds an SRT file
// as an MP4 selectable subtitle track.
func EmbedSubtitles(ctx context.Context, runner executil.Runner, videoPath, subtitlePath, outputPath string) error {
	if empty, err := isEmptyTextFile(subtitlePath); err != nil {
		return err
	} else if empty {
		return copyVideoAudio(ctx, runner, videoPath, outputPath)
	}
	return atomicMediaOutput(outputPath, func(tempPath string) error {
		args := []string{
			"-hide_banner", "-nostdin", "-y",
			"-i", videoPath,
			"-i", subtitlePath,
			"-map", "0:v:0", "-map", "0:a?", "-map", "1:0",
			"-c:v", "copy",
			"-c:a", "copy",
			"-c:s", "mov_text",
			"-movflags", "+faststart",
			tempPath,
		}
		if err := runner.Run(ctx, "ffmpeg", args, executil.Options{}); err != nil {
			return fmt.Errorf("embed subtitles: %w", err)
		}
		return nil
	})
}

// BurnInSubtitles re-encodes the video with the SRT rendered into the pixels and
// copies the original audio streams.
func BurnInSubtitles(ctx context.Context, runner executil.Runner, videoPath, subtitlePath, outputPath string) error {
	if empty, err := isEmptyTextFile(subtitlePath); err != nil {
		return err
	} else if empty {
		return copyVideoAudio(ctx, runner, videoPath, outputPath)
	}
	if err := requireSubtitlesFilter(ctx, runner); err != nil {
		return err
	}
	return atomicMediaOutput(outputPath, func(tempPath string) error {
		args := []string{
			"-hide_banner", "-nostdin", "-y",
			"-i", videoPath,
			"-map", "0:v:0", "-map", "0:a?",
			"-vf", subtitlesFilter(subtitlePath),
			"-c:v", "libx264", "-crf", "18", "-preset", "medium",
			"-c:a", "copy",
			"-movflags", "+faststart",
			tempPath,
		}
		if err := runner.Run(ctx, "ffmpeg", args, executil.Options{}); err != nil {
			return fmt.Errorf("burn subtitles into video: %w", err)
		}
		return nil
	})
}

func requireSubtitlesFilter(ctx context.Context, runner executil.Runner) error {
	output, err := runner.Output(ctx, "ffmpeg", []string{"-hide_banner", "-filters"}, executil.Options{})
	if err != nil {
		return fmt.Errorf("inspect ffmpeg subtitle filters: %w", err)
	}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "subtitles" {
			return nil
		}
	}
	return fmt.Errorf("ffmpeg subtitles filter is unavailable; use an ffmpeg build with libass to burn subtitles into video")
}

func subtitlesFilter(path string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(path)
	return "subtitles=filename='" + escaped + "'"
}

func copyVideoAudio(ctx context.Context, runner executil.Runner, videoPath, outputPath string) error {
	return atomicMediaOutput(outputPath, func(tempPath string) error {
		args := []string{
			"-hide_banner", "-nostdin", "-y",
			"-i", videoPath,
			"-map", "0:v:0", "-map", "0:a?",
			"-c", "copy",
			"-movflags", "+faststart",
			tempPath,
		}
		if err := runner.Run(ctx, "ffmpeg", args, executil.Options{}); err != nil {
			return fmt.Errorf("copy original video/audio: %w", err)
		}
		return nil
	})
}

func isEmptyTextFile(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read subtitle file %q: %w", path, err)
	}
	return strings.TrimSpace(string(data)) == "", nil
}

// ProbeDuration returns a media file's duration.
func ProbeDuration(ctx context.Context, runner executil.Runner, path string) (time.Duration, error) {
	output, err := runner.Output(ctx, "ffprobe", []string{
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	}, executil.Options{})
	if err != nil {
		return 0, fmt.Errorf("probe duration of %q: %w", path, err)
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(output), 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		return 0, fmt.Errorf("ffprobe returned invalid duration %q for %s", strings.TrimSpace(output), path)
	}
	return time.Duration(seconds * float64(time.Second)), nil
}

// FitPCMToSlot converts a WAV to mono PCM16, applies a bounded speed-up, pads,
// and trims it to an exact subtitle slot.
func FitPCMToSlot(
	ctx context.Context,
	runner executil.Runner,
	inputWAV, outputWAV string,
	slot time.Duration,
	sampleRate int,
	speedup float64,
	trimmedFallback bool,
) error {
	if slot <= 0 {
		return fmt.Errorf("slot duration must be positive")
	}
	filters := make([]string, 0, 5)
	if math.Abs(speedup-1.0) > 1e-6 {
		filters = append(filters, BuildAtempoChain(speedup))
	}
	slotSeconds := slot.Seconds()
	filters = append(filters,
		fmt.Sprintf("apad=whole_dur=%.6f", slotSeconds),
		fmt.Sprintf("atrim=0:%.6f", slotSeconds),
	)
	if trimmedFallback && slot > 120*time.Millisecond {
		fadeStart := math.Max(0, slotSeconds-0.06)
		filters = append(filters, fmt.Sprintf("afade=t=out:st=%.6f:d=0.06", fadeStart))
	}
	filters = append(filters, "asetpts=N/SR/TB")

	args := []string{
		"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
		"-i", inputWAV,
		"-vn", "-ac", "1", "-ar", strconv.Itoa(sampleRate),
		"-c:a", "pcm_s16le",
		"-af", strings.Join(filters, ","),
		outputWAV,
	}
	if err := runner.Run(ctx, "ffmpeg", args, executil.Options{Quiet: true}); err != nil {
		return fmt.Errorf("fit synthesized audio to slot: %w", err)
	}
	return nil
}

// TranscodeWAV converts the final PCM WAV to the requested audio container.
func TranscodeWAV(ctx context.Context, runner executil.Runner, inputWAV, outputPath string) error {
	extension := strings.ToLower(filepath.Ext(outputPath))
	if extension == ".wav" {
		return copyFileAtomic(inputWAV, outputPath)
	}
	args := []string{"-hide_banner", "-nostdin", "-loglevel", "error", "-y", "-i", inputWAV}
	switch extension {
	case ".mp3":
		args = append(args, "-q:a", "2")
	case ".m4a", ".aac":
		args = append(args, "-c:a", "aac", "-b:a", "192k")
	default:
		return fmt.Errorf("unsupported audio output extension %q", extension)
	}
	return atomicMediaOutput(outputPath, func(tempPath string) error {
		commandArgs := append(append([]string(nil), args...), tempPath)
		if err := runner.Run(ctx, "ffmpeg", commandArgs, executil.Options{Quiet: true}); err != nil {
			return fmt.Errorf("transcode synchronized audio: %w", err)
		}
		return nil
	})
}

// BuildAtempoChain keeps each ffmpeg atempo factor in [0.5, 2.0].
func BuildAtempoChain(target float64) string {
	if target <= 0 || math.IsNaN(target) || math.IsInf(target, 0) {
		return "anull"
	}
	factors := make([]float64, 0, 4)
	remaining := target
	for remaining < 0.5 {
		factors = append(factors, 0.5)
		remaining /= 0.5
	}
	for remaining > 2.0 {
		factors = append(factors, 2.0)
		remaining /= 2.0
	}
	if math.Abs(remaining-1.0) > 1e-9 {
		factors = append(factors, remaining)
	}
	if len(factors) == 0 {
		return "anull"
	}
	parts := make([]string, 0, len(factors))
	for _, factor := range factors {
		parts = append(parts, fmt.Sprintf("atempo=%.8f", factor))
	}
	return strings.Join(parts, ",")
}

func atomicMediaOutput(destination string, render func(string) error) error {
	dir := filepath.Dir(destination)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	extension := filepath.Ext(destination)
	temp, err := os.CreateTemp(dir, ".ai-video-dubber-*"+extension)
	if err != nil {
		return fmt.Errorf("create temporary media output: %w", err)
	}
	tempName := temp.Name()
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return fmt.Errorf("close temporary media output: %w", err)
	}
	defer func() { _ = os.Remove(tempName) }()

	if err := render(tempName); err != nil {
		return err
	}
	info, err := os.Stat(tempName)
	if err != nil {
		return fmt.Errorf("inspect rendered media output: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("rendered media output is empty: %s", tempName)
	}
	if err := os.Chmod(tempName, 0o644); err != nil {
		return fmt.Errorf("set rendered media permissions: %w", err)
	}
	if err := replaceOutputFile(tempName, destination); err != nil {
		return fmt.Errorf("replace output %q: %w", destination, err)
	}
	return nil
}

func copyFileAtomic(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open %q: %w", source, err)
	}
	defer input.Close()

	dir := filepath.Dir(destination)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".audio-copy-*")
	if err != nil {
		return fmt.Errorf("create temporary output: %w", err)
	}
	tempName := temp.Name()
	defer func() { _ = os.Remove(tempName) }()

	if _, err := io.Copy(temp, input); err != nil {
		_ = temp.Close()
		return fmt.Errorf("copy audio data: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("sync temporary output: %w", err)
	}
	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		return fmt.Errorf("set temporary output permissions: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary output: %w", err)
	}
	if err := replaceOutputFile(tempName, destination); err != nil {
		return fmt.Errorf("replace output %q: %w", destination, err)
	}
	return nil
}

func replaceOutputFile(source, destination string) error {
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
