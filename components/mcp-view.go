package components

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/BalanceBalls/nekot/mcpclient"
	"github.com/BalanceBalls/nekot/util"
	zone "github.com/lrstanley/bubblezone/v2"
)

const (
	mcpHelpHeight         = 2
	mcpDetailFooterHeight = 1
	mcpZonePrefix         = "mcp_server_"
)

type MCPManager interface {
	Statuses() []mcpclient.ServerStatus
	ToolsForServer(string) []util.ToolDefinition
	SetEnabled(string, bool) error
	Reconnect(string) error
	Authorize(string) error
	Logout(string) error
	Reload() error
}

type mcpListItem struct {
	status mcpclient.ServerStatus
}

func (i mcpListItem) FilterValue() string { return i.status.ID }

type mcpItemDelegate struct {
	itemStyle         lipgloss.Style
	selectedItemStyle lipgloss.Style
}

func (d mcpItemDelegate) Height() int                             { return 1 }
func (d mcpItemDelegate) Spacing() int                            { return 0 }
func (d mcpItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d mcpItemDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	server, ok := item.(mcpListItem)
	if !ok {
		return
	}

	toggle := "[ ]"
	if server.status.Enabled {
		toggle = "[x]"
	}
	row := fmt.Sprintf(
		"%s %s  %s  %d/%d tools",
		toggle,
		server.status.ID,
		server.status.State,
		server.status.ExposedTools,
		server.status.DiscoveredTools,
	)
	row = zone.Mark(mcpZonePrefix+server.status.ID, util.TrimListItem(row, m.Width()))

	render := d.itemStyle.Render
	if index == m.Index() {
		render = func(values ...string) string {
			return d.selectedItemStyle.Render("> " + strings.Join(values, " "))
		}
	}

	fmt.Fprint(w, render(row))
}

type mcpActionResult struct {
	err error
}

func runMCPAction(action func() error) tea.Cmd {
	return func() tea.Msg { return mcpActionResult{err: action()} }
}

type MCPView struct {
	manager       MCPManager
	list          list.Model
	toolsViewport viewport.Model
	statuses      []mcpclient.ServerStatus
	inspecting    bool
	actionError   string
	width         int
	height        int

	emptyStyle       lipgloss.Style
	detailTitleStyle lipgloss.Style
	detailLabelStyle lipgloss.Style
	detailValueStyle lipgloss.Style
	errorStyle       lipgloss.Style
}

func NewMCPView(manager MCPManager, width, height int, colors util.SchemeColors) MCPView {
	statuses := []mcpclient.ServerStatus(nil)
	if manager != nil {
		statuses = manager.Statuses()
	}
	items := makeMCPItems(statuses)
	delegate := mcpItemDelegate{
		itemStyle: lipgloss.NewStyle().
			PaddingLeft(util.ListItemPaddingLeft).
			Foreground(colors.DefaultTextColor),
		selectedItemStyle: lipgloss.NewStyle().
			PaddingLeft(util.ListItemPaddingLeft).
			Foreground(colors.AccentColor),
	}
	serverList := list.New(items, delegate, width, mcpListHeight(height, 0))
	serverList.SetStatusBarItemName("server", "servers")
	serverList.SetShowTitle(false)
	serverList.SetShowHelp(false)
	serverList.SetFilteringEnabled(false)
	serverList.DisableQuitKeybindings()
	serverList.Paginator.ActiveDot = lipgloss.NewStyle().
		Foreground(colors.HighlightColor).
		Render(util.ActiveDot)
	serverList.Paginator.InactiveDot = lipgloss.NewStyle().
		Foreground(colors.DefaultTextColor).
		Render(util.InactiveDot)
	toolsViewport := viewport.New(
		viewport.WithWidth(width),
		viewport.WithHeight(0),
	)
	toolsViewport.FillHeight = true

	view := MCPView{
		manager:       manager,
		list:          serverList,
		toolsViewport: toolsViewport,
		statuses:      statuses,
		width:         width,
		height:        height,
		emptyStyle: lipgloss.NewStyle().
			PaddingLeft(util.ListItemPaddingLeft).
			Foreground(colors.DefaultTextColor),
		detailTitleStyle: lipgloss.NewStyle().
			PaddingLeft(util.ListItemPaddingLeft).
			Bold(true).
			Foreground(colors.AccentColor),
		detailLabelStyle: lipgloss.NewStyle().
			PaddingLeft(util.ListItemPaddingLeft).
			Foreground(colors.MainColor),
		detailValueStyle: lipgloss.NewStyle().
			Foreground(colors.DefaultTextColor),
		errorStyle: lipgloss.NewStyle().
			PaddingLeft(util.ListItemPaddingLeft).
			Foreground(colors.ErrorColor),
	}
	view.resizeList()
	return view
}

