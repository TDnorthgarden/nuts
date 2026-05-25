package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TaskView 任务视图
type TaskView struct {
	client        *HTTPClient
	styles        *Styles
	list          list.Model
	tasks         []Task
	historyTasks  []Task
	loading       bool
	selected      *Task
	showActive    bool
	width         int
	height        int
	selectedIndex int
}

// Task 任务结构
type Task struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	State         string            `json:"state"`

	CreatedAt     time.Time         `json:"created_at"`
	Metadata      map[string]string `json:"metadata"`
}

// TaskItem 任务列表项
type TaskItem struct {
	task      Task
	isHistory bool
}

func (i TaskItem) FilterValue() string { return i.task.Name }
func (i TaskItem) Title() string {
	if i.isHistory {
		return "[H] " + i.task.Name
	}
	return i.task.Name
}
func (i TaskItem) Description() string { return i.task.State }

// NewTaskView 创建任务视图
func NewTaskView(client *HTTPClient, styles *Styles) *TaskView {
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Tasks"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)

	return &TaskView{
		client:     client,
		styles:     styles,
		list:       l,
		showActive: true,
		loading:    true,
	}
}

// Init 初始化
func (v *TaskView) Init() tea.Cmd {
	return tea.Batch(v.Refresh(), v.RefreshHistory())
}

// Refresh 刷新任务列表
func (v *TaskView) Refresh() tea.Cmd {
	return func() tea.Msg {
		resp, err := v.client.Get("/api/v1/tasks")
		if err != nil {
			return err
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("api error: %s", resp.Message)
		}

		// 解析任务数据
		data, ok := resp.Data.(map[string]interface{})
		if !ok {
			return tasksUpdatedMsg{tasks: []Task{}}
		}

		tasksSlice, ok := data["tasks"].([]interface{})
		if !ok {
			return tasksUpdatedMsg{tasks: []Task{}}
		}

		var tasks []Task
		for _, t := range tasksSlice {
			taskMap, ok := t.(map[string]interface{})
			if !ok {
				continue
			}
			tasks = append(tasks, parseTask(taskMap))
		}

		return tasksUpdatedMsg{tasks: tasks}
	}
}

// RefreshHistory 刷新任务历史
func (v *TaskView) RefreshHistory() tea.Cmd {
	return func() tea.Msg {
		resp, err := v.client.Get("/api/v1/tasks?include_completed=true")
		if err != nil {
			return err
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("api error: %s", resp.Message)
		}

		// 解析任务数据
		data, ok := resp.Data.(map[string]interface{})
		if !ok {
			return taskHistoryUpdatedMsg{tasks: []Task{}}
		}

		tasksSlice, ok := data["tasks"].([]interface{})
		if !ok {
			return taskHistoryUpdatedMsg{tasks: []Task{}}
		}

		var tasks []Task
		for _, t := range tasksSlice {
			taskMap, ok := t.(map[string]interface{})
			if !ok {
				continue
			}
			tasks = append(tasks, parseTask(taskMap))
		}

		return taskHistoryUpdatedMsg{tasks: tasks}
	}
}

// tasksUpdatedMsg 任务更新消息
type tasksUpdatedMsg struct {
	tasks []Task
}

// taskHistoryUpdatedMsg 任务历史更新消息
type taskHistoryUpdatedMsg struct {
	tasks []Task
}

// Update 更新视图
func (v *TaskView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tasksUpdatedMsg:
		v.tasks = msg.tasks
		v.loading = false
		if v.selectedIndex >= len(v.tasks) {
			v.selectedIndex = 0
		}

	case taskHistoryUpdatedMsg:
		v.historyTasks = msg.tasks
		v.loading = false
		if v.selectedIndex >= len(v.historyTasks) {
			v.selectedIndex = 0
		}

	case error:
		v.loading = false
		// 显示错误信息
		fmt.Printf("[TaskView] Error: %v\n", msg)

	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height

	case tea.KeyMsg:
		if v.selected != nil {
			if msg.String() == "esc" {
				v.selected = nil
				return v, nil
			}
		} else {
			switch msg.String() {
			case "up":
				v.selectedIndex--
				if v.selectedIndex < 0 {
					v.selectedIndex = 0
				}
				return v, nil
			case "down":
				v.selectedIndex++
				var maxIndex int
				if v.showActive {
					maxIndex = len(v.tasks) - 1
				} else {
					maxIndex = len(v.historyTasks) - 1
				}
				if v.selectedIndex > maxIndex {
					v.selectedIndex = maxIndex
				}
				return v, nil
			case "enter":
				var tasks []Task
				if v.showActive {
					tasks = v.tasks
				} else {
					tasks = v.historyTasks
				}
				if len(tasks) > 0 && v.selectedIndex < len(tasks) {
					v.selected = &tasks[v.selectedIndex]
					return v, nil
				}
			case "h":
				v.showActive = !v.showActive
				v.selectedIndex = 0
				return v, v.Refresh()
			case "r":
				if v.showActive {
					return v, v.Refresh()
				} else {
					return v, v.RefreshHistory()
				}
			}
		}
	}
	return v, nil
}

