// Package gui provides the Fyne desktop interface.
package gui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/ai-video-dubber/ai-video-dubber-go/assets"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/config"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/executil"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/language"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/pipeline"
)

const (
	maxLogBytes        = 64_000
	logRefreshInterval = 100 * time.Millisecond
	guiModeDub         = "Dub"
	guiModeSubtitle    = "Subtitle"
)

var whisperModelOptions = []string{"tiny", "base", "small", "medium", "large-v3"}

// Run creates the desktop application and blocks until it exits.
func Run(projectDir string) {
	application := app.NewWithID("io.github.ai-video-dubber")
	application.Settings().SetTheme(newDubberTheme())
	window := application.NewWindow("AI Video Dubber")
	window.SetIcon(assets.Icon)
	ui := newUI(application, window, projectDir)
	window.SetContent(ui.content())
	window.Resize(fyne.NewSize(760, 920))
	window.SetFixedSize(false)
	window.CenterOnScreen()
	window.ShowAndRun()
}

type ui struct {
	application fyne.App
	window      fyne.Window
	projectDir  string

	fileLabel           *widget.Label
	apiBase             *widget.Entry
	apiKey              *widget.Entry
	model               *widget.Entry
	whisperModel        *widget.Select
	mode                *widget.RadioGroup
	burnIn              *widget.Check
	language            *widget.Select
	browse              *widget.Button
	start               *widget.Button
	cancel              *widget.Button
	copyLogButton       *widget.Button
	openLogFolderButton *widget.Button
	logEntry            *widget.Entry
	stepsBox            *fyne.Container
	steps               []*stepIndicator

	mu         sync.Mutex
	logBuilder strings.Builder
	logFile    *os.File
	logPath    string
	cancelRun  context.CancelFunc
	running    bool

	logRefreshPending bool
	logRefreshDirty   bool
}

func newUI(application fyne.App, window fyne.Window, projectDir string) *ui {
	result := &ui{application: application, window: window, projectDir: projectDir}
	result.fileLabel = widget.NewLabel("No file selected")
	result.fileLabel.Truncation = fyne.TextTruncateEllipsis
	result.apiBase = widget.NewEntry()
	result.apiBase.SetText(application.Preferences().StringWithFallback("api_base", config.DefaultAPIBase))
	result.apiKey = widget.NewPasswordEntry()
	result.apiKey.SetText(config.DefaultAPIKey)
	result.model = widget.NewEntry()
	result.model.SetText(application.Preferences().String("model"))
	result.whisperModel = widget.NewSelect(whisperModelOptions, nil)
	selectedWhisperModel := application.Preferences().StringWithFallback("whisper_model", defaultWhisperModelSelection())
	if !contains(whisperModelOptions, selectedWhisperModel) {
		selectedWhisperModel = config.DefaultWhisperModel
	}
	result.whisperModel.SetSelected(selectedWhisperModel)

	labels := make([]string, 0, len(language.Supported()))
	for _, item := range language.Supported() {
		labels = append(labels, item.DisplayName)
	}
	result.language = widget.NewSelect(labels, nil)
	selectedLanguage := application.Preferences().StringWithFallback("language", labels[0])
	if !contains(labels, selectedLanguage) {
		selectedLanguage = labels[0]
	}
	result.language.SetSelected(selectedLanguage)

	result.mode = widget.NewRadioGroup([]string{guiModeDub, guiModeSubtitle}, func(string) { result.refreshModeControls() })
	result.mode.Horizontal = true
	result.mode.Required = true
	result.mode.SetSelected(guiModeDub)
	result.burnIn = widget.NewCheck("Burn subtitles into video", func(value bool) {
		application.Preferences().SetBool("subtitle_burn_in", value)
		result.refreshModeControls()
	})
	result.burnIn.SetChecked(application.Preferences().Bool("subtitle_burn_in"))

	result.browse = widget.NewButton("Browse…", result.openFileDialog)
	result.browse.Importance = widget.HighImportance
	result.start = widget.NewButton("▶  Start Dubbing", result.startPipeline)
	result.start.Importance = widget.HighImportance
	result.cancel = widget.NewButton("Cancel", result.cancelPipeline)
	result.cancel.Importance = widget.DangerImportance
	result.cancel.Disable()
	result.copyLogButton = widget.NewButtonWithIcon("Copy log", theme.ContentCopyIcon(), result.copyLog)
	result.copyLogButton.Disable()
	result.openLogFolderButton = widget.NewButtonWithIcon("Open log folder", theme.FolderOpenIcon(), result.openLogFolder)
	result.openLogFolderButton.Disable()

	result.logEntry = widget.NewMultiLineEntry()
	result.logEntry.Wrapping = fyne.TextWrapWord
	result.logEntry.TextStyle = fyne.TextStyle{Monospace: true}
	result.logEntry.Disable()
	result.refreshModeControls()
	return result
}

