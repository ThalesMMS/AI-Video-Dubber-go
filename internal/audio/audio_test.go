package audio

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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

func TestCopyFileAtomicUsesReadablePermissions(t *testing.T) {
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
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("destination permissions = %o, want 644", got)
	}
}
