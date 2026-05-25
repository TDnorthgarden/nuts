package log

import (
	"fmt"
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ZapLogger zap日志实现
type ZapLogger struct {
	logger *zap.Logger
	sugar  *zap.SugaredLogger
}

// NewZapLogger creates a ZapLogger with the given log level.
func NewZapLogger(level string) (*ZapLogger, error) {
	return NewZapLoggerWithConfig(ZapConfig{
		Level:    level,
		Encoding: "console",
	})
}

// NewZapLoggerWithConfig 使用配置创建zap日志器
func NewZapLoggerWithConfig(cfg ZapConfig) (*ZapLogger, error) {
	config := zap.NewProductionConfig()

	// 设置日志级别
	var zapLevel zapcore.Level
	switch cfg.Level {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		return nil, fmt.Errorf("invalid log level: %q (valid: debug, info, warn, error)", cfg.Level)
	}
	config.Level = zap.NewAtomicLevelAt(zapLevel)

	// 设置编码格式
	if cfg.Encoding == "console" {
		config.Encoding = "console"
	} else {
		config.Encoding = "json"
	}

	// 配置输出格式
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	// 配置输出路径
	if cfg.OutputPath != "" {
		// 输出到文件
		file, err := os.OpenFile(cfg.OutputPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return nil, err
		}
		config.OutputPaths = []string{cfg.OutputPath}
		config.ErrorOutputPaths = []string{cfg.OutputPath}
		file.Close()
	}

	logger, err := config.Build()
	if err != nil {
		return nil, err
	}

	// 包裹 SamplingCore，降低高频日志输出量
	if cfg.SamplingEnabled {
		initial := cfg.SamplingInitial
		if initial <= 0 {
			initial = 3
		}
		thereafter := cfg.SamplingThereafter
		if thereafter <= 0 {
			thereafter = 1000
		}
		tick := time.Duration(cfg.SamplingTickMillis) * time.Millisecond
		if tick <= 0 {
			tick = time.Second
		}
		logger = logger.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
			return zapcore.NewSamplerWithOptions(core, tick, initial, thereafter)
		}))
	}

	return &ZapLogger{
		logger: logger,
		sugar:  logger.Sugar(),
	}, nil
}

// Debug 调试日志
func (l *ZapLogger) Debug(msg string, fields ...Field) {
	l.sugar.Debugw(msg, l.toArgs(fields...)...)
}

// Info 信息日志
func (l *ZapLogger) Info(msg string, fields ...Field) {
	l.sugar.Infow(msg, l.toArgs(fields...)...)
}

// Warn 警告日志
func (l *ZapLogger) Warn(msg string, fields ...Field) {
	l.sugar.Warnw(msg, l.toArgs(fields...)...)
}

// Error 错误日志
func (l *ZapLogger) Error(msg string, fields ...Field) {
	l.sugar.Errorw(msg, l.toArgs(fields...)...)
}

// Fatal 致命错误日志
func (l *ZapLogger) Fatal(msg string, fields ...Field) {
	l.sugar.Fatalw(msg, l.toArgs(fields...)...)
}

// With 添加上下文字段
func (l *ZapLogger) With(fields ...Field) Logger {
	zapFields := make([]zap.Field, 0, len(fields))
	for _, f := range fields {
		zapFields = append(zapFields, zap.Any(f.Key, f.Value))
	}
	newLogger := l.logger.With(zapFields...)
	return &ZapLogger{
		logger: newLogger,
		sugar:  newLogger.Sugar(),
	}
}

// Sync 刷新日志缓冲区
func (l *ZapLogger) Sync() error {
	return l.logger.Sync()
}

// toArgs 转换字段为zap参数
func (l *ZapLogger) toArgs(fields ...Field) []interface{} {
	args := make([]interface{}, 0, len(fields)*2)
	for _, f := range fields {
		args = append(args, f.Key, f.Value)
	}
	return args
}

// 包级别默认日志器（非 nil，使用 noopLogger 兜底）
var defaultLogger Logger = &noopLogger{}

// SetDefault 设置默认日志器
func SetDefault(logger Logger) {
	defaultLogger = logger
}

// GetDefault 获取默认日志器
func GetDefault() Logger {
	return defaultLogger
}
