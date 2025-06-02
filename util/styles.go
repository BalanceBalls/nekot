package util

import "github.com/charmbracelet/lipgloss"

var subduedColor = lipgloss.AdaptiveColor{Light: "#9B9B9B", Dark: "#5C5C5C"}
var HelpStyle = lipgloss.NewStyle().Padding(0, 0, 0, 2).Foreground(subduedColor)

const ActiveDot = "■"
const InactiveDot = "•"

const ListHeadingDot = "■"

const TipsSeparator = " • "
