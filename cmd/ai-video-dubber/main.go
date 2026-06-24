package main

import (
	"os"
	"path/filepath"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/cli"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/gui"
)

func main() {
	projectDir := resolveProjectDir()
	if len(os.Args) == 1 || (len(os.Args) == 2 && os.Args[1] == "gui") {
		gui.Run(projectDir)
		return
	}
	os.Exit(cli.Run(os.Args[1:], projectDir))
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
