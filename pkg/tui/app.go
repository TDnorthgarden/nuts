package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ViewType 表示当前视图类型
type ViewType int

const (
	ViewStatus ViewType = iota
	ViewDatasources
	ViewPolicies
	ViewTasks
	ViewDebug
	ViewTraces
	ViewHelp
)

// App 是 TUI 应用的主模型
type App struct {
	serverURL   string
	client      *HTTPClient
	width       int
	height      int
	currentView ViewType
	spinner     spinner.Model
	loading     bool
	err         error
	lastUpdate  time.Time

	// 各视图的数据
	statusView     *StatusView
	datasourceView *DatasourceView
	policyView     *PolicyView
	taskView       *TaskView
	debugView      *DebugView
	traceView      *TraceView

	// 导航历史
	previousView ViewType

	// 样式
	styles *Styles
}

// Styles 包含所有 UI 样式
type Styles struct {
	Title        lipgloss.Style
	Header       lipgloss.Style
	Footer       lipgloss.Style
	Menu         lipgloss.Style
	MenuSelected lipgloss.Style
	Content      lipgloss.Style
	Error        lipgloss.Style
	Success      lipgloss.Style
	Warning      lipgloss.Style
	Info         lipgloss.Style
	Border       lipgloss.Style
}

// NewStyles 创建默认样式
func NewStyles() *Styles {
	return &Styles{
		Title: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFD700")).
			Background(lipgloss.Color("#333333")).
			Padding(0, 1).
			Width(50),
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#4B0082")).
			Padding(0, 1),
		Footer: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#CCCCCC")).
			Background(lipgloss.Color("#333333")).
			Padding(0, 1),
		Menu: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#CCCCCC")).
			Padding(0, 1),
		MenuSelected: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFD700")).
			Background(lipgloss.Color("#4B0082")).
			Padding(0, 1),
		Content: lipgloss.NewStyle().
			Padding(1),
		Error: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF6B6B")),
		Success: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#51CF66")),
		Warning: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFD93D")),
		Info: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#74C0FC")),
		Border: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#666666")),
	}
}

// NewApp 创建新的 TUI 应用
func NewApp(serverURL string) *App {
	return NewAppWithToken(serverURL, "")
}

// NewAppWithToken 创建新的 TUI 应用，指定服务端地址和认证 token
func NewAppWithToken(serverURL, token string) *App {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD700"))

	client := NewHTTPClientWithToken(serverURL, token)
	styles := NewStyles()

	return &App{
		serverURL:      serverURL,
		client:         client,
		spinner:        s,
		currentView:    ViewStatus,
		styles:         styles,
		statusView:     NewStatusView(client, styles),
		datasourceView: NewDatasourceView(client, styles),
		policyView:     NewPolicyView(client, styles),
		taskView:       NewTaskView(client, styles),
		debugView:      NewDebugView(client, styles),
		traceView:      NewTraceView(client, styles),
	}
}

// Run 启动 TUI 应用
func (a *App) Run() error {
	p := tea.NewProgram(a, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// Init 初始化 TUI 应用
type tickMsg time.Time

func (a *App) Init() tea.Cmd {
	return tea.Batch(
		a.spinner.Tick,
		tickCmd(),
		a.taskView.Init(),
	)
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update 处理消息
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// 先让子视图处理消息（包括Esc键）
	var cmd tea.Cmd
	switch a.currentView {
	case ViewStatus:
		_, cmd = a.statusView.Update(msg)
	case ViewDatasources:
		_, cmd = a.datasourceView.Update(msg)
	case ViewPolicies:
		_, cmd = a.policyView.Update(msg)
	case ViewTasks:
		_, cmd = a.taskView.Update(msg)
	case ViewDebug:
		_, cmd = a.debugView.Update(msg)
	case ViewTraces:
		_, cmd = a.traceView.Update(msg)
	case ViewHelp:
		// Help视图不处理消息
	}

	// 如果子视图已经处理了消息（返回了cmd），就不继续处理
	if cmd != nil {
		var spinnerCmd tea.Cmd
		a.spinner, spinnerCmd = a.spinner.Update(msg)
		return a, tea.Batch(cmd, spinnerCmd)
	}

	// 子视图没有处理，继续处理全局快捷键
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("q", "ctrl+c"))):
			return a, tea.Quit
		case key.Matches(msg, key.NewBinding(key.WithKeys("1"))):
			a.currentView = ViewStatus
			return a, a.statusView.Refresh()
		case key.Matches(msg, key.NewBinding(key.WithKeys("2"))):
			a.currentView = ViewDatasources
			return a, a.datasourceView.Refresh()
		case key.Matches(msg, key.NewBinding(key.WithKeys("3"))):
			a.currentView = ViewPolicies
			return a, a.policyView.Refresh()
		case key.Matches(msg, key.NewBinding(key.WithKeys("4"))):
			a.currentView = ViewTasks
			return a, a.taskView.Refresh()
		case key.Matches(msg, key.NewBinding(key.WithKeys("5"))):
			a.currentView = ViewDebug
			return a, a.debugView.Refresh()
		case key.Matches(msg, key.NewBinding(key.WithKeys("6"))):
			a.currentView = ViewTraces
			a.traceView.width = a.width
			a.traceView.height = a.height
			return a, a.traceView.EnterInput()
		case key.Matches(msg, key.NewBinding(key.WithKeys("?"))):
			if a.currentView != ViewHelp {
				a.currentView = ViewHelp
			} else {
				a.currentView = ViewStatus
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("r"))):
			return a, a.refreshCurrentView()
		}

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		// 传递窗口大小消息给当前视图
		switch a.currentView {
		case ViewStatus:
			_, cmd = a.statusView.Update(msg)
		case ViewDatasources:
			_, cmd = a.datasourceView.Update(msg)
		case ViewPolicies:
			_, cmd = a.policyView.Update(msg)
		case ViewTasks:
			_, cmd = a.taskView.Update(msg)
		case ViewDebug:
			_, cmd = a.debugView.Update(msg)
		case ViewTraces:
			_, cmd = a.traceView.Update(msg)
		}

	case tickMsg:
		a.lastUpdate = time.Time(msg)
		return a, tea.Batch(tickCmd(), a.refreshCurrentView())

	case error:
		a.err = msg

	case OpenTraceMsg:
		a.previousView = a.currentView
		a.traceView.SetTraceID(msg.TraceID)
		a.traceView.width = a.width
		a.traceView.height = a.height
		a.currentView = ViewTraces
		return a, a.traceView.Refresh()

	case BackMsg:
		if a.previousView != 0 {
			a.currentView = a.previousView
			a.previousView = ViewStatus
		} else {
			a.currentView = ViewTasks
		}
		return a, nil
	}

	var spinnerCmd tea.Cmd
	a.spinner, spinnerCmd = a.spinner.Update(msg)

	return a, tea.Batch(cmd, spinnerCmd)
}

