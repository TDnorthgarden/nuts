package policy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sig-cloudnative/nuts/pkg/common"
)

// HTTPHandler 策略引擎HTTP处理器
type HTTPHandler struct {
	manager PolicyManager
	engine  PolicyEngine
}

// NewHTTPHandler 创建HTTP处理器
func NewHTTPHandler(manager PolicyManager, engine PolicyEngine) *HTTPHandler {
	return &HTTPHandler{
		manager: manager,
		engine:  engine,
	}
}

// RegisterRoutes 注册路由
func (h *HTTPHandler) RegisterRoutes(router *gin.Engine) {
	api := router.Group("/api/v1/policies")
	{
		// 策略CRUD
		api.POST("", h.CreatePolicy)
		api.GET("", h.ListPolicies)
		api.GET("/:id", h.GetPolicy)
		api.PUT("/:id", h.UpdatePolicy)
		api.DELETE("/:id", h.DeletePolicy)

		// 策略启用/禁用
		api.POST("/:id/enable", h.EnablePolicy)
		api.POST("/:id/disable", h.DisablePolicy)

		// 策略评估
		api.POST("/evaluate", h.EvaluatePolicy)

		// 策略验证（校验 DSL 语法，不保存）
		api.POST("/validate", h.ValidatePolicy)

		// 统计信息
		api.GET("/stats", h.GetStats)
	}
}

// RegisterRoutesToMux 注册路由到 http.ServeMux
func (h *HTTPHandler) RegisterRoutesToMux(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/policies", h.handlePolicies)
	mux.HandleFunc("/api/v1/policies/", h.handlePolicyDetail)
	mux.HandleFunc("/api/v1/policies/validate", h.handleValidatePolicy)
}

// handlePolicies 处理策略列表和创建请求
func (h *HTTPHandler) handlePolicies(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		policies, err := h.manager.ListPolicies()
		if err != nil {
			w.WriteHeader(common.ErrorCodeToHTTPStatus(common.CodeInternalError))
			json.NewEncoder(w).Encode(common.ErrorWithCode(common.CodeInternalError, err.Error()))
			return
		}
		json.NewEncoder(w).Encode(common.Success(policies))

	case http.MethodPost:
		var policy Policy
		if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(common.Error(400, err.Error()))
			return
		}

		if err := h.manager.AddPolicy(&policy); err != nil {
			w.WriteHeader(common.ErrorCodeToHTTPStatus(common.CodeInternalError))
			json.NewEncoder(w).Encode(common.ErrorWithCode(common.CodeInternalError, err.Error()))
			return
		}

		json.NewEncoder(w).Encode(common.Success(policy))

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(common.Error(405, "method not allowed"))
	}
}

// handlePolicyDetail 处理策略详情和控制请求
func (h *HTTPHandler) handlePolicyDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Path[len("/api/v1/policies/"):]
	parts := strings.Split(path, "/")
	id := parts[0]

	// 如果有第二个路径段（如 enable/disable），交给控制处理
	if len(parts) >= 2 && (parts[1] == "enable" || parts[1] == "disable") {
		h.handlePolicyControl(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		policy, err := h.manager.GetPolicy(id)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(common.Error(404, err.Error()))
			return
		}
		json.NewEncoder(w).Encode(common.Success(policy))

	case http.MethodPut:
		var p Policy
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(common.Error(400, err.Error()))
			return
		}
		p.ID = id

		if err := h.manager.UpdatePolicy(&p); err != nil {
			w.WriteHeader(common.ErrorCodeToHTTPStatus(common.CodeInternalError))
			json.NewEncoder(w).Encode(common.ErrorWithCode(common.CodeInternalError, err.Error()))
			return
		}

		json.NewEncoder(w).Encode(common.Success(p))

	case http.MethodDelete:
		if err := h.manager.RemovePolicy(id); err != nil {
			w.WriteHeader(common.ErrorCodeToHTTPStatus(common.CodeInternalError))
			json.NewEncoder(w).Encode(common.ErrorWithCode(common.CodeInternalError, err.Error()))
			return
		}

		json.NewEncoder(w).Encode(common.Success(nil))

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(common.Error(405, "method not allowed"))
	}
}

