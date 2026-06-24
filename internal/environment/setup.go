// Package environment prepares the Python ML runtime used by Whisper and Piper.
package environment

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/config"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/executil"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/tts"
)

// Setup creates the runtime and makes sure the requested Piper voice is available.
func Setup(ctx context.Context, runner executil.Runner, cfg config.Config, voice string) (string, error) {
	pythonExe, err := SetupRuntime(ctx, runner, cfg)
	if err != nil {
		return "", err
	}
	if err := tts.Prepare(ctx, runner, pythonExe, voice, cfg.VoiceDataDir); err != nil {
		return "", err
	}
	if runner.Log != nil {
		runner.Log("Environment ready.")
	}
	return pythonExe, nil
}

// SetupRuntime creates the virtual environment and installs Whisper/Piper when missing.
func SetupRuntime(ctx context.Context, runner executil.Runner, cfg config.Config) (string, error) {
	for _, executable := range []string{"ffmpeg", "ffprobe", cfg.PythonBin} {
		if err := executil.Require(executable); err != nil {
			return "", err
		}
	}
	if err := validatePythonVersion(ctx, runner, cfg.PythonBin); err != nil {
		return "", err
	}
	pythonExe := config.VenvPython(cfg.VenvDir)
	if _, err := os.Stat(pythonExe); err != nil {
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("inspect virtual environment: %w", err)
		}
		if runner.Log != nil {
			runner.Log("Creating Python virtual environment at " + cfg.VenvDir + "...")
		}
		if err := os.MkdirAll(filepath.Dir(cfg.VenvDir), 0o755); err != nil {
			return "", fmt.Errorf("create virtual environment parent directory: %w", err)
		}
		if err := runner.Run(ctx, cfg.PythonBin, []string{"-m", "venv", cfg.VenvDir}, executil.Options{}); err != nil {
			return "", fmt.Errorf("create Python virtual environment: %w", err)
		}
	}

	_, importErr := runner.Output(ctx, pythonExe, []string{"-c", "import whisper, piper"}, executil.Options{})
	_, piperErr := runner.Output(ctx, pythonExe, []string{"-m", "piper", "--help"}, executil.Options{})
	if importErr != nil || piperErr != nil {
		if runner.Log != nil {
			runner.Log("Installing Whisper and Piper dependencies (first run may take several minutes)...")
		}
		if err := runner.Run(ctx, pythonExe, []string{"-m", "pip", "install", "--upgrade", "pip", "wheel", "setuptools"}, executil.Options{}); err != nil {
			return "", fmt.Errorf("upgrade Python packaging tools: %w", err)
		}
		if err := runner.Run(ctx, pythonExe, []string{"-m", "pip", "install", "--upgrade", "openai-whisper", "piper-tts"}, executil.Options{}); err != nil {
			return "", fmt.Errorf("install Whisper/Piper dependencies: %w", err)
		}
	} else if runner.Log != nil {
		runner.Log("Python dependencies are already installed.")
	}

	return pythonExe, nil
}

func validatePythonVersion(ctx context.Context, runner executil.Runner, pythonBin string) error {
	output, err := runner.Output(ctx, pythonBin, []string{
		"-c", "import sys; print(f'{sys.version_info.major}.{sys.version_info.minor}')",
	}, executil.Options{})
	if err != nil {
		return fmt.Errorf("inspect Python version: %w", err)
	}
	version := strings.TrimSpace(output)
	var major, minor int
	if _, err := fmt.Sscanf(version, "%d.%d", &major, &minor); err != nil {
		return fmt.Errorf("could not parse Python version %q", version)
	}
	if major < 3 || (major == 3 && minor < 10) {
		return fmt.Errorf("Python 3.10 or newer is required; found %s", version)
	}
	return nil
}
