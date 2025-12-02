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

//go:embed glamour-styles/groovebox-light.json
var grooveBoxLightThemeBytes []byte

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
	black     = "#000000"
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
	grooveboxOrange      = "#DD843B"
	grooveboxOrangeLight = "#593b11"
	grooveboxGreen       = "#98971A"
	grooveboxGreenLight  = "#7f9150"
	grooveboxBlue        = "#458588"
	grooveboxBlueLight   = "#73959e"
	grooveboxPurple      = "#B16286"
	grooveboxRed         = "#FB4934"
	grooveboxRedLight    = "#803a32"
	grooveboxGrey        = "#EBDBB2"
	grooveboxGreyLight   = "#333028"
	grooveboxYellow      = "#C0A568"
	grooveboxBlack       = "#222911"
)

type SchemeColors struct {
	MainColor            lipgloss.AdaptiveColor
	AccentColor          lipgloss.AdaptiveColor
	HighlightColor       lipgloss.AdaptiveColor
	DefaultTextColor     lipgloss.AdaptiveColor
	ErrorColor           lipgloss.AdaptiveColor
	NormalTabBorderColor lipgloss.AdaptiveColor
	ActiveTabBorderColor lipgloss.AdaptiveColor
	RendererThemeOption  glamour.TermRendererOption
}

func (s ColorScheme) GetColors() SchemeColors {
	defaultColors := SchemeColors{
		MainColor:            lipgloss.AdaptiveColor{Dark: pink100, Light: pink100},
		AccentColor:          lipgloss.AdaptiveColor{Dark: pink200, Light: pink200},
		HighlightColor:       lipgloss.AdaptiveColor{Dark: pink300, Light: pink300},
		DefaultTextColor:     lipgloss.AdaptiveColor{Dark: white, Light: white},
		ErrorColor:           lipgloss.AdaptiveColor{Dark: red, Light: red},
		NormalTabBorderColor: lipgloss.AdaptiveColor{Dark: lightGrey, Light: lightGrey},
		ActiveTabBorderColor: lipgloss.AdaptiveColor{Dark: pink300, Light: pink300},
		RendererThemeOption:  glamour.WithStylesFromJSONBytes(pinkThemeBytes),
	}

	switch s {
	case SmoothBlue:
		return SchemeColors{
			MainColor:            lipgloss.AdaptiveColor{Dark: pinkYellow, Light: pinkYellow},
			AccentColor:          lipgloss.AdaptiveColor{Dark: lightGreen, Light: lightGreen},
			HighlightColor:       lipgloss.AdaptiveColor{Dark: smoothRed, Light: smoothRed},
			DefaultTextColor:     lipgloss.AdaptiveColor{Dark: white, Light: white},
			ErrorColor:           lipgloss.AdaptiveColor{Dark: red, Light: red},
			NormalTabBorderColor: lipgloss.AdaptiveColor{Dark: smoothBlue, Light: smoothBlue},
			ActiveTabBorderColor: lipgloss.AdaptiveColor{Dark: pinkYellow, Light: pinkYellow},
			RendererThemeOption:  glamour.WithStylesFromJSONBytes(blueThemeBytes),
		}

	case Groovebox:
		themeBytes := grooveBoxThemeBytes
		if !lipgloss.HasDarkBackground() {
			themeBytes = grooveBoxLightThemeBytes
		}
		return SchemeColors{
			MainColor:            lipgloss.AdaptiveColor{Dark: grooveboxOrange, Light: grooveboxOrangeLight},
			AccentColor:          lipgloss.AdaptiveColor{Dark: grooveboxGreen, Light: grooveboxGreenLight},
			HighlightColor:       lipgloss.AdaptiveColor{Dark: grooveboxBlue, Light: grooveboxBlueLight},
			DefaultTextColor:     lipgloss.AdaptiveColor{Dark: grooveboxGrey, Light: grooveboxGreyLight},
			ErrorColor:           lipgloss.AdaptiveColor{Dark: grooveboxRed, Light: grooveboxRedLight},
			NormalTabBorderColor: lipgloss.AdaptiveColor{Dark: grooveboxYellow, Light: grooveboxYellow},
			ActiveTabBorderColor: lipgloss.AdaptiveColor{Dark: grooveboxGreen, Light: grooveboxGreen},
			RendererThemeOption:  glamour.WithStylesFromJSONBytes(themeBytes),
		}

	case OriginalPink:
		return defaultColors

	default:
		return defaultColors
	}
}
