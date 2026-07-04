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

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"github.com/ai-video-dubber/ai-video-dubber-go/assets"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/config"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/language"
	"github.com/ai-video-dubber/ai-video-dubber-go/internal/pipeline"
)

const maxLogBytes = 240_000

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

	fileLabel *widget.Label
	apiBase   *widget.Entry
	apiKey    *widget.Entry
	model     *widget.Entry
	language  *widget.Select
	browse    *widget.Button
	start     *widget.Button
	cancel    *widget.Button
	logEntry  *widget.Entry
	steps     []*stepIndicator

	mu         sync.Mutex
	logBuilder strings.Builder
	logFile    *os.File
	cancelRun  context.CancelFunc
	running    bool
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

	result.browse = widget.NewButton("Browse…", result.openFileDialog)
	result.browse.Importance = widget.HighImportance
	result.start = widget.NewButton("▶  Start Dubbing", result.startPipeline)
	result.start.Importance = widget.HighImportance
	result.cancel = widget.NewButton("Cancel", result.cancelPipeline)
	result.cancel.Importance = widget.DangerImportance
	result.cancel.Disable()

	result.logEntry = widget.NewMultiLineEntry()
	result.logEntry.Wrapping = fyne.TextWrapWord
	result.logEntry.TextStyle = fyne.TextStyle{Monospace: true}
	result.logEntry.Disable()
	result.steps = make([]*stepIndicator, pipeline.StepCount)
	for index := pipeline.Step(0); index < pipeline.StepCount; index++ {
		result.steps[index] = newStepIndicator(int(index)+1, pipeline.StepLabels[index])
	}
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
	subtitle := canvas.NewText("Dub any video into another language, automatically.", colorDim)
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

	languageCard := card(3, "Choose target language", u.language)

	stepObjects := make([]fyne.CanvasObject, 0, len(u.steps)+1)
	for _, indicator := range u.steps {
		stepObjects = append(stepObjects, indicator.root)
	}
	logBackground := canvas.NewRectangle(colorInput)
	logBackground.CornerRadius = 10
	logBackground.StrokeColor = colorBorder
	logBackground.StrokeWidth = 1
	logBox := container.NewStack(logBackground, container.NewPadded(u.logEntry))
	logBox = container.NewGridWrap(fyne.NewSize(680, 200), logBox)
	stepObjects = append(stepObjects, container.NewPadded(logBox))
	progressCard := card(4, "Pipeline progress", container.NewVBox(stepObjects...))

	body := container.NewVBox(
		header,
		fileCard,
		llmCard,
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

	u.application.Preferences().SetString("api_base", apiBase)
	u.application.Preferences().SetString("model", strings.TrimSpace(u.model.Text))
	u.application.Preferences().SetString("language", lang.DisplayName)
	u.openLogFile(inputPath)
	u.resetRun()
	u.setRunning(true)
	ctx, cancel := context.WithCancel(context.Background())
	u.mu.Lock()
	u.cancelRun = cancel
	u.mu.Unlock()

	cfg := config.Defaults()
	cfg.InputPath = inputPath
	cfg.LanguageCode = lang.Code
	cfg.APIBase = apiBase
	cfg.APIKey = u.apiKey.Text
	cfg.Model = strings.TrimSpace(u.model.Text)
	cfg.Force = true
	if value := strings.TrimSpace(os.Getenv("WHISPER_MODEL")); value != "" {
		cfg.WhisperModel = value
	}
	if value := strings.TrimSpace(os.Getenv("VENV_DIR")); value != "" {
		cfg.VenvDir = value
	}
	if value := strings.TrimSpace(os.Getenv("DATA_DIR")); value != "" {
		cfg.VoiceDataDir = value
	}

	go func() {
		defer u.closeLogFile()
		result, runErr := (pipeline.Pipeline{ProjectDir: u.projectDir, Observer: u}).Run(ctx, cfg)
		switch {
		case runErr == nil:
			u.OnLog("")
			u.OnLog("✓  Done! Your dubbed video is ready.")
		case errors.Is(runErr, context.Canceled):
			u.OnLog("Pipeline cancelled by user.")
		default:
			u.OnLog("ERROR: " + runErr.Error())
		}

		fyne.Do(func() {
			u.setRunning(false)
			if runErr == nil {
				dialog.ShowInformation("Success", "Your dubbed video has been created:\n\n"+result.Paths.FinalVideo, u.window)
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
		u.cancel.Enable()
	} else {
		u.start.Enable()
		u.browse.Enable()
		u.language.Enable()
		u.cancel.Disable()
	}
}

func (u *ui) resetRun() {
	u.mu.Lock()
	u.logBuilder.Reset()
	u.mu.Unlock()
	u.logEntry.SetText("")
	for _, indicator := range u.steps {
		indicator.setState(pipeline.StatePending)
	}
}

func (u *ui) openLogFile(inputPath string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.logFile != nil {
		_ = u.logFile.Close()
		u.logFile = nil
	}
	logDir := filepath.Join(u.projectDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return
	}
	timestamp := time.Now().Format("20060102-150405")
	base := filepath.Base(inputPath)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "" {
		base = "dubbing"
	}
	logPath := filepath.Join(logDir, base+"-"+timestamp+".log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	u.logFile = file
	u.logFile.WriteString("[" + time.Now().Format("15:04:05") + "] Log started: " + logPath + "\n")
	u.logFile.WriteString("[" + time.Now().Format("15:04:05") + "] Input: " + inputPath + "\n")
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

// OnLog implements pipeline.Observer.
func (u *ui) OnLog(line string) {
	u.mu.Lock()
	if u.logBuilder.Len() > 0 {
		u.logBuilder.WriteByte('\n')
	}
	u.logBuilder.WriteString(line)
	text := u.logBuilder.String()
	if len(text) > maxLogBytes {
		text = strings.ToValidUTF8(text[len(text)-maxLogBytes:], "")
		if newline := strings.IndexByte(text, '\n'); newline >= 0 {
			text = text[newline+1:]
		}
		u.logBuilder.Reset()
		u.logBuilder.WriteString(text)
	}
	if u.logFile != nil && strings.TrimSpace(line) != "" {
		u.logFile.WriteString("[" + time.Now().Format("15:04:05") + "] " + line + "\n")
	}
	u.mu.Unlock()
	fyne.Do(func() {
		u.logEntry.SetText(text)
		lines := strings.Split(text, "\n")
		u.logEntry.CursorRow = len(lines) - 1
		if len(lines) > 0 {
			u.logEntry.CursorColumn = len([]rune(lines[len(lines)-1]))
		}
		u.logEntry.Refresh()
	})
}

// OnStep implements pipeline.Observer.
func (u *ui) OnStep(step pipeline.Step, state pipeline.State) {
	if step < 0 || step >= pipeline.StepCount {
		return
	}
	fyne.Do(func() { u.steps[step].setState(state) })
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
