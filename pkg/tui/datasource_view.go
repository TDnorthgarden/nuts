package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DatasourceView 数据源视图
type DatasourceView struct {
	client      *HTTPClient
	styles      *Styles
	datasources []Datasource
	selected    *DatasourceDetail
	width       int
	height      int
	loading     bool
}

// Datasource 数据源结构
type Datasource struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

// DatasourceDetail 数据源详情
type DatasourceDetail struct {
	ID     string
	Status string
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

		// 解析数据 - API 返回的是字符串数组 ["mock"]
		var datasources []Datasource
		data, ok := resp.Data.([]interface{})
		if !ok {
			return datasourcesUpdatedMsg{datasources: []Datasource{}}
		}

		for _, ds := range data {
			dsName, ok := ds.(string)
			if !ok {
				continue
			}
			datasources = append(datasources, Datasource{
				ID:     dsName,
				Name:   dsName + " Datasource",
				Type:   dsName,
				Status: "active",
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
			return datasourceStatusUpdatedMsg{detail: &DatasourceDetail{ID: id, Status: "unknown"}}
		}

		status := getString(data, "status")
		return datasourceStatusUpdatedMsg{detail: &DatasourceDetail{ID: id, Status: status}}
	}
}

// DisableDatasource 禁用数据源
func (v *DatasourceView) DisableDatasource(id string) tea.Cmd {
	return func() tea.Msg {
		_, err := v.client.Post("/api/v1/datasources/"+id+"/disable", nil)
		if err != nil {
			return err
		}
		return v.RefreshStatus(id)
	}
}

// SwitchDatasource 切换数据源
func (v *DatasourceView) SwitchDatasource(id string) tea.Cmd {
	return func() tea.Msg {
		_, err := v.client.Post("/api/v1/datasources/"+id+"/switch", nil)
		if err != nil {
			return err
		}
		return v.RefreshStatus(id)
	}
}

// datasourcesUpdatedMsg 数据源更新消息
type datasourcesUpdatedMsg struct {
	datasources []Datasource
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
			v.selected.Status = msg.detail.Status
		}
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	case tea.KeyMsg:
		if v.selected != nil {
			switch msg.String() {
			case "esc":
				v.selected = nil
			case "d":
				return v, v.DisableDatasource(v.selected.ID)
			case "s":
				return v, v.SwitchDatasource(v.selected.ID)
			case "D":
				return v, v.DisableDatasource(v.selected.ID)
			case "S":
				return v, v.SwitchDatasource(v.selected.ID)
			}
		} else {
			// 选择数据源
			if len(v.datasources) > 0 && msg.String() == "enter" {
				v.selected = &DatasourceDetail{ID: v.datasources[0].ID}
				return v, v.RefreshStatus(v.selected.ID)
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

	content := fmt.Sprintf(`
ID              Name            Type            Status
─────────────────────────────────────────────────────────────
`)

	if len(v.datasources) == 0 {
		content += "No datasources configured"
	} else {
		for _, ds := range v.datasources {
			statusStyle := v.styles.Info
			if ds.Status == "active" {
				statusStyle = v.styles.Success
			} else if ds.Status == "disabled" {
				statusStyle = v.styles.Error
			}
			content += fmt.Sprintf("%-15s %-15s %-15s %s\n",
				ds.ID, ds.Name, ds.Type, statusStyle.Render(ds.Status))
		}
	}

	content += "\nPress Enter to view details"

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		v.styles.Content.Render(content),
	)
}

// renderDetail 渲染详情
func (v *DatasourceView) renderDetail() string {
	statusStyle := v.styles.Info
	if v.selected.Status == "active" {
		statusStyle = v.styles.Success
	} else if v.selected.Status == "disabled" {
		statusStyle = v.styles.Error
	}

	detail := fmt.Sprintf(`
Datasource Details
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ID:     %s
Status: %s

Actions:
  [d] Disable
  [s] Switch
  [Esc] Back to list
`, v.selected.ID, statusStyle.Render(v.selected.Status))

	return v.styles.Content.Render(detail)
}
