package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

var (
	colorBackground = hexColor(0x1e, 0x1e, 0x2e)
	colorCard       = hexColor(0x2a, 0x2a, 0x3c)
	colorInput      = hexColor(0x18, 0x18, 0x25)
	colorForeground = hexColor(0xcd, 0xd6, 0xf4)
	colorDim        = hexColor(0x6c, 0x70, 0x86)
	colorAccent     = hexColor(0x89, 0xb4, 0xfa)
	colorHover      = hexColor(0x74, 0xc7, 0xec)
	colorSuccess    = hexColor(0xa6, 0xe3, 0xa1)
	colorError      = hexColor(0xf3, 0x8b, 0xa8)
	colorBorder     = hexColor(0x31, 0x32, 0x44)
)

type dubberTheme struct {
	base fyne.Theme
}

func newDubberTheme() fyne.Theme { return &dubberTheme{base: theme.DefaultTheme()} }

func (t *dubberTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return colorBackground
	case theme.ColorNameForeground:
		return colorForeground
	case theme.ColorNameDisabled:
		return colorDim
	case theme.ColorNameInputBackground:
		return colorInput
	case theme.ColorNameInputBorder:
		return colorBorder
	case theme.ColorNamePrimary:
		return colorAccent
	case theme.ColorNameHover:
		return colorHover
	case theme.ColorNamePressed:
		return colorHover
	case theme.ColorNameSelection:
		return colorAccent
	case theme.ColorNameSeparator:
		return colorBorder
	case theme.ColorNamePlaceHolder:
		return colorDim
	case theme.ColorNameButton:
		return hexColor(0x45, 0x47, 0x5a)
	case theme.ColorNameDisabledButton:
		return hexColor(0x31, 0x32, 0x44)
	case theme.ColorNameScrollBar:
		return colorDim
	}
	return t.base.Color(name, theme.VariantDark)
}

func (t *dubberTheme) Font(style fyne.TextStyle) fyne.Resource    { return t.base.Font(style) }
func (t *dubberTheme) Icon(name fyne.ThemeIconName) fyne.Resource { return t.base.Icon(name) }
func (t *dubberTheme) Size(name fyne.ThemeSizeName) float32       { return t.base.Size(name) }

func hexColor(red, green, blue uint8) color.Color {
	return color.NRGBA{R: red, G: green, B: blue, A: 0xff}
}
