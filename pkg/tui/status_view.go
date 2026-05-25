package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// StatusView 状态视图
type StatusView struct {
	client *HTTPClient
	styles *Styles
	status *StatusInfo
	width  int
	height int
}

// StatusInfo 服务端状态信息
type StatusInfo struct {
	Address string `json:"address"`
	Status  string `json:"status"`
	Version string `json:"version"`
}

// NewStatusView 创建状态视图
func NewStatusView(client *HTTPClient, styles *Styles) *StatusView {
	return &StatusView{
		client: client,
		styles: styles,
	}
}

// Init 初始化
func (v *StatusView) Init() tea.Cmd {
	return v.Refresh()
}

// Refresh 刷新状态
func (v *StatusView) Refresh() tea.Cmd {
	return func() tea.Msg {
		resp, err := v.client.Get("/api/v1/status")
		if err != nil {
			return err
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("api error: %s", resp.Message)
		}

		// 解析数据
		data, ok := resp.Data.(map[string]interface{})
		if !ok {
			return statusUpdatedMsg{status: &StatusInfo{}}
		}

		status := &StatusInfo{
			Address: getString(data, "address"),
			Status:  getString(data, "status"),
			Version: getString(data, "version"),
		}

		return statusUpdatedMsg{status: status}
	}
}

// statusUpdatedMsg 状态更新消息
type statusUpdatedMsg struct {
	status *StatusInfo
}

// Update 更新视图
func (v *StatusView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case statusUpdatedMsg:
		v.status = msg.status
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	}
	return v, nil
}

// View 渲染视图
func (v *StatusView) View() string {
	if v.width == 0 {
		return "Loading..."
	}

	if v.status == nil {
		return v.styles.Content.Render("No status information loaded. Press 'r' to refresh.")
	}

	header := v.styles.Header.Render("Server Status")

	statusStyle := v.styles.Success
	if v.status.Status != "running" {
		statusStyle = v.styles.Error
	}

	content := fmt.Sprintf(`
Address: %s
Status:  %s
Version: %s
`, v.status.Address, statusStyle.Render(v.status.Status), v.status.Version)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		v.styles.Content.Render(content),
	)
}