func (u *ui) content() fyne.CanvasObject {
	headerIcon := canvas.NewImageFromResource(assets.Icon)
	headerIcon.FillMode = canvas.ImageFillContain
	headerIcon.SetMinSize(fyne.NewSize(40, 40))
	iconBackground := canvas.NewRectangle(colorAccentSoft)
	iconBackground.CornerRadius = 16
	iconBackground.StrokeColor = colorBorder
	iconBackground.StrokeWidth = 1
	iconBadge := container.NewGridWrap(fyne.NewSize(64, 64), container.NewStack(iconBackground, container.NewPadded(headerIcon)))

	title := canvas.NewText("AI Video Dubber", colorForeground)
	title.TextSize = 28
	title.TextStyle = fyne.TextStyle{Bold: true}
	subtitle := canvas.NewText("Dub or subtitle any video into another language, automatically.", colorDim)
	subtitle.TextSize = 14
	header := container.NewBorder(nil, nil, iconBadge, nil, container.NewVBox(layout.NewSpacer(), title, subtitle, layout.NewSpacer()))

	fileRow := container.NewBorder(nil, nil, nil, u.browse, u.fileLabel)
	fileCard := card(1, "Select a video file", fileRow)

	endpointRow := formRow("Endpoint:", u.apiBase, nil)
	keyRow := formRow("API Key:", u.apiKey, nil)
	hint := canvas.NewText("(blank = auto-detect)", colorDim)
	hint.TextSize = 11
	modelRow := formRow("Model:", u.model, hint)
	llmCard := card(2, "LLM API settings (for translation)", container.NewVBox(endpointRow, keyRow, modelRow))

	whisperRow := formRow("Whisper:", u.whisperModel, nil)
	speechCard := card(3, "Local speech settings", whisperRow)

	modeCard := card(4, "Choose output mode", container.NewVBox(u.mode, u.burnIn))
	languageCard := card(5, "Choose target language", u.language)

	u.stepsBox = container.NewVBox()
	u.rebuildStepIndicators()
	logBackground := canvas.NewRectangle(colorInput)
	logBackground.CornerRadius = 10
	logBackground.StrokeColor = colorBorder
	logBackground.StrokeWidth = 1
	logBox := container.NewStack(logBackground, container.NewPadded(u.logEntry))
	logBox = container.NewGridWrap(fyne.NewSize(680, 300), logBox)
	logActions := container.NewHBox(layout.NewSpacer(), u.copyLogButton, u.openLogFolderButton)
	progressCard := card(6, "Pipeline progress", container.NewVBox(u.stepsBox, logActions, container.NewPadded(logBox)))

	body := container.NewVBox(
		header,
		fileCard,
		llmCard,
		speechCard,
		modeCard,
		languageCard,
		progressCard,
	)
	scroll := container.NewVScroll(container.NewPadded(body))

	footerBackground := canvas.NewRectangle(colorCard)
	footerBackground.TopLeftCornerRadius = 18
	footerBackground.TopRightCornerRadius = 18
	footerBackground.StrokeColor = colorBorder
	footerBackground.StrokeWidth = 1
	cancelCell := container.NewGridWrap(fyne.NewSize(140, 44), u.cancel)
	buttons := container.NewBorder(nil, nil, nil, cancelCell, u.start)
	footer := container.NewStack(footerBackground, container.NewPadded(buttons))
	return container.NewBorder(nil, footer, nil, nil, scroll)
}

