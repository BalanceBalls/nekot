package views

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/sessions"
	"github.com/BalanceBalls/nekot/util"
	_ "modernc.org/sqlite"
)

func TestAttachmentToBase64UsesInMemoryContent(t *testing.T) {
	data := []byte("clipboard image")
	view := MainView{config: config.Config{MaxAttachmentSizeMb: 1}}
	attachment := util.Attachment{
		Path:    "clipboard-image-1.png",
		Content: base64.StdEncoding.EncodeToString(data),
		Type:    "img",
	}

	content, err := view.attachmentToBase64(attachment)
	if err != nil {
		t.Fatalf("attachmentToBase64 returned an error: %v", err)
	}
	if content != attachment.Content {
		t.Fatal("in-memory attachment content changed")
	}
}

func TestCancelPersistsResultsForUnresolvedToolCalls(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE sessions (sessions_id INTEGER PRIMARY KEY, sessions_messages JSON NOT NULL);
		INSERT INTO sessions (sessions_id, sessions_messages) VALUES (1, '[]');
	`); err != nil {
		t.Fatal(err)
	}
	assistantTurn := util.LocalStoreMessage{
		Role: "assistant",
		ToolCalls: []util.ToolCall{
			{Id: "call-1", Function: util.ToolFunction{Name: "tool-one", Args: map[string]any{}}},
			{Id: "call-2", Function: util.ToolFunction{Name: "tool-two", Args: map[string]any{}}},
		},
	}
	view := MainView{
		sessionService: *sessions.NewSessionService(db),
		sessionOrchestrator: sessions.Orchestrator{
			CurrentSessionID:        1,
			ArrayOfMessages:         []util.LocalStoreMessage{assistantTurn},
			ResponseProcessingState: util.AwaitingToolCallResult,
		},
	}
	if command := view.CancelProcessing(); command == nil {
		t.Fatal("CancelProcessing() returned nil")
	}
	var stored []byte
	if err := db.QueryRow(`SELECT sessions_messages FROM sessions WHERE sessions_id = 1`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	var messages []util.LocalStoreMessage
	if err := json.Unmarshal(stored, &messages); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[1].Role != "tool" || len(messages[1].ToolCalls) != 2 {
		t.Fatalf("stored messages = %#v", messages)
	}
	for _, toolCall := range messages[1].ToolCalls {
		if toolCall.Result == nil || !strings.Contains(*toolCall.Result, "cancelled") {
			t.Fatalf("cancelled tool result = %#v", toolCall)
		}
	}
}

func TestToolApprovalQueueAllowAndDeny(t *testing.T) {
	request := sessions.ToolApprovalRequest{
		ToolCall: util.ToolCall{Id: "call-1", Function: util.ToolFunction{
			Name: "mcp__server__tool__12345678", Args: map[string]any{"count": float64(2)},
		}},
		Tool: util.ToolDefinition{ServerID: "server", OriginalName: "tool"},
	}
	view := MainView{terminalWidth: 80, terminalHeight: 24}
	model, _ := view.Update(request)
	view = model.(MainView)
	if len(view.pendingApprovals) != 1 || !view.controlsLocked {
		t.Fatalf("approval queue = %#v", view.pendingApprovals)
	}

	model, command := view.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view = model.(MainView)
	if len(view.pendingApprovals) != 0 || command == nil {
		t.Fatalf("approval queue after allow = %#v", view.pendingApprovals)
	}
	decision, ok := command().(sessions.ToolApprovalDecision)
	if !ok || !decision.Allow || decision.ToolCall.Id != "call-1" {
		t.Fatalf("allow decision = %#v", decision)
	}

	model, _ = view.Update(request)
	view = model.(MainView)
	model, command = view.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	decision, ok = command().(sessions.ToolApprovalDecision)
	if !ok || decision.Allow {
		t.Fatalf("deny decision = %#v", decision)
	}
}

func TestQueuedApprovalDoesNotBlockDecisionMessages(t *testing.T) {
	view := MainView{pendingApprovals: []sessions.ToolApprovalRequest{
		{ToolCall: util.ToolCall{Id: "call-2"}},
	}}
	model, command := view.Update(sessions.ToolApprovalDecision{
		ToolCall: util.ToolCall{Id: "call-1", Function: util.ToolFunction{Name: "missing"}},
		Allow:    false,
	})
	view = model.(MainView)
	if command == nil || len(view.pendingApprovals) != 1 {
		t.Fatalf("decision was blocked; command = %v, queue = %#v", command, view.pendingApprovals)
	}
}

func TestToolApprovalRendersAtNarrowWidth(t *testing.T) {
	view := MainView{terminalWidth: 42, terminalHeight: 18}
	approval := sessions.ToolApprovalRequest{
		ToolCall: util.ToolCall{Function: util.ToolFunction{
			Args: map[string]any{"long": strings.Repeat("value", 12)},
		}},
		Tool: util.ToolDefinition{ServerID: "server", OriginalName: "tool"},
	}
	rendered := view.renderToolApproval(approval)
	if lipglossWidth := maxLineWidth(rendered); lipglossWidth > view.terminalWidth {
		t.Fatalf("approval width = %d, terminal width = %d", lipglossWidth, view.terminalWidth)
	}
}

func TestToolApprovalRendersAtVeryNarrowWidth(t *testing.T) {
	view := MainView{terminalWidth: 20, terminalHeight: 12}
	rendered := view.renderToolApproval(sessions.ToolApprovalRequest{
		Tool: sessionsToolDefinition("server", "tool"),
	})
	if width := maxLineWidth(rendered); width > view.terminalWidth {
		t.Fatalf("approval width = %d, terminal width = %d", width, view.terminalWidth)
	}
}

func sessionsToolDefinition(serverID, originalName string) util.ToolDefinition {
	return util.ToolDefinition{ServerID: serverID, OriginalName: originalName}
}

func maxLineWidth(value string) int {
	width := 0
	for _, line := range strings.Split(value, "\n") {
		if lineWidth := lipgloss.Width(line); lineWidth > width {
			width = lineWidth
		}
	}
	return width
}

func TestAttachmentToBase64RejectsInvalidContent(t *testing.T) {
	view := MainView{config: config.Config{MaxAttachmentSizeMb: 1}}
	attachment := util.Attachment{
		Path:    "clipboard-image-1.png",
		Content: "not base64",
		Type:    "img",
	}

	if _, err := view.attachmentToBase64(attachment); err == nil {
		t.Fatal("expected invalid attachment content error")
	}
}

func TestAttachmentToBase64RechecksSizeLimit(t *testing.T) {
	data := []byte(strings.Repeat("x", 1024*1024+1))
	view := MainView{config: config.Config{MaxAttachmentSizeMb: 1}}
	attachment := util.Attachment{
		Path:    "clipboard-image-1.png",
		Content: base64.StdEncoding.EncodeToString(data),
		Type:    "img",
	}

	if _, err := view.attachmentToBase64(attachment); err == nil {
		t.Fatal("expected attachment size error")
	}
}
