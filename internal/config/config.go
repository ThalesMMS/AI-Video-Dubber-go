// Package config contains runtime configuration shared by the GUI and CLI.
package config

import (
	"os"
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
	VoiceDataDir         string
	Force                bool
	KeepTemp             bool
	TranslationBatchSize int
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
		c.VenvDir = filepath.Join(projectDir, ".venv")
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
	if runtime.GOOS == "windows" {
		return "python"
	}
	return "python3"
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
