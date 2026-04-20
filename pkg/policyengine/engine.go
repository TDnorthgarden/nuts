package policyengine

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/nuts-project/nuts/pkg/libdslgo"
	"github.com/nuts-project/nuts/pkg/policy"
	"github.com/nuts-project/nuts/pkg/statemachine"
	"github.com/nuts-project/nuts/pkg/task"
)

// Engine implements policy engine using libdslgo
type Engine struct {
	mu                 sync.RWMutex
	dslEngine          *libdslgo.Engine
	policies           map[string]*policy.Policy
	ruleNameToPolicyID map[string]string // Maps rule names from YAML to policy IDs
	notifier           policy.PolicyNotifier
	running            bool
	taskManager        *task.TaskManager
}

// NewEngine creates a new policy engine
func NewEngine() *Engine {
	return &Engine{
		dslEngine:          libdslgo.NewEngine(),
		policies:           make(map[string]*policy.Policy),
		ruleNameToPolicyID: make(map[string]string),
		taskManager:        task.NewTaskManager(nil),
	}
}

// SetNotifier sets the policy notifier
func (e *Engine) SetNotifier(notifier policy.PolicyNotifier) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.notifier = notifier
	// Update task manager notifier
	e.taskManager.SetNotifier(notifier)
}

// Start starts the policy engine
func (e *Engine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("policy engine is already running")
	}

	// Compile DSL rules
	if err := e.dslEngine.Compile(false); err != nil {
		return fmt.Errorf("failed to compile DSL rules: %w", err)
	}

	e.running = true
	log.Println("Policy engine started successfully")
	return nil
}

// Stop stops the policy engine
func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return nil
	}

	e.running = false
	log.Println("Policy engine stopped")
	return nil
}

// Receive implements PolicyReceiver interface
func (e *Engine) Receive(p *policy.Policy) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Validate policy
	if p.ID == "" {
		return fmt.Errorf("policy ID cannot be empty")
	}
	if p.Name == "" {
		return fmt.Errorf("policy name cannot be empty")
	}
	if len(p.Metrics) == 0 {
		return fmt.Errorf("policy must have at least one metric")
	}
	if p.Duration <= 0 {
		return fmt.Errorf("policy duration must be positive")
	}

	// Set timestamps
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now

	// Store policy
	e.policies[p.ID] = p

	log.Printf("Policy received: ID=%s, Name=%s, Metrics=%d, Duration=%ds",
		p.ID, p.Name, len(p.Metrics), p.Duration)

	return nil
}

// Update implements PolicyReceiver interface
func (e *Engine) Update(p *policy.Policy) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if policy exists
	if _, exists := e.policies[p.ID]; !exists {
		return fmt.Errorf("policy %s not found", p.ID)
	}

	// Update timestamp
	p.UpdatedAt = time.Now()

	// Update policy
	e.policies[p.ID] = p

	log.Printf("Policy updated: ID=%s, Name=%s", p.ID, p.Name)
	return nil
}

// Delete implements PolicyReceiver interface
func (e *Engine) Delete(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if policy exists
	if _, exists := e.policies[id]; !exists {
		return fmt.Errorf("policy %s not found", id)
	}

	// Delete policy
	delete(e.policies, id)

	// Clean up rule name to policy ID mapping
	for ruleName, policyID := range e.ruleNameToPolicyID {
		if policyID == id {
			delete(e.ruleNameToPolicyID, ruleName)
		}
	}

	log.Printf("Policy deleted: ID=%s", id)
	return nil
}

// Get implements PolicyReceiver interface
func (e *Engine) Get(id string) (*policy.Policy, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	p, exists := e.policies[id]
	if !exists {
		return nil, fmt.Errorf("policy %s not found", id)
	}

	return p, nil
}

// List implements PolicyReceiver interface
func (e *Engine) List() ([]*policy.Policy, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	policies := make([]*policy.Policy, 0, len(e.policies))
	for _, p := range e.policies {
		policies = append(policies, p)
	}

	return policies, nil
}

