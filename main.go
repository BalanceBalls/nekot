package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/joho/godotenv"
	"github.com/muesli/reflow/wrap"
	"github.com/tearingItUp786/chatgpt-tui/config"
	"github.com/tearingItUp786/chatgpt-tui/migrations"
	"github.com/tearingItUp786/chatgpt-tui/sessions"
	"github.com/tearingItUp786/chatgpt-tui/settings"
	"github.com/tearingItUp786/chatgpt-tui/util"
)

type model struct {
	ready            bool
	focused          util.FocusPane
	viewMode         util.ViewMode
	promptInputMode  util.PrompInputMode
	msgChan          chan sessions.ProcessResult
	error            util.ErrorEvent
	currentSessionID string

	chatViewMessageContainer lipgloss.Style
	promptContainer          lipgloss.Style
	viewport                 viewport.Model
	promptInput              textinput.Model
	settingsModel            settings.Model
	sessionModel             sessions.Model
	terminalWidth            int
	terminalHeight           int
}

func initialModal(db *sql.DB, ctx context.Context) model {
	ti := textinput.New()
	ti.Placeholder = "Ask ChatGPT a question!"
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(util.ActiveTabBorderColor))

	si := settings.New(db)
	sm := sessions.New(db, ctx)

	msgChan := make(chan sessions.ProcessResult)

	return model{
		viewMode:         util.NormalMode,
		focused:          util.PromptType,
		promptInputMode:  util.PromptNormalMode,
		promptInput:      ti,
		settingsModel:    si,
		currentSessionID: "",
		sessionModel:     sm,
		msgChan:          msgChan,
		chatViewMessageContainer: lipgloss.NewStyle().
			Border(lipgloss.ThickBorder()).
			BorderForeground(util.NormalTabBorderColor).
			MarginRight(1),

		promptContainer: lipgloss.NewStyle().
			AlignVertical(lipgloss.Bottom).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(util.ActiveTabBorderColor).
			MaxHeight(4).
			MarginTop(1),
	}
}

