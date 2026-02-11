package components

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BalanceBalls/nekot/util"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

type ContextPicker struct {
	list         list.Model
	SelectedPath string
	PrevView     util.ViewMode
	PrevInput    string
	quitting     bool
	baseDir      string
	showIcons    bool
	maxDepth     int
	preview      string // Preview of selected file (first 10 lines)
}

var contextPickerTips = "/ filter • enter select • esc cancel"

var contextPickerListItemSpan = lipgloss.NewStyle().
	PaddingLeft(util.ListItemPaddingLeft)

var contextPickerListItemSpanSelected = lipgloss.NewStyle().
	PaddingLeft(util.ListItemPaddingLeft)

type ContextPickerItem struct {
	Path     string
	Name     string
	IsFolder bool
	Size     int64
	Icon     string
}

func (i ContextPickerItem) FilterValue() string { return zone.Mark(i.Path, i.Name) }

type contextPickerItemDelegate struct{}

func (d contextPickerItemDelegate) Height() int                             { return 1 }
func (d contextPickerItemDelegate) Spacing() int                            { return 0 }
func (d contextPickerItemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d contextPickerItemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(ContextPickerItem)
	if !ok {
		return
	}

	icon := ""
	if i.IsFolder {
		icon = "📁 "
	} else {
		icon = "📄 "
	}

	str := fmt.Sprintf("%s%s", icon, i.Name)
	str = util.TrimListItem(str, m.Width())
	str = zone.Mark(i.Path, str)

	fn := contextPickerListItemSpan.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			row := "> " + strings.Join(s, " ")
			return contextPickerListItemSpanSelected.Render(row)
		}
	}

	fmt.Fprint(w, fn(str))
}

func (l *ContextPicker) View() string {
	if l.list.FilterState() == list.Filtering {
		l.list.SetShowStatusBar(false)
	} else {
		l.list.SetShowStatusBar(true)
	}

	content := l.list.View()
	
	// Add preview section above the list if available
	if l.preview != "" {
		previewStyle := lipgloss.NewStyle().
			PaddingLeft(1).
			MaxHeight(10) // Display 10 lines
		
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			previewStyle.Render(l.preview),
			content,
		)
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		content,
		util.HelpStyle.Render(contextPickerTips))
}

func (l *ContextPicker) GetSelectedItem() (ContextPickerItem, bool) {
	item, ok := l.list.SelectedItem().(ContextPickerItem)
	return item, ok
}

func (l ContextPicker) VisibleItems() []list.Item {
	return l.list.VisibleItems()
}

func (l ContextPicker) IsFiltering() bool {
	return l.list.SettingFilter()
}

func (l *ContextPicker) Update(msg tea.Msg) (ContextPicker, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {

	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q":
			l.quitting = true
			return *l, util.SendViewModeChangedMsg(l.PrevView)
		case "enter":
			if item, ok := l.GetSelectedItem(); ok {
				l.SelectedPath = item.Path
				l.quitting = true
				return *l, util.SendViewModeChangedMsg(l.PrevView)
			}
		}

	case tea.MouseMsg:
		if msg.Button == tea.MouseButtonWheelUp {
			l.list.CursorUp()
			return *l, nil
		}

		if msg.Button == tea.MouseButtonWheelDown {
			l.list.CursorDown()
			return *l, nil
		}
	}

	l.list, cmd = l.list.Update(msg)
	
	// Update preview when selection changes
	if item, ok := l.GetSelectedItem(); ok && !item.IsFolder {
		l.updatePreview(item.Path)
	} else {
		l.preview = ""
	}
	
	return *l, cmd
}

func (l *ContextPicker) updatePreview(filePath string) {
	// Get file info to check size
	info, err := os.Stat(filePath)
	if err != nil {
		util.Slog.Error("failed to get file info for preview", "path", filePath, "error", err.Error())
		l.preview = ""
		return
	}

	// Don't preview files larger than 5MB to save performance
	const maxPreviewSize = 5 * 1024 * 1024 // 5MB in bytes
	if info.Size() > maxPreviewSize {
		util.Slog.Debug("file too large for preview", "path", filePath, "size", info.Size())
		l.preview = ""
		return
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		util.Slog.Error("failed to read file for preview", "path", filePath, "error", err.Error())
		l.preview = ""
		return
	}

	lines := strings.Split(string(content), "\n")
	if len(lines) > 10 {
		lines = lines[:10]
	}
	
	l.preview = strings.Join(lines, "\n")
}