// updateList 更新列表内容
func (v *TaskView) updateList() {
	var tasks []Task
	if v.showActive {
		// 显示当前任务（非 completed 状态）
		for _, task := range v.tasks {
			if task.State != "completed" && task.State != "failed" {
				tasks = append(tasks, task)
			}
		}
	} else {
		// 显示历史任务（completed 状态）
		for _, task := range v.historyTasks {
			if task.State == "completed" || task.State == "failed" {
				tasks = append(tasks, task)
			}
		}
	}

	// 如果没有任务，显示所有任务
	if len(tasks) == 0 && v.showActive {
		tasks = v.tasks
	}
	if len(tasks) == 0 && !v.showActive {
		tasks = v.historyTasks
	}

	items := make([]list.Item, len(tasks))
	for i, task := range tasks {
		items[i] = TaskItem{task: task, isHistory: !v.showActive}
	}
	v.list.SetItems(items)
}

// refreshCurrentList 刷新当前列表
func (v *TaskView) refreshCurrentList() tea.Cmd {
	return nil
}

// View 渲染视图
func (v *TaskView) View() string {
	if v.selected != nil {
		return v.renderTaskDetail()
	}

	if v.loading {
		return "Loading..."
	}

	viewType := "Active Tasks"
	if !v.showActive {
		viewType = "Task History"
	}

	header := v.styles.Header.Render(fmt.Sprintf("📋 %s (Press Enter for details, 'h' to toggle, 'r' to refresh)", viewType))

	var tasks []Task
	if v.showActive {
		for _, task := range v.tasks {
			if task.State != "completed" && task.State != "failed" {
				tasks = append(tasks, task)
			}
		}
	} else {
		for _, task := range v.historyTasks {
			if task.State == "completed" || task.State == "failed" {
				tasks = append(tasks, task)
			}
		}
	}

	content := fmt.Sprintf(`
ID              Name            State
────────────────────────────────────────
`)

	if len(tasks) == 0 {
		content += "No tasks"
	} else {
		for i, task := range tasks {
			stateStyle := v.styles.Info
			if task.State == "completed" {
				stateStyle = v.styles.Success
			} else if task.State == "failed" {
				stateStyle = v.styles.Error
			}
			prefix := "  "
			if i == v.selectedIndex {
				prefix = "▶ "
			}
			content += fmt.Sprintf("%s%-15s %-15s %s\n",
				prefix, task.ID, task.Name, stateStyle.Render(task.State))
		}
	}

	content += "\nPress Enter to view details"

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		v.styles.Content.Render(content),
	)
}

// renderTaskDetail 渲染任务详情
func (v *TaskView) renderTaskDetail() string {
	if v.selected == nil {
		return ""
	}

	task := v.selected
	stateStyle := v.styles.Info
	switch task.State {
	case "completed":
		stateStyle = v.styles.Success
	case "failed":
		stateStyle = v.styles.Error
	case "pending":
		stateStyle = v.styles.Warning
	}

	detail := fmt.Sprintf(`
Task Details
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

ID:       %s
Name:     %s
State:    %s
Created:  %s

Metadata:
`, task.ID, task.Name, stateStyle.Render(task.State), task.CreatedAt.Format("2006-01-02 15:04:05"))

	if len(task.Metadata) > 0 {
		for k, v := range task.Metadata {
			detail += fmt.Sprintf("  %s: %s\n", k, v)
		}
	} else {
		detail += "  (no metadata)\n"
	}

	detail += "\nPress Esc to go back"

	return v.styles.Content.Render(detail)
}

// parseTask 解析任务数据
func parseTask(data map[string]interface{}) Task {
	task := Task{
		ID:    getString(data, "id"),
		Name:  getString(data, "name"),
		State: getString(data, "state"),
	}

	return task
}
