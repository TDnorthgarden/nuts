package common

import (
	"errors"
	"fmt"
)

// AppError 结构化应用错误
// 内部错误携带 ErrorCode，便于日志分类统计和指标聚合
type AppError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%d] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error { return e.Cause }

// NewAppError 创建结构化错误
func NewAppError(code ErrorCode, msg string) *AppError {
	return &AppError{Code: code, Message: msg}
}

// WrapError 包装底层错误为结构化错误
func WrapError(code ErrorCode, cause error, msg string) *AppError {
	return &AppError{Code: code, Message: msg, Cause: cause}
}

// IsAppError 从 error 中提取 AppError
func IsAppError(err error) (*AppError, bool) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

// GetErrorCode 从 error 中提取 ErrorCode，非 AppError 返回 CodeInternalError
func GetErrorCode(err error) ErrorCode {
	if appErr, ok := IsAppError(err); ok {
		return appErr.Code
	}
	return CodeInternalError
}