func NewContextPicker(prevView util.ViewMode, prevInput string, colors util.SchemeColors, showIcons bool, maxDepth int) ContextPicker {
	baseDir, err := os.Getwd()
	if err != nil {
		util.Slog.Error("failed to get current directory", "error", err.Error())
		baseDir, _ = os.UserHomeDir()
	}

	// Create a temporary ContextPicker to call scanDirectory
	tempPicker := ContextPicker{}
	items := tempPicker.scanDirectory(baseDir, 0, maxDepth)

	h := 20 // Default height
	l := list.New(items, contextPickerItemDelegate{}, 80, h)

	l.SetStatusBarItemName("item", "items")
	l.SetShowTitle(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()

	l.Paginator.ActiveDot = lipgloss.NewStyle().Foreground(colors.HighlightColor).Render(util.ActiveDot)
	l.Paginator.InactiveDot = lipgloss.NewStyle().Foreground(colors.DefaultTextColor).Render(util.InactiveDot)
	contextPickerListItemSpan = contextPickerListItemSpan.Foreground(colors.DefaultTextColor)
	contextPickerListItemSpanSelected = contextPickerListItemSpanSelected.Foreground(colors.AccentColor)
	l.FilterInput.PromptStyle = l.FilterInput.PromptStyle.Foreground(colors.ActiveTabBorderColor).PaddingBottom(0).Margin(0)
	l.FilterInput.Cursor.Style = l.FilterInput.Cursor.Style.Foreground(colors.NormalTabBorderColor)

	return ContextPicker{
		list:      l,
		PrevView:  prevView,
		PrevInput: prevInput,
		baseDir:   baseDir,
		showIcons: showIcons,
		maxDepth:  maxDepth,
	}
}

func (l *ContextPicker) scanDirectory(dir string, currentDepth, maxDepth int) []list.Item {
	var items []list.Item

	util.Slog.Debug("scanDirectory called", "dir", dir, "currentDepth", currentDepth, "maxDepth", maxDepth)

	if currentDepth > maxDepth {
		util.Slog.Debug("scanDirectory: maxDepth reached, returning", "dir", dir, "currentDepth", currentDepth)
		return items
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		util.Slog.Error("failed to read directory", "path", dir, "error", err.Error())
		return items
	}

	util.Slog.Debug("scanDirectory: reading entries", "dir", dir, "entryCount", len(entries))

	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files
		if strings.HasPrefix(name, ".") {
			continue
		}

		fullPath := filepath.Join(dir, name)

		info, err := entry.Info()
		if err != nil {
			continue
		}

		if entry.IsDir() {
			util.Slog.Debug("scanDirectory: adding folder", "path", fullPath, "name", name)
			items = append(items, ContextPickerItem{
				Path:     fullPath,
				Name:     name,
				IsFolder: true,
				Size:     0,
				Icon:     "📁",
			})
			// Recursively scan subdirectories to add files from them
			subItems := l.scanDirectory(fullPath, currentDepth+1, maxDepth)
			items = append(items, subItems...)
		} else {
			// Check if it's a media file
			ext := strings.ToLower(filepath.Ext(name))
			if slices.Contains(util.MediaExtensions, ext) {
				util.Slog.Debug("scanDirectory: skipping media file", "path", fullPath, "ext", ext)
				continue
			}

			util.Slog.Debug("scanDirectory: adding file", "path", fullPath, "name", name)
			items = append(items, ContextPickerItem{
				Path:     fullPath,
				Name:     name,
				IsFolder: false,
				Size:     info.Size(),
				Icon:     "📄",
			})
		}
	}

	util.Slog.Debug("scanDirectory: returning items", "dir", dir, "itemCount", len(items))
	return items
}

func (l *ContextPicker) SetSize(w, h int) {
	if w > 2 && h > 2 {
		l.list.SetWidth(w)
		l.list.SetHeight(h - 1) // Account for tips row
	}
}