// refreshCurrentView 刷新当前视图
func (a *App) refreshCurrentView() tea.Cmd {
	switch a.currentView {
	case ViewStatus:
		return a.statusView.Refresh()
	case ViewDatasources:
		return a.datasourceView.Refresh()
	case ViewPolicies:
		return a.policyView.Refresh()
	case ViewTasks:
		return a.taskView.Refresh()
	case ViewDebug:
		return a.debugView.Refresh()
	case ViewTraces:
		return a.traceView.Refresh()
	}
	return nil
}

// View 渲染 UI
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "Loading..."
	}

	// 标题栏
	title := a.styles.Title.Render("🥜 Nuts TUI - " + a.serverURL)

	// 菜单栏
	menu := a.renderMenu()

	// 内容区
	var content string
	switch a.currentView {
	case ViewStatus:
		content = a.statusView.View()
	case ViewDatasources:
		content = a.datasourceView.View()
	case ViewPolicies:
		content = a.policyView.View()
	case ViewTasks:
		content = a.taskView.View()
	case ViewDebug:
		content = a.debugView.View()
	case ViewTraces:
		content = a.traceView.View()
	case ViewHelp:
		content = a.renderHelp()
	}

	// 状态栏
	status := a.renderStatus()

	// 错误提示
	var errorMsg string
	if a.err != nil {
		errorMsg = a.styles.Error.Render(fmt.Sprintf("Error: %v", a.err))
		a.err = nil // 清除错误
	}

	// 组合所有部分
	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		menu,
		content,
		errorMsg,
		status,
	)
}

// renderMenu 渲染菜单
func (a *App) renderMenu() string {
	items := []struct {
		key   string
		label string
		view  ViewType
	}{
		{"1", "Status", ViewStatus},
		{"2", "Datasources", ViewDatasources},
		{"3", "Policies", ViewPolicies},
		{"4", "Tasks", ViewTasks},
		{"5", "Debug", ViewDebug},
		{"6", "Traces", ViewTraces},
		{"?", "Help", ViewHelp},
		{"q", "Quit", -1},
	}

	var menuItems []string
	for _, item := range items {
		style := a.styles.Menu
		if item.view == a.currentView {
			style = a.styles.MenuSelected
		}
		menuItems = append(menuItems, style.Render(fmt.Sprintf("[%s] %s", item.key, item.label)))
	}

	return lipgloss.JoinHorizontal(lipgloss.Left, menuItems...)
}

// renderStatus 渲染状态栏
func (a *App) renderStatus() string {
	status := fmt.Sprintf("Last update: %s | Press ? for help", a.lastUpdate.Format("15:04:05"))
	return a.styles.Footer.Render(status)
}

// renderHelp 渲染帮助信息
func (a *App) renderHelp() string {
	help := `
Keyboard Shortcuts:

  1          - Status View (Server status)
  2          - Datasources View (Manage datasources)
  3          - Policies View (View and manage policies)
  4          - Tasks View (Task list and history)
  5          - Debug View (Runtime debug info)
  6          - Traces View (Event trace timeline)
  ?          - Toggle this help

Navigation:
  r          - Refresh current view
  q/ctrl+c   - Quit
  ↑/↓        - Navigate list
  Enter      - Select/View details
  Esc        - Go back

Datasource View:
  Enter      - View datasource details
  d          - Disable datasource
  s          - Switch datasource

Policy View:
  Enter      - View policy details

Task View:
  Enter      - View task details
  h          - View task history
`
	return a.styles.Content.Render(help)
}
