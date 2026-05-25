package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PolicyView 策略视图
type PolicyView struct {
	client        *HTTPClient
	styles        *Styles
	list          list.Model
	policies      []Policy
	selected      *PolicyDetail
	width         int
	height        int
	loading       bool
	selectedIndex int
}

// Policy 策略结构
type Policy struct {
	ID      string `json:"id"`
	Name    string `json:"description"`
	Enabled bool   `json:"enabled"`
}

// PolicyDetail 策略详情
type PolicyDetail struct {
	ID        string
	Name      string
	Dsl       string
	DslEngine string
	Enabled   bool
	Version   int
}

// PolicyItem 策略列表项
type PolicyItem struct {
	policy Policy
}

func (i PolicyItem) FilterValue() string { return i.policy.Name }
func (i PolicyItem) Title() string       { return fmt.Sprintf("[%s] %s", i.policy.ID, i.policy.Name) }
func (i PolicyItem) Description() string {
	status := "disabled"
	if i.policy.Enabled {
		status = "enabled"
	}
	return fmt.Sprintf("Status: %s", status)
}

// NewPolicyView 创建策略视图
func NewPolicyView(client *HTTPClient, styles *Styles) *PolicyView {
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Policies"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)

	return &PolicyView{
		client:  client,
		styles:  styles,
		list:    l,
		loading: true,
	}
}

// Init 初始化
func (v *PolicyView) Init() tea.Cmd {
	return v.Refresh()
}

// Refresh 刷新策略列表
func (v *PolicyView) Refresh() tea.Cmd {
	return func() tea.Msg {
		resp, err := v.client.Get("/api/v1/policies")
		if err != nil {
			return err
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("api error: %s", resp.Message)
		}

		// 解析数据 - API 返回的是对象数组
		var policies []Policy
		data, ok := resp.Data.([]interface{})
		if !ok {
			return policiesUpdatedMsg{policies: []Policy{}}
		}

		for _, p := range data {
			pMap, ok := p.(map[string]interface{})
			if !ok {
				continue
			}
			enabled := false
			if e, ok := pMap["enabled"].(bool); ok {
				enabled = e
			}
			policies = append(policies, Policy{
				ID:      getString(pMap, "id"),
				Name:    getString(pMap, "description"),
				Enabled: enabled,
			})
		}

		return policiesUpdatedMsg{policies: policies}
	}
}

// GetPolicyDetail 获取策略详情
func (v *PolicyView) GetPolicyDetail(id string) tea.Cmd {
	return func() tea.Msg {
		resp, err := v.client.Get("/api/v1/policies/" + id)
		if err != nil {
			return err
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("api error: %s", resp.Message)
		}

		data, ok := resp.Data.(map[string]interface{})
		if !ok {
			return policyDetailUpdatedMsg{detail: &PolicyDetail{ID: id}}
		}

		version := int(getFloat(data, "version"))
		detail := &PolicyDetail{
			ID:        getString(data, "id"),
			Name:      getString(data, "description"),
			Dsl:       getString(data, "dsl"),
			DslEngine: getString(data, "dsl_engine"),
			Enabled:   getBool(data, "enabled"),
			Version:   version,
		}

		return policyDetailUpdatedMsg{detail: detail}
	}
}

// policiesUpdatedMsg 策略更新消息
type policiesUpdatedMsg struct {
	policies []Policy
}

// policyDetailUpdatedMsg 策略详情更新消息
type policyDetailUpdatedMsg struct {
	detail *PolicyDetail
}

// Update 更新视图
func (v *PolicyView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case policiesUpdatedMsg:
		v.policies = msg.policies
		v.loading = false
		if v.selectedIndex >= len(v.policies) {
			v.selectedIndex = 0
		}

	case policyDetailUpdatedMsg:
		v.selected = msg.detail

	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height

	case tea.KeyMsg:
		if v.selected != nil {
			if msg.String() == "esc" {
				v.selected = nil
			}
		} else {
			switch msg.String() {
			case "up":
				if v.selectedIndex > 0 {
					v.selectedIndex--
				}
			case "down":
				if v.selectedIndex < len(v.policies)-1 {
					v.selectedIndex++
				}
			case "enter":
				if len(v.policies) > 0 && v.selectedIndex < len(v.policies) {
					return v, v.GetPolicyDetail(v.policies[v.selectedIndex].ID)
				}
			}
		}
	}
	return v, nil
}

// View 渲染视图
func (v *PolicyView) View() string {
	if v.selected != nil {
		return v.renderDetail()
	}

	if v.loading {
		return "Loading..."
	}

	header := v.styles.Header.Render("📋 Policies (Press Enter for details, 'r' to refresh)")

	content := fmt.Sprintf(`
ID              Name            Status
─────────────────────────────────────────────────────────────
`)

	if len(v.policies) == 0 {
		content += "No policies configured"
	} else {
		for i, policy := range v.policies {
			statusStyle := v.styles.Error
			if policy.Enabled {
				statusStyle = v.styles.Success
			}
			prefix := "  "
			if i == v.selectedIndex {
				prefix = "> "
			}
			content += fmt.Sprintf("%s%-15s %-15s %s\n",
				prefix, policy.ID, policy.Name, statusStyle.Render(fmt.Sprintf("%v", policy.Enabled)))
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
func (v *PolicyView) renderDetail() string {
	statusStyle := v.styles.Error
	if v.selected.Enabled {
		statusStyle = v.styles.Success
	}

	detail := fmt.Sprintf(`
Policy Details
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ID:         %s
Name:       %s
DSL:        %s
DSL Engine: %s
Version:    %d
Enabled:    %s

Press Esc to go back
`,
		v.selected.ID,
		v.selected.Name,
		v.selected.Dsl,
		v.selected.DslEngine,
		v.selected.Version,
		statusStyle.Render(fmt.Sprintf("%v", v.selected.Enabled)),
	)

	return v.styles.Content.Render(detail)
}