func card(number int, titleText string, content fyne.CanvasObject) fyne.CanvasObject {
	title := canvas.NewText(titleText, colorForeground)
	title.TextSize = 16
	title.TextStyle = fyne.TextStyle{Bold: true}
	titleRow := container.NewBorder(nil, nil, badge(number, colorAccentSoft, colorAccent), nil, container.NewCenter(title))

	background := canvas.NewRectangle(colorCard)
	background.CornerRadius = 14
	background.StrokeColor = colorBorder
	background.StrokeWidth = 1
	body := container.NewStack(background, container.NewPadded(content))
	return container.NewVBox(titleRow, body)
}

// badge draws a small rounded numbered chip, used both for section headers
// and (via stepIndicator) the pipeline progress list, so both share one
// consistent "numbered stepper" visual language.
func badge(number int, background, foreground color.Color) fyne.CanvasObject {
	circle := canvas.NewRectangle(background)
	circle.CornerRadius = 13
	text := canvas.NewText(fmt.Sprintf("%d", number), foreground)
	text.TextSize = 14
	text.TextStyle = fyne.TextStyle{Bold: true}
	text.Alignment = fyne.TextAlignCenter
	return container.NewGridWrap(fyne.NewSize(26, 26), container.NewStack(circle, container.NewCenter(text)))
}

func formRow(labelText string, control fyne.CanvasObject, trailing fyne.CanvasObject) fyne.CanvasObject {
	label := canvas.NewText(labelText, colorDim)
	label.TextSize = 13
	labelCell := container.NewGridWrap(fyne.NewSize(90, 38), label)
	center := control
	if trailing != nil {
		center = container.NewBorder(nil, nil, nil, trailing, control)
	}
	return container.NewBorder(nil, nil, labelCell, nil, center)
}

func (u *ui) openFileDialog() {
	picker := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, u.window)
			return
		}
		if reader == nil {
			return
		}
		path := localURIPath(reader.URI())
		_ = reader.Close()
		if path != "" {
			u.fileLabel.SetText(path)
		}
	}, u.window)
	picker.SetFilter(storage.NewExtensionFileFilter([]string{".mp4", ".mkv", ".avi", ".mov", ".webm"}))
	picker.Resize(fyne.NewSize(900, 600))
	picker.Show()
}

