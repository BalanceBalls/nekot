package components

import (
	"errors"
	"os"
	"time"

	"github.com/BalanceBalls/nekot/util"
	"github.com/charmbracelet/bubbles/filepicker"
	tea "github.com/charmbracelet/bubbletea"
)

type FilePicker struct {
	filepicker    filepicker.Model
	SelectedFile  string
	PrevView      util.ViewMode
	PrevInputData string
	quitting      bool
	err           error
}

func NewFilePicker(prevView util.ViewMode, prevInput string, colors util.SchemeColors) FilePicker {
	fp := filepicker.New()
	fp.ShowSize = true

	fp.Styles.Directory = fp.Styles.Directory.
		Foreground(colors.HighlightColor)

	fp.Styles.File = fp.Styles.File.
		Foreground(colors.NormalTabBorderColor)

	fp.Styles.Cursor = fp.Styles.Cursor.
		Foreground(colors.ActiveTabBorderColor)

	fp.Styles.Selected = fp.Styles.Selected.
		Foreground(colors.ActiveTabBorderColor)

	// fp.AllowedTypes = []string{}                           //[]string{".png", ".jpg", ".jpeg", ".webp", ".gif"}
	fp.CurrentDirectory, _ = os.UserHomeDir()

	filePicker := FilePicker{
		filepicker:    fp,
		PrevView:      prevView,
		PrevInputData: prevInput,
	}
	return filePicker
}

type clearErrorMsg struct{}

func clearErrorAfter(t time.Duration) tea.Cmd {
	return tea.Tick(t, func(_ time.Time) tea.Msg {
		return clearErrorMsg{}
	})
}

func (m FilePicker) Init() tea.Cmd {
	return m.filepicker.Init()
}

func (m FilePicker) Update(msg tea.Msg) (FilePicker, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q":
			m.quitting = true
			return m, util.SendViewModeChangedMsg(util.NormalMode)
		}

	case clearErrorMsg:
		m.err = nil
	}

	var cmd tea.Cmd
	m.filepicker, cmd = m.filepicker.Update(msg)

	if didSelect, path := m.filepicker.DidSelectFile(msg); didSelect {
		m.SelectedFile = path
	}

	if didSelect, path := m.filepicker.DidSelectDisabledFile(msg); didSelect {
		m.err = errors.New(path + " is not valid.")
		m.SelectedFile = ""
		return m, tea.Batch(cmd, clearErrorAfter(2*time.Second))
	}

	return m, cmd
}

func (m FilePicker) View() string {
	if m.quitting {
		return ""
	}
	return m.filepicker.View()
}

func (m *FilePicker) SetSize(w, h int) {
	if w > 2 && h > 2 {
		m.filepicker.SetHeight(h)
	}
}
