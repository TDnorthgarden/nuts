package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DatasourceView 数据源视图
type DatasourceView struct {
	client      *HTTPClient
	styles      *Styles
	datasources []DatasourceInfo
	cursor      int // 列表光标位置
	selected    *DatasourceDetail
	width       int
	height      int
	loading     bool
}

// DatasourceInfo 数据源信息（匹配 API 返回的 {name, active} 格式）
type DatasourceInfo struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// DatasourceDetail 数据源详情
type DatasourceDetail struct {
	ID     string
	Active bool
}

// NewDatasourceView 创建数据源视图
func NewDatasourceView(client *HTTPClient, styles *Styles) *DatasourceView {
	return &DatasourceView{
		client:  client,
		styles:  styles,
		loading: true,
	}
}

// Init 初始化
func (v *DatasourceView) Init() tea.Cmd {
	return v.Refresh()
}

// Refresh 刷新数据源列表
func (v *DatasourceView) Refresh() tea.Cmd {
	return func() tea.Msg {
		resp, err := v.client.Get("/api/v1/datasources")
		if err != nil {
			return err
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("api error: %s", resp.Message)
		}

		// API 返回 [{name, active}, ...] 格式
		var datasources []DatasourceInfo
		data, ok := resp.Data.([]interface{})
		if !ok {
			return datasourcesUpdatedMsg{datasources: []DatasourceInfo{}}
		}

		for _, item := range data {
			dsMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			name := getString(dsMap, "name")
			if name == "" {
				continue
			}

			active := false
			if a, ok := dsMap["active"]; ok {
				active, _ = a.(bool)
			}

			datasources = append(datasources, DatasourceInfo{
				Name:   name,
				Active: active,
			})
		}

		return datasourcesUpdatedMsg{datasources: datasources}
	}
}

// RefreshStatus 刷新数据源状态
func (v *DatasourceView) RefreshStatus(id string) tea.Cmd {
	return func() tea.Msg {
		resp, err := v.client.Get("/api/v1/datasources/" + id)
		if err != nil {
			return err
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("api error: %s", resp.Message)
		}

		data, ok := resp.Data.(map[string]interface{})
		if !ok {
			return datasourceStatusUpdatedMsg{detail: &DatasourceDetail{ID: id}}
		}

		active := false
		if a, ok := data["active"]; ok {
			active, _ = a.(bool)
		}

		return datasourceStatusUpdatedMsg{detail: &DatasourceDetail{ID: id, Active: active}}
	}
}

// DisableDatasource 禁用数据源
func (v *DatasourceView) DisableDatasource(id string) tea.Cmd {
	return func() tea.Msg {
		_, err := v.client.Post("/api/v1/datasources/"+id+"/disable", nil)
		if err != nil {
			return err
		}
		return v.Refresh()
	}
}

// SwitchDatasource 切换数据源
func (v *DatasourceView) SwitchDatasource(id string) tea.Cmd {
	return func() tea.Msg {
		_, err := v.client.Post("/api/v1/datasources/"+id+"/switch", nil)
		if err != nil {
			return err
		}
		return v.Refresh()
	}
}

// datasourcesUpdatedMsg 数据源更新消息
type datasourcesUpdatedMsg struct {
	datasources []DatasourceInfo
}

// datasourceStatusUpdatedMsg 数据源状态更新消息
type datasourceStatusUpdatedMsg struct {
	detail *DatasourceDetail
}

// Update 更新视图
func (v *DatasourceView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case datasourcesUpdatedMsg:
		v.datasources = msg.datasources
		v.loading = false
	case datasourceStatusUpdatedMsg:
		if v.selected != nil && v.selected.ID == msg.detail.ID {
			v.selected = msg.detail
		}
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	case tea.KeyMsg:
		if v.selected != nil {
			switch msg.String() {
			case "esc":
				v.selected = nil
				return v, v.Refresh()
			case "d", "D":
				return v, v.DisableDatasource(v.selected.ID)
			case "s", "S":
				return v, v.SwitchDatasource(v.selected.ID)
			}
		} else {
			switch msg.String() {
			case "up", "k":
				if v.cursor > 0 {
					v.cursor--
				}
			case "down", "j":
				if v.cursor < len(v.datasources)-1 {
					v.cursor++
				}
			case "enter":
				if len(v.datasources) > 0 {
					sel := v.datasources[v.cursor]
					v.selected = &DatasourceDetail{
						ID:     sel.Name,
						Active: sel.Active,
					}
					return v, v.RefreshStatus(sel.Name)
				}
			}
		}
	}
	return v, nil
}

// View 渲染视图
func (v *DatasourceView) View() string {
	if v.loading {
		return "Loading..."
	}

	if v.selected != nil {
		return v.renderDetail()
	}

	header := v.styles.Header.Render(" Datasources")

	// 表格头
	tableHeader := fmt.Sprintf("%-4s %-20s %s\n", "", "Name", "Status")
	separator := strings.Repeat("─", 60) + "\n"

	content := tableHeader + separator

	if len(v.datasources) == 0 {
		content += "No datasources registered"
	} else {
		for i, ds := range v.datasources {
			cursor := "  " // 未选中
			if i == v.cursor {
				cursor = v.styles.Success.Render("▶ ")
			}

			statusStr := "inactive"
			statusStyle := v.styles.Info
			if ds.Active {
				statusStr = "active"
				statusStyle = v.styles.Success
			}

			content += fmt.Sprintf("%s%-20s %s\n",
				cursor, ds.Name, statusStyle.Render(statusStr))
		}
	}

	content += "\n↑↓ Navigate  Enter View Details  Esc Back"

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		v.styles.Content.Render(content),
	)
}

// renderDetail 渲染详情
func (v *DatasourceView) renderDetail() string {
	statusStr := "inactive"
	statusStyle := v.styles.Info
	if v.selected.Active {
		statusStr = "active"
		statusStyle = v.styles.Success
	}

	detail := fmt.Sprintf(`
Datasource Details
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Name:   %s
Status: %s

Actions:
  [d] Disable
  [s] Switch
  [Esc] Back to list
`, v.selected.ID, statusStyle.Render(statusStr))

	return v.styles.Content.Render(detail)
}
