package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-yaml"
	"github.com/nuts-project/nuts/pkg/policy"
	"github.com/nuts-project/nuts/pkg/policyengine"
	"github.com/nuts-project/nuts/pkg/statemachine"
)

// strictDecoder wraps json.Decoder to reject unknown fields
type strictDecoder struct {
	*json.Decoder
}

func (d *strictDecoder) Decode(v interface{}) error {
	// First decode into a map to check for unknown fields
	var raw map[string]interface{}
	if err := d.Decoder.Decode(&raw); err != nil {
		return err
	}

	// Marshal and unmarshal to validate against the struct
	data, err := json.Marshal(raw)
	if err != nil {
		return err
	}

	// Use json.Unmarshal with DisallowUnknownFields
	dec := json.NewDecoder(io.NopCloser(bytes.NewReader(data)))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// Handler handles HTTP API requests
type Handler struct {
	policyEngine *policyengine.Engine
}

// NewHandler creates a new API handler
func NewHandler(policyEngine *policyengine.Engine) *Handler {
	return &Handler{
		policyEngine: policyEngine,
	}
}

// RegisterRoutes registers all API routes
func (h *Handler) RegisterRoutes(router *gin.Engine) {
	api := router.Group("/api/v1")
	{
		policies := api.Group("/policies")
		{
			policies.POST("", h.CreatePolicy)
			policies.PUT("/:id", h.UpdatePolicy)
			policies.DELETE("/:id", h.DeletePolicy)
			policies.GET("/:id", h.GetPolicy)
			policies.GET("", h.ListPolicies)
		}

		tasks := api.Group("/tasks")
		{
			tasks.GET("", h.ListTasks)
			tasks.GET("/:id", h.GetTask)
			tasks.GET("/state", h.ListTasksByState)
			tasks.GET("/policy/:id", h.ListTasksByPolicy)
			tasks.GET("/cgroup/:id", h.ListTasksByCgroup)
		}
	}
}

// CreatePolicyRequest represents the request to create a policy
type CreatePolicyRequest struct {
	ID       string              `json:"id" binding:"required"`
	Name     string              `json:"name" binding:"required"`
	Metrics  map[string][]string `json:"metrics" binding:"required"` // key: category, value: list of script names
	Duration int64               `json:"duration" binding:"required,min=1"`
	Rule     string              `json:"rule" binding:"required"` // DSL rule in YAML format (can include macros, lists, etc.)
}

// UpdatePolicyRequest represents the request to update a policy
type UpdatePolicyRequest struct {
	Name     string              `json:"name"`
	Metrics  map[string][]string `json:"metrics"` // key: category, value: list of script names
	Duration int64               `json:"duration"`
	Rule     string              `json:"rule"`
}

// CreatePolicy creates a new policy
func (h *Handler) CreatePolicy(c *gin.Context) {
	// Use strict decoder to reject unknown fields
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()

	var req CreatePolicyRequest
	if err := decoder.Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if policy already exists
	if _, err := h.policyEngine.Get(req.ID); err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Policy already exists"})
		return
	}

	// Create policy
	p := &policy.Policy{
		ID:       req.ID,
		Name:     req.Name,
		Metrics:  req.Metrics,
		Duration: req.Duration,
		Rule:     req.Rule, // Save the original user rule
	}

	// Validate policy
	if err := p.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Add rule to DSL engine (parse YAML format)
	if err := h.policyEngine.AddRule(req.ID, req.Rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Receive policy
	if err := h.policyEngine.Receive(p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, p)
}

// UpdatePolicy updates an existing policy
func (h *Handler) UpdatePolicy(c *gin.Context) {
	id := c.Param("id")

	// Use strict decoder to reject unknown fields
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()

	var req UpdatePolicyRequest
	if err := decoder.Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get existing policy
	p, err := h.policyEngine.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Policy not found"})
		return
	}

	// Update fields if provided
	if req.Name != "" {
		p.Name = req.Name
	}
	if req.Metrics != nil {
		p.Metrics = req.Metrics
	}
	if req.Duration > 0 {
		p.Duration = req.Duration
	}

	// Validate policy after updates
	if err := p.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Update rule if provided
	if req.Rule != "" {
		// Parse new rule YAML to extract rule names
		var newRuleList []map[string]interface{}
		if err := yaml.Unmarshal([]byte(req.Rule), &newRuleList); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid rule YAML format"})
			return
		}

		// Parse existing rule YAML to extract rule names
		var existingRuleList []map[string]interface{}
		if p.Rule != "" {
			if err := yaml.Unmarshal([]byte(p.Rule), &existingRuleList); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid existing rule YAML format"})
				return
			}
		}

		// Create a map of existing rule names for quick lookup
		existingRuleNames := make(map[string]bool)
		for _, rule := range existingRuleList {
			if ruleName, ok := rule["rule"].(string); ok {
				existingRuleNames[ruleName] = true
			}
		}

		// Merge rules: replace if same rule name, append if new
		mergedRules := make([]map[string]interface{}, 0)
		// First add all existing rules
		mergedRules = append(mergedRules, existingRuleList...)
		// Then add new rules, replacing if rule name exists
		for _, newRule := range newRuleList {
			if ruleName, ok := newRule["rule"].(string); ok {
				if existingRuleNames[ruleName] {
					// Replace existing rule
					for i, existingRule := range mergedRules {
						if existingRuleName, ok := existingRule["rule"].(string); ok && existingRuleName == ruleName {
							mergedRules[i] = newRule
							break
						}
					}
				} else {
					// Append new rule
					mergedRules = append(mergedRules, newRule)
				}
			}
		}

		// Convert merged rules back to YAML
		mergedRuleYAML, err := yaml.Marshal(mergedRules)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to merge rules"})
			return
		}

		// Remove old rules from DSL engine
		h.policyEngine.RemoveRule(id)
		// Add merged rules to DSL engine
		if err := h.policyEngine.AddRule(id, string(mergedRuleYAML)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// Update policy rule field
		p.Rule = string(mergedRuleYAML)
	}

	// Update policy
	if err := h.policyEngine.Update(p); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, p)
}

