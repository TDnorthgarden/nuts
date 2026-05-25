package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DebugView 运行时调试信息视图
type DebugView struct {
	client *HTTPClient
	styles *Styles
	debug  *DebugVars
	width  int
	height int
}

// DebugVars 运行时调试信息
type DebugVars struct {
	Goroutines int                    `json:"goroutines"`
	NumCPU     int                    `json:"num_cpu"`
	GoVersion  string                 `json:"go_version"`
	Started    bool                   `json:"started"`
	Stopped    bool                   `json:"stopped"`
	Memory     map[string]interface{} `json:"memory"`
	TaskQueue  map[string]interface{} `json:"task_queue"`
	EventBus   map[string]interface{} `json:"eventbus"`
}

// NewDebugView 创建调试信息视图
func NewDebugView(client *HTTPClient, styles *Styles) *DebugView {
	return &DebugView{
		client: client,
		styles: styles,
	}
}

// Init 初始化
func (v *DebugView) Init() tea.Cmd {
	return v.Refresh()
}

// Refresh 刷新调试信息
func (v *DebugView) Refresh() tea.Cmd {
	return func() tea.Msg {
		resp, err := v.client.Get("/api/v1/debug/vars")
		if err != nil {
			return debugVarsUpdatedMsg{debug: nil, err: err}
		}

		if !resp.IsSuccess() {
			return debugVarsUpdatedMsg{debug: nil, err: fmt.Errorf("api error: %s", resp.Message)}
		}

		data, ok := resp.Data.(map[string]interface{})
		if !ok {
			return debugVarsUpdatedMsg{debug: nil}
		}

		debug := &DebugVars{
			Goroutines: int(getFloat(data, "goroutines")),
			NumCPU:     int(getFloat(data, "num_cpu")),
			GoVersion:  getString(data, "go_version"),
			Started:    getBool(data, "started"),
			Stopped:    getBool(data, "stopped"),
		}

		if mem, ok := data["memory"].(map[string]interface{}); ok {
			debug.Memory = mem
		}
		if queue, ok := data["task_queue"].(map[string]interface{}); ok {
			debug.TaskQueue = queue
		}
		if eb, ok := data["eventbus"].(map[string]interface{}); ok {
			debug.EventBus = eb
		}

		return debugVarsUpdatedMsg{debug: debug}
	}
}

// debugVarsUpdatedMsg 调试信息更新消息
type debugVarsUpdatedMsg struct {
	debug *DebugVars
	err   error
}

// Update 更新视图
func (v *DebugView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case debugVarsUpdatedMsg:
		v.debug = msg.debug
		if msg.err != nil {
			return v, nil
		}
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	}
	return v, nil
}

// View 渲染视图
func (v *DebugView) View() string {
	if v.width == 0 {
		return "Loading..."
	}

	if v.debug == nil {
		return v.styles.Content.Render("No debug information loaded. Press 'r' to refresh.")
	}

	section := func(title string) string {
		return v.styles.Header.Render(title)
	}

	var content string

	// Runtime
	content += section("Runtime")
	content += fmt.Sprintf("\nGoroutines : %d", v.debug.Goroutines)
	content += fmt.Sprintf("\nCPUs       : %d", v.debug.NumCPU)
	content += fmt.Sprintf("\nGo Version : %s", v.debug.GoVersion)

	statusText := v.styles.Success.Render("running")
	if v.debug.Stopped {
		statusText = v.styles.Error.Render("stopped")
	} else if !v.debug.Started {
		statusText = v.styles.Warning.Render("starting")
	}
	content += fmt.Sprintf("\nStatus     : %s", statusText)

	// Memory
	if v.debug.Memory != nil {
		content += "\n\n" + section("Memory")
		for _, key := range []string{"alloc_bytes", "total_alloc_bytes", "sys_bytes", "heap_alloc_bytes", "heap_sys_bytes"} {
			if val, ok := v.debug.Memory[key]; ok {
				label := key
				switch key {
				case "alloc_bytes":
					label = "Alloc"
				case "total_alloc_bytes":
					label = "Total Alloc"
				case "sys_bytes":
					label = "Sys"
				case "heap_alloc_bytes":
					label = "Heap Alloc"
				case "heap_sys_bytes":
					label = "Heap Sys"
				}
				content += fmt.Sprintf("\n%-12s: %s", label, formatBytes(val))
			}
		}
		if gc, ok := v.debug.Memory["gc_cycles"]; ok {
			content += fmt.Sprintf("\n%-12s: %v", "GC Cycles", gc)
		}
	}

	// Task Queue
	if v.debug.TaskQueue != nil {
		content += "\n\n" + section("Task Queue")
		for _, state := range []string{"pending", "processing", "completed", "failed", "cancelled", "timeout"} {
			if count, ok := v.debug.TaskQueue[state]; ok {
				content += fmt.Sprintf("\n%-12s: %v", state, count)
			}
		}
		content += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")).Render("─────────────")
		if total, ok := v.debug.TaskQueue["active_total"]; ok {
			content += fmt.Sprintf("\n%-12s: %v", "active", total)
		}
		if archived, ok := v.debug.TaskQueue["archived_total"]; ok {
			content += fmt.Sprintf("\n%-12s: %v", "archived", archived)
		}
	}

	// EventBus
	if v.debug.EventBus != nil {
		content += "\n\n" + section("EventBus")
		if has, ok := v.debug.EventBus["has_subscribers"]; ok {
			label := v.styles.Error.Render("no subscribers")
			if b, ok := has.(bool); ok && b {
				label = v.styles.Success.Render("has subscribers")
			}
			content += fmt.Sprintf("\nSubscribers: %s", label)
		}
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		v.styles.Header.Render("Debug"),
		v.styles.Content.Render(content),
	)
}