func (u *ui) startPipeline() {
	inputPath := strings.TrimSpace(u.fileLabel.Text)
	if inputPath == "" || inputPath == "No file selected" {
		dialog.ShowInformation("No file", "Please select a video file first.", u.window)
		return
	}
	info, err := os.Stat(inputPath)
	if err != nil || !info.Mode().IsRegular() {
		dialog.ShowError(fmt.Errorf("video file was not found: %s", inputPath), u.window)
		return
	}
	apiBase := strings.TrimSpace(u.apiBase.Text)
	parsedURL, err := url.Parse(apiBase)
	if err != nil || !parsedURL.IsAbs() || parsedURL.Opaque != "" || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		dialog.ShowError(fmt.Errorf("invalid API endpoint: %s", apiBase), u.window)
		return
	}
	lang, err := language.ByDisplayName(u.language.Selected)
	if err != nil {
		dialog.ShowError(err, u.window)
		return
	}
	runMode := u.selectedMode()

	u.application.Preferences().SetString("api_base", apiBase)
	u.application.Preferences().SetString("model", strings.TrimSpace(u.model.Text))
	if u.whisperModel != nil {
		u.application.Preferences().SetString("whisper_model", strings.TrimSpace(u.whisperModel.Selected))
	}
	u.application.Preferences().SetString("language", lang.DisplayName)
	u.openLogFile(inputPath, runMode)
	u.resetRun()
	u.setRunning(true)
	ctx, cancel := context.WithCancel(context.Background())
	u.mu.Lock()
	u.cancelRun = cancel
	u.mu.Unlock()

	cfg := config.Defaults()
	cfg.Mode = runMode
	cfg.InputPath = inputPath
	cfg.LanguageCode = lang.Code
	cfg.APIBase = apiBase
	cfg.APIKey = u.apiKey.Text
	cfg.Model = strings.TrimSpace(u.model.Text)
	cfg.Force = true
	cfg.SubtitleBurnIn = runMode == config.ModeSubtitle && u.burnIn != nil && u.burnIn.Checked
	u.applyRuntimeSettings(&cfg)

	go func() {
		defer u.closeLogFile()
		result, runErr := (pipeline.Pipeline{ProjectDir: u.projectDir, Observer: u}).Run(ctx, cfg)
		switch {
		case runErr == nil:
			u.OnLog("")
			if cfg.Mode == config.ModeSubtitle {
				if cfg.SubtitleBurnIn {
					u.OnLog("✓  Done! Your burned-in subtitled video is ready.")
				} else {
					u.OnLog("✓  Done! Your subtitled video is ready.")
				}
			} else {
				u.OnLog("✓  Done! Your dubbed video is ready.")
			}
		case errors.Is(runErr, context.Canceled):
			u.OnLog("Pipeline cancelled by user.")
		default:
			u.OnLog("ERROR: " + runErr.Error())
		}

		fyne.Do(func() {
			u.setRunning(false)
			if runErr == nil {
				if cfg.Mode == config.ModeSubtitle {
					title := "Your subtitled video has been created:"
					if cfg.SubtitleBurnIn {
						title = "Your burned-in subtitled video has been created:"
					}
					dialog.ShowInformation("Success", title+"\n\n"+result.OutputVideo+"\n\nSubtitle file:\n"+result.SubtitleSRT, u.window)
				} else {
					dialog.ShowInformation("Success", "Your dubbed video has been created:\n\n"+result.OutputVideo, u.window)
				}
			} else if !errors.Is(runErr, context.Canceled) {
				dialog.ShowError(runErr, u.window)
			}
		})
	}()
}

func (u *ui) cancelPipeline() {
	u.mu.Lock()
	cancel := u.cancelRun
	u.mu.Unlock()
	if cancel != nil {
		u.cancel.Disable()
		u.OnLog("Cancellation requested...")
		cancel()
	}
}

func (u *ui) setRunning(running bool) {
	u.mu.Lock()
	u.running = running
	if !running {
		u.cancelRun = nil
	}
	u.mu.Unlock()
	if running {
		u.start.Disable()
		u.browse.Disable()
		u.language.Disable()
		if u.whisperModel != nil {
			u.whisperModel.Disable()
		}
		u.mode.Disable()
		u.burnIn.Disable()
		u.cancel.Enable()
	} else {
		u.start.Enable()
		u.browse.Enable()
		u.language.Enable()
		if u.whisperModel != nil {
			u.whisperModel.Enable()
		}
		u.mode.Enable()
		u.refreshModeControls()
		u.cancel.Disable()
	}
}

func (u *ui) applyRuntimeSettings(cfg *config.Config) {
	if u.whisperModel != nil {
		if value := strings.TrimSpace(u.whisperModel.Selected); value != "" {
			cfg.WhisperModel = value
		}
	} else if value := strings.TrimSpace(os.Getenv("WHISPER_MODEL")); value != "" {
		cfg.WhisperModel = value
	}
	if value := strings.TrimSpace(os.Getenv("VENV_DIR")); value != "" {
		cfg.VenvDir = value
	}
	if value := strings.TrimSpace(os.Getenv("DATA_DIR")); value != "" {
		cfg.VoiceDataDir = value
	}
}

func defaultWhisperModelSelection() string {
	if value := strings.TrimSpace(os.Getenv("WHISPER_MODEL")); value != "" {
		return value
	}
	return config.DefaultWhisperModel
}

func (u *ui) resetRun() {
	u.mu.Lock()
	u.logBuilder.Reset()
	u.logRefreshPending = false
	u.logRefreshDirty = false
	u.mu.Unlock()
	u.logEntry.SetText("")
	u.rebuildStepIndicators()
	for _, indicator := range u.steps {
		indicator.setState(pipeline.StatePending)
	}
}

