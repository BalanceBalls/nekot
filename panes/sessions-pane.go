package panes

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tearingItUp786/nekot/components"
	"github.com/tearingItUp786/nekot/config"
	"github.com/tearingItUp786/nekot/sessions"
	"github.com/tearingItUp786/nekot/user"
	"github.com/tearingItUp786/nekot/util"
)

const NoTargetSession = -1

type operationMode int

const (
	defaultMode operationMode = iota
	editMode
	deleteMode
)

type sessionsKeyMap struct {
	addNew key.Binding
	delete key.Binding
	rename key.Binding
	cancel key.Binding
	apply  key.Binding
}

var defaultSessionsKeyMap = sessionsKeyMap{
	delete: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "d delete")),
	rename: key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "e edit")),
	cancel: key.NewBinding(key.WithKeys(tea.KeyEsc.String()), key.WithHelp("esc", "cancel action")),
	apply:  key.NewBinding(key.WithKeys(tea.KeyEnter.String()), key.WithHelp("esc", "switch to session/apply renaming")),
	addNew: key.NewBinding(key.WithKeys("ctrl+n"), key.WithHelp("ctrl+n", "ctrl+n add new")),
}

var tips = []string{
	defaultSessionsKeyMap.addNew.Help().Desc,
	strings.Join([]string{
		defaultSessionsKeyMap.rename.Help().Desc,
		util.TipsSeparator,
		defaultSessionsKeyMap.delete.Help().Desc,
		util.TipsSeparator,
		"/ filter"}, ""),
}
var tipsOffset = len(tips) - 1 // 1 is the input field height

type SessionsPane struct {
	sessionsListData []sessions.Session
	sessionsList     components.SessionsList
	textInput        textinput.Model
	sessionService   *sessions.SessionService
	userService      *user.UserService
	container        lipgloss.Style
	colors           util.SchemeColors
	currentSession   sessions.Session
	operationMode    operationMode
	keyMap           sessionsKeyMap

	sessionsListReady  bool
	currentSessionId   int
	operationTargetId  int
	currentSessionName string
	isFocused          bool
	terminalWidth      int
	terminalHeight     int
	mainCtx            context.Context
}

func NewSessionsPane(db *sql.DB, ctx context.Context) SessionsPane {
	ss := sessions.NewSessionService(db)
	us := user.NewUserService(db)

	config, ok := config.FromContext(ctx)
	if !ok {
		fmt.Println("No config found")
		panic("No config found in context")
	}
	colors := config.ColorScheme.GetColors()

	return SessionsPane{
		mainCtx:           ctx,
		operationMode:     defaultMode,
		operationTargetId: NoTargetSession,
		keyMap:            defaultSessionsKeyMap,
		colors:            colors,
		sessionService:    ss,
		userService:       us,
		isFocused:         false,
		terminalWidth:     util.DefaultTerminalWidth,
		terminalHeight:    util.DefaultTerminalHeight,
		container: lipgloss.NewStyle().
			AlignVertical(lipgloss.Top).
			Border(lipgloss.ThickBorder(), true).
			BorderForeground(colors.NormalTabBorderColor),
	}
}

func (p SessionsPane) Init() tea.Cmd {
	return nil
}