// Match implements PolicyMatcher interface
func (e *Engine) Match(event *policy.Event) (*policy.MatchResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	log.Printf("[PolicyEngine] Evaluating event: Type=%s, Pod=%s, Container=%s, Cgroup=%s, PID=%d",
		event.Type, event.PodName, event.Container, event.CgroupID, event.PID)

	// Convert policy.Event to libdslgo.Event (map[string]interface{})
	dslEvent := libdslgo.Event{
		"event.type":     event.Type,
		"event.id":       event.ID,
		"cgroup.id":      event.CgroupID,
		"pod.name":       event.PodName,
		"pod.namespace":  event.Namespace,
		"container.name": event.Container,
		"pid":            event.PID,
		"timestamp":      event.Timestamp,
	}

	// Add metadata to the event
	for k, v := range event.Metadata {
		dslEvent[k] = v
	}

	// Evaluate all policies against event
	matchedRules, err := e.dslEngine.EvaluateAllWithTimeout(dslEvent, 5*time.Second)
	if err != nil {
		log.Printf("[PolicyEngine] Failed to evaluate policies: %v", err)
		return nil, fmt.Errorf("failed to evaluate policies: %w", err)
	}

	// Check if any rule matched
	if len(matchedRules) == 0 {
		log.Printf("[PolicyEngine] No policy matched for event: Type=%s, Pod=%s, Container=%s",
			event.Type, event.PodName, event.Container)
		return &policy.MatchResult{
			Matched: false,
			Reason:  "No policy matched for event",
		}, nil
	}

	// Get first matched rule
	rule := matchedRules[0]

	// Find the policy that contains this rule using rule name to policy ID mapping
	policyID, exists := e.ruleNameToPolicyID[rule.Rule]
	if !exists {
		log.Printf("[PolicyEngine] Matched rule not found in mapping: Rule=%s", rule.Rule)
		return &policy.MatchResult{
			Matched: false,
			Reason:  "Matched rule not found in policy list",
		}, nil
	}

	matchedPolicy, exists := e.policies[policyID]
	if !exists {
		log.Printf("[PolicyEngine] Policy not found for matched rule: Rule=%s, PolicyID=%s", rule.Rule, policyID)
		return &policy.MatchResult{
			Matched: false,
			Reason:  "Policy not found for matched rule",
		}, nil
	}

	// Log the match result
	log.Printf("[PolicyEngine] Policy MATCHED: PolicyID=%s, PolicyName=%s, Cgroup=%s, Duration=%ds, Metrics=%v",
		matchedPolicy.ID, matchedPolicy.Name, event.CgroupID, matchedPolicy.Duration, matchedPolicy.Metrics)

	return &policy.MatchResult{
		PolicyID: matchedPolicy.ID,
		Metrics:  matchedPolicy.Metrics,
		Duration: matchedPolicy.Duration,
		Matched:  true,
		Reason:   fmt.Sprintf("Matched policy: %s", matchedPolicy.Name),
	}, nil
}

// AddRule adds a DSL rule to the engine
// The rule parameter is a YAML string that can contain:
// - rule: the main rule definition
// - macro: reusable conditions
// - list: collections of values
func (e *Engine) AddRule(policyID string, rule string) error {
	log.Printf("[PolicyEngine] Adding DSL rule for policy: PolicyID=%s", policyID)
	log.Printf("[PolicyEngine] Rule YAML content:\n%s", rule)

	// Build mapping from rule names to policy ID
	// We need to capture the rule names that were just added
	// Since we can't access unexported fields directly, we'll use a different approach:
	// We'll parse the YAML ourselves to extract rule names
	e.mu.Lock()

	// Parse YAML to extract rule names
	// The YAML can be either a list of rules or a single rule
	var yamlList []interface{}
	if err := yaml.Unmarshal([]byte(rule), &yamlList); err == nil {
		// Iterate through the list and extract rule names
		for _, item := range yamlList {
			if ruleMap, ok := item.(map[string]interface{}); ok {
				if ruleName, ok := ruleMap["rule"].(string); ok {
					e.ruleNameToPolicyID[ruleName] = policyID
					log.Printf("[PolicyEngine] Mapped rule name '%s' to policy ID '%s'", ruleName, policyID)
				}
			}
		}
	}
	e.mu.Unlock()

	// Parse the YAML rule (can include macros, lists, etc.)
	// Note: ParseFile uses its own lock, so we don't need to hold e.mu here
	if err := e.dslEngine.ParseFile([]byte(rule)); err != nil {
		log.Printf("[PolicyEngine] Failed to parse rule for policy %s: %v", policyID, err)
		return fmt.Errorf("failed to parse rule: %w", err)
	}

	// Compile the rules with force=false to fail if any rule is invalid
	// Note: Compile uses its own lock, so we don't need to hold e.mu here
	if err := e.dslEngine.Compile(false); err != nil {
		log.Printf("[PolicyEngine] Failed to compile rules for policy %s: %v", policyID, err)
		// Clean up rule name to policy ID mapping for failed rules
		e.mu.Lock()
		var ruleNamesToRemove []string
		for ruleName, pid := range e.ruleNameToPolicyID {
			if pid == policyID {
				ruleNamesToRemove = append(ruleNamesToRemove, ruleName)
				delete(e.ruleNameToPolicyID, ruleName)
			}
		}
		e.mu.Unlock()

		// Remove rules from DSL engine
		for _, ruleName := range ruleNamesToRemove {
			// Remove from Rules map
			delete(e.dslEngine.Rules, ruleName)

			// Remove from RulesSlice
			for i, r := range e.dslEngine.RulesSlice {
				if r.Rule == ruleName {
					e.dslEngine.RulesSlice = append(e.dslEngine.RulesSlice[:i], e.dslEngine.RulesSlice[i+1:]...)
					break
				}
			}
		}

		return fmt.Errorf("failed to compile rules: %w", err)
	}

	// Log the parsed rules, macros, and lists
	e.mu.RLock()
	rulesCount := len(e.dslEngine.Rules)
	macrosCount := len(e.dslEngine.Macros)
	listsCount := len(e.dslEngine.Lists)
	e.mu.RUnlock()

	log.Printf("[PolicyEngine] DSL rule successfully added for policy %s: Rules=%d, Macros=%d, Lists=%d",
		policyID, rulesCount, macrosCount, listsCount)
	return nil
}

