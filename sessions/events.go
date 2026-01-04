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

type SaveQuickChat struct{}

func SendSaveQuickChatMsg() tea.Cmd {
	return func() tea.Msg { return SaveQuickChat{} }
}

type RefreshSessionsList struct{}

func SendRefreshSessionsListMsg() tea.Cmd {
	return func() tea.Msg { return RefreshSessionsList{} }
}

type ResponseChunkProcessed struct {
	PreviousMsgArray []util.LocalStoreMessage
	ChunkMessage     string
	IsComplete       bool
}

func SendResponseChunkProcessedMsg(msg string, previousMsgs []util.LocalStoreMessage, isComplete bool) tea.Cmd {
	return func() tea.Msg {
		return ResponseChunkProcessed{
			PreviousMsgArray: previousMsgs,
			ChunkMessage:     msg,
			IsComplete:       isComplete,
		}
	}
}

type InferenceFinalized struct {
	Response   util.LocalStoreMessage
	IsToolCall bool
}

func FinalizeResponse(response util.LocalStoreMessage, isToolCall bool) tea.Cmd {
	return func() tea.Msg {
		return InferenceFinalized{
			Response:   response,
			IsToolCall: isToolCall,
		}
	}
}

type ToolCallRequest struct {
	ToolCall util.ToolCall
}

func ExecuteToolCallRequest(tc util.ToolCall) tea.Cmd {
	return func() tea.Msg {
		return ToolCallRequest{
			ToolCall: tc,
		}
	}
}

type ToolCallComplete struct {
	Id        string
	IsSuccess bool
	Name      string
	Result    string
}