// A command that waits for the activity on a channel.
func waitForActivity(sub chan sessions.ProcessResult) tea.Cmd {
	return func() tea.Msg {
		someMessage := <-sub
		return someMessage
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.promptInput.Cursor.BlinkCmd(),
		waitForActivity(m.msgChan),
		m.sessionModel.Init(),
		m.settingsModel.Init(),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd                    tea.Cmd
		cmds                   []tea.Cmd
		enableUpdateOfViewport = true
	)

	isPromptFocused := m.focused == util.PromptType
	isChatMessagesFocused := m.focused == util.ChatMessagesType

	// the settings model is actually an input into the session model
	m.sessionModel, cmd = m.sessionModel.Update(msg)
	cmds = append(cmds, cmd)

	if m.sessionModel.ProcessingMode == sessions.IDLE {
		m.settingsModel, cmd = m.settingsModel.Update(msg)
		cmds = append(cmds, cmd)
	}

	if m.focused == util.PromptType && m.promptInputMode == util.PromptInsertMode &&
		m.sessionModel.ProcessingMode == sessions.IDLE {
		m.promptInput, cmd = m.promptInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	switch msg := msg.(type) { // each time we get a new message coming in from the model
	// lets handle it and pass it to the lower model
	case sessions.LoadDataFromDB:
		oldContent := m.sessionModel.GetMessagesAsPrettyString()
		if oldContent == "" {
			oldContent = util.MotivationalMessage
		}
		m.chatViewMessageContainer.Width(m.terminalWidth / 3 * 2)
		m.viewport.SetContent(wrap.String(oldContent, m.terminalWidth/3*2))
		return m, cmd

	case sessions.UpdateCurrentSession:
		oldContent := m.sessionModel.GetMessagesAsPrettyString()
		if oldContent == "" {
			oldContent = util.MotivationalMessage
		}
		m.viewport.SetContent(wrap.String(oldContent, m.terminalWidth/3*2))
		return m, cmd

	// these are the messages that come in as a stream from the chat gpt api
	// we append the content to the viewport and scroll
	case sessions.ProcessResult:
		util.Log("main ProcessResult: ")
		oldContent := m.sessionModel.GetMessagesAsPrettyString()
		styledBufferMessage := sessions.RenderBotMessage(m.sessionModel.CurrentAnswer, m.terminalWidth/3*2)

		if styledBufferMessage != "" {
			styledBufferMessage = "\n" + styledBufferMessage
		}
		m.viewport.SetContent(wrap.String(oldContent+styledBufferMessage, m.terminalWidth/3*2))
		m.viewport.GotoBottom()

		cmds = append(cmds, waitForActivity(m.msgChan))

	case util.ErrorEvent:
		util.Log("Error: ", msg.Message)
		m.sessionModel.ProcessingMode = sessions.IDLE
		m.error = msg

	case tea.KeyMsg:

		if !isChatMessagesFocused {
			enableUpdateOfViewport = false
		}

		switch keypress := msg.String(); keypress {
		case "i":
			if m.focused == util.PromptType && m.promptInputMode == util.PromptNormalMode {
				m.promptInputMode = util.PromptInsertMode
				m.promptInput.Focus()
				cmds = append(cmds, m.promptInput.Cursor.BlinkCmd())
			}
		case "y":
			if m.focused == util.ChatMessagesType {
				latestBotMessage, err := m.sessionModel.GetLatestBotMessage()
				if err == nil {
					clipboard.WriteAll(latestBotMessage)
				}

			}

		case "Y":
			if m.focused == util.ChatMessagesType {
				clipboard.WriteAll(m.sessionModel.GetMessagesAsString())
			}

		case "ctrl+o":
			m.focused = util.PromptType
			m.promptContainer = m.promptContainer.Copy().BorderForeground(util.ActiveTabBorderColor)
			m.sessionModel, _ = m.sessionModel.Update(util.MakeFocusMsg(m.focused == util.SessionsType))
			m.settingsModel, _ = m.settingsModel.Update(util.MakeFocusMsg(m.focused == util.SettingsType))

			cmds = append(cmds, cmd)

			switch m.viewMode {
			case util.NormalMode:
				m.viewMode = util.ZenMode
				m.chatViewMessageContainer.BorderForeground(util.NormalTabBorderColor).Width(m.terminalWidth - 2)
			case util.ZenMode:
				m.viewMode = util.NormalMode
				m.chatViewMessageContainer.BorderForeground(util.NormalTabBorderColor).Width(m.terminalWidth / 3 * 2)
			}

		}

		switch msg.Type {

		case tea.KeyTab:
			if m.focused == util.PromptType && m.promptInputMode == util.PromptInsertMode {
				break
			}

			m.focused = util.GetNewFocusMode(m.viewMode, m.focused)
			m.sessionModel, _ = m.sessionModel.Update(util.MakeFocusMsg(m.focused == util.SessionsType))
			m.settingsModel, _ = m.settingsModel.Update(util.MakeFocusMsg(m.focused == util.SettingsType))
			m.chatViewMessageContainer.BorderForeground(util.NormalTabBorderColor)

			switch m.focused {

			case util.PromptType:
				m.promptInputMode = util.PromptNormalMode
				m.promptContainer = m.promptContainer.BorderForeground(util.ActiveTabBorderColor)
				m.promptInput.PromptStyle = m.promptInput.PromptStyle.Copy().Foreground(lipgloss.Color(util.ActiveTabBorderColor))

			case util.ChatMessagesType:
				m.promptInputMode = util.PromptNormalMode
				m.chatViewMessageContainer.BorderForeground(util.ActiveTabBorderColor)
				m.promptContainer = m.promptContainer.BorderForeground(util.NormalTabBorderColor)
				m.promptInput.PromptStyle = m.promptInput.PromptStyle.Copy().Foreground(lipgloss.Color(util.NormalTabBorderColor))
				m.promptInput.Blur()

			default:
				m.promptInputMode = util.PromptNormalMode
				m.promptContainer = m.promptContainer.BorderForeground(util.NormalTabBorderColor)
				m.promptInput.PromptStyle = m.promptInput.PromptStyle.Foreground(lipgloss.Color(util.NormalTabBorderColor))
				m.promptInput.Blur()
			}

		case tea.KeyEscape:
			if m.focused == util.PromptType {
				m.promptInputMode = util.PromptNormalMode
				m.promptInput.Blur()
			}

		case tea.KeyCtrlC:
			return m, tea.Quit

		case tea.KeyEnter:
			if isPromptFocused && m.sessionModel.ProcessingMode == sessions.IDLE {
				// Start CallChatGpt on Enter key
				m.error = util.ErrorEvent{}
				m.sessionModel.ArrayOfMessages = append(m.sessionModel.ArrayOfMessages, sessions.ConstructUserMessage(m.promptInput.Value()))
				log.Println("key enter")
				m.promptInput.SetValue("")
				m.promptInput.Focus()

				m.promptInputMode = util.PromptInsertMode
				m.sessionModel.ProcessingMode = sessions.PROCESSING
				return m, tea.Batch(m.sessionModel.CallChatGpt(m.msgChan), m.promptInput.Cursor.BlinkCmd())
			}
		}

	case tea.WindowSizeMsg:
		m.terminalWidth = msg.Width
		m.terminalHeight = msg.Height

		m.promptContainer = m.promptContainer.Copy().MaxWidth(m.terminalWidth).
			Width(m.terminalWidth - 2)

		m.chatViewMessageContainer.Width(m.terminalWidth / 3 * 2)
		if m.viewMode == util.ZenMode {
			m.chatViewMessageContainer.Width(m.terminalWidth - 2)
		}

		// TODO: get rid of this magic number
		promptContainerHeight := m.promptContainer.GetHeight() + 5

		if !m.ready {
			// Since this program is using the full size of the viewport we
			// need to wait until we've received the window dimensions before
			// we can initialize the viewport. The initial dimensions come in
			// quickly, though asynchronously, which is why we wait for them
			// here.
			m.viewport = viewport.New(msg.Width, msg.Height-promptContainerHeight)
			m.viewport.Style.MaxHeight(msg.Height)
			m.ready = true
			m.promptInput.Width = msg.Width - 3
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - promptContainerHeight
			m.promptInput.Width = msg.Width - 3
		}
		yolo := m.chatViewMessageContainer.GetWidth()
		m.settingsModel.Update(util.MakeWindowResizeMsg(yolo))
		m.sessionModel.Update(util.MakeWindowResizeMsg(yolo))
	}

	if enableUpdateOfViewport {
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	var windowViews string

	settingsAndSessionViews := lipgloss.JoinVertical(
		lipgloss.Left,
		m.settingsModel.View(),
		m.sessionModel.View(),
	)

	strToRender := m.viewport.View()
	if m.error.Message != "" {
		strToRender = m.error.Message
	}

	secondaryScreen := ""
	if m.viewMode == util.NormalMode {
		secondaryScreen = settingsAndSessionViews
	}

	mainView := m.chatViewMessageContainer.Render(strToRender)

	windowViews = lipgloss.NewStyle().
		Align(lipgloss.Right, lipgloss.Right).
		Render(
			lipgloss.JoinHorizontal(
				lipgloss.Top,
				mainView,
				secondaryScreen,
			),
		)

	lowerPromptView := "> Please wait ..."
	if m.sessionModel.ProcessingMode == sessions.IDLE {
		lowerPromptView = m.promptInput.View()
	}

	promptView := m.promptContainer.Render(
		lowerPromptView,
	)

	return lipgloss.NewStyle().Render(
		windowViews,
		promptView,
	)
}

func main() {
	env := os.Getenv("FOO_ENV")
	if "" == env {
		env = "development"
	}

	godotenv.Load(".env." + env + ".local")
	if "test" != env {
		godotenv.Load(".env.local")
	}
	godotenv.Load(".env." + env)
	godotenv.Load() // The Original .env

	appPath, err := util.GetAppDataPath()
	f, err := tea.LogToFile(filepath.Join(appPath, "debug.log"), "debug")
	if err != nil {
		fmt.Println("fatal:", err)
		os.Exit(1)
	}
	defer f.Close()

	apiKey := os.Getenv("CHAT_GPT_API_KEY")
	if "" == apiKey {
		fmt.Println("CHAT_GPT_API_KEY not set; set it in your profile")
		fmt.Printf("export CHAT_GPT_API_KEY=your_key in the config for :%v \n", os.Getenv("SHELL"))
		fmt.Println("Exiting...")
		os.Exit(1)
	}

	// delete files if in dev mode
	util.DeleteFilesIfDevMode()
	// validate config
	configToUse := config.CreateAndValidateConfig()

	// run migrations for our database
	db := util.InitDb()
	err = util.MigrateFS(db, migrations.FS, ".")
	if err != nil {
		log.Println("Error: ", err)
		panic(err)
	}
	defer db.Close()

	ctx := context.Background()
	ctxWithConfig := config.WithConfig(ctx, &configToUse)

	p := tea.NewProgram(
		initialModal(db, ctxWithConfig),
		tea.WithAltScreen(),
		// tea.WithMouseCellMotion(), // turn on mouse support so we can track the mouse wheel
	)
	_, err = p.Run()
	if err != nil {
		log.Fatal(err)
	}
}