func (p SessionsPane) Update(msg tea.Msg) (SessionsPane, tea.Cmd) {
	var (
		cmds []tea.Cmd
		cmd  tea.Cmd
	)

	switch msg := msg.(type) {

	case util.AddNewSessionMsg:
		cmds = append(cmds, p.addNewSession())

	case sessions.LoadDataFromDB:
		p.currentSession = msg.Session
		p.sessionsListData = msg.AllSessions
		p.currentSessionId = msg.CurrentActiveSessionID
		listItems := constructSessionsListItems(msg.AllSessions, msg.CurrentActiveSessionID)
		w, h := util.CalcSessionsListSize(p.terminalWidth, p.terminalHeight, 0)
		p.sessionsList = components.NewSessionsList(listItems, w, h, p.colors)
		p.operationMode = defaultMode
		p.sessionsListReady = true

	case util.FocusEvent:
		p.isFocused = msg.IsFocused
		p.operationMode = defaultMode
		width, height := util.CalcSessionsListSize(p.terminalWidth, p.terminalHeight, tipsOffset)
		p.sessionsList.SetSize(width, height)

	case tea.WindowSizeMsg:
		p.terminalWidth = msg.Width
		p.terminalHeight = msg.Height
		width, height := util.CalcSessionsPaneSize(p.terminalWidth, p.terminalHeight)
		p.container = p.container.Width(width).Height(height)
		if p.sessionsListReady {
			offset := 0
			if p.isFocused {
				offset = tipsOffset
			}
			width, height = util.CalcSessionsListSize(p.terminalWidth, p.terminalHeight, offset)
			p.sessionsList.SetSize(width, height)
		}

	case util.ProcessingStateChanged:
		if !msg.IsProcessing {
			session, err := p.sessionService.GetSession(p.currentSessionId)
			if err != nil {
				util.MakeErrorMsg(err.Error())
			}
			cmds = append(cmds, p.handleUpdateCurrentSession(session))
		}

	case tea.KeyMsg:
		if p.isFocused && !p.sessionsList.IsFiltering() {
			switch p.operationMode {
			case defaultMode:
				cmd := p.handleDefaultMode(msg)
				cmds = append(cmds, cmd)
			case deleteMode:
				cmd = p.handleDeleteMode(msg)
				cmds = append(cmds, cmd)
			case editMode:
				cmd = p.handleEditMode(msg)
				cmds = append(cmds, cmd)
			}
		}
	}

	if p.isFocused && p.operationTargetId == NoTargetSession && p.operationMode == defaultMode {
		p.sessionsList, cmd = p.sessionsList.Update(msg)
		cmds = append(cmds, cmd)
	}

	return p, tea.Batch(cmds...)
}

func (p SessionsPane) View() string {
	listView := p.normalListView()
	borderColor := p.colors.NormalTabBorderColor
	if !p.isFocused {
		return p.container.BorderForeground(borderColor).Render(
			lipgloss.JoinVertical(lipgloss.Left,
				p.listHeader("[Sessions]"),
				listView,
				"",
			),
		)
	}

	if p.sessionsList.IsFiltering() {
		p.sessionsList.SetShowStatusBar(false)
	} else {
		p.sessionsList.SetShowStatusBar(true)
	}

	borderColor = p.colors.ActiveTabBorderColor
	lowerRows := ""
	if p.operationTargetId != NoTargetSession {
		lowerRows = "\n" + p.textInput.View()
	} else {
		lowerRows = util.HelpStyle.Render(strings.Join(tips, "\n"))
	}

	return p.container.BorderForeground(borderColor).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			p.listHeader("[Sessions]"),
			p.sessionsList.EditListView(),
			lowerRows,
		),
	)
}

func (p *SessionsPane) handleDefaultMode(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd

	switch {

	case key.Matches(msg, p.keyMap.apply):
		i, ok := p.sessionsList.GetSelectedItem()
		if ok {
			session, err := p.sessionService.GetSession(i.Id)
			p.currentSessionId = i.Id
			if err != nil {
				util.MakeErrorMsg(err.Error())
			}

			cmd = p.handleUpdateCurrentSession(session)
		}

	case key.Matches(msg, p.keyMap.rename):
		p.operationMode = editMode
		ti := textinput.New()
		ti.PromptStyle = lipgloss.NewStyle().PaddingLeft(util.DefaultElementsPadding)
		p.textInput = ti
		i, ok := p.sessionsList.GetSelectedItem()
		if ok {
			p.operationTargetId = i.Id
			p.textInput.Placeholder = "New Session Name"
		}
		p.textInput.Focus()
		p.textInput.CharLimit = 100

	case key.Matches(msg, p.keyMap.delete):
		i, ok := p.sessionsList.GetSelectedItem()
		if p.currentSession.ID == i.Id {
			break
		}

		p.operationMode = deleteMode
		ti := textinput.New()
		ti.PromptStyle = lipgloss.NewStyle().PaddingLeft(util.DefaultElementsPadding)
		p.textInput = ti
		if ok {
			p.operationTargetId = i.Id
			p.textInput.Placeholder = "Delete session? y/n"
			p.textInput.Validate = util.DeleteSessionValidator
		}

		p.textInput.Focus()
		p.textInput.CharLimit = 1
	}

	return cmd
}

func (p *SessionsPane) addNewSession() tea.Cmd {
	currentTime := time.Now()
	formattedTime := currentTime.Format(time.ANSIC)
	defaultSessionName := fmt.Sprintf("%s", formattedTime)
	newSession, _ := p.sessionService.InsertNewSession(defaultSessionName, []util.MessageToSend{})

	cmd := p.handleUpdateCurrentSession(newSession)
	p.updateSessionsList()
	return cmd
}

