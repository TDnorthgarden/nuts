package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// StateMachineView 状态机配置视图
type StateMachineView struct {
	client *HTTPClient
	styles *Styles
	config *StateMachineConfig
	width  int
	height int
}

// StateMachineConfig 状态机配置
type StateMachineConfig struct {
	Name           string                 `json:"name"`
	InitialState   string                 `json:"initial_state"`
	TerminalStates []string               `json:"terminal_states"`
	States         map[string]StateConfig `json:"states"`
	Transitions    []TransitionConfig     `json:"transitions"`
}

// StateConfig 状态配置
type StateConfig struct {
	Description  string `json:"description"`
	StateTimeout string `json:"state_timeout"`
	AutoRetry    bool   `json:"auto_retry"`
}

// TransitionConfig 转换配置
type TransitionConfig struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Allowed bool   `json:"allowed"`
}

// NewStateMachineView 创建状态机视图
func NewStateMachineView(client *HTTPClient, styles *Styles) *StateMachineView {
	return &StateMachineView{
		client: client,
		styles: styles,
	}
}

// Init 初始化
func (v *StateMachineView) Init() tea.Cmd {
	return v.Refresh()
}

// Refresh 刷新状态机配置
func (v *StateMachineView) Refresh() tea.Cmd {
	return func() tea.Msg {
		resp, err := v.client.Get("/api/v1/statemachine/config")
		if err != nil {
			return err
		}

		if !resp.IsSuccess() {
			return fmt.Errorf("api error: %s", resp.Message)
		}

		// 解析数据
		data, ok := resp.Data.(map[string]interface{})
		if !ok {
			return stateMachineConfigUpdatedMsg{config: &StateMachineConfig{}}
		}

		config := &StateMachineConfig{
			Name:           getString(data, "name"),
			InitialState:   getString(data, "initial_state"),
			TerminalStates: []string{},
			States:         make(map[string]StateConfig),
			Transitions:    []TransitionConfig{},
		}

		// 解析 terminal_states
		if ts, ok := data["terminal_states"].([]interface{}); ok {
			for _, t := range ts {
				if s, ok := t.(string); ok {
					config.TerminalStates = append(config.TerminalStates, s)
				}
			}
		}

		// 解析 states
		if states, ok := data["states"].(map[string]interface{}); ok {
			for name, stateData := range states {
				if stateMap, ok := stateData.(map[string]interface{}); ok {
					config.States[name] = StateConfig{
						Description:  getString(stateMap, "description"),
						StateTimeout: getString(stateMap, "state_timeout"),
						AutoRetry:    getBool(stateMap, "auto_retry"),
					}
				}
			}
		}

		// 解析 transitions
		if transitions, ok := data["transitions"].([]interface{}); ok {
			for _, t := range transitions {
				if tMap, ok := t.(map[string]interface{}); ok {
					config.Transitions = append(config.Transitions, TransitionConfig{
						From:    getString(tMap, "from"),
						To:      getString(tMap, "to"),
						Allowed: getBool(tMap, "allowed"),
					})
				}
			}
		}

		return stateMachineConfigUpdatedMsg{config: config}
	}
}

// stateMachineConfigUpdatedMsg 状态机配置更新消息
type stateMachineConfigUpdatedMsg struct {
	config *StateMachineConfig
}

// Update 更新视图
func (v *StateMachineView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case stateMachineConfigUpdatedMsg:
		v.config = msg.config
	case tea.WindowSizeMsg:
		v.width = msg.Width
		v.height = msg.Height
	}
	return v, nil
}

// View 渲染视图
func (v *StateMachineView) View() string {
	if v.width == 0 {
		return "Loading..."
	}

	if v.config == nil {
		return v.styles.Content.Render("No state machine configuration loaded. Press 'r' to refresh.")
	}

	header := v.styles.Header.Render("⚙️ State Machine Configuration")

	content := fmt.Sprintf(`
Name:           %s
Initial State:   %s
Terminal States: %v

States:
─────────────────────────────────────────────────────────────
`, v.config.Name, v.config.InitialState, v.config.TerminalStates)

	if v.config.States != nil {
		for name, state := range v.config.States {
			content += fmt.Sprintf("  %s: %s (state_timeout: %s, auto_retry: %v)\n",
				name, state.Description, state.StateTimeout, state.AutoRetry)
		}
	}

	content += "\nTransitions:\n"
	content += "─────────────────────────────────────────────────────────────\n"
	if v.config.Transitions != nil {
		for _, t := range v.config.Transitions {
			allowedStyle := v.styles.Error
			if t.Allowed {
				allowedStyle = v.styles.Success
			}
			content += fmt.Sprintf("  %s -> %s: %s\n", t.From, t.To, allowedStyle.Render(fmt.Sprintf("%v", t.Allowed)))
		}
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		v.styles.Content.Render(content),
	)
}
