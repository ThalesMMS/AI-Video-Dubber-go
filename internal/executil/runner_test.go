package executil

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunnerUsesToolPathAndEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	tool := filepath.Join(dir, "fake-ffprobe")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nprintf '%s\\n' \"$AI_VIDEO_DUBBER_TEST\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	runner := Runner{
		Tools: map[string]string{"ffprobe": tool},
		Env:   []string{"AI_VIDEO_DUBBER_TEST=bundled"},
	}
	output, err := runner.Output(context.Background(), "ffprobe", nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output) != "bundled" {
		t.Fatalf("output = %q", output)
	}
}
