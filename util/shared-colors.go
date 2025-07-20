package util

import (
	_ "embed"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type ColorScheme string

const (
	OriginalPink ColorScheme = "pink"
	SmoothBlue   ColorScheme = "blue"
	Groovebox    ColorScheme = "groove"
)

//go:embed glamour-styles/groovebox.json
var grooveBoxThemeBytes []byte

//go:embed glamour-styles/pink.json
var pinkThemeBytes []byte

//go:embed glamour-styles/blue.json
var blueThemeBytes []byte

var (
	pink100   = "#F2B3E8"
	pink200   = "#8C3A87"
	pink300   = "#BD54BF"
	purple    = "#432D59"
	red       = "#DE3163"
	white     = "#FFFFFF"
	lightGrey = "#bbbbbb"
)

var (
	smoothBlue = "#90a0d3"
	pinkYellow = "#e3b89f"
	cyan       = "#c3f7f5"
	lightGreen = "#a0d390"
	blue       = "#6b81c5"
	smoothRed  = "#af5f5f"
)

var (
	grooveboxOrange    = "#DD843B"
	grooveboxGreen     = "#98971a"
	grooveboxBlue      = "#458588"
	grooveboxPurple    = "#B16286"
	grooveboxRed       = "#FB4934"
	grooveboxLightGrey = "#EBDBB2"
	grooveboxYellow    = "#C0A568"
)

type SchemeColors struct {
	MainColor            lipgloss.Color
	AccentColor          lipgloss.Color
	HighlightColor       lipgloss.Color
	DefaultTextColor     lipgloss.Color
	ErrorColor           lipgloss.Color
	NormalTabBorderColor lipgloss.Color
	ActiveTabBorderColor lipgloss.Color
	RendererThemeOption  glamour.TermRendererOption
}

func (s ColorScheme) GetColors() SchemeColors {
	defaultColors := SchemeColors{
		MainColor:            lipgloss.Color(pink100),
		AccentColor:          lipgloss.Color(pink200),
		HighlightColor:       lipgloss.Color(pink300),
		DefaultTextColor:     lipgloss.Color(white),
		ErrorColor:           lipgloss.Color(red),
		NormalTabBorderColor: lipgloss.Color(lightGrey),
		ActiveTabBorderColor: lipgloss.Color(pink300),
		RendererThemeOption:  glamour.WithStylesFromJSONBytes(pinkThemeBytes),
	}

	switch s {
	case SmoothBlue:
		return SchemeColors{
			MainColor:            lipgloss.Color(pinkYellow),
			AccentColor:          lipgloss.Color(lightGreen),
			HighlightColor:       lipgloss.Color(smoothRed),
			DefaultTextColor:     lipgloss.Color(white),
			ErrorColor:           lipgloss.Color(red),
			NormalTabBorderColor: lipgloss.Color(smoothBlue),
			ActiveTabBorderColor: lipgloss.Color(pinkYellow),
			RendererThemeOption:  glamour.WithStylesFromJSONBytes(blueThemeBytes),
		}

	case Groovebox:
		return SchemeColors{
			MainColor:            lipgloss.Color(grooveboxOrange),
			AccentColor:          lipgloss.Color(grooveboxGreen),
			HighlightColor:       lipgloss.Color(grooveboxBlue),
			DefaultTextColor:     lipgloss.Color(grooveboxLightGrey),
			ErrorColor:           lipgloss.Color(grooveboxRed),
			NormalTabBorderColor: lipgloss.Color(grooveboxYellow),
			ActiveTabBorderColor: lipgloss.Color(grooveboxGreen),
			RendererThemeOption:  glamour.WithStylesFromJSONBytes(grooveBoxThemeBytes),
		}

	case OriginalPink:
		return defaultColors

	default:
		return defaultColors
	}
}