func (u *ui) openLogFile(inputPath string, mode config.Mode) {
	u.mu.Lock()
	if u.logFile != nil {
		_ = u.logFile.Close()
		u.logFile = nil
	}
	u.logPath = ""
	u.mu.Unlock()
	u.setLogActionButtons(false)

	logDir := filepath.Join(u.projectDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return
	}
	timestamp := time.Now().Format("20060102-150405")
	base := filepath.Base(inputPath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "" {
		base = string(mode)
	}
	logPath := filepath.Join(logDir, base+"-"+timestamp+".log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return
	}
	u.mu.Lock()
	u.logFile = file
	u.logPath = logPath
	u.logFile.WriteString("[" + time.Now().Format("15:04:05") + "] Log started: " + logPath + "\n")
	u.logFile.WriteString("[" + time.Now().Format("15:04:05") + "] Input: " + inputPath + "\n")
	u.logFile.WriteString("[" + time.Now().Format("15:04:05") + "] Mode: " + string(mode) + "\n")
	u.mu.Unlock()
	u.setLogActionButtons(true)
}

func (u *ui) closeLogFile() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.logFile != nil {
		u.logFile.WriteString("[" + time.Now().Format("15:04:05") + "] Log finished.\n")
		_ = u.logFile.Close()
		u.logFile = nil
	}
}

func (u *ui) copyLog() {
	text, err := u.logTextForCopy()
	if err != nil {
		if u.window != nil {
			dialog.ShowError(err, u.window)
		}
		return
	}
	if strings.TrimSpace(text) == "" {
		if u.window != nil {
			dialog.ShowInformation("No log", "There is no log to copy yet.", u.window)
		}
		return
	}
	if u.application != nil && u.application.Clipboard() != nil {
		u.application.Clipboard().SetContent(text)
	}
	if u.window != nil {
		dialog.ShowInformation("Log copied", "The current log was copied to the clipboard.", u.window)
	}
}

func (u *ui) openLogFolder() {
	folder, err := u.currentLogFolder()
	if err != nil {
		if u.window != nil {
			dialog.ShowError(err, u.window)
		}
		return
	}
	target, err := fileURL(folder)
	if err != nil {
		if u.window != nil {
			dialog.ShowError(err, u.window)
		}
		return
	}
	if u.application != nil {
		if err := u.application.OpenURL(target); err != nil && u.window != nil {
			dialog.ShowError(err, u.window)
		}
	}
}

func (u *ui) logTextForCopy() (string, error) {
	u.mu.Lock()
	logPath := u.logPath
	fallback := u.logBuilder.String()
	u.mu.Unlock()

	if logPath != "" {
		data, err := os.ReadFile(filepath.Clean(logPath))
		if err == nil {
			return string(data), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("read log file: %w", err)
		}
	}
	return fallback, nil
}

