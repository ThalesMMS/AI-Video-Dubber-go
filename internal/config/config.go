// Package config contains runtime configuration shared by the GUI and CLI.
package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	DefaultAPIBase        = "http://localhost:8000"
	DefaultAPIKey         = "apikey"
	DefaultWhisperModel   = "large-v3"
	DefaultSourceLanguage = "en"
	DefaultBatchSize      = 15
)

// Config configures a complete dubbing run.
type Config struct {
	InputPath            string
	OutputPath           string
	LanguageCode         string
	APIBase              string
	APIKey               string
	Model                string
	WhisperModel         string
	SourceLanguage       string
	PythonBin            string
	VenvDir              string
	FFmpegBin            string
	FFprobeBin           string
	VoiceDataDir         string
	Force                bool
	KeepTemp             bool
	TranslationBatchSize int
}

// BundledResources describes the relocatable tools shipped beside a binary.
type BundledResources struct {
	Root       string
	PythonBin  string
	FFmpegBin  string
	FFprobeBin string
}

// Defaults returns platform-aware defaults.
func Defaults() Config {
	return Config{
		LanguageCode:         "pt-BR",
		APIBase:              DefaultAPIBase,
		APIKey:               DefaultAPIKey,
		WhisperModel:         DefaultWhisperModel,
		SourceLanguage:       DefaultSourceLanguage,
		PythonBin:            defaultPython(),
		FFmpegBin:            defaultTool("ffmpeg"),
		FFprobeBin:           defaultTool("ffprobe"),
		VoiceDataDir:         defaultVoiceDataDir(),
		Force:                false,
		TranslationBatchSize: DefaultBatchSize,
	}
}

// Normalize fills empty values and cleans path-like values.
func (c Config) Normalize(projectDir string) Config {
	defaults := Defaults()
	if strings.TrimSpace(c.LanguageCode) == "" {
		c.LanguageCode = defaults.LanguageCode
	}
	if strings.TrimSpace(c.APIBase) == "" {
		c.APIBase = defaults.APIBase
	}
	c.APIBase = strings.TrimRight(strings.TrimSpace(c.APIBase), "/")
	if c.APIKey == "" {
		c.APIKey = defaults.APIKey
	}
	if strings.TrimSpace(c.WhisperModel) == "" {
		c.WhisperModel = defaults.WhisperModel
	}
	if strings.TrimSpace(c.SourceLanguage) == "" {
		c.SourceLanguage = defaults.SourceLanguage
	}
	if strings.TrimSpace(c.PythonBin) == "" {
		c.PythonBin = defaults.PythonBin
	}
	if strings.TrimSpace(c.VenvDir) == "" {
		c.VenvDir = strings.TrimSpace(os.Getenv("VENV_DIR"))
	}
	if strings.TrimSpace(c.VenvDir) == "" && !IsBundledPython(c.PythonBin) {
		c.VenvDir = filepath.Join(projectDir, ".venv")
	}
	if strings.TrimSpace(c.FFmpegBin) == "" {
		c.FFmpegBin = defaults.FFmpegBin
	}
	if strings.TrimSpace(c.FFprobeBin) == "" {
		c.FFprobeBin = defaults.FFprobeBin
	}
	if strings.TrimSpace(c.VoiceDataDir) == "" {
		c.VoiceDataDir = defaults.VoiceDataDir
	}
	if c.TranslationBatchSize <= 0 {
		c.TranslationBatchSize = defaults.TranslationBatchSize
	}
	return c
}

func defaultPython() string {
	if value := strings.TrimSpace(os.Getenv("PYTHON_BIN")); value != "" {
		return value
	}
	if resources, ok := bundledResources(); ok {
		return resources.PythonBin
	}
	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
}

func defaultTool(name string) string {
	if _, err := exec.LookPath(name); err == nil {
		return name
	}
	resources, ok := bundledResources()
	if !ok {
		return name
	}
	switch name {
	case "ffmpeg":
		return resources.FFmpegBin
	case "ffprobe":
		return resources.FFprobeBin
	default:
		return name
	}
}

func defaultVoiceDataDir() string {
	if value := strings.TrimSpace(os.Getenv("DATA_DIR")); value != "" {
		return value
	}
	cacheDir, err := os.UserCacheDir()
	if err == nil && cacheDir != "" {
		return filepath.Join(cacheDir, "piper-voices")
	}
	homeDir, err := os.UserHomeDir()
	if err == nil && homeDir != "" {
		return filepath.Join(homeDir, ".cache", "piper-voices")
	}
	return filepath.Join(".", ".cache", "piper-voices")
}

// VenvPython returns the Python executable inside a virtual environment.
func VenvPython(venvDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvDir, "Scripts", "python.exe")
	}
	return filepath.Join(venvDir, "bin", "python")
}

// IsBundledPython reports whether path points at the embedded app Python.
func IsBundledPython(path string) bool {
	clean := filepath.Clean(strings.TrimSpace(path))
	slashed := filepath.ToSlash(clean)
	if strings.Contains(slashed, ".app/Contents/Resources/python/bin/python") {
		return true
	}
	if resources, ok := bundledResources(); ok {
		return clean == filepath.Clean(resources.PythonBin)
	}
	return false
}

