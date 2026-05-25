package task

import "fmt"

// ValidateStateMachineConfig 校验状态机配置的合法性和完备性
// 包含通用的图校验 + auto_retry 循环检测
func ValidateStateMachineConfig(cfg *StateMachineConfig) error {
	return cfg.Validate()
}

type visitState int

const (
	unvisited visitState = iota
	visiting
	visited
)

// Validate 校验状态机配置的合法性和完备性
//
// 检查项：
//  1. 初始状态和终态都在 states 中定义
//  2. 所有 transition 的 from/to 都在 states 中定义
//  3. 从初始状态可达所有状态（无孤立状态）
//  4. 每个非终态都能到达至少一个终态（无死路）
//  5. 不存在不含终态的循环（无无限循环）
//  6. auto_retry 循环检测：从 auto_retry=true 的状态出发，沿 retry_to_state 路径
//     不会形成不含终态的无限循环
func (cfg *StateMachineConfig) Validate() error {
	if cfg.InitialState == "" {
		return fmt.Errorf("initial_state is required")
	}
	if _, ok := cfg.States[cfg.InitialState]; !ok {
		return fmt.Errorf("initial_state %q not defined in states", cfg.InitialState)
	}

	if len(cfg.TerminalStates) == 0 {
		return fmt.Errorf("at least one terminal_state is required")
	}
	for _, s := range cfg.TerminalStates {
		if _, ok := cfg.States[s]; !ok {
			return fmt.Errorf("terminal_state %q not defined in states", s)
		}
	}

	graph := map[string][]string{}
	terminalSet := map[string]bool{}
	for _, s := range cfg.TerminalStates {
		terminalSet[s] = true
	}
	for _, t := range cfg.Transitions {
		if !t.Allowed {
			continue
		}
		if _, ok := cfg.States[t.From]; !ok {
			return fmt.Errorf("transition from: state %q not defined in states", t.From)
		}
		if _, ok := cfg.States[t.To]; !ok {
			return fmt.Errorf("transition to: state %q not defined in states", t.To)
		}
		graph[t.From] = append(graph[t.From], t.To)
	}

	// 将 auto_retry 状态的隐式 retry_to_state 边加入图（跳过自环）
	for name, stateCfg := range cfg.States {
		if stateCfg.AutoRetry {
			target := stateCfg.RetryToState
			if target == "" {
				target = cfg.InitialState
			}
			if _, ok := cfg.States[target]; !ok {
				return fmt.Errorf("auto_retry state %q: retry_to_state %q not defined in states", name, target)
			}
			if target != name {
				graph[name] = append(graph[name], target)
			}
		}
	}

	reachable := reachableStates(cfg.InitialState, graph)
	for name := range cfg.States {
		if !reachable[name] && name != cfg.InitialState {
			return fmt.Errorf("state %q is unreachable from initial state %q", name, cfg.InitialState)
		}
	}

	for name := range cfg.States {
		if terminalSet[name] {
			continue
		}
		canReach, path := canReachTerminal(name, graph, terminalSet)
		if !canReach {
			return fmt.Errorf("non-terminal state %q cannot reach any terminal state (path: %v)", name, path)
		}
	}

	// 有 StateTimeout 且 AutoRetry=false 的状态必须存在直达终态的转换
	// 因为 handleTaskArchive 只尝试直接转换，不走多跳路径
	for name, stateCfg := range cfg.States {
		if terminalSet[name] {
			continue
		}
		if stateCfg.AutoRetry {
			continue
		}
		if !hasConfiguredStateTimeout(stateCfg.StateTimeout) {
			continue
		}
		hasDirectTerminal := false
		for _, target := range cfg.TerminalStates {
			if cfg.IsTransitionAllowed(name, target) {
				hasDirectTerminal = true
				break
			}
		}
		if !hasDirectTerminal {
			return fmt.Errorf(
				"state %q has state_timeout=%q and auto_retry=false, "+
					"but no direct transition to any terminal state %v; "+
					"add a transition like {from: %q, to: <terminal>, allowed: true}",
				name, stateCfg.StateTimeout, cfg.TerminalStates, name,
			)
		}
	}

	cycles := findCycles(cfg.InitialState, graph)
	for _, cycle := range cycles {
		allNonTerminal := true
		for _, s := range cycle {
			if terminalSet[s] {
				allNonTerminal = false
				break
			}
		}
		if allNonTerminal {
			return fmt.Errorf("infinite loop detected (no terminal state in cycle): %v", cycle)
		}
	}

	return nil
}

func reachableStates(start string, graph map[string][]string) map[string]bool {
	reachable := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, next := range graph[cur] {
			if !reachable[next] {
				reachable[next] = true
				queue = append(queue, next)
			}
		}
	}
	return reachable
}

func canReachTerminal(start string, graph map[string][]string, terminalSet map[string]bool) (bool, []string) {
	visited := map[string]bool{}
	var dfs func(s string, path []string) (bool, []string)
	dfs = func(s string, path []string) (bool, []string) {
		if visited[s] {
			return false, path
		}
		if terminalSet[s] {
			return true, path
		}
		visited[s] = true
		for _, next := range graph[s] {
			found, p := dfs(next, append(path, next))
			if found {
				return true, p
			}
		}
		return false, path
	}
	return dfs(start, []string{start})
}

func findCycles(start string, graph map[string][]string) [][]string {
	var cycles [][]string
	stateMap := map[string]visitState{}
	pathStack := []string{}
	var dfs func(s string)
	dfs = func(s string) {
		stateMap[s] = visiting
		pathStack = append(pathStack, s)
		for _, next := range graph[s] {
			switch stateMap[next] {
			case unvisited:
				dfs(next)
			case visiting:
				cycle := []string{}
				for i := len(pathStack) - 1; i >= 0; i-- {
					cycle = append([]string{pathStack[i]}, cycle...)
					if pathStack[i] == next {
						break
					}
				}
				cycle = normalizeCycle(cycle)
				cycles = append(cycles, cycle)
			}
		}
		pathStack = pathStack[:len(pathStack)-1]
		stateMap[s] = visited
	}
	dfs(start)
	return uniqueCycles(cycles)
}

func normalizeCycle(cycle []string) []string {
	if len(cycle) == 0 {
		return cycle
	}
	minIdx := 0
	for i := 1; i < len(cycle); i++ {
		if cycle[i] < cycle[minIdx] {
			minIdx = i
		}
	}
	result := make([]string, len(cycle))
	for i := 0; i < len(cycle); i++ {
		result[i] = cycle[(minIdx+i)%len(cycle)]
	}
	return result
}

// hasConfiguredStateTimeout 检查状态是否显式配置了超时时间
// 空字符串或 "0s" 表示未配置超时
func hasConfiguredStateTimeout(raw string) bool {
	return raw != "" && raw != "0s"
}

func uniqueCycles(cycles [][]string) [][]string {
	seen := map[string]bool{}
	var result [][]string
	for _, c := range cycles {
		key := fmt.Sprintf("%v", c)
		if !seen[key] {
			seen[key] = true
			result = append(result, c)
		}
	}
	return result
}
