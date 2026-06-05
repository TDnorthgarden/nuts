package task

import (
	"github.com/sig-cloudnative/nuts/pkg/config"
)

// StateMachineConfig 状态机配置
type StateMachineConfig struct {
	Name           string                 `toml:"name"`
	InitialState   string                 `toml:"initial_state"`
	TerminalStates []string               `toml:"terminal_states"`
	States         map[string]StateConfig `toml:"states"`
	Transitions    []TransitionConfig     `toml:"transitions"`
	PayloadBuilder string                 `toml:"payload_builder,omitempty"`
	MaxProcessTime string                 `toml:"max_processing_time,omitempty"`
}


// StateConfig 状态配置
type StateConfig struct {
	Description  string `toml:"description,omitempty"`
	AutoRetry    bool   `toml:"auto_retry,omitempty"`
	MaxRetries   int    `toml:"max_retries,omitempty"`
	RetryToState string `toml:"retry_to_state,omitempty"`
}

// TransitionConfig 状态转换配置
type TransitionConfig struct {
	From    string `toml:"from"`
	To      string `toml:"to"`
	Allowed bool   `toml:"allowed,omitempty"` // 是否允许此转换
}

// IsTerminalState 检查是否为终态
func (cfg *StateMachineConfig) IsTerminalState(state string) bool {
	for _, terminal := range cfg.TerminalStates {
		if terminal == state {
			return true
		}
	}
	return false
}

// IsTransitionAllowed 检查状态转换是否允许
func (cfg *StateMachineConfig) IsTransitionAllowed(from, to string) bool {
	for _, t := range cfg.Transitions {
		if t.From == from && t.To == to && t.Allowed {
			return true
		}
	}
	return false
}

// LoadStateMachineConfig 从配置管理器加载状态机配置
func LoadStateMachineConfig(cfg config.ConfigManager) (*StateMachineConfig, error) {
	// 从配置中获取状态机配置
		smMap := cfg.GetMap("statemachine")
	if smMap == nil {
		// 使用默认配置
		return &StateMachineConfig{
			Name:         "task_lifecycle",
			InitialState: "pending",
			TerminalStates: []string{"completed"},
			States: map[string]StateConfig{
				"pending":   {},
				"completed": {},
			},
			Transitions: []TransitionConfig{
				{From: "pending", To: "completed", Allowed: true},
			},
		}, nil
	}

	smConfig := &StateMachineConfig{
		Name:           getStringFromMap(smMap, "name", "task_lifecycle"),
		InitialState:   getStringFromMap(smMap, "initial_state", "pending"),
		PayloadBuilder: getStringFromMap(smMap, "payload_builder", "default"),
		TerminalStates: getStringSliceFromMap(smMap, "terminal_states"),
		States:         make(map[string]StateConfig),
		Transitions:    make([]TransitionConfig, 0),
	}

	// 解析 states
	if statesMap, ok := smMap["states"].(map[string]interface{}); ok {
		for name, cfg := range statesMap {
			if stateCfg, ok := cfg.(map[string]interface{}); ok {
			smConfig.States[name] = StateConfig{
				Description:  getStringFromMap(stateCfg, "description", ""),
				AutoRetry:    getBoolFromMap(stateCfg, "auto_retry", false),
				MaxRetries:   getIntFromMap(stateCfg, "max_retries", 0),
				RetryToState: getStringFromMap(stateCfg, "retry_to_state", ""),
			}
			}
		}
	}

	// 解析 transitions
	if transitionsSlice, ok := smMap["transitions"].([]interface{}); ok {
		for _, t := range transitionsSlice {
			if tMap, ok := t.(map[string]interface{}); ok {
				transition := TransitionConfig{
					From:    getStringFromMap(tMap, "from", ""),
					To:      getStringFromMap(tMap, "to", ""),
					Allowed: getBoolFromMap(tMap, "allowed", true),
				}
				if transition.From != "" && transition.To != "" {
					smConfig.Transitions = append(smConfig.Transitions, transition)
				}
			}
		}
	}

	return smConfig, nil
}

// getStringSliceFromMap 从 map 中获取字符串切片
func getStringSliceFromMap(m map[string]interface{}, key string) []string {
	if val, ok := m[key]; ok {
		if slice, ok := val.([]interface{}); ok {
			result := make([]string, 0, len(slice))
			for _, item := range slice {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
	}
	return nil
}

// getBoolFromMap 从 map 中获取布尔值
func getBoolFromMap(m map[string]interface{}, key string, defaultVal bool) bool {
	if val, ok := m[key]; ok {
		if b, ok := val.(bool); ok {
			return b
		}
	}
	return defaultVal
}

// getIntFromMap 从 map 中获取整数值
func getIntFromMap(m map[string]interface{}, key string, defaultVal int) int {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case int64:
			return int(v)
		case float64:
			return int(v)
		}
	}
	return defaultVal
}

// getStringFromMap 从 map 中获取字符串值
func getStringFromMap(m map[string]interface{}, key, defaultVal string) string {
	if val, ok := m[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return defaultVal
}