// ToolPaths returns command-name replacements for runtime subprocesses.
func (c Config) ToolPaths() map[string]string {
	tools := make(map[string]string, 2)
	if strings.TrimSpace(c.FFmpegBin) != "" {
		tools["ffmpeg"] = c.FFmpegBin
	}
	if strings.TrimSpace(c.FFprobeBin) != "" {
		tools["ffprobe"] = c.FFprobeBin
	}
	return tools
}

// RuntimeEnv returns environment overrides needed by embedded subprocesses.
func (c Config) RuntimeEnv() []string {
	env := make([]string, 0, 2)
	if espeakDataPath := runtimeEspeakDataPath(c.PythonBin); espeakDataPath != "" {
		env = append(env, "ESPEAK_DATA_PATH="+espeakDataPath)
	}
	dirs := make([]string, 0, 2)
	addDir := func(path string) {
		path = strings.TrimSpace(path)
		if !filepath.IsAbs(path) {
			return
		}
		dir := filepath.Dir(path)
		for _, existing := range dirs {
			if existing == dir {
				return
			}
		}
		dirs = append(dirs, dir)
	}
	addDir(c.PythonBin)
	addDir(c.FFmpegBin)
	addDir(c.FFprobeBin)
	if len(dirs) == 0 {
		return nil
	}
	pathParts := append([]string(nil), dirs...)
	if current := os.Getenv("PATH"); current != "" {
		pathParts = append(pathParts, current)
	}
	env = append(env, "PATH="+strings.Join(pathParts, string(os.PathListSeparator)))
	return env
}

func runtimeEspeakDataPath(pythonBin string) string {
	dataDir, ok := bundledEspeakDataDir(pythonBin)
	if !ok {
		return ""
	}
	if shortDir, ok := shortEspeakDataDir(dataDir); ok {
		return shortDir
	}
	return dataDir
}

func bundledEspeakDataDir(pythonBin string) (string, bool) {
	if !IsBundledPython(pythonBin) {
		return "", false
	}
	pythonRoot := filepath.Dir(filepath.Dir(filepath.Clean(pythonBin)))
	matches, err := filepath.Glob(filepath.Join(pythonRoot, "lib", "python*", "site-packages", "piper", "espeak-ng-data"))
	if err != nil {
		return "", false
	}
	for _, match := range matches {
		if fileExists(filepath.Join(match, "phontab")) {
			return match, true
		}
	}
	return "", false
}

func shortEspeakDataDir(target string) (string, bool) {
	cacheDir, err := os.UserCacheDir()
	if err != nil || cacheDir == "" {
		return "", false
	}
	linkParent := filepath.Join(cacheDir, "AI-Video-Dubber", "runtime")
	linkPath := filepath.Join(linkParent, "espeak-ng-data")
	if err := os.MkdirAll(linkParent, 0o755); err != nil {
		return "", false
	}
	if info, err := os.Lstat(linkPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if resolved, err := filepath.EvalSymlinks(linkPath); err == nil && resolved == target {
				return linkPath, true
			}
		}
		if err := os.RemoveAll(linkPath); err != nil {
			return "", false
		}
	} else if !os.IsNotExist(err) {
		return "", false
	}
	if err := os.Symlink(target, linkPath); err != nil {
		return "", false
	}
	return linkPath, true
}

func bundledResources() (BundledResources, bool) {
	executable, err := os.Executable()
	if err != nil {
		return BundledResources{}, false
	}
	return resolveBundledResources(executable, fileExists)
}

func resolveBundledResources(executable string, exists func(string) bool) (BundledResources, bool) {
	executable = filepath.Clean(executable)
	executableDir := filepath.Dir(executable)
	candidates := make([]string, 0, 3)
	if filepath.Base(executableDir) == "MacOS" {
		contentsDir := filepath.Dir(executableDir)
		if filepath.Base(contentsDir) == "Contents" && strings.HasSuffix(filepath.Base(filepath.Dir(contentsDir)), ".app") {
			candidates = append(candidates, filepath.Join(contentsDir, "Resources"))
		}
	}
	candidates = append(candidates,
		executableDir,
		filepath.Join(executableDir, "Resources"),
		filepath.Join(filepath.Dir(executableDir), "Resources"),
	)
	for _, root := range candidates {
		resources := resourcesAt(root)
		if exists(resources.PythonBin) && exists(resources.FFmpegBin) && exists(resources.FFprobeBin) {
			return resources, true
		}
	}
	return BundledResources{}, false
}

func resourcesAt(root string) BundledResources {
	return BundledResources{
		Root:       root,
		PythonBin:  filepath.Join(root, "python", "bin", "python3"),
		FFmpegBin:  filepath.Join(root, "ffmpeg", "ffmpeg"),
		FFprobeBin: filepath.Join(root, "ffmpeg", "ffprobe"),
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
