// Command ai-video-dubber-cli provides a headless build that does not link
// against the Fyne desktop runtime. It is useful on servers and in CI.
package main

import (
	"os"
	"path/filepath"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], resolveProjectDir()))
}

func resolveProjectDir() string {
	if value := os.Getenv("AI_VIDEO_DUBBER_HOME"); value != "" {
		if absolute, err := filepath.Abs(value); err == nil {
			return absolute
		}
		return value
	}
	if current, err := os.Getwd(); err == nil {
		if _, err := os.Stat(filepath.Join(current, "go.mod")); err == nil {
			return current
		}
	}
	if executable, err := os.Executable(); err == nil {
		directory := filepath.Dir(executable)
		if filepath.Base(directory) == "bin" {
			directory = filepath.Dir(directory)
		}
		return directory
	}
	return "."
}
