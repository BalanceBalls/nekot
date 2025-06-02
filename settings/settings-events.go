package settings

import (
	"github.com/BalanceBalls/nekot/util"
	tea "github.com/charmbracelet/bubbletea"
)

type UpdateSettingsEvent struct {
	Settings util.Settings
	Err      error
}

func MakeSettingsUpdateMsg(s util.Settings, err error) tea.Cmd {
	return func() tea.Msg {
		return UpdateSettingsEvent{Settings: s, Err: err}
	}
}
