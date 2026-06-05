package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TraceView 追踪视图
type TraceView struct {
	client      *HTTPClient
	styles      *Styles
	input       textinput.Model
	traceID     string
	timeline    *TraceTimeline
	taskEvents  []EventLogEntry
	loading     bool
	showEvents  bool
	selectedIdx int
	width       int
	height      int
	errMsg      string
}

// TraceTimeline 追踪时间线
type TraceTimeline struct {
	TraceID  string          `json:"trace_id"`
	Entries  []EventLogEntry `json:"entries"`
	Tasks    []TaskSummary   `json:"tasks"`
	Duration float64         `json:"duration"`
}

// TaskSummary 任务摘要
type TaskSummary struct {
	ID        string    `json:"id"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EventLogEntry 事件日志条目
type EventLogEntry struct {
	ID            string  `json:"id"`
	TraceID       string  `json:"trace_id"`
	EventID       string  `json:"event_id"`
	Stage         string  `json:"stage"`
	EventType     string  `json:"event_type"`
	Source        string  `json:"source"`
	Timestamp     time.Time `json:"timestamp"`
	TaskID        string  `json:"task_id,omitempty"`
	OldState      string  `json:"old_state,omitempty"`
	NewState      string  `json:"new_state,omitempty"`
	ComponentName string  `json:"component_name,omitempty"`
	Success       *bool   `json:"success,omitempty"`
	Message       string  `json:"message,omitempty"`
}

// NewTraceView 创建追踪视图
func NewTraceView(client *HTTPClient, styles *Styles) *TraceView {
	ti := textinput.New()
	ti.Placeholder = "Enter TraceID (hex)"
	ti.CharLimit = 64
	ti.Width = 50

	return &TraceView{
		client: client,
		styles: styles,
		input:  ti,
	}
}

// SetTraceID 设置要查询的 TraceID（从外部导航进入时调用，跳过输入）
func (v *TraceView) SetTraceID(traceID string) {
	v.traceID = traceID
	v.input.SetValue(traceID)
	v.input.Blur()
	v.timeline = nil
	v.taskEvents = nil
	v.showEvents = false
	v.selectedIdx = 0
	v.loading = true
	v.errMsg = ""
}

// EnterInput 进入输入模式（按 6 时调用）
func (v *TraceView) EnterInput() tea.Cmd {
	v.traceID = ""
	v.timeline = nil
	v.taskEvents = nil
	v.showEvents = false
	v.loading = false
	v.errMsg = ""
	v.input.SetValue("")
	v.input.Focus()
	return textinput.Blink
}

// hasData 是否已加载了追踪数据
func (v *TraceView) hasData() bool {
	return v.timeline != nil
}

// Init 初始化
func (v *TraceView) Init() tea.Cmd {
	return nil
}

// Refresh 刷新追踪数据
func (v *TraceView) Refresh() tea.Cmd {
	if v.traceID == "" {
		return nil
	}
	return v.fetchTrace(v.traceID)
}

// fetchTrace 发起追踪查询
func (v *TraceView) fetchTrace(traceID string) tea.Cmd {
	return func() tea.Msg {
		resp, err := v.client.Get("/api/v1/traces/" + traceID)
		if err != nil {
			return traceErrMsg{err: err}
		}
		if !resp.IsSuccess() {
			return traceErrMsg{err: fmt.Errorf("api error: %s", resp.Message)}
		}

		data, ok := resp.Data.(map[string]any)
		if !ok {
			return traceUpdatedMsg{timeline: &TraceTimeline{TraceID: traceID}}
		}

		timeline := &TraceTimeline{
			TraceID: getString(data, "trace_id"),
		}
		if timeline.TraceID == "" {
			timeline.TraceID = traceID
		}

		if dur, ok := data["duration"].(float64); ok {
			timeline.Duration = dur
		}

		if entries, ok := data["entries"].([]any); ok {
			for _, e := range entries {
				if entryMap, ok := e.(map[string]any); ok {
					timeline.Entries = append(timeline.Entries, parseEventLogEntry(entryMap))
				}
			}
		}

		if tasks, ok := data["tasks"].([]any); ok {
			for _, t := range tasks {
				if taskMap, ok := t.(map[string]any); ok {
					timeline.Tasks = append(timeline.Tasks, TaskSummary{
						ID:    getString(taskMap, "id"),
						State: getString(taskMap, "state"),
					})
				}
			}
		}

		return traceUpdatedMsg{timeline: timeline}
	}
}

// fetchTaskEvents 获取任务事件
func (v *TraceView) fetchTaskEvents(taskID string) tea.Cmd {
	return func() tea.Msg {
		resp, err := v.client.Get("/api/v1/tasks/" + taskID + "/events")
		if err != nil {
			return taskEventsErrMsg{err: err}
		}
		if !resp.IsSuccess() {
			return taskEventsErrMsg{err: fmt.Errorf("api error: %s", resp.Message)}
		}

		data, ok := resp.Data.(map[string]any)
		if !ok {
			return taskEventsUpdatedMsg{entries: nil}
		}

		var entries []EventLogEntry
		if events, ok := data["events"].([]any); ok {
			for _, e := range events {
				if entryMap, ok := e.(map[string]any); ok {
					entries = append(entries, parseEventLogEntry(entryMap))
				}
			}
		}

		return taskEventsUpdatedMsg{entries: entries}
	}
}

type traceUpdatedMsg struct {
	timeline *TraceTimeline
}

type traceErrMsg struct {
	err error
}

type taskEventsUpdatedMsg struct {
	entries []EventLogEntry
}

type taskEventsErrMsg struct {
	err error
}

// BackMsg 从 TraceView 返回上一级视图
type BackMsg struct{}

// Update 更新视图
func (v *TraceView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case traceUpdatedMsg:
		v.timeline = msg.timeline
		v.loading = false
		v.errMsg = ""
		return v, nil

	case traceErrMsg:
		v.loading = false
		v.errMsg = msg.err.Error()
		return v, nil

	case taskEventsUpdatedMsg:
		v.taskEvents = msg.entries
		v.showEvents = true
		v.selectedIdx = 0
		return v, nil

	case taskEventsErrMsg:
		v.errMsg = msg.err.Error()
		return v, nil

	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
		return v, nil

	case tea.KeyMsg:
		return v.handleKey(msg)
	}
	return v, nil
}

// handleKey 统一处理按键
func (v *TraceView) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 输入框有焦点时，所有按键先交给输入框处理
	if v.input.Focused() {
		switch msg.String() {
		case "esc":
			v.input.Blur()
			v.input.SetValue("")
			// 如果已有数据，退出输入模式回到数据显示
			if v.hasData() {
				return v, nil
			}
			// 没有数据，返回上一级
			return v, func() tea.Msg { return BackMsg{} }

		case "enter":
			traceID := strings.TrimSpace(v.input.Value())
			v.input.Blur()
			if traceID != "" {
				v.traceID = traceID
				v.loading = true
				v.errMsg = ""
				return v, v.fetchTrace(traceID)
			}
			return v, nil

		default:
			// 所有其他按键交给 textinput 处理
			var cmd tea.Cmd
			v.input, cmd = v.input.Update(msg)
			return v, cmd
		}
	}

	// 输入框无焦点，处理导航按键
	switch msg.String() {
	case "esc":
		if v.showEvents {
			v.showEvents = false
			v.taskEvents = nil
			return v, nil
		}
		// 返回上一级
		v.traceID = ""
		v.timeline = nil
		v.errMsg = ""
		return v, func() tea.Msg { return BackMsg{} }

	case "/":
		// 进入搜索模式
		v.input.SetValue("")
		v.input.Focus()
		return v, textinput.Blink

	case "r":
		if v.traceID != "" {
			v.loading = true
			v.errMsg = ""
			return v, v.Refresh()
		}

	case "e":
		if v.hasData() && !v.showEvents && len(v.timeline.Entries) > 0 {
			if v.selectedIdx < len(v.timeline.Entries) {
				taskID := v.timeline.Entries[v.selectedIdx].TaskID
				if taskID != "" {
					return v, v.fetchTaskEvents(taskID)
				}
			}
		}

	case "up":
		if v.selectedIdx > 0 {
			v.selectedIdx--
		}
		return v, nil

	case "down":
		maxIdx := 0
		if v.showEvents {
			maxIdx = len(v.taskEvents) - 1
		} else if v.hasData() {
			maxIdx = len(v.timeline.Entries) - 1
		}
		if v.selectedIdx < maxIdx {
			v.selectedIdx++
		}
		return v, nil
	}
	return v, nil
}

// View 渲染视图
func (v *TraceView) View() string {
	if v.width == 0 {
		return "Loading..."
	}

	// 输入框有焦点 或 没有任何数据 → 显示输入界面
	if v.input.Focused() || (!v.hasData() && !v.loading) {
		return v.renderInput()
	}

	if v.loading {
		return v.styles.Content.Render("Loading trace " + v.traceID + " ...")
	}

	if v.errMsg != "" {
		return v.styles.Content.Render("Error: " + v.errMsg + "\n\nPress [/] to search again, [Esc] to go back")
	}

	if !v.hasData() {
		return v.styles.Content.Render("No data for trace: " + v.traceID)
	}

	if v.showEvents {
		return v.renderTaskEvents()
	}

	return v.renderTimeline()
}

// renderInput 渲染输入界面
func (v *TraceView) renderInput() string {
	header := v.styles.Header.Render("Trace Lookup")

	content := "\n  Enter a TraceID to view its event timeline.\n\n"
	content += "  " + v.input.View() + "\n"
	content += "\n  [Enter] Search  [Esc] Back"

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		v.styles.Content.Render(content),
	)
}

// renderTimeline 渲染追踪时间线
func (v *TraceView) renderTimeline() string {
	header := v.styles.Header.Render(fmt.Sprintf("Trace: %s", v.traceID))

	summary := fmt.Sprintf("Entries: %d  |  Tasks: %d  |  Duration: %s",
		len(v.timeline.Entries), len(v.timeline.Tasks),
		formatDuration(v.timeline.Duration))

	var sb strings.Builder
	if len(v.timeline.Tasks) > 0 {
		sb.WriteString("\nTasks:\n")
		for _, t := range v.timeline.Tasks {
			stateStyle := v.styles.Info
			switch t.State {
			case "completed":
				stateStyle = v.styles.Success
			case "failed":
				stateStyle = v.styles.Error
			case "pending":
				stateStyle = v.styles.Warning
			}
			fmt.Fprintf(&sb, "  %s  %s\n", t.ID, stateStyle.Render(t.State))
		}
	}

	sb.WriteString("\nTimeline:\n")
	fmt.Fprintf(&sb, "  %-8s %-14s %-24s %-12s %-10s %s\n",
		"Time", "Stage", "EventType", "TaskID", "State", "Message")
	sb.WriteString("  ")
	sb.WriteString(repeatChar('-', 90))
	sb.WriteString("\n")

	if len(v.timeline.Entries) == 0 {
		sb.WriteString("  (no entries)\n")
	} else {
		for i, entry := range v.timeline.Entries {
			prefix := "  "
			if i == v.selectedIdx {
				prefix = "▶ "
			}

			stateStr := ""
			if entry.OldState != "" || entry.NewState != "" {
				stateStr = fmt.Sprintf("%s→%s", entry.OldState, entry.NewState)
			}

			ts := entry.Timestamp.Format("15:04:05")
			stageStyle := v.stageStyle(entry.Stage)

			fmt.Fprintf(&sb, "%s%-8s %-14s %-24s %-12s %-10s %s\n",
				prefix, ts,
				stageStyle.Render(entry.Stage),
				truncate(entry.EventType, 24),
				truncate(entry.TaskID, 12),
				stateStr,
				truncate(entry.Message, 30),
			)
		}
	}

	hint := "\n[↑/↓] Navigate  [e] Task events  [r] Refresh  [/] Search  [Esc] Back"

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		v.styles.Content.Render(summary+sb.String()),
		v.styles.Footer.Render(hint),
	)
}

// renderTaskEvents 渲染任务事件
func (v *TraceView) renderTaskEvents() string {
	taskID := ""
	if len(v.timeline.Entries) > 0 && v.selectedIdx < len(v.timeline.Entries) {
		taskID = v.timeline.Entries[v.selectedIdx].TaskID
	}

	header := v.styles.Header.Render(fmt.Sprintf("Events for task: %s", taskID))

	var sb strings.Builder
	sb.WriteString("\n")
	fmt.Fprintf(&sb, "  %-8s %-14s %-24s %-10s %s\n",
		"Time", "Stage", "EventType", "State", "Message")
	sb.WriteString("  ")
	sb.WriteString(repeatChar('-', 80))
	sb.WriteString("\n")

	if len(v.taskEvents) == 0 {
		sb.WriteString("  (no events)\n")
	} else {
		for i, entry := range v.taskEvents {
			prefix := "  "
			if i == v.selectedIdx {
				prefix = "▶ "
			}

			stateStr := ""
			if entry.OldState != "" || entry.NewState != "" {
				stateStr = fmt.Sprintf("%s→%s", entry.OldState, entry.NewState)
			}

			ts := entry.Timestamp.Format("15:04:05")
			stageStyle := v.stageStyle(entry.Stage)

			fmt.Fprintf(&sb, "%s%-8s %-14s %-24s %-10s %s\n",
				prefix, ts,
				stageStyle.Render(entry.Stage),
				truncate(entry.EventType, 24),
				stateStr,
				truncate(entry.Message, 30),
			)
		}
	}

	hint := "\n[↑/↓] Navigate  [Esc] Back to timeline"

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		v.styles.Content.Render(sb.String()),
		v.styles.Footer.Render(hint),
	)
}

// stageStyle 根据阶段返回样式
func (v *TraceView) stageStyle(stage string) lipgloss.Style {
	switch stage {
	case "datasource":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#74C0FC"))
	case "policy_match":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FFD93D"))
	case "task_create":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#51CF66"))
	case "task_state":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#B197FC"))
	case "command":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FF922B"))
	case "timeout":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#CCCCCC"))
	}
}

// parseEventLogEntry 解析事件日志条目
func parseEventLogEntry(data map[string]any) EventLogEntry {
	entry := EventLogEntry{
		ID:            getString(data, "id"),
		TraceID:       getString(data, "trace_id"),
		EventID:       getString(data, "event_id"),
		Stage:         getString(data, "stage"),
		EventType:     getString(data, "event_type"),
		Source:        getString(data, "source"),
		TaskID:        getString(data, "task_id"),
		OldState:      getString(data, "old_state"),
		NewState:      getString(data, "new_state"),
		ComponentName: getString(data, "component_name"),
		Message:       getString(data, "message"),
	}

	if ts, ok := data["timestamp"].(string); ok {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			entry.Timestamp = t
		}
	}

	if success, ok := data["success"].(bool); ok {
		entry.Success = &success
	}

	return entry
}

// truncate 截断字符串
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-2] + ".."
}

// repeatChar 重复字符
func repeatChar(ch byte, count int) string {
	buf := make([]byte, count)
	for i := range buf {
		buf[i] = ch
	}
	return string(buf)
}

// formatDuration 格式化持续时间
func formatDuration(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second))
	if d < time.Second {
		return fmt.Sprintf("%.0fms", float64(d.Milliseconds()))
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%.1fm", d.Minutes())
}