func makeMCPItems(statuses []mcpclient.ServerStatus) []list.Item {
	items := make([]list.Item, 0, len(statuses))
	for _, status := range statuses {
		items = append(items, mcpListItem{status: status})
	}
	return items
}

func mcpListHeight(height, errorRows int) int {
	height -= mcpHelpHeight
	height -= errorRows
	return max(0, height)
}

func (v *MCPView) syncItems() tea.Cmd {
	if v.manager == nil {
		return nil
	}

	statuses := v.manager.Statuses()
	if slices.Equal(statuses, v.statuses) {
		return nil
	}

	selectedID := ""
	if selected, ok := v.selectedStatus(); ok {
		selectedID = selected.ID
	}
	v.statuses = statuses
	cmd := v.list.SetItems(makeMCPItems(statuses))
	for index, status := range statuses {
		if status.ID == selectedID {
			v.list.Select(index)
			break
		}
	}
	if len(statuses) == 0 {
		v.inspecting = false
	}
	return cmd
}

func (v MCPView) selectedStatus() (mcpclient.ServerStatus, bool) {
	selected, ok := v.list.SelectedItem().(mcpListItem)
	if !ok {
		return mcpclient.ServerStatus{}, false
	}
	return selected.status, true
}

func (v *MCPView) resizeList() {
	errorRows := 0
	if selected, ok := v.selectedStatus(); ok && selected.LastError != "" {
		errorRows++
	}
	if v.actionError != "" {
		errorRows++
	}
	v.list.SetWidth(v.width)
	v.list.SetHeight(mcpListHeight(v.height, errorRows))
}

func (v *MCPView) SetSize(width, height int) {
	v.width = width
	v.height = height
	v.resizeList()
	if v.inspecting {
		v.refreshDetailView(false)
	}
}

func (v *MCPView) Reset() tea.Cmd {
	v.inspecting = false
	v.actionError = ""
	v.toolsViewport.GotoTop()
	v.resizeList()
	return v.syncItems()
}

func (v MCPView) IsInspecting() bool {
	return v.inspecting
}

func (v *MCPView) selectClickedServer(msg tea.MouseReleaseMsg) bool {
	if msg.Button != tea.MouseLeft {
		return false
	}
	for index, item := range v.list.Items() {
		server, ok := item.(mcpListItem)
		if ok && zone.Get(mcpZonePrefix+server.status.ID).InBounds(msg) {
			v.list.Select(index)
			v.resizeList()
			return true
		}
	}
	return false
}

func (v MCPView) Update(msg tea.Msg) (MCPView, tea.Cmd) {
	var cmds []tea.Cmd
	cmds = append(cmds, v.syncItems())
	v.resizeList()
	if v.inspecting {
		v.refreshDetailView(false)
	}

	switch msg := msg.(type) {
	case mcpActionResult:
		if msg.err != nil {
			v.actionError = msg.err.Error()
		} else {
			v.actionError = ""
		}
		v.resizeList()
		cmds = append(cmds, v.syncItems())
		return v, tea.Batch(cmds...)

	case tea.MouseReleaseMsg:
		if v.selectClickedServer(msg) {
			return v, tea.Batch(cmds...)
		}

	case tea.MouseWheelMsg:
		if v.inspecting {
			var cmd tea.Cmd
			v.toolsViewport, cmd = v.toolsViewport.Update(msg)
			cmds = append(cmds, cmd)
			return v, tea.Batch(cmds...)
		}
		if msg.Button == tea.MouseWheelUp {
			v.list.CursorUp()
			return v, tea.Batch(cmds...)
		}
		if msg.Button == tea.MouseWheelDown {
			v.list.CursorDown()
			return v, tea.Batch(cmds...)
		}

	case tea.KeyPressMsg:
		if v.inspecting {
			if msg.Code == tea.KeyEsc || msg.Code == tea.KeyEnter {
				v.inspecting = false
				return v, tea.Batch(cmds...)
			}
			var cmd tea.Cmd
			v.toolsViewport, cmd = v.toolsViewport.Update(msg)
			cmds = append(cmds, cmd)
			return v, tea.Batch(cmds...)
		}

		if msg.String() == "ctrl+r" && v.manager != nil {
			cmds = append(cmds, runMCPAction(v.manager.Reload))
			return v, tea.Batch(cmds...)
		}

		selected, ok := v.selectedStatus()
		if !ok || v.manager == nil {
			return v, tea.Batch(cmds...)
		}
		switch msg.String() {
		case "enter":
			v.inspecting = true
			v.refreshDetailView(true)
			return v, tea.Batch(cmds...)
		case " ", "space":
			cmds = append(cmds, runMCPAction(func() error {
				return v.manager.SetEnabled(selected.ID, !selected.Enabled)
			}))
			return v, tea.Batch(cmds...)
		case "r":
			cmds = append(cmds, runMCPAction(func() error {
				return v.manager.Reconnect(selected.ID)
			}))
			return v, tea.Batch(cmds...)
		case "a":
			v.inspecting = true
			v.refreshDetailView(true)
			cmds = append(cmds, runMCPAction(func() error {
				return v.manager.Authorize(selected.ID)
			}))
			return v, tea.Batch(cmds...)
		case "x":
			cmds = append(cmds, runMCPAction(func() error {
				return v.manager.Logout(selected.ID)
			}))
			return v, tea.Batch(cmds...)
		}
	}

	var cmd tea.Cmd
	v.list, cmd = v.list.Update(msg)
	v.resizeList()
	cmds = append(cmds, cmd)
	return v, tea.Batch(cmds...)
}

