package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	fynetest "fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/config"
)

func TestAppendDisplayLogKeepsNewestCompleteLines(t *testing.T) {
	var builder strings.Builder

	appendDisplayLog(&builder, "alpha", 12)
	appendDisplayLog(&builder, "bravo", 12)
	text := appendDisplayLog(&builder, "charlie", 12)

	if text != "charlie" {
		t.Fatalf("text = %q, want %q", text, "charlie")
	}
	if builder.String() != text {
		t.Fatalf("builder = %q, want %q", builder.String(), text)
	}
}

func TestAppendDisplayLogPreservesValidUTF8(t *testing.T) {
	var builder strings.Builder

	appendDisplayLog(&builder, "\u00e1\u00e1\u00e1\u00e1\u00e1", 6)
	text := appendDisplayLog(&builder, "fim", 6)

	if !utf8.ValidString(text) {
		t.Fatalf("text is not valid UTF-8: %q", text)
	}
	if text != "fim" {
		t.Fatalf("text = %q, want %q", text, "fim")
	}
}

func TestCursorEnd(t *testing.T) {
	row, column := cursorEnd("one\nd\u00f3i")

	if row != 1 || column != 3 {
		t.Fatalf("cursor = (%d, %d), want (1, 3)", row, column)
	}
}

func TestOpenLogFileUsesPrivatePermissions(t *testing.T) {
	ui := &ui{projectDir: t.TempDir()}
	ui.openLogFile("input.mp4", config.ModeDub)
	if ui.logFile == nil {
		t.Fatal("log file was not opened")
	}
	logPath := ui.logFile.Name()
	ui.closeLogFile()

	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("log permissions = %o, want 600", got)
	}
}

func TestOnLogRedactsSecretsBeforePersisting(t *testing.T) {
	ui := &ui{projectDir: t.TempDir(), logRefreshPending: true}
	ui.openLogFile("input.mp4", config.ModeDub)
	if ui.logFile == nil {
		t.Fatal("log file was not opened")
	}
	logPath := ui.logFile.Name()

	ui.OnLog("OPENAI_API_KEY=super-secret")
	ui.closeLogFile()

	data, err := os.ReadFile(filepath.Clean(logPath))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "super-secret") {
		t.Fatalf("secret leaked to GUI log:\n%s", string(data))
	}
	if !strings.Contains(string(data), "OPENAI_API_KEY=[REDACTED]") {
		t.Fatalf("redacted secret missing from GUI log:\n%s", string(data))
	}
	if strings.Contains(ui.logBuilder.String(), "super-secret") {
		t.Fatalf("secret leaked to display log:\n%s", ui.logBuilder.String())
	}
}

func TestLogTextForCopyUsesPersistedLog(t *testing.T) {
	ui := &ui{projectDir: t.TempDir(), logRefreshPending: true}
	ui.openLogFile("input.mp4", config.ModeDub)
	if ui.logFile == nil {
		t.Fatal("log file was not opened")
	}

	ui.OnLog("copyable log line")

	text, err := ui.logTextForCopy()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "copyable log line") {
		t.Fatalf("persisted log line missing from copy text:\n%s", text)
	}
	if !strings.Contains(text, "Log started:") {
		t.Fatalf("log header missing from copy text:\n%s", text)
	}
}

func TestCopyLogCopiesPersistedLogToClipboard(t *testing.T) {
	application := fynetest.NewApp()
	ui := &ui{application: application, projectDir: t.TempDir(), logRefreshPending: true}
	ui.openLogFile("input.mp4", config.ModeDub)
	if ui.logFile == nil {
		t.Fatal("log file was not opened")
	}
	ui.OnLog("clipboard log line")

	ui.copyLog()

	if got := application.Clipboard().Content(); !strings.Contains(got, "clipboard log line") {
		t.Fatalf("clipboard = %q, want persisted log content", got)
	}
}

func TestCurrentLogFolderAndFileURL(t *testing.T) {
	projectDir := t.TempDir()
	ui := &ui{projectDir: projectDir}
	ui.openLogFile("input.mp4", config.ModeDub)
	if ui.logFile == nil {
		t.Fatal("log file was not opened")
	}

	folder, err := ui.currentLogFolder()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(projectDir, "logs"); folder != want {
		t.Fatalf("folder = %q, want %q", folder, want)
	}
	target, err := fileURL(folder)
	if err != nil {
		t.Fatal(err)
	}
	if target.Scheme != "file" {
		t.Fatalf("file URL scheme = %q, want file", target.Scheme)
	}
}

func TestValidateAPIEndpointReportsSpecificProblem(t *testing.T) {
	err := validateAPIEndpoint("http://localhost:8000/v1?token=abc")
	if err == nil {
		t.Fatal("validateAPIEndpoint accepted API base with query string")
	}
	if !strings.Contains(err.Error(), "query string or fragment") {
		t.Fatalf("error = %q, want specific API base validation", err.Error())
	}
}

func TestApplyRuntimeSettingsUsesSelectedWhisperModel(t *testing.T) {
	t.Setenv("WHISPER_MODEL", "large-v3")
	selectModel := widget.NewSelect(whisperModelOptions, nil)
	selectModel.SetSelected("small")
	ui := &ui{whisperModel: selectModel}
	cfg := config.Defaults()

	ui.applyRuntimeSettings(&cfg)

	if cfg.WhisperModel != "small" {
		t.Fatalf("WhisperModel = %q, want small", cfg.WhisperModel)
	}
}