// handlePolicyControl 处理策略控制请求（enable/disable）
func (h *HTTPHandler) handlePolicyControl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	path := r.URL.Path[len("/api/v1/policies/"):]
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(common.Error(400, "invalid path format"))
		return
	}

	id := parts[0]
	action := parts[1]

	switch action {
	case "enable":
		if err := h.manager.EnablePolicy(id); err != nil {
			w.WriteHeader(common.ErrorCodeToHTTPStatus(common.CodeInternalError))
			json.NewEncoder(w).Encode(common.ErrorWithCode(common.CodeInternalError, err.Error()))
			return
		}
		json.NewEncoder(w).Encode(common.Success(map[string]string{"id": id, "action": "enabled"}))

	case "disable":
		if err := h.manager.DisablePolicy(id); err != nil {
			w.WriteHeader(common.ErrorCodeToHTTPStatus(common.CodeInternalError))
			json.NewEncoder(w).Encode(common.ErrorWithCode(common.CodeInternalError, err.Error()))
			return
		}
		json.NewEncoder(w).Encode(common.Success(map[string]string{"id": id, "action": "disabled"}))

	default:
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(common.Error(400, fmt.Sprintf("unknown action: %s", action)))
	}
}

// handleValidatePolicy handles policy validation requests.
func (h *HTTPHandler) handleValidatePolicy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(common.Error(405, "method not allowed"))
		return
	}

	var policy Policy
	if err := json.NewDecoder(r.Body).Decode(&policy); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(common.Error(400, err.Error()))
		return
	}

	// 添加结构体验证
	if err := policy.Validate(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(common.ErrorWithCode(common.CodeInvalidParam, err.Error()))
		return
	}

	if err := h.manager.ValidatePolicyTemp(&policy); err != nil {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(common.ErrorWithCode(common.CodePolicyInvalidDSL, err.Error()))
		return
	}

	json.NewEncoder(w).Encode(common.Success(map[string]string{
		"message": "DSL syntax is valid",
	}))
}

