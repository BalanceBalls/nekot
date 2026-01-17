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

//go:embed glamour-styles/pink-light.json
var pinkLightThemeBytes []byte

//go:embed glamour-styles/blue.json
var blueThemeBytes []byte

//go:embed glamour-styles/blue-light.json
var blueLightThemeBytes []byte

var (
	pinkThemeLightPink       = "#d48ac8"
	pinkThemePurple          = "#8C3A87"
	pinkThemeDarkPurpleLight = "#432D59"
	pinkThemeSolidPink       = "#BD54BF"
	pinkThemeBlueLight       = "#617a85"
	pinkThemeGrey            = "#9c9a97"
	pinkThemeRed             = "#DE3163"
	pinkThemeWhite           = "#FFFFFF"
	pinkThemeLightGrey       = "#bbbbbb"
	pinkThemeDarkGreyLight   = "#807c7c"
)

var (
	blueThemeSmoothBlue      = "#90a0d3"
	blueThemeDarkBlueLight   = "#39456b"
	blueThemePinkYellow      = "#e3b89f"
	blueThemePinkYellowLight = "#535e8a"
	blueThemeLightGreen      = "#70b55b"
	blueThemeRed             = "#DE3163"
	blueThemeSmoothRed       = "#8a7774"
	blueThemeWhite           = "#FFFFFF"
)

var (
	grooveboxOrange      = "#DD843B"
	grooveboxOrangeLight = "#a16f2a"
	grooveboxGreen       = "#98971A"
	grooveboxGreenLight  = "#7f9150"
	grooveboxBlue        = "#458588"
	grooveboxBlueLight   = "#73959e"
	grooveboxRed         = "#FB4934"
	grooveboxRedLight    = "#803a32"
	grooveboxGrey        = "#EBDBB2"
	grooveboxGreyLight   = "#3d2e07"
	grooveboxYellow      = "#C0A568"
	grooveboxYellowLight = "#917536"
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
		MainColor:            lipgloss.AdaptiveColor{Dark: pinkThemeLightPink, Light: pinkThemeLightPink},
		AccentColor:          lipgloss.AdaptiveColor{Dark: pinkThemePurple, Light: pinkThemePurple},
		HighlightColor:       lipgloss.AdaptiveColor{Dark: pinkThemeGrey, Light: pinkThemeBlueLight},
		DefaultTextColor:     lipgloss.AdaptiveColor{Dark: pinkThemeWhite, Light: pinkThemeDarkPurpleLight},
		ErrorColor:           lipgloss.AdaptiveColor{Dark: pinkThemeRed, Light: pinkThemeRed},
		NormalTabBorderColor: lipgloss.AdaptiveColor{Dark: pinkThemeLightGrey, Light: pinkThemeDarkGreyLight},
		ActiveTabBorderColor: lipgloss.AdaptiveColor{Dark: pinkThemeSolidPink, Light: pinkThemeSolidPink},
		RendererThemeOption:  glamour.WithStylesFromJSONBytes(pinkThemeBytes),
	}

	switch s {
	case SmoothBlue:
		themeBytes := blueThemeBytes
		if !lipgloss.HasDarkBackground() {
			themeBytes = blueLightThemeBytes
		}
		return SchemeColors{
			MainColor:            lipgloss.AdaptiveColor{Dark: blueThemePinkYellow, Light: blueThemePinkYellowLight},
			AccentColor:          lipgloss.AdaptiveColor{Dark: blueThemeLightGreen, Light: blueThemeLightGreen},
			HighlightColor:       lipgloss.AdaptiveColor{Dark: blueThemeSmoothRed, Light: blueThemeSmoothRed},
			DefaultTextColor:     lipgloss.AdaptiveColor{Dark: blueThemeWhite, Light: blueThemeDarkBlueLight},
			ErrorColor:           lipgloss.AdaptiveColor{Dark: blueThemeRed, Light: blueThemeSmoothRed},
			NormalTabBorderColor: lipgloss.AdaptiveColor{Dark: blueThemeSmoothBlue, Light: blueThemeSmoothBlue},
			ActiveTabBorderColor: lipgloss.AdaptiveColor{Dark: blueThemePinkYellow, Light: blueThemePinkYellowLight},
			RendererThemeOption:  glamour.WithStylesFromJSONBytes(themeBytes),
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
			NormalTabBorderColor: lipgloss.AdaptiveColor{Dark: grooveboxYellow, Light: grooveboxYellowLight},
			ActiveTabBorderColor: lipgloss.AdaptiveColor{Dark: grooveboxGreen, Light: grooveboxGreenLight},
			RendererThemeOption:  glamour.WithStylesFromJSONBytes(themeBytes),
		}

	case OriginalPink:
		themeBytes := pinkThemeBytes
		if !lipgloss.HasDarkBackground() {
			themeBytes = pinkLightThemeBytes
		}
		defaultColors.RendererThemeOption = glamour.WithStylesFromJSONBytes(themeBytes)
		return defaultColors

	default:
		return defaultColors
	}
}