func (u *ui) currentLogFolder() (string, error) {
	u.mu.Lock()
	logPath := u.logPath
	u.mu.Unlock()
	if logPath == "" {
		return "", errors.New("no log file has been created yet")
	}
	folder := filepath.Dir(logPath)
	info, err := os.Stat(folder)
	if err != nil {
		return "", fmt.Errorf("open log folder: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("open log folder: %s is not a directory", folder)
	}
	return folder, nil
}

func (u *ui) setLogActionButtons(enabled bool) {
	if u.copyLogButton != nil {
		if enabled {
			u.copyLogButton.Enable()
		} else {
			u.copyLogButton.Disable()
		}
	}
	if u.openLogFolderButton != nil {
		if enabled {
			u.openLogFolderButton.Enable()
		} else {
			u.openLogFolderButton.Disable()
		}
	}
}

func fileURL(path string) (*url.URL, error) {
	return url.Parse(storage.NewFileURI(path).String())
}

// OnLog implements pipeline.Observer.
func (u *ui) OnLog(line string) {
	line = executil.RedactSecrets(line)
	u.mu.Lock()
	appendDisplayLog(&u.logBuilder, line, maxLogBytes)
	if u.logFile != nil && strings.TrimSpace(line) != "" {
		u.logFile.WriteString("[" + time.Now().Format("15:04:05") + "] " + line + "\n")
	}
	if u.logRefreshPending {
		u.logRefreshDirty = true
		u.mu.Unlock()
		return
	}
	u.logRefreshPending = true
	u.mu.Unlock()
	u.scheduleLogRefresh()
}

func (u *ui) scheduleLogRefresh() {
	time.AfterFunc(logRefreshInterval, func() {
		fyne.Do(u.flushLogEntry)
	})
}

func (u *ui) flushLogEntry() {
	u.mu.Lock()
	text := u.logBuilder.String()
	u.logRefreshDirty = false
	u.mu.Unlock()

	u.logEntry.SetText(text)
	u.logEntry.CursorRow, u.logEntry.CursorColumn = cursorEnd(text)
	u.logEntry.Refresh()

	u.mu.Lock()
	needsRefresh := u.logRefreshDirty
	if needsRefresh {
		u.logRefreshDirty = false
	} else {
		u.logRefreshPending = false
	}
	u.mu.Unlock()
	if needsRefresh {
		u.scheduleLogRefresh()
	}
}

func appendDisplayLog(builder *strings.Builder, line string, maxBytes int) string {
	if builder.Len() > 0 {
		builder.WriteByte('\n')
	}
	builder.WriteString(line)
	text := builder.String()
	if maxBytes <= 0 || len(text) <= maxBytes {
		return text
	}
	text = strings.ToValidUTF8(text[len(text)-maxBytes:], "")
	if newline := strings.IndexByte(text, '\n'); newline >= 0 {
		text = text[newline+1:]
	}
	builder.Reset()
	builder.WriteString(text)
	return text
}

func cursorEnd(text string) (row, column int) {
	row = strings.Count(text, "\n")
	lastLineStart := strings.LastIndexByte(text, '\n') + 1
	column = utf8.RuneCountInString(text[lastLineStart:])
	return row, column
}

// OnStep implements pipeline.Observer.
func (u *ui) OnStep(step pipeline.Step, state pipeline.State) {
	if step < 0 || int(step) >= len(u.steps) {
		return
	}
	fyne.Do(func() { u.steps[step].setState(state) })
}

func (u *ui) selectedMode() config.Mode {
	if u.mode != nil && u.mode.Selected == guiModeSubtitle {
		return config.ModeSubtitle
	}
	return config.ModeDub
}

func (u *ui) refreshModeControls() {
	subtitleMode := u.selectedMode() == config.ModeSubtitle
	if u.start != nil {
		if subtitleMode {
			u.start.SetText("▶  Start Subtitling")
		} else {
			u.start.SetText("▶  Start Dubbing")
		}
	}
	if u.burnIn != nil {
		if subtitleMode {
			u.burnIn.Show()
			if u.running {
				u.burnIn.Disable()
			} else {
				u.burnIn.Enable()
			}
		} else {
			u.burnIn.Hide()
			u.burnIn.Disable()
		}
	}
	if u.stepsBox != nil && !u.running {
		u.rebuildStepIndicators()
	}
}

func (u *ui) rebuildStepIndicators() {
	if u.stepsBox == nil {
		return
	}
	labels := pipeline.StepLabelsForModeOptions(u.selectedMode(), u.selectedMode() == config.ModeSubtitle && u.burnIn != nil && u.burnIn.Checked)
	u.steps = make([]*stepIndicator, 0, len(labels))
	u.stepsBox.Objects = nil
	for index, label := range labels {
		indicator := newStepIndicator(index+1, label)
		u.steps = append(u.steps, indicator)
		u.stepsBox.Add(indicator.root)
	}
	u.stepsBox.Refresh()
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func localURIPath(uri fyne.URI) string {
	path := filepath.FromSlash(uri.Path())
	// file:// URIs on Windows commonly expose /C:/...; os.Stat expects C:\... .
	if runtime.GOOS == "windows" && len(path) >= 3 && (path[0] == '/' || path[0] == '\\') && path[2] == ':' {
		path = path[1:]
	}
	return path
}
