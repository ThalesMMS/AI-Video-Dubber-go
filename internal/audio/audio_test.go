package audio

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/config"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/executil"
)

func TestBuildPaths(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "video.test.mp4")
	paths, err := BuildPaths(input, "pt-BR", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(paths.TranslatedSRT, "video.test.pt-BR.srt") || !strings.HasSuffix(paths.FinalVideo, "video.test.pt-BR.synced.mp4") {
		t.Fatalf("unexpected paths: %#v", paths)
	}
	if _, err := BuildPaths(filepath.Join(dir, "no-extension"), "pt-BR", ""); err == nil {
		t.Fatal("BuildPaths() accepted an extensionless input")
	}
}

func TestBuildPathsForMode(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "video.test.mp4")

	dubPaths, err := BuildPathsForMode(input, "pt-BR", "", config.ModeDub)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(dubPaths.FinalVideo, "video.test.pt-BR.synced.mp4") {
		t.Fatalf("dub FinalVideo = %q", dubPaths.FinalVideo)
	}

	subtitlePaths, err := BuildPathsForMode(input, "pt-BR", "", config.ModeSubtitle)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(subtitlePaths.FinalVideo, "video.test.pt-BR.subtitled.mp4") {
		t.Fatalf("subtitle FinalVideo = %q", subtitlePaths.FinalVideo)
	}
	if !strings.HasSuffix(subtitlePaths.TranslatedSRT, "video.test.pt-BR.srt") {
		t.Fatalf("subtitle TranslatedSRT = %q", subtitlePaths.TranslatedSRT)
	}

	burnedPaths, err := BuildPathsForModeOptions(input, "pt-BR", "", config.ModeSubtitle, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(burnedPaths.FinalVideo, "video.test.pt-BR.burned-in.mp4") {
		t.Fatalf("burn-in FinalVideo = %q", burnedPaths.FinalVideo)
	}

	explicit := filepath.Join(dir, "custom-output.mp4")
	explicitPaths, err := BuildPathsForMode(input, "pt-BR", explicit, config.ModeSubtitle)
	if err != nil {
		t.Fatal(err)
	}
	if explicitPaths.FinalVideo != explicit {
		t.Fatalf("explicit FinalVideo = %q, want %q", explicitPaths.FinalVideo, explicit)
	}
}

func TestBuildAtempoChain(t *testing.T) {
	for _, target := range []float64{0.125, 0.75, 1, 1.12, 4.5} {
		chain := BuildAtempoChain(target)
		if target == 1 {
			if chain != "anull" {
				t.Fatalf("target 1 chain = %q", chain)
			}
			continue
		}
		product := 1.0
		for _, part := range strings.Split(chain, ",") {
			value, err := strconv.ParseFloat(strings.TrimPrefix(part, "atempo="), 64)
			if err != nil {
				t.Fatalf("parse %q: %v", part, err)
			}
			if value < 0.5 || value > 2.0 {
				t.Fatalf("factor %f is outside ffmpeg range", value)
			}
			product *= value
		}
		if math.Abs(product-target) > 1e-6 {
			t.Fatalf("chain %q product=%f, want %f", chain, product, target)
		}
	}
}

func TestWriteAndConcatenateSilenceWAV(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.wav")
	second := filepath.Join(dir, "second.wav")
	output := filepath.Join(dir, "joined.wav")
	const sampleRate = 8000
	if err := WriteSilencePCM16Mono(first, (250 * time.Millisecond).Nanoseconds(), sampleRate); err != nil {
		t.Fatal(err)
	}
	if err := WriteSilencePCM16Mono(second, (750 * time.Millisecond).Nanoseconds(), sampleRate); err != nil {
		t.Fatal(err)
	}
	if err := ConcatenatePCM16Mono([]string{first, second}, output, sampleRate); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != wavHeaderSize+sampleRate*2 {
		t.Fatalf("joined WAV size = %d, want %d", len(data), wavHeaderSize+sampleRate*2)
	}
	if string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" || string(data[36:40]) != "data" {
		t.Fatalf("invalid WAV header: %q", data[:44])
	}
	if got := binary.LittleEndian.Uint32(data[24:28]); got != sampleRate {
		t.Fatalf("sample rate = %d", got)
	}
	if got := binary.LittleEndian.Uint32(data[40:44]); got != sampleRate*2 {
		t.Fatalf("data size = %d", got)
	}
}