func (p *SessionsPane) handleUpdateCurrentSession(session sessions.Session) tea.Cmd {
	p.currentSession = session
	p.userService.UpdateUserCurrentActiveSession(1, session.ID)

	p.currentSessionId = session.ID
	p.currentSessionName = session.SessionName

	listItems := constructSessionsListItems(p.sessionsListData, p.currentSessionId)
	p.sessionsList.SetItems(listItems)

	return sessions.SendUpdateCurrentSessionMsg(session)
}

func (p *SessionsPane) handleDeleteMode(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	p.textInput, cmd = p.textInput.Update(msg)

	switch {

	case key.Matches(msg, p.keyMap.apply):
		decision := p.textInput.Value()
		switch decision {
		case "y":
			p.sessionService.DeleteSession(p.operationTargetId)
			p.updateSessionsList()
			p.operationTargetId = NoTargetSession
			p.operationMode = defaultMode
		case "n":
			p.operationMode = defaultMode
			p.operationTargetId = NoTargetSession
		}

	case key.Matches(msg, p.keyMap.cancel):
		p.operationMode = defaultMode
		p.operationTargetId = NoTargetSession
	}

	return cmd
}

func (p *SessionsPane) handleEditMode(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	p.textInput, cmd = p.textInput.Update(msg)

	switch {

	case key.Matches(msg, p.keyMap.apply):
		if p.textInput.Value() != "" {
			p.sessionService.UpdateSessionName(p.operationTargetId, p.textInput.Value())
			p.updateSessionsList()
			p.operationTargetId = NoTargetSession
			p.operationMode = defaultMode
		}

	case key.Matches(msg, p.keyMap.cancel):
		p.operationMode = defaultMode
		p.operationTargetId = NoTargetSession
	}

	return cmd
}

func constructSessionsListItems(sessions []sessions.Session, currentSessionId int) []list.Item {
	items := []list.Item{}

	for _, session := range sessions {
		anItem := components.SessionListItem{
			Id:       session.ID,
			Text:     session.SessionName,
			IsActive: session.ID == currentSessionId,
		}
		items = append(items, anItem)
	}

	return items
}

func (p *SessionsPane) updateSessionsList() {
	p.sessionsListData, _ = p.sessionService.GetAllSessions()
	items := constructSessionsListItems(p.sessionsListData, p.currentSessionId)
	p.sessionsList.SetItems(items)
}

func (p SessionsPane) listHeader(str ...string) string {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.ThickBorder()).
		BorderBottom(true).
		Bold(true).
		Foreground(p.colors.DefaultTextColor).
		BorderForeground(p.colors.DefaultTextColor).
		MarginLeft(util.ListItemMarginLeft).
		Render(str...)
}

func (p SessionsPane) listItem(heading string, value string, isActive bool, widthCap int) string {
	headingColor := p.colors.MainColor
	color := p.colors.DefaultTextColor
	if isActive {
		colorValue := p.colors.ActiveTabBorderColor
		color = colorValue
		headingColor = colorValue
	}
	headingEl := lipgloss.NewStyle().
		PaddingLeft(util.ListItemPaddingLeft).
		Foreground(lipgloss.Color(headingColor)).
		Bold(isActive).
		Render
	spanEl := lipgloss.NewStyle().
		Foreground(lipgloss.Color(color)).
		Render

	value = util.TrimListItem(value, widthCap)

	return headingEl(util.ListHeadingDot+" "+heading, spanEl(value))
}

func (p SessionsPane) normalListView() string {
	sessionListItems := []string{}
	listWidth := p.sessionsList.GetWidth()
	for _, session := range p.sessionsListData {
		isCurrentSession := p.currentSessionId == session.ID
		sessionListItems = append(
			sessionListItems,
			p.listItem(fmt.Sprint(session.ID), session.SessionName, isCurrentSession, listWidth),
		)
	}

	w, h := util.CalcSessionsListSize(p.terminalWidth, p.terminalHeight, 0)

	return lipgloss.NewStyle().
		Width(w).
		Height(h).
		MaxHeight(h).
		Render(strings.Join(sessionListItems, "\n"))
}

func (p SessionsPane) AllowFocusChange() bool {
	return p.operationMode == defaultMode
}
