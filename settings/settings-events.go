package settings

import (
	tea "charm.land/bubbletea/v2"
	"github.com/BalanceBalls/nekot/util"
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