func TestCopyFileAtomicUsesPrivatePermissions(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.wav")
	destination := filepath.Join(dir, "destination.wav")
	if err := os.WriteFile(source, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyFileAtomic(source, destination); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("destination permissions = %o, want 600", got)
	}
}

func TestAtomicMediaOutputUsesPrivatePermissions(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "destination.mp4")
	err := atomicMediaOutput(destination, func(tempPath string) error {
		return os.WriteFile(tempPath, []byte("media"), 0o666)
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("destination permissions = %o, want 600", got)
	}
}

func TestProbeDurationUsesRunnerToolPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	ffprobe := filepath.Join(dir, "ffprobe")
	if err := os.WriteFile(ffprobe, []byte("#!/bin/sh\nprintf '12.500\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := executil.Runner{Tools: map[string]string{"ffprobe": ffprobe}}

	duration, err := ProbeDuration(context.Background(), runner, "input.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if duration != 12500*time.Millisecond {
		t.Fatalf("duration = %s", duration)
	}
}

func TestEmbedSubtitlesUsesFFmpegMovTextAndCopiesOriginalStreams(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	ffmpeg := filepath.Join(dir, "ffmpeg")
	argsPath := filepath.Join(dir, "args.txt")
	if err := os.WriteFile(ffmpeg, []byte(`#!/bin/sh
printf '%s\n' "$@" > "$CAPTURE_ARGS"
last=""
for arg do
  last="$arg"
done
printf media > "$last"
`), 0o755); err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(dir, "source.mp4")
	subtitle := filepath.Join(dir, "source.pt-BR.srt")
	output := filepath.Join(dir, "source.pt-BR.subtitled.mp4")
	if err := os.WriteFile(video, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subtitle, []byte("1\n00:00:00,000 --> 00:00:01,000\nOlá\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := executil.Runner{
		Tools: map[string]string{"ffmpeg": ffmpeg},
		Env:   []string{"CAPTURE_ARGS=" + argsPath},
	}

	if err := EmbedSubtitles(context.Background(), runner, video, subtitle, output); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "media" {
		t.Fatalf("output content = %q", data)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(argsData)), "\n")
	for _, want := range []string{
		"-i", video, subtitle,
		"-map", "0:v:0", "0:a?", "1:0",
		"-c:v", "copy", "-c:a", "copy", "-c:s", "mov_text",
	} {
		if !hasArg(args, want) {
			t.Fatalf("ffmpeg args missing %q:\n%s", want, string(argsData))
		}
	}
}

func TestBurnInSubtitlesUsesFFmpegFilterAndCopiesOriginalAudio(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	ffmpeg := filepath.Join(dir, "ffmpeg")
	argsPath := filepath.Join(dir, "args.txt")
	if err := os.WriteFile(ffmpeg, []byte(`#!/bin/sh
if [ "$1" = "-hide_banner" ] && [ "$2" = "-filters" ]; then
  printf ' T. subtitles         V->V       Render text subtitles onto input video using libass\n'
  exit 0
fi
printf '%s\n' "$@" > "$CAPTURE_ARGS"
last=""
for arg do
  last="$arg"
done
printf media > "$last"
`), 0o755); err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(dir, "source.mp4")
	subtitle := filepath.Join(dir, "source subtitle.pt-BR.srt")
	output := filepath.Join(dir, "source.pt-BR.burned-in.mp4")
	if err := os.WriteFile(video, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subtitle, []byte("1\n00:00:00,000 --> 00:00:01,000\nOlá\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := executil.Runner{
		Tools: map[string]string{"ffmpeg": ffmpeg},
		Env:   []string{"CAPTURE_ARGS=" + argsPath},
	}

	if err := BurnInSubtitles(context.Background(), runner, video, subtitle, output); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "media" {
		t.Fatalf("output content = %q", data)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(argsData)), "\n")
	if countArg(args, "-i") != 1 || hasArg(args, subtitle) || hasArg(args, "mov_text") {
		t.Fatalf("burn-in ffmpeg args should read subtitles through a video filter only:\n%s", string(argsData))
	}
	filter := argAfter(args, "-vf")
	if !strings.Contains(filter, "subtitles=filename=") || !strings.Contains(filter, subtitle) {
		t.Fatalf("subtitle filter = %q, want path %q", filter, subtitle)
	}
	for _, want := range []string{
		"-i", video,
		"-map", "0:v:0", "0:a?",
		"-c:v", "libx264", "-crf", "18", "-preset", "medium",
		"-c:a", "copy",
	} {
		if !hasArg(args, want) {
			t.Fatalf("ffmpeg args missing %q:\n%s", want, string(argsData))
		}
	}
}

func TestBurnInSubtitlesRequiresSubtitlesFilter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	ffmpeg := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(ffmpeg, []byte(`#!/bin/sh
if [ "$1" = "-hide_banner" ] && [ "$2" = "-filters" ]; then
  printf ' .. null              V->V       Pass the source unchanged to the output\n'
  exit 0
fi
exit 1
`), 0o755); err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(dir, "source.mp4")
	subtitle := filepath.Join(dir, "source.pt-BR.srt")
	output := filepath.Join(dir, "source.pt-BR.burned-in.mp4")
	if err := os.WriteFile(video, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subtitle, []byte("1\n00:00:00,000 --> 00:00:01,000\nOlá\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := BurnInSubtitles(context.Background(), executil.Runner{Tools: map[string]string{"ffmpeg": ffmpeg}}, video, subtitle, output)
	if err == nil || !strings.Contains(err.Error(), "subtitles filter") {
		t.Fatalf("BurnInSubtitles error = %v, want subtitles filter message", err)
	}
}

func TestEmbedSubtitlesWithEmptyFileCopiesOriginalStreamsOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	ffmpeg := filepath.Join(dir, "ffmpeg")
	argsPath := filepath.Join(dir, "args.txt")
	if err := os.WriteFile(ffmpeg, []byte(`#!/bin/sh
printf '%s\n' "$@" > "$CAPTURE_ARGS"
last=""
for arg do
  last="$arg"
done
printf media > "$last"
`), 0o755); err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(dir, "source.mp4")
	subtitle := filepath.Join(dir, "empty.srt")
	output := filepath.Join(dir, "source.pt-BR.subtitled.mp4")
	if err := os.WriteFile(video, []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subtitle, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	runner := executil.Runner{
		Tools: map[string]string{"ffmpeg": ffmpeg},
		Env:   []string{"CAPTURE_ARGS=" + argsPath},
	}

	if err := EmbedSubtitles(context.Background(), runner, video, subtitle, output); err != nil {
		t.Fatal(err)
	}
	argsData, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(argsData)), "\n")
	if hasArg(args, subtitle) || hasArg(args, "mov_text") || hasArg(args, "1:0") {
		t.Fatalf("empty subtitle ffmpeg args unexpectedly include subtitle input:\n%s", string(argsData))
	}
	for _, want := range []string{"-i", video, "-map", "0:v:0", "0:a?", "-c", "copy"} {
		if !hasArg(args, want) {
			t.Fatalf("ffmpeg args missing %q:\n%s", want, string(argsData))
		}
	}
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func countArg(args []string, want string) int {
	count := 0
	for _, arg := range args {
		if arg == want {
			count++
		}
	}
	return count
}

func argAfter(args []string, key string) string {
	for index, arg := range args {
		if arg == key && index+1 < len(args) {
			return args[index+1]
		}
	}
	return ""
}
