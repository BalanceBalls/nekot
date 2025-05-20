package util

import (
	_ "embed"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

//go:embed short-manual.md
var manualContent string

const DefaultSettingsId = 0
const DefaultRequestTimeOutSec = 5
const ChunkIndexStart = 1
const WordWrapDelta = 5

const ErrorHelp = "\n\n > *Mechanism, I restore thy spirit!\n > Let the God-Machine breathe half-life \n > unto thy veins and render thee functional* "

func GetManual(w int, colors SchemeColors) string {
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithPreservedNewLines(),
		glamour.WithWordWrap(40),
		colors.RendererThemeOption,
	)
	output, _ := renderer.Render(manualContent)
	return lipgloss.NewStyle().
		MaxWidth(w).
		Render(output)
}
