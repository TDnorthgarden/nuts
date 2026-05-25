package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// EventView 事件视图
type EventView struct {
	client *HTTPClient
	styles *Styles
	events []Event
	width  int
	height int
}

// Event 事件结构
type Event struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Source    string `json:"source"`
	Timestamp string `json:"timestamp"`
}

// NewEventView 创建事件视图
func NewEventView(client *HTTPClient, styles *Styles) *EventView {
	return &EventView{
		client: client,
		styles: styles,
	}
}

// Init 初始化
func (v *EventView) Init() tea.Cmd {
	return v.Refresh()
}

// Refresh 刷新事件列表
func (v *EventView) Refresh() tea.Cmd {
	return func() tea.Msg {
		return eventsUpdatedMsg{events: []Event{}}
	}
}

// eventsUpdatedMsg 事件更新消息
type eventsUpdatedMsg struct {
	events []Event
}

// Update 更新视图
func (v *EventView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case eventsUpdatedMsg:
		v.events = msg.events
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	}
	return v, nil
}

// View 渲染视图
func (v *EventView) View() string {
	if v.width == 0 {
		return "Loading..."
	}

	header := v.styles.Header.Render("📊 Event Stream (Real-time)")
	
	content := fmt.Sprintf(`
ID              Type            Source          Timestamp
─────────────────────────────────────────────────────────────
`)

	if len(v.events) == 0 {
		content += "Waiting for events..."
	} else {
		for _, e := range v.events {
			content += fmt.Sprintf("%-15s %-15s %-15s %s\n", 
				e.ID, e.Type, e.Source, e.Timestamp)
		}
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		v.styles.Content.Render(content),
	)
}
