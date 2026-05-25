package common

// ErrorCode 错误码定义
type ErrorCode int

const (
	// 成功
	CodeSuccess ErrorCode = 0

	// 系统级错误 (1-999)
	CodeInternalError      ErrorCode = 1  // 内部错误
	CodeInvalidParam       ErrorCode = 2  // 参数错误
	CodeUnauthorized       ErrorCode = 3  // 未授权
	CodeForbidden          ErrorCode = 4  // 禁止访问
	CodeNotFound           ErrorCode = 5  // 资源不存在
	CodeAlreadyExists      ErrorCode = 6  // 资源已存在
	CodeServiceUnavailable ErrorCode = 7  // 服务不可用
	CodeTimeout            ErrorCode = 8  // 超时
	CodeRateLimited        ErrorCode = 9  // 限流

	// 数据源错误 (1000-1999)
	CodeDataSourceNotFound    ErrorCode = 1000 // 数据源不存在
	CodeDataSourceConnectFail ErrorCode = 1001 // 数据源连接失败
	CodeDataSourceAuthFail    ErrorCode = 1002 // 数据源认证失败
	CodeDataSourceTimeout     ErrorCode = 1003 // 数据源超时

	// 策略引擎错误 (2000-2999)
	CodePolicyNotFound    ErrorCode = 2000 // 策略不存在
	CodePolicyInvalidDSL  ErrorCode = 2001 // DSL语法错误
	CodePolicyCompileFail ErrorCode = 2002 // 策略编译失败

	// 任务调度错误 (3000-3999)
	CodeTaskNotFound         ErrorCode = 3000 // 任务不存在
	CodeTaskInvalidState     ErrorCode = 3001 // 任务状态无效
	CodeTaskCreateFail       ErrorCode = 3002 // 任务创建失败
	CodeTaskResourceExhaust  ErrorCode = 3003 // 任务资源耗尽
	CodeTaskStateMismatch    ErrorCode = 3004 // 状态不匹配
	CodeTaskTransitionDenied ErrorCode = 3005 // 转换不允许
	CodeTaskVersionConflict  ErrorCode = 3006 // 版本冲突

	// EventBus错误 (4000-4999)
	CodeEventBusConnectFail ErrorCode = 4000 // EventBus连接失败
	CodeEventPublishFail    ErrorCode = 4001 // 事件发布失败
	CodeEventBusTimeout     ErrorCode = 4002 // EventBus超时
	CodeEventSubscribeFail  ErrorCode = 4003 // 订阅失败

	// 配置错误 (5000-5999)
	CodeConfigInvalid   ErrorCode = 5000 // 配置无效
	CodeConfigMissing   ErrorCode = 5001 // 配置缺失
	CodeConfigParseFail ErrorCode = 5002 // 配置解析失败
)

// ErrorCodeToHTTPStatus 错误码转HTTP状态码
func ErrorCodeToHTTPStatus(code ErrorCode) int {
	switch {
	case code == CodeSuccess:
		return 200
	case code == CodeInvalidParam:
		return 400
	case code == CodeUnauthorized:
		return 401
	case code == CodeForbidden:
		return 403
	case code == CodeNotFound:
		return 404
	case code == CodeAlreadyExists:
		return 409
	case code == CodeRateLimited:
		return 429
	case code >= CodeInternalError && code < 1000:
		return 500
	default:
		return 500
	}
}

// String 返回错误码描述
func (c ErrorCode) String() string {
	switch c {
	case CodeSuccess:
		return "success"
	case CodeInternalError:
		return "internal error"
	case CodeInvalidParam:
		return "invalid parameter"
	case CodeNotFound:
		return "resource not found"
	case CodeDataSourceNotFound:
		return "datasource not found"
	case CodeTaskNotFound:
		return "task not found"
	case CodeTaskInvalidState:
		return "invalid task state"
	case CodeTaskStateMismatch:
		return "task state mismatch"
	case CodeTaskTransitionDenied:
		return "task transition denied"
	case CodeTaskVersionConflict:
		return "task version conflict"
	case CodePolicyNotFound:
		return "policy not found"
	case CodeEventBusConnectFail:
		return "eventbus connect failed"
	case CodeEventPublishFail:
		return "event publish failed"
	case CodeEventBusTimeout:
		return "eventbus timeout"
	case CodeEventSubscribeFail:
		return "event subscribe failed"
	default:
		return "unknown error"
	}
}
