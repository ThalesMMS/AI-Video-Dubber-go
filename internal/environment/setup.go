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
	return setupRuntime(ctx, runner, cfg, runtimeDependencies{
		Name:        "Whisper/Piper",
		Verify:      verifyPythonDependencies,
		InstallArgs: []string{"-m", "pip", "install", "--upgrade", "openai-whisper", "piper-tts"},
	})
}

// SetupWhisperRuntime creates the runtime needed for transcription-only flows.
func SetupWhisperRuntime(ctx context.Context, runner executil.Runner, cfg config.Config) (string, error) {
	return setupRuntime(ctx, runner, cfg, runtimeDependencies{
		Name:        "Whisper",
		Verify:      verifyWhisperDependency,
		InstallArgs: []string{"-m", "pip", "install", "--upgrade", "openai-whisper"},
	})
}

type runtimeDependencies struct {
	Name        string
	Verify      func(context.Context, executil.Runner, string) error
	InstallArgs []string
}

func setupRuntime(ctx context.Context, runner executil.Runner, cfg config.Config, deps runtimeDependencies) (string, error) {
	for _, executable := range []string{cfg.FFmpegBin, cfg.FFprobeBin, cfg.PythonBin} {
		if err := executil.Require(executable); err != nil {
			return "", err
		}
	}
	if err := validatePythonVersion(ctx, runner, cfg.PythonBin); err != nil {
		return "", err
	}
	if config.IsBundledPython(cfg.PythonBin) {
		if err := deps.Verify(ctx, runner, cfg.PythonBin); err != nil {
			return "", fmt.Errorf("bundled Python is missing %s dependencies: %w", deps.Name, err)
		}
		if runner.Log != nil {
			runner.Log("Using bundled Python runtime.")
		}
		return cfg.PythonBin, nil
	}
	if strings.TrimSpace(cfg.VenvDir) == "" {
		return "", fmt.Errorf("virtual environment directory is required when Python is not bundled")
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

	if err := deps.Verify(ctx, runner, pythonExe); err != nil {
		if runner.Log != nil {
			runner.Log("Installing " + deps.Name + " dependencies (first run may take several minutes)...")
		}
		if err := runner.Run(ctx, pythonExe, []string{"-m", "pip", "install", "--upgrade", "pip", "wheel", "setuptools"}, executil.Options{}); err != nil {
			return "", fmt.Errorf("upgrade Python packaging tools: %w", err)
		}
		if err := runner.Run(ctx, pythonExe, deps.InstallArgs, executil.Options{}); err != nil {
			return "", fmt.Errorf("install %s dependencies: %w", deps.Name, err)
		}
	} else if runner.Log != nil {
		runner.Log("Python dependencies are already installed.")
	}

	return pythonExe, nil
}

func verifyWhisperDependency(ctx context.Context, runner executil.Runner, pythonExe string) error {
	if _, err := runner.Output(ctx, pythonExe, []string{"-c", "import whisper"}, executil.Options{}); err != nil {
		return fmt.Errorf("import whisper: %w", err)
	}
	return nil
}

func verifyPythonDependencies(ctx context.Context, runner executil.Runner, pythonExe string) error {
	if _, err := runner.Output(ctx, pythonExe, []string{"-c", "import whisper, piper"}, executil.Options{}); err != nil {
		return fmt.Errorf("import whisper and piper: %w", err)
	}
	if _, err := runner.Output(ctx, pythonExe, []string{"-m", "piper", "--help"}, executil.Options{}); err != nil {
		return fmt.Errorf("run piper module: %w", err)
	}
	return nil
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
