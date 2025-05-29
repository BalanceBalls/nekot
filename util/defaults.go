package util

import (
	_ "embed"
)

//go:embed short-manual.md
var ManualContent string

const DefaultSettingsId = 0
const DefaultRequestTimeOutSec = 5
const ChunkIndexStart = 1
const WordWrapDelta = 5

const ErrorHelp = "\n\n > *Mechanism, I restore thy spirit!\n > Let the God-Machine breathe half-life \n > unto thy veins and render thee functional* "
const QuickChatWarning = " > *Quick chat is active.* \n > The conversation will not be stored as a session. \n > Use `ctrl+x` to save a quick chat \n <!-------->"
