package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	project := t.TempDir()
	cfg := (Config{APIBase: " http://localhost:8000/v1/ ", TranslationBatchSize: -1}).Normalize(project)
	if cfg.APIBase != "http://localhost:8000/v1" {
		t.Fatalf("APIBase = %q", cfg.APIBase)
	}
	if cfg.LanguageCode != "pt-BR" || cfg.WhisperModel == "" || cfg.TranslationBatchSize != DefaultBatchSize {
		t.Fatalf("defaults not applied: %#v", cfg)
	}
	if cfg.Mode != ModeDub {
		t.Fatalf("Mode = %q, want %q", cfg.Mode, ModeDub)
	}
	if cfg.VenvDir != filepath.Join(project, ".venv") {
		t.Fatalf("VenvDir = %q", cfg.VenvDir)
	}
	if !strings.Contains(VenvPython(cfg.VenvDir), ".venv") {
		t.Fatalf("VenvPython = %q", VenvPython(cfg.VenvDir))
	}
}

func TestResolveBundledResourcesFromAppExecutable(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "AI-Video-Dubber.app", "Contents", "MacOS", "ai-video-dubber")
	resourcesDir := filepath.Join(dir, "AI-Video-Dubber.app", "Contents", "Resources")
	python := filepath.Join(resourcesDir, "python", "bin", "python3")
	ffmpeg := filepath.Join(resourcesDir, "ffmpeg", "ffmpeg")
	ffprobe := filepath.Join(resourcesDir, "ffmpeg", "ffprobe")
	for _, path := range []string{executable, python, ffmpeg, ffprobe} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	resources, ok := resolveBundledResources(executable, fileExists)
	if !ok {
		t.Fatal("bundled resources were not detected")
	}
	if resources.Root != resourcesDir || resources.PythonBin != python || resources.FFmpegBin != ffmpeg || resources.FFprobeBin != ffprobe {
		t.Fatalf("resources = %#v", resources)
	}
}

func TestResolveBundledResourcesFromSidecarExecutable(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "ai-video-dubber-cli")
	python := filepath.Join(dir, "python", "bin", "python3")
	ffmpeg := filepath.Join(dir, "ffmpeg", "ffmpeg")
	ffprobe := filepath.Join(dir, "ffmpeg", "ffprobe")
	for _, path := range []string{executable, python, ffmpeg, ffprobe} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	resources, ok := resolveBundledResources(executable, fileExists)
	if !ok {
		t.Fatal("sidecar resources were not detected")
	}
	if resources.Root != dir || resources.PythonBin != python || resources.FFmpegBin != ffmpeg || resources.FFprobeBin != ffprobe {
		t.Fatalf("resources = %#v", resources)
	}
}

func TestNormalizeSkipsVenvForBundledPython(t *testing.T) {
	t.Setenv("VENV_DIR", "")
	project := t.TempDir()
	python := filepath.Join(project, "AI-Video-Dubber.app", "Contents", "Resources", "python", "bin", "python3")

	cfg := (Config{PythonBin: python}).Normalize(project)
	if cfg.VenvDir != "" {
		t.Fatalf("VenvDir = %q, want empty for bundled Python", cfg.VenvDir)
	}
	if !IsBundledPython(cfg.PythonBin) {
		t.Fatalf("PythonBin = %q was not classified as bundled", cfg.PythonBin)
	}
}

func TestRuntimeEnvPrependsBundledResourceDirectories(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	dir := t.TempDir()
	python := filepath.Join(dir, "python", "bin", "python3")
	ffmpeg := filepath.Join(dir, "ffmpeg", "ffmpeg")
	ffprobe := filepath.Join(dir, "ffmpeg", "ffprobe")
	cfg := Config{PythonBin: python, FFmpegBin: ffmpeg, FFprobeBin: ffprobe}

	env := cfg.RuntimeEnv()
	want := "PATH=" + filepath.Dir(python) + string(os.PathListSeparator) + filepath.Dir(ffmpeg) + string(os.PathListSeparator) + "/usr/bin"
	if len(env) != 1 || env[0] != want {
		t.Fatalf("RuntimeEnv() = %#v, want %#v", env, []string{want})
	}
}

func TestRuntimeEnvSetsShortEspeakDataPathForBundledPython(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	pythonRoot := filepath.Join(dir, "AI-Video-Dubber.app", "Contents", "Resources", "python")
	python := filepath.Join(pythonRoot, "bin", "python3")
	espeakData := filepath.Join(pythonRoot, "lib", "python3.12", "site-packages", "piper", "espeak-ng-data")
	for _, path := range []string{python, filepath.Join(espeakData, "phontab")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	env := (Config{PythonBin: python}).RuntimeEnv()
	espeakPath := envValue(env, "ESPEAK_DATA_PATH")
	if espeakPath == "" {
		t.Fatalf("RuntimeEnv() = %#v, missing ESPEAK_DATA_PATH", env)
	}
	if espeakPath == espeakData {
		t.Fatalf("ESPEAK_DATA_PATH = %q, want short relocated path", espeakPath)
	}
	resolved, err := filepath.EvalSymlinks(espeakPath)
	if err != nil {
		t.Fatalf("resolve ESPEAK_DATA_PATH: %v", err)
	}
	wantResolved, err := filepath.EvalSymlinks(espeakData)
	if err != nil {
		t.Fatalf("resolve bundled espeak data: %v", err)
	}
	if resolved != wantResolved {
		t.Fatalf("ESPEAK_DATA_PATH resolves to %q, want %q", resolved, wantResolved)
	}
	if _, err := os.Stat(filepath.Join(espeakPath, "phontab")); err != nil {
		t.Fatalf("phontab through ESPEAK_DATA_PATH: %v", err)
	}
}

func envValue(env []string, name string) string {
	prefix := name + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}