func (v *MCPView) View() string {
	if v.manager == nil {
		return lipgloss.NewStyle().Width(v.width).Height(v.height).Render(
			v.emptyStyle.Render("MCP manager unavailable"),
		)
	}
	if len(v.statuses) == 0 {
		return lipgloss.NewStyle().Width(v.width).Height(v.height).Render(
			lipgloss.JoinVertical(
				lipgloss.Left,
				v.emptyStyle.Render("No MCP servers configured"),
				util.HelpStyle.Render("ctrl+r reload config"),
			),
		)
	}
	if v.inspecting {
		return v.renderDetailView()
	}

	parts := []string{v.list.View()}
	if selected, ok := v.selectedStatus(); ok && selected.LastError != "" {
		parts = append(parts, v.errorStyle.Render(
			util.TrimListItem("Error: "+selected.LastError, v.width),
		))
	}
	if v.actionError != "" {
		parts = append(parts, v.errorStyle.Render("Action: "+v.actionError))
	}
	parts = append(parts, util.HelpStyle.Render(strings.Join([]string{
		"space toggle" + util.TipsSeparator + "enter inspect" + util.TipsSeparator + "r reconnect",
		"a authorize" + util.TipsSeparator + "x logout" + util.TipsSeparator + "ctrl+r reload",
	}, "\n")))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (v *MCPView) detailRow(label, value string) string {
	return v.detailLabelStyle.Render(util.ListHeadingDot+" "+label+":") + " " +
		v.detailValueStyle.Render(value)
}

func (v *MCPView) detailHeader(selected mcpclient.ServerStatus) []string {
	lines := []string{
		v.detailTitleStyle.Render(selected.ID),
		v.detailRow("Status", string(selected.State)),
		v.detailRow("Transport", selected.Transport),
		v.detailRow("Tools", fmt.Sprintf("%d exposed / %d discovered", selected.ExposedTools, selected.DiscoveredTools)),
	}
	if selected.LastError != "" {
		lines = append(lines, v.errorStyle.Render(
			util.TrimListItem("Error: "+selected.LastError, v.width),
		))
	}
	if selected.AuthorizationURL != "" {
		lines = append(lines, v.detailRow(
			"Authorization URL",
			util.TrimListItem(selected.AuthorizationURL, v.width),
		))
	}
	return append(lines, "", v.detailLabelStyle.Render("Exposed tools"))
}

func (v *MCPView) detailToolLines(selected mcpclient.ServerStatus) []string {
	tools := v.manager.ToolsForServer(selected.ID)
	if len(tools) == 0 {
		return []string{v.emptyStyle.Render("none")}
	}

	lines := make([]string, 0, len(tools))
	for _, tool := range tools {
		approval := "approval"
		if !tool.RequiresApproval {
			approval = "trusted"
		}
		lines = append(lines, v.detailValueStyle.
			PaddingLeft(util.ListItemPaddingLeft).
			Render(util.TrimListItem(fmt.Sprintf("%s  [%s]", tool.OriginalName, approval), v.width)))
	}
	return lines
}

func (v *MCPView) refreshDetailView(resetPosition bool) {
	selected, ok := v.selectedStatus()
	if !ok || v.manager == nil {
		return
	}
	headerHeight := len(v.detailHeader(selected))
	v.toolsViewport.SetWidth(v.width)
	v.toolsViewport.SetHeight(max(0, v.height-headerHeight-mcpDetailFooterHeight))
	v.toolsViewport.SetContentLines(v.detailToolLines(selected))
	if resetPosition {
		v.toolsViewport.GotoTop()
	}
}

func (v *MCPView) detailFooter() string {
	help := "j/k scroll  enter/esc back"
	if v.toolsViewport.TotalLineCount() > v.toolsViewport.Height() {
		help += fmt.Sprintf("  %d%%", int(v.toolsViewport.ScrollPercent()*100))
	}
	return util.HelpStyle.Render(help)
}

func (v *MCPView) renderDetailView() string {
	selected, ok := v.selectedStatus()
	if !ok {
		return ""
	}

	parts := v.detailHeader(selected)
	if v.toolsViewport.Height() > 0 {
		parts = append(parts, v.toolsViewport.View())
	}
	parts = append(parts, v.detailFooter())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
