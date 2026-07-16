package components

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/BalanceBalls/nekot/mcpclient"
	"github.com/BalanceBalls/nekot/util"
	zone "github.com/lrstanley/bubblezone/v2"
)

type fakeMCPManager struct {
	statuses  []mcpclient.ServerStatus
	tools     map[string][]util.ToolDefinition
	lastCall  string
	actionErr error
}

func (m *fakeMCPManager) Statuses() []mcpclient.ServerStatus {
	return append([]mcpclient.ServerStatus(nil), m.statuses...)
}

func (m *fakeMCPManager) ToolsForServer(id string) []util.ToolDefinition {
	return append([]util.ToolDefinition(nil), m.tools[id]...)
}

func (m *fakeMCPManager) SetEnabled(id string, enabled bool) error {
	m.lastCall = "toggle:" + id
	for index := range m.statuses {
		if m.statuses[index].ID == id {
			m.statuses[index].Enabled = enabled
		}
	}
	return m.actionErr
}

func (m *fakeMCPManager) Reconnect(id string) error {
	m.lastCall = "reconnect:" + id
	return m.actionErr
}

func (m *fakeMCPManager) Authorize(id string) error {
	m.lastCall = "authorize:" + id
	return m.actionErr
}

func (m *fakeMCPManager) Logout(id string) error {
	m.lastCall = "logout:" + id
	return m.actionErr
}

func (m *fakeMCPManager) Reload() error {
	m.lastCall = "reload"
	return m.actionErr
}

func testMCPManager() *fakeMCPManager {
	return &fakeMCPManager{
		statuses: []mcpclient.ServerStatus{
			{
				ID:              "github",
				Transport:       "streamable-http",
				Enabled:         true,
				State:           mcpclient.ServerConnected,
				DiscoveredTools: 3,
				ExposedTools:    2,
			},
		},
		tools: map[string][]util.ToolDefinition{
			"github": {
				{OriginalName: "search_issues", RequiresApproval: true},
				{OriginalName: "get_issue", RequiresApproval: false},
			},
		},
	}
}

func runMCPViewCommand(view MCPView, cmd tea.Cmd) MCPView {
	if cmd == nil {
		return view
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, batchedCmd := range batch {
			view = runMCPViewCommand(view, batchedCmd)
		}
		return view
	}
	view, _ = view.Update(msg)
	return view
}

func TestMCPViewRendersListAndDetails(t *testing.T) {
	zone.NewGlobal()
	manager := testMCPManager()
	view := NewMCPView(manager, 80, 20, util.OriginalPink.GetColors())

	listView := view.View()
	for _, expected := range []string{"[x] github", "connected", "2/3 tools", "space toggle"} {
		if !strings.Contains(listView, expected) {
			t.Fatalf("list view does not contain %q:\n%s", expected, listView)
		}
	}

	view, _ = view.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !view.IsInspecting() {
		t.Fatal("enter did not open the server detail view")
	}
	detailView := view.View()
	for _, expected := range []string{
		"github",
		"Status:",
		"streamable-http",
		"search_issues  [approval]",
		"get_issue  [trusted]",
	} {
		if !strings.Contains(detailView, expected) {
			t.Fatalf("detail view does not contain %q:\n%s", expected, detailView)
		}
	}

	view, _ = view.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if view.IsInspecting() {
		t.Fatal("escape did not return to the server list")
	}
}

func TestMCPViewRunsActionsAndRefreshesStatus(t *testing.T) {
	zone.NewGlobal()
	manager := testMCPManager()
	view := NewMCPView(manager, 80, 20, util.SmoothBlue.GetColors())

	view, cmd := view.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	view = runMCPViewCommand(view, cmd)

	if manager.lastCall != "toggle:github" {
		t.Fatalf("unexpected action: %q", manager.lastCall)
	}
	if strings.Contains(view.View(), "[x] github") {
		t.Fatal("view did not refresh after disabling the server")
	}
	if !strings.Contains(view.View(), "[ ] github") {
		t.Fatalf("disabled status is missing:\n%s", view.View())
	}
}

func TestMCPViewShowsActionErrors(t *testing.T) {
	zone.NewGlobal()
	manager := testMCPManager()
	manager.actionErr = errors.New("connection refused")
	view := NewMCPView(manager, 80, 20, util.Groovebox.GetColors())

	view, cmd := view.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
	view = runMCPViewCommand(view, cmd)

	if !strings.Contains(view.View(), "Action: connection refused") {
		t.Fatalf("action error is missing:\n%s", view.View())
	}
}

func TestMCPViewToolListScrollsWithinDetailHeight(t *testing.T) {
	zone.NewGlobal()
	manager := testMCPManager()
	manager.statuses[0].DiscoveredTools = 14
	manager.statuses[0].ExposedTools = 14
	manager.tools["github"] = nil
	for index := range 14 {
		manager.tools["github"] = append(manager.tools["github"], util.ToolDefinition{
			OriginalName:     fmt.Sprintf("tool_%02d", index),
			RequiresApproval: true,
		})
	}
	view := NewMCPView(manager, 60, 11, util.OriginalPink.GetColors())
	view, _ = view.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	initial := view.View()
	if !strings.Contains(initial, "tool_00") || strings.Contains(initial, "tool_13") {
		t.Fatalf("initial detail viewport is not clipped to its available height:\n%s", initial)
	}
	if lines := strings.Count(initial, "\n") + 1; lines > 11 {
		t.Fatalf("detail view rendered %d lines into an 11-line component", lines)
	}

	for range 10 {
		view, _ = view.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	scrolled := view.View()
	if view.toolsViewport.YOffset() == 0 || !strings.Contains(scrolled, "tool_13") {
		t.Fatalf("tool viewport did not scroll to the final tools:\n%s", scrolled)
	}
	if lines := strings.Count(scrolled, "\n") + 1; lines > 11 {
		t.Fatalf("scrolled detail view rendered %d lines into an 11-line component", lines)
	}
}
