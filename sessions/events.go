package sessions

import (
	tea "charm.land/bubbletea/v2"
	"github.com/BalanceBalls/nekot/util"
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

type ToolApprovalRequest struct {
	ToolCall util.ToolCall
	Tool     util.ToolDefinition
}

func RequestToolApproval(tc util.ToolCall, tool util.ToolDefinition) tea.Cmd {
	return func() tea.Msg {
		return ToolApprovalRequest{ToolCall: tc, Tool: tool}
	}
}

type ToolApprovalDecision struct {
	ToolCall util.ToolCall
	Allow    bool
}

func SendToolApprovalDecision(tc util.ToolCall, allow bool) tea.Cmd {
	return func() tea.Msg {
		return ToolApprovalDecision{ToolCall: tc, Allow: allow}
	}
}

type ToolCallComplete struct {
	Id           string
	IsSuccess    bool
	Name         string
	Result       string
	Source       util.ToolSource
	ServerID     string
	OriginalName string
}

type SessionTitleGeneratedMsg struct {
	SessionID int
	Title     string
}

func SendSessionTitleGeneratedMsg(sessionID int, title string) tea.Cmd {
	return func() tea.Msg {
		return SessionTitleGeneratedMsg{
			SessionID: sessionID,
			Title:     title,
		}
	}
}