// RemoveRule removes a DSL rule from the engine
func (e *Engine) RemoveRule(policyID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Collect rule names to remove
	var ruleNamesToRemove []string
	for ruleName, pid := range e.ruleNameToPolicyID {
		if pid == policyID {
			ruleNamesToRemove = append(ruleNamesToRemove, ruleName)
		}
	}

	// Remove rules from DSL engine
	for _, ruleName := range ruleNamesToRemove {
		// Remove from Rules map
		delete(e.dslEngine.Rules, ruleName)

		// Remove from RulesSlice
		for i, rule := range e.dslEngine.RulesSlice {
			if rule.Rule == ruleName {
				e.dslEngine.RulesSlice = append(e.dslEngine.RulesSlice[:i], e.dslEngine.RulesSlice[i+1:]...)
				break
			}
		}

		// Clean up rule name to policy ID mapping
		delete(e.ruleNameToPolicyID, ruleName)
	}

	log.Printf("Rule removed for policy %s: %d rules removed", policyID, len(ruleNamesToRemove))
	return nil
}

// StopTask transitions a task to stopped state
func (e *Engine) StopTask(taskID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	policyTask, exists := e.taskManager.GetTask(taskID)
	if !exists {
		return fmt.Errorf("task %s not found", taskID)
	}

	if err := policyTask.Stop("Task stopped"); err != nil {
		return fmt.Errorf("failed to stop task: %w", err)
	}

	log.Printf("[PolicyEngine] Task %s stopped", taskID)
	return nil
}

// CompleteTask transitions a task to completed state
func (e *Engine) CompleteTask(taskID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	policyTask, exists := e.taskManager.GetTask(taskID)
	if !exists {
		return fmt.Errorf("task %s not found", taskID)
	}

	if err := policyTask.Complete("All phases completed successfully"); err != nil {
		return fmt.Errorf("failed to complete task: %w", err)
	}

	// Notify task completion
	if e.notifier != nil {
		if err := e.notifier.NotifyTaskCompleted(taskID, policyTask.CgroupID, policyTask.PolicyID); err != nil {
			log.Printf("[PolicyEngine] Failed to notify task completion: %v", err)
		}
	}

	log.Printf("[PolicyEngine] Task %s completed successfully", taskID)
	return nil
}

// FailTask transitions a task to failed state
func (e *Engine) FailTask(taskID string, reason string, err error) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	policyTask, exists := e.taskManager.GetTask(taskID)
	if !exists {
		return fmt.Errorf("task %s not found", taskID)
	}

	if err := policyTask.Fail(reason, err); err != nil {
		return fmt.Errorf("failed to mark task as failed: %w", err)
	}

	// Notify task failure
	if e.notifier != nil {
		if err := e.notifier.NotifyTaskFailed(taskID, policyTask.CgroupID, policyTask.PolicyID, err); err != nil {
			log.Printf("[PolicyEngine] Failed to notify task failure: %v", err)
		}
	}

	log.Printf("[PolicyEngine] Task %s failed: %s - %v", taskID, reason, err)
	return nil
}

// GetTask retrieves a task by ID
func (e *Engine) GetTask(taskID string) (*task.Task, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.taskManager.GetTask(taskID)
}

// ListTasks returns all tasks
func (e *Engine) ListTasks() []*task.Task {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.taskManager.ListTasks()
}

// ListTasksByPolicy returns all tasks for a specific policy
func (e *Engine) ListTasksByPolicy(policyID string) []*task.Task {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.taskManager.ListTasksByPolicy(policyID)
}

// ListTasksByCgroup returns all tasks for a specific cgroup
func (e *Engine) ListTasksByCgroup(cgroupID string) []*task.Task {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.taskManager.ListTasksByCgroup(cgroupID)
}

// ListTasksByState returns all tasks in a specific state
func (e *Engine) ListTasksByState(state statemachine.State) []*task.Task {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.taskManager.ListTasksByState(state)
}

// GetTaskManager returns the task manager
func (e *Engine) GetTaskManager() *task.TaskManager {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.taskManager
}

// CleanupOldTasks removes completed or failed tasks older than specified duration
func (e *Engine) CleanupOldTasks(olderThan time.Duration) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	count := e.taskManager.CleanupCompletedTasks(olderThan)
	log.Printf("[PolicyEngine] Cleaned up %d old tasks", count)
	return count
}
