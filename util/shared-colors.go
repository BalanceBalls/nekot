package util

import (
	_ "embed"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
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
	MainColor            compat.AdaptiveColor
	AccentColor          compat.AdaptiveColor
	HighlightColor       compat.AdaptiveColor
	DefaultTextColor     compat.AdaptiveColor
	ErrorColor           compat.AdaptiveColor
	NormalTabBorderColor compat.AdaptiveColor
	ActiveTabBorderColor compat.AdaptiveColor
	RendererThemeOption  glamour.TermRendererOption
}

func adaptiveColor(dark, light string) compat.AdaptiveColor {
	return compat.AdaptiveColor{
		Dark:  lipgloss.Color(dark),
		Light: lipgloss.Color(light),
	}
}

func (s ColorScheme) GetColors() SchemeColors {
	defaultThemeBytes := pinkThemeBytes
	if !compat.HasDarkBackground {
		defaultThemeBytes = pinkLightThemeBytes
	}
	defaultColors := SchemeColors{
		MainColor:            adaptiveColor(pinkThemeLightPink, pinkThemeLightPink),
		AccentColor:          adaptiveColor(pinkThemePurple, pinkThemePurple),
		HighlightColor:       adaptiveColor(pinkThemeGrey, pinkThemeBlueLight),
		DefaultTextColor:     adaptiveColor(pinkThemeWhite, pinkThemeDarkPurpleLight),
		ErrorColor:           adaptiveColor(pinkThemeRed, pinkThemeRed),
		NormalTabBorderColor: adaptiveColor(pinkThemeLightGrey, pinkThemeDarkGreyLight),
		ActiveTabBorderColor: adaptiveColor(pinkThemeSolidPink, pinkThemeSolidPink),
		RendererThemeOption:  glamour.WithStylesFromJSONBytes(defaultThemeBytes),
	}

	switch s {
	case SmoothBlue:
		themeBytes := blueThemeBytes
		if !compat.HasDarkBackground {
			themeBytes = blueLightThemeBytes
		}
		return SchemeColors{
			MainColor:            adaptiveColor(blueThemePinkYellow, blueThemePinkYellowLight),
			AccentColor:          adaptiveColor(blueThemeLightGreen, blueThemeLightGreen),
			HighlightColor:       adaptiveColor(blueThemeSmoothRed, blueThemeSmoothRed),
			DefaultTextColor:     adaptiveColor(blueThemeWhite, blueThemeDarkBlueLight),
			ErrorColor:           adaptiveColor(blueThemeRed, blueThemeRed),
			NormalTabBorderColor: adaptiveColor(blueThemeSmoothBlue, blueThemeSmoothBlue),
			ActiveTabBorderColor: adaptiveColor(blueThemePinkYellow, blueThemePinkYellowLight),
			RendererThemeOption:  glamour.WithStylesFromJSONBytes(themeBytes),
		}

	case Groovebox:
		themeBytes := grooveBoxThemeBytes
		if !compat.HasDarkBackground {
			themeBytes = grooveBoxLightThemeBytes
		}
		return SchemeColors{
			MainColor:            adaptiveColor(grooveboxOrange, grooveboxOrangeLight),
			AccentColor:          adaptiveColor(grooveboxGreen, grooveboxGreenLight),
			HighlightColor:       adaptiveColor(grooveboxBlue, grooveboxBlueLight),
			DefaultTextColor:     adaptiveColor(grooveboxGrey, grooveboxGreyLight),
			ErrorColor:           adaptiveColor(grooveboxRed, grooveboxRedLight),
			NormalTabBorderColor: adaptiveColor(grooveboxYellow, grooveboxYellowLight),
			ActiveTabBorderColor: adaptiveColor(grooveboxGreen, grooveboxGreenLight),
			RendererThemeOption:  glamour.WithStylesFromJSONBytes(themeBytes),
		}

	case OriginalPink:
		themeBytes := pinkThemeBytes
		if !compat.HasDarkBackground {
			themeBytes = pinkLightThemeBytes
		}
		defaultColors.RendererThemeOption = glamour.WithStylesFromJSONBytes(themeBytes)
		return defaultColors

	default:
		return defaultColors
	}
}
