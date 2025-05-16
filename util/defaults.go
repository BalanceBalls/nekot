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

func GetManual(w int, colors SchemeColors) string {
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithPreservedNewLines(),
		glamour.WithWordWrap(w),
		colors.RendererThemeOption,
	)
	output, _ := renderer.Render(manualContent)
	return lipgloss.NewStyle().
		PaddingLeft(0).
		Render(output)
}