// DeletePolicy deletes a policy
func (h *Handler) DeletePolicy(c *gin.Context) {
	id := c.Param("id")

	// Remove rule from DSL engine
	h.policyEngine.RemoveRule(id)

	// Delete policy
	if err := h.policyEngine.Delete(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Policy deleted successfully"})
}

// GetPolicy retrieves a policy by ID
func (h *Handler) GetPolicy(c *gin.Context) {
	id := c.Param("id")

	p, err := h.policyEngine.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, p)
}

// ListPolicies retrieves all policies
func (h *Handler) ListPolicies(c *gin.Context) {
	policies, err := h.policyEngine.List()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"policies": policies})
}

// ListTasks retrieves all tasks
func (h *Handler) ListTasks(c *gin.Context) {
	tasks := h.policyEngine.ListTasks()

	// Convert tasks to response format
	taskResponses := make([]TaskResponse, 0, len(tasks))
	for _, t := range tasks {
		taskResponses = append(taskResponses, TaskResponse{
			ID:        t.ID,
			PolicyID:  t.PolicyID,
			CgroupID:  t.CgroupID,
			State:     string(t.GetState()),
			StartTime: t.StartTime.Format("2006-01-02T15:04:05Z07:00"),
			EndTime:   t.EndTime.Format("2006-01-02T15:04:05Z07:00"),
			Duration:  t.GetDuration().String(),
			Error:     getErrorString(t.Error),
		})
	}

	c.JSON(http.StatusOK, gin.H{"tasks": taskResponses})
}

