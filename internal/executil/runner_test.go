package executil

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRequirePreservesLookPathError(t *testing.T) {
	t.Setenv("PATH", "")

	err := Require("ai-video-dubber-definitely-missing-tool")

	if err == nil {
		t.Fatal("Require() succeeded, want lookup error")
	}
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("error = %v, want exec.ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), "required executable") {
		t.Fatalf("error = %v, want required executable context", err)
	}
}

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

func TestLineWriterFlushesCarriageReturnProgress(t *testing.T) {
	var lines []string
	writer := newLineWriter(func(line string) { lines = append(lines, line) }, false, 1024)

	if _, err := writer.Write([]byte("Downloading 10%\rDownloading 20%\rDone\n")); err != nil {
		t.Fatal(err)
	}

	want := []string{"Downloading 10%", "Downloading 20%", "Done"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %#v, want %#v", lines, want)
	}
	for index := range want {
		if lines[index] != want[index] {
			t.Fatalf("lines[%d] = %q, want %q; all lines=%#v", index, lines[index], want[index], lines)
		}
	}
}

func TestLineWriterTailKeepsValidUTF8AfterByteTruncation(t *testing.T) {
	writer := newLineWriter(nil, true, 2)

	if _, err := writer.Write([]byte("éx")); err != nil {
		t.Fatal(err)
	}

	tail := writer.Tail()
	if !utf8.ValidString(tail) {
		t.Fatalf("tail = %q, want valid UTF-8", tail)
	}
	if tail != "x" {
		t.Fatalf("tail = %q, want only complete UTF-8 suffix", tail)
	}
}

func TestCancellationErrorPreservesContextAndWaitError(t *testing.T) {
	waitErr := errors.New("exit status 7")

	err := cancellationError(context.Canceled, waitErr)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if !strings.Contains(err.Error(), "exit status 7") {
		t.Fatalf("error = %v, want wait error detail", err)
	}
}

func TestRunCancellationPreservesProcessWaitError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test")
	}
	dir := t.TempDir()
	tool := filepath.Join(dir, "slow-tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nprintf 'ready\\n'\nwhile true; do sleep 1; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	var readyClosed bool
	runner := Runner{Log: func(line string) {
		if line == "ready" && !readyClosed {
			readyClosed = true
			close(ready)
		}
	}}
	done := make(chan error, 1)
	go func() { done <- runner.Run(ctx, tool, nil, Options{}) }()

	<-ready
	cancel()
	err := <-done
	if err == nil {
		t.Fatal("Run succeeded after cancellation")
	}
	var commandErr *CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("error type = %T, want *CommandError", err)
	}
	if !errors.Is(commandErr.Err, context.Canceled) {
		t.Fatalf("command error = %v, want context.Canceled", commandErr.Err)
	}
	if !strings.Contains(commandErr.Err.Error(), "process wait after cancellation") {
		t.Fatalf("command error = %v, want preserved wait error", commandErr.Err)
	}
	if !strings.Contains(err.Error(), "process wait after cancellation") {
		t.Fatalf("display error = %v, want preserved wait error", err)
	}
}
