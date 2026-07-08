package executil

import (
	"context"
	"errors"
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

func TestRunnerRedactsSecretsFromLogsAndErrorTail(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	tool := filepath.Join(dir, "secret-tool")
	if err := os.WriteFile(tool, []byte(`#!/bin/sh
printf '%s\n' 'Authorization: Bearer sk-live-secret' 'OPENAI_API_KEY=abc123secret' >&2
exit 7
`), 0o755); err != nil {
		t.Fatal(err)
	}

	var lines []string
	runner := Runner{Log: func(line string) { lines = append(lines, line) }}
	err := runner.Run(context.Background(), tool, nil, Options{})
	if err == nil {
		t.Fatal("Run() succeeded, want command failure")
	}
	var commandErr *CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("error type = %T, want *CommandError", err)
	}
	combined := strings.Join(lines, "\n") + "\n" + commandErr.Output + "\n" + err.Error()
	for _, secret := range []string{"sk-live-secret", "abc123secret"} {
		if strings.Contains(combined, secret) {
			t.Fatalf("secret %q leaked in output:\n%s", secret, combined)
		}
	}
	for _, want := range []string{"Authorization: Bearer [REDACTED]", "OPENAI_API_KEY=[REDACTED]"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("redacted output missing %q:\n%s", want, combined)
		}
	}
}

func TestOutputRedactsSecretsFromCommandError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	tool := filepath.Join(dir, "secret-output")
	if err := os.WriteFile(tool, []byte(`#!/bin/sh
printf '%s\n' '--api-key cli-secret-value'
exit 2
`), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := (Runner{}).Output(context.Background(), tool, nil, Options{})
	if err == nil {
		t.Fatal("Output() succeeded, want command failure")
	}
	var commandErr *CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("error type = %T, want *CommandError", err)
	}
	combined := commandErr.Output + "\n" + err.Error()
	if strings.Contains(combined, "cli-secret-value") {
		t.Fatalf("secret leaked in command error:\n%s", combined)
	}
	if !strings.Contains(combined, "--api-key [REDACTED]") {
		t.Fatalf("redacted CLI flag missing from command error:\n%s", combined)
	}
}
