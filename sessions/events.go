package sessions

import (
	"github.com/BalanceBalls/nekot/util"
	tea "github.com/charmbracelet/bubbletea"
)

type LoadDataFromDB struct {
	Session                Session
	AllSessions            []Session
	CurrentActiveSessionID int
}

// Final Message is the concatenated string from the chat gpt stream
type FinalProcessMessage struct {
	FinalMessage string
}

func SendFinalProcessMessage(msg string) tea.Cmd {
	return func() tea.Msg {
		return FinalProcessMessage{
			FinalMessage: msg,
		}
	}
}

type UpdateCurrentSession struct {
	Session Session
}

func SendUpdateCurrentSessionMsg(session Session) tea.Cmd {
	return func() tea.Msg {
		return UpdateCurrentSession{
			Session: session,
		}
	}
}

type ResponseChunkProcessed struct {
	PreviousMsgArray []util.MessageToSend
	ChunkMessage     string
}

func SendResponseChunkProcessedMsg(msg string, previousMsgs []util.MessageToSend) tea.Cmd {
	return func() tea.Msg {
		return ResponseChunkProcessed{
			PreviousMsgArray: previousMsgs,
			ChunkMessage:     msg,
		}
	}
}
