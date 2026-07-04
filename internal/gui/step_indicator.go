package gui

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"

	"github.com/ai-video-dubber/ai-video-dubber-go/internal/pipeline"
)

type stepIndicator struct {
	root    fyne.CanvasObject
	circle  *canvas.Rectangle
	icon    *canvas.Text
	label   *canvas.Text
	rowFill *canvas.Rectangle
	number  int
}

func newStepIndicator(number int, label string) *stepIndicator {
	circle := canvas.NewRectangle(colorInput)
	circle.CornerRadius = 15
	circle.StrokeColor = colorBorder
	circle.StrokeWidth = 1

	icon := canvas.NewText(fmt.Sprintf("%d", number), colorDim)
	icon.TextSize = 13
	icon.TextStyle = fyne.TextStyle{Bold: true}
	icon.Alignment = fyne.TextAlignCenter
	iconCell := container.NewGridWrap(fyne.NewSize(30, 30), container.NewStack(circle, container.NewCenter(icon)))

	text := canvas.NewText(label, colorDim)
	text.TextSize = 14

	rowFill := canvas.NewRectangle(color.Transparent)
	rowFill.CornerRadius = 8
	row := container.NewBorder(nil, nil, iconCell, nil, text)
	root := container.NewStack(rowFill, container.NewPadded(row))
	return &stepIndicator{root: root, circle: circle, icon: icon, label: text, rowFill: rowFill, number: number}
}

func (s *stepIndicator) setState(state pipeline.State) {
	circleColor := colorInput
	iconColor := colorDim
	iconText := fmt.Sprintf("%d", s.number)
	labelColor := colorDim
	rowColor := color.Color(color.Transparent)
	switch state {
	case pipeline.StateRunning:
		circleColor = colorAccent
		iconColor = colorCrust
		iconText = "●"
		labelColor = colorForeground
		rowColor = colorAccentSoft
	case pipeline.StateDone:
		circleColor = colorSuccess
		iconColor = colorCrust
		iconText = "✓"
		labelColor = colorForeground
	case pipeline.StateError:
		circleColor = colorError
		iconColor = colorCrust
		iconText = "✗"
		labelColor = colorError
		rowColor = colorErrorSoft
	}
	s.circle.FillColor = circleColor
	s.icon.Text = iconText
	s.icon.Color = iconColor
	s.label.Color = labelColor
	s.rowFill.FillColor = rowColor
	s.circle.Refresh()
	s.icon.Refresh()
	s.label.Refresh()
	s.rowFill.Refresh()
}
