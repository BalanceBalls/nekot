package util

import (
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
)

var SubduedColor = compat.AdaptiveColor{
	Light: lipgloss.Color("#9B9B9B"),
	Dark:  lipgloss.Color("#5C5C5C"),
}
var HelpStyle = lipgloss.NewStyle().Padding(0, 0, 0, 2).Foreground(SubduedColor)

const ActiveDot = "■"
const InactiveDot = "•"

const ListHeadingDot = "■"

const TipsSeparator = " • "
