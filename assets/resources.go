// Package assets embeds application resources into the binary.
package assets

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed icon.svg
var iconSVG []byte

// Icon is the application/window icon.
var Icon = fyne.NewStaticResource("ai-video-dubber.svg", iconSVG)
