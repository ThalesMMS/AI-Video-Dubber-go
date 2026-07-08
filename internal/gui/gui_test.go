package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2/container"
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

func TestCancelPipelineRelabelsButtonAndLogsTeardownWait(t *testing.T) {
	cancelled := false
	cancelButton := widget.NewButton("Cancel", nil)
	ui := &ui{
		cancel:            cancelButton,
		cancelRun:         func() { cancelled = true },
		running:           true,
		logRefreshPending: true,
	}
	defer ui.stopCancelFeedback()

	ui.cancelPipeline()

	if !cancelled {
		t.Fatal("cancel function was not called")
	}
	if cancelButton.Text != "Cancelling..." {
		t.Fatalf("cancel button text = %q, want Cancelling...", cancelButton.Text)
	}
	if !cancelButton.Disabled() {
		t.Fatal("cancel button stayed enabled after cancellation request")
	}
	if !strings.Contains(ui.logBuilder.String(), "Cancellation requested. Waiting for running tools to stop") {
		t.Fatalf("cancel log missing:\n%s", ui.logBuilder.String())
	}
}

func TestCancelFeedbackMessageIncludesElapsedWait(t *testing.T) {
	got := cancelFeedbackMessage(12 * time.Second)

	if !strings.Contains(got, "Still cancelling after 12s") {
		t.Fatalf("message = %q, want elapsed wait", got)
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

func TestNewUIDefaultsRegenerateExistingFilesOff(t *testing.T) {
	application := fynetest.NewApp()
	ui := newUI(application, application.NewWindow("test"), t.TempDir())

	if ui.force == nil {
		t.Fatal("force checkbox was not created")
	}
	if ui.force.Checked {
		t.Fatal("regenerate existing files checkbox defaults to on")
	}
}

func TestApplyRunOptionsUsesRegenerateCheckbox(t *testing.T) {
	force := widget.NewCheck("Regenerate existing files", nil)
	burnIn := widget.NewCheck("Burn subtitles into video", nil)
	ui := &ui{force: force, burnIn: burnIn}

	cfg := config.Defaults()
	ui.applyRunOptions(&cfg, config.ModeDub)
	if cfg.Force {
		t.Fatal("Force = true with unchecked regenerate checkbox")
	}

	force.SetChecked(true)
	burnIn.SetChecked(true)
	cfg = config.Defaults()
	ui.applyRunOptions(&cfg, config.ModeSubtitle)
	if !cfg.Force {
		t.Fatal("Force = false with checked regenerate checkbox")
	}
	if !cfg.SubtitleBurnIn {
		t.Fatal("SubtitleBurnIn = false with checked burn-in option in subtitle mode")
	}

	cfg = config.Defaults()
	ui.applyRunOptions(&cfg, config.ModeDub)
	if cfg.SubtitleBurnIn {
		t.Fatal("SubtitleBurnIn = true in dub mode")
	}
}

func TestRefreshModeControlsUpdatesStartTextBurnInAndSteps(t *testing.T) {
	mode := widget.NewRadioGroup([]string{guiModeDub, guiModeSubtitle}, nil)
	mode.SetSelected(guiModeDub)
	ui := &ui{
		start:    widget.NewButton("", nil),
		mode:     mode,
		burnIn:   widget.NewCheck("Burn subtitles into video", nil),
		stepsBox: container.NewVBox(),
	}

	ui.refreshModeControls()
	if ui.start.Text != "▶  Start Dubbing" {
		t.Fatalf("start text = %q, want dubbing", ui.start.Text)
	}
	if ui.burnIn.Visible() {
		t.Fatal("burn-in option visible in dub mode")
	}
	if len(ui.steps) != 6 {
		t.Fatalf("dub steps = %d, want 6", len(ui.steps))
	}

	ui.mode.SetSelected(guiModeSubtitle)
	ui.burnIn.SetChecked(true)
	ui.refreshModeControls()
	if ui.start.Text != "▶  Start Subtitling" {
		t.Fatalf("start text = %q, want subtitling", ui.start.Text)
	}
	if !ui.burnIn.Visible() {
		t.Fatal("burn-in option hidden in subtitle mode")
	}
	if len(ui.steps) != 5 {
		t.Fatalf("subtitle steps = %d, want 5", len(ui.steps))
	}
	if ui.steps[4].label.Text != "Create burned-in video" {
		t.Fatalf("last subtitle step = %q, want burn-in label", ui.steps[4].label.Text)
	}
}