// CreatePolicy 创建策略
// @Summary 创建策略
// @Tags policies
// @Accept json
// @Produce json
// @Param policy body Policy true "策略信息"
// @Success 200 {object} common.APIResponse
// @Router /api/v1/policies [post]
func (h *HTTPHandler) CreatePolicy(c *gin.Context) {
	var policy Policy
	if err := c.ShouldBindJSON(&policy); err != nil {
		c.JSON(http.StatusBadRequest, common.ErrorWithCode(common.CodeInvalidParam, err.Error()))
		return
	}

	// 添加验证
	if err := policy.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, common.ErrorWithCode(common.CodeInvalidParam, err.Error()))
		return
	}

	if err := h.manager.AddPolicy(&policy); err != nil {
		c.JSON(http.StatusInternalServerError, common.ErrorWithCode(common.CodeInternalError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, common.Success(policy))
}

// ListPolicies 列出策略
// @Summary 列出策略
// @Tags policies
// @Produce json
// @Success 200 {object} common.APIResponse
// @Router /api/v1/policies [get]
func (h *HTTPHandler) ListPolicies(c *gin.Context) {
	policies, err := h.manager.ListPolicies()
	if err != nil {
		c.JSON(http.StatusInternalServerError, common.ErrorWithCode(common.CodeInternalError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, common.Success(policies))
}

// GetPolicy 获取策略
// @Summary 获取策略
// @Tags policies
// @Produce json
// @Param id path string true "策略ID"
// @Success 200 {object} common.APIResponse
// @Router /api/v1/policies/{id} [get]
func (h *HTTPHandler) GetPolicy(c *gin.Context) {
	id := c.Param("id")
	policy, err := h.manager.GetPolicy(id)
	if err != nil {
		c.JSON(http.StatusNotFound, common.ErrorWithCode(common.CodeNotFound, err.Error()))
		return
	}

	c.JSON(http.StatusOK, common.Success(policy))
}

// UpdatePolicy 更新策略
// @Summary 更新策略
// @Tags policies
// @Accept json
// @Produce json
// @Param id path string true "策略ID"
// @Param policy body Policy true "策略信息"
// @Success 200 {object} common.APIResponse
// @Router /api/v1/policies/{id} [put]
func (h *HTTPHandler) UpdatePolicy(c *gin.Context) {
	id := c.Param("id")

	var policy Policy
	if err := c.ShouldBindJSON(&policy); err != nil {
		c.JSON(http.StatusBadRequest, common.ErrorWithCode(common.CodeInvalidParam, err.Error()))
		return
	}

	// 添加验证
	if err := policy.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, common.ErrorWithCode(common.CodeInvalidParam, err.Error()))
		return
	}

	policy.ID = id

	if err := h.manager.UpdatePolicy(&policy); err != nil {
		c.JSON(http.StatusInternalServerError, common.ErrorWithCode(common.CodeInternalError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, common.Success(policy))
}

// DeletePolicy 删除策略
// @Summary 删除策略
// @Tags policies
// @Produce json
// @Param id path string true "策略ID"
// @Success 200 {object} common.APIResponse
// @Router /api/v1/policies/{id} [delete]
func (h *HTTPHandler) DeletePolicy(c *gin.Context) {
	id := c.Param("id")

	if err := h.manager.RemovePolicy(id); err != nil {
		c.JSON(http.StatusInternalServerError, common.ErrorWithCode(common.CodeInternalError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, common.Success(nil))
}

// EnablePolicy 启用策略
// @Summary 启用策略
// @Tags policies
// @Produce json
// @Param id path string true "策略ID"
// @Success 200 {object} common.APIResponse
// @Router /api/v1/policies/{id}/enable [post]
func (h *HTTPHandler) EnablePolicy(c *gin.Context) {
	id := c.Param("id")

	if err := h.manager.EnablePolicy(id); err != nil {
		c.JSON(http.StatusInternalServerError, common.ErrorWithCode(common.CodeInternalError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, common.Success(nil))
}

// DisablePolicy 禁用策略
// @Summary 禁用策略
// @Tags policies
// @Produce json
// @Param id path string true "策略ID"
// @Success 200 {object} common.APIResponse
// @Router /api/v1/policies/{id}/disable [post]
func (h *HTTPHandler) DisablePolicy(c *gin.Context) {
	id := c.Param("id")

	if err := h.manager.DisablePolicy(id); err != nil {
		c.JSON(http.StatusInternalServerError, common.ErrorWithCode(common.CodeInternalError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, common.Success(nil))
}

// EvaluatePolicyRequest 评估请求
type EvaluatePolicyRequest struct {
	PolicyID string        `json:"policy_id"`
	Event    *common.Event `json:"event"`
}

// EvaluatePolicyResponse 评估响应
type EvaluatePolicyResponse struct {
	Matched bool   `json:"matched"`
	Error   string `json:"error,omitempty"`
}

// ValidatePolicy 校验策略 DSL 语法（不保存）
// @Summary 校验策略 DSL 语法（不保存到服务端）
// @Tags policies
// @Accept json
// @Produce json
// @Param policy body Policy true "策略配置"
// @Success 200 {object} common.APIResponse
// @Router /api/v1/policies/validate [post]
func (h *HTTPHandler) ValidatePolicy(c *gin.Context) {
	var policy Policy
	if err := c.ShouldBindJSON(&policy); err != nil {
		c.JSON(http.StatusBadRequest, common.ErrorWithCode(common.CodeInvalidParam, err.Error()))
		return
	}

	// 仅校验 DSL 语法，不保存策略
	if err := h.manager.ValidatePolicyTemp(&policy); err != nil {
		c.JSON(http.StatusOK, common.ErrorWithCode(common.CodePolicyInvalidDSL, err.Error()))
		return
	}

	c.JSON(http.StatusOK, common.Success(map[string]string{
		"message": "DSL syntax is valid",
	}))
}

// EvaluatePolicy 评估策略
// @Summary 评估策略
// @Tags policies
// @Accept json
// @Produce json
// @Param request body EvaluatePolicyRequest true "评估请求"
// @Success 200 {object} common.APIResponse
// @Router /api/v1/policies/evaluate [post]
func (h *HTTPHandler) EvaluatePolicy(c *gin.Context) {
	var req EvaluatePolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, common.ErrorWithCode(common.CodeInvalidParam, err.Error()))
		return
	}

	matched, err := h.engine.Evaluate(req.PolicyID, req.Event)
	if err != nil {
		c.JSON(http.StatusOK, common.Success(EvaluatePolicyResponse{
			Matched: false,
			Error:   err.Error(),
		}))
		return
	}

	c.JSON(http.StatusOK, common.Success(EvaluatePolicyResponse{
		Matched: matched,
	}))
}

// GetStats 获取统计信息
// @Summary 获取统计信息
// @Tags policies
// @Produce json
// @Success 200 {object} common.APIResponse
// @Router /api/v1/policies/stats [get]
func (h *HTTPHandler) GetStats(c *gin.Context) {
	managerStats := h.manager.GetStats()
	engineStats := h.engine.GetStats()

	stats := map[string]interface{}{
		"manager": managerStats,
		"engine":  engineStats,
	}

	c.JSON(http.StatusOK, common.Success(stats))
}

// SetupPolicyAPI 设置策略API
func SetupPolicyAPI(router *gin.Engine, manager PolicyManager, engine PolicyEngine) {
	handler := NewHTTPHandler(manager, engine)
	handler.RegisterRoutes(router)
}
