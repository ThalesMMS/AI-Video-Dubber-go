package gui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/pipeline"
)

type stepIndicator struct {
	root  fyne.CanvasObject
	icon  *canvas.Text
	label *canvas.Text
}

func newStepIndicator(number int, label string) *stepIndicator {
	icon := canvas.NewText("○", colorDim)
	icon.TextSize = 18
	icon.Alignment = fyne.TextAlignCenter
	iconCell := container.NewGridWrap(fyne.NewSize(34, 30), icon)

	text := canvas.NewText(fmt.Sprintf("Step %d:  %s", number, label), colorDim)
	text.TextSize = 14
	root := container.NewBorder(nil, nil, iconCell, nil, text)
	return &stepIndicator{root: root, icon: icon, label: text}
}

func (s *stepIndicator) setState(state pipeline.State) {
	character := "○"
	iconColor := colorDim
	labelColor := colorDim
	switch state {
	case pipeline.StateRunning:
		character = "◉"
		iconColor = colorAccent
		labelColor = colorForeground
	case pipeline.StateDone:
		character = "✓"
		iconColor = colorSuccess
		labelColor = colorForeground
	case pipeline.StateError:
		character = "✗"
		iconColor = colorError
		labelColor = colorError
	}
	s.icon.Text = character
	s.icon.Color = iconColor
	s.label.Color = labelColor
	s.icon.Refresh()
	s.label.Refresh()
}