// GetTask retrieves a task by ID
func (h *Handler) GetTask(c *gin.Context) {
	id := c.Param("id")

	task, exists := h.policyEngine.GetTask(id)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}

	response := TaskResponse{
		ID:        task.ID,
		PolicyID:  task.PolicyID,
		CgroupID:  task.CgroupID,
		State:     string(task.GetState()),
		StartTime: task.StartTime.Format("2006-01-02T15:04:05Z07:00"),
		EndTime:   task.EndTime.Format("2006-01-02T15:04:05Z07:00"),
		Duration:  task.GetDuration().String(),
		Error:     getErrorString(task.Error),
	}

	c.JSON(http.StatusOK, response)
}

// ListTasksByState retrieves tasks by state
func (h *Handler) ListTasksByState(c *gin.Context) {
	stateStr := c.Query("state")
	if stateStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "state parameter is required"})
		return
	}

	state := statemachine.State(stateStr)
	tasks := h.policyEngine.ListTasksByState(state)

	// Convert tasks to response format
	taskResponses := make([]TaskResponse, 0, len(tasks))
	for _, t := range tasks {
		taskResponses = append(taskResponses, TaskResponse{
			ID:        t.ID,
			PolicyID:  t.PolicyID,
			CgroupID:  t.CgroupID,
			State:     string(t.GetState()),
			StartTime: t.StartTime.Format("2006-01-02T15:04:05Z07:00"),
			EndTime:   t.EndTime.Format("2006-01-02T15:04:05Z07:00"),
			Duration:  t.GetDuration().String(),
			Error:     getErrorString(t.Error),
		})
	}

	c.JSON(http.StatusOK, gin.H{"tasks": taskResponses})
}

// ListTasksByPolicy retrieves tasks by policy ID
func (h *Handler) ListTasksByPolicy(c *gin.Context) {
	policyID := c.Param("id")

	tasks := h.policyEngine.ListTasksByPolicy(policyID)

	// Convert tasks to response format
	taskResponses := make([]TaskResponse, 0, len(tasks))
	for _, t := range tasks {
		taskResponses = append(taskResponses, TaskResponse{
			ID:        t.ID,
			PolicyID:  t.PolicyID,
			CgroupID:  t.CgroupID,
			State:     string(t.GetState()),
			StartTime: t.StartTime.Format("2006-01-02T15:04:05Z07:00"),
			EndTime:   t.EndTime.Format("2006-01-02T15:04:05Z07:00"),
			Duration:  t.GetDuration().String(),
			Error:     getErrorString(t.Error),
		})
	}

	c.JSON(http.StatusOK, gin.H{"tasks": taskResponses})
}

// ListTasksByCgroup retrieves tasks by cgroup ID
func (h *Handler) ListTasksByCgroup(c *gin.Context) {
	cgroupID := c.Param("id")

	tasks := h.policyEngine.ListTasksByCgroup(cgroupID)

	// Convert tasks to response format
	taskResponses := make([]TaskResponse, 0, len(tasks))
	for _, t := range tasks {
		taskResponses = append(taskResponses, TaskResponse{
			ID:        t.ID,
			PolicyID:  t.PolicyID,
			CgroupID:  t.CgroupID,
			State:     string(t.GetState()),
			StartTime: t.StartTime.Format("2006-01-02T15:04:05Z07:00"),
			EndTime:   t.EndTime.Format("2006-01-02T15:04:05Z07:00"),
			Duration:  t.GetDuration().String(),
			Error:     getErrorString(t.Error),
		})
	}

	c.JSON(http.StatusOK, gin.H{"tasks": taskResponses})
}

// TaskResponse represents a task in API response
type TaskResponse struct {
	ID        string `json:"id"`
	PolicyID  string `json:"policy_id"`
	CgroupID  string `json:"cgroup_id"`
	State     string `json:"state"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Duration  string `json:"duration"`
	Error     string `json:"error,omitempty"`
}

// getErrorString converts error to string
func getErrorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
