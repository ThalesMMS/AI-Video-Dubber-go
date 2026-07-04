package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// Catppuccin Mocha inspired palette.
var (
	colorBackground = hexColor(0x1e, 0x1e, 0x2e)
	colorCard       = hexColor(0x28, 0x28, 0x3c)
	colorCardAlt    = hexColor(0x31, 0x32, 0x4a)
	colorInput      = hexColor(0x18, 0x18, 0x25)
	colorForeground = hexColor(0xcd, 0xd6, 0xf4)
	colorDim        = hexColor(0x8a, 0x8f, 0xad)
	colorAccent     = hexColor(0x89, 0xb4, 0xfa)
	colorAccentDim  = hexColor(0x6c, 0x8d, 0xca)
	colorHover      = hexColor(0x74, 0xc7, 0xec)
	colorSuccess    = hexColor(0xa6, 0xe3, 0xa1)
	colorWarning    = hexColor(0xf9, 0xe2, 0xaf)
	colorError      = hexColor(0xf3, 0x8b, 0xa8)
	colorBorder     = hexColor(0x3b, 0x3d, 0x54)
	// colorCrust is a near-black tone used as text drawn on top of the light
	// pastel accent/success/error colors above, matching Catppuccin's own
	// convention of pairing bright accents with a very dark foreground.
	colorCrust = hexColor(0x11, 0x11, 0x1b)
)

func alpha(c color.NRGBA, a uint8) color.NRGBA {
	c.A = a
	return c
}

var (
	colorAccentSoft    = alpha(colorAccent.(color.NRGBA), 0x30)
	colorSuccessSoft   = alpha(colorSuccess.(color.NRGBA), 0x26)
	colorErrorSoft     = alpha(colorError.(color.NRGBA), 0x26)
	colorSelectionSoft = alpha(colorAccent.(color.NRGBA), 0x55)
	colorFocusSoft     = alpha(colorAccent.(color.NRGBA), 0x30)
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
		return colorAccentDim
	case theme.ColorNameFocus:
		return colorFocusSoft
	case theme.ColorNameSelection:
		return colorSelectionSoft
	case theme.ColorNameSeparator:
		return colorBorder
	case theme.ColorNamePlaceHolder:
		return colorDim
	case theme.ColorNameButton:
		return colorCardAlt
	case theme.ColorNameDisabledButton:
		return hexColor(0x2c, 0x2d, 0x3f)
	case theme.ColorNameScrollBar:
		return colorDim
	case theme.ColorNameScrollBarBackground:
		return colorCard
	case theme.ColorNameHyperlink:
		return colorHover
	case theme.ColorNameMenuBackground:
		return colorCard
	case theme.ColorNameOverlayBackground:
		return colorBackground
	case theme.ColorNameHeaderBackground:
		return colorCard
	case theme.ColorNameSuccess:
		return colorSuccess
	case theme.ColorNameWarning:
		return colorWarning
	case theme.ColorNameError:
		return colorError
	case theme.ColorNameForegroundOnPrimary,
		theme.ColorNameForegroundOnSuccess,
		theme.ColorNameForegroundOnWarning,
		theme.ColorNameForegroundOnError:
		return colorCrust
	case theme.ColorNameShadow:
		return color.NRGBA{A: 0x50}
	}
	return t.base.Color(name, theme.VariantDark)
}

func (t *dubberTheme) Font(style fyne.TextStyle) fyne.Resource    { return t.base.Font(style) }
func (t *dubberTheme) Icon(name fyne.ThemeIconName) fyne.Resource { return t.base.Icon(name) }

func (t *dubberTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 8
	case theme.SizeNameInnerPadding:
		return 14
	case theme.SizeNameInputRadius:
		return 10
	case theme.SizeNameSelectionRadius:
		return 6
	case theme.SizeNameScrollBarRadius:
		return 6
	case theme.SizeNameScrollBar:
		return 10
	case theme.SizeNameHeadingText:
		return 26
	case theme.SizeNameSubHeadingText:
		return 17
	}
	return t.base.Size(name)
}

func hexColor(red, green, blue uint8) color.Color {
	return color.NRGBA{R: red, G: green, B: blue, A: 0xff}
}
