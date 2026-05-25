package common

import (
	"fmt"
	"time"
)

// APIResponse 统一API响应结构
type APIResponse struct {
	// Code 业务状态码（非HTTP状态码）
	Code int `json:"code"`

	// Message 人类可读的消息
	Message string `json:"message"`

	// Data 响应数据（成功时）
	Data interface{} `json:"data,omitempty"`

	// RequestID 请求追踪ID
	RequestID string `json:"request_id"`

	// Timestamp 响应时间戳
	Timestamp time.Time `json:"timestamp"`
}

// Success 构造成功响应
func Success(data interface{}) *APIResponse {
	return &APIResponse{
		Code:      0,
		Message:   "success",
		Data:      data,
		RequestID: GenerateUUID(),
		Timestamp: time.Now(),
	}
}

// Error 构造错误响应
func Error(code int, message string) *APIResponse {
	return &APIResponse{
		Code:      code,
		Message:   message,
		RequestID: GenerateUUID(),
		Timestamp: time.Now(),
	}
}

// ErrorWithCode 使用ErrorCode构造错误响应
func ErrorWithCode(code ErrorCode, message string) *APIResponse {
	return Error(int(code), message)
}

// Errorf 构造带格式的错误响应
func Errorf(code int, format string, args ...interface{}) *APIResponse {
	return Error(code, fmt.Sprintf(format, args...))
}
