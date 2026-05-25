package log

// ZapConfig Zap日志配置
type ZapConfig struct {
	Level      string
	Encoding   string
	OutputPath string // 输出路径，为空时输出到 stdout

	// Sampling 采样配置
	SamplingEnabled    bool  // 是否启用采样
	SamplingInitial    int   // 前 N 条全量输出
	SamplingThereafter int   // 之后每 N 条采样 1 条
	SamplingTickMillis int64 // 采样计数器重置周期（毫秒）
}

// Logger 日志接口
type Logger interface {
	// Debug 调试日志
	Debug(msg string, fields ...Field)

	// Info 信息日志
	Info(msg string, fields ...Field)

	// Warn 警告日志
	Warn(msg string, fields ...Field)

	// Error 错误日志
	Error(msg string, fields ...Field)

	// Fatal 致命错误日志
	Fatal(msg string, fields ...Field)

	// With 添加上下文字段
	With(fields ...Field) Logger

	// Sync 刷新日志缓冲区
	Sync() error
}

// Field 日志字段
type Field struct {
	Key   string
	Value interface{}
}

// String 创建字符串字段
func String(key, val string) Field {
	return Field{Key: key, Value: val}
}

// Int 创建整数字段
func Int(key string, val int) Field {
	return Field{Key: key, Value: val}
}

// Error 创建错误字段
func Error(err error) Field {
	return Field{Key: "error", Value: err}
}

// Any 创建任意类型字段
func Any(key string, val interface{}) Field {
	return Field{Key: key, Value: val}
}
