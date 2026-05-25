package datasource

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/sig-cloudnative/nuts/pkg/common"
)

// HTTPHandler 数据源HTTP处理器
type HTTPHandler struct {
	manager *DataSourceManager
}

// NewHTTPHandler 创建HTTP处理器
func NewHTTPHandler(manager *DataSourceManager) *HTTPHandler {
	return &HTTPHandler{
		manager: manager,
	}
}

// RegisterRoutes 注册路由到 http.ServeMux
func (h *HTTPHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/v1/datasources", h.HandleDataSources)
	mux.HandleFunc("/api/v1/datasources/", h.HandleDataSourceDetail)
}

// HandleDataSources 处理数据源列表请求
func (h *HTTPHandler) HandleDataSources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		// 获取已注册的数据源列表
		dsList := h.manager.List()
		resp := common.Success(dsList)
		json.NewEncoder(w).Encode(resp)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		resp := common.Error(405, "method not allowed")
		json.NewEncoder(w).Encode(resp)
	}
}

// HandleDataSourceDetail 处理数据源详情请求和控制请求
func (h *HTTPHandler) HandleDataSourceDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Path[len("/api/v1/datasources/"):]
	parts := strings.Split(path, "/")
	id := parts[0]

	// 如果有第二个路径段（如 disable/switch），交给控制处理
	if len(parts) >= 2 && (parts[1] == "disable" || parts[1] == "switch") {
		h.HandleDataSourceControl(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		wrapper, err := h.manager.GetWrapper(id)
		if err != nil {
			resp := common.Error(404, err.Error())
			json.NewEncoder(w).Encode(resp)
			return
		}
		// 返回实际状态：id 和是否 active
		resp := common.Success(map[string]interface{}{
			"id":     id,
			"active": wrapper.Active,
		})
		json.NewEncoder(w).Encode(resp)

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		resp := common.Error(405, "method not allowed")
		json.NewEncoder(w).Encode(resp)
	}
}

// HandleDataSourceControl 处理数据源控制请求（disable/switch）
func (h *HTTPHandler) HandleDataSourceControl(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// 解析路径：/api/v1/datasources/{id}/disable|switch
	path := r.URL.Path[len("/api/v1/datasources/"):]
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		resp := common.Error(400, "invalid path format")
		json.NewEncoder(w).Encode(resp)
		return
	}

	id := parts[0]
	action := parts[1]

	switch action {
	case "disable":
		if err := h.manager.Stop(id); err != nil {
			resp := common.ErrorWithCode(common.CodeInternalError, fmt.Sprintf("disable datasource: %v", err))
			w.WriteHeader(common.ErrorCodeToHTTPStatus(common.CodeInternalError))
			json.NewEncoder(w).Encode(resp)
			return
		}
		resp := common.Success(map[string]string{"id": id, "action": "disabled"})
		json.NewEncoder(w).Encode(resp)

	case "switch":
		// 检查目标数据源是否存在
		_, err := h.manager.Get(id)
		if err != nil {
			resp := common.Error(404, fmt.Sprintf("datasource %s not found", id))
			json.NewEncoder(w).Encode(resp)
			return
		}

		// 停止所有数据源
		h.manager.StopAll()

		// 启动目标数据源
		if err := h.manager.StartByName(id); err != nil {
			resp := common.ErrorWithCode(common.CodeInternalError, fmt.Sprintf("switch to datasource: %v", err))
			w.WriteHeader(common.ErrorCodeToHTTPStatus(common.CodeInternalError))
			json.NewEncoder(w).Encode(resp)
			return
		}

		resp := common.Success(map[string]string{"id": id, "action": "switched"})
		json.NewEncoder(w).Encode(resp)

	default:
		resp := common.Error(400, fmt.Sprintf("unknown action: %s", action))
		json.NewEncoder(w).Encode(resp)
	}
}
