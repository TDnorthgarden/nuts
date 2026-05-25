package log

// noopLogger 静默丢弃所有日志输出
type noopLogger struct{}

func (l *noopLogger) Debug(msg string, fields ...Field) {}
func (l *noopLogger) Info(msg string, fields ...Field)  {}
func (l *noopLogger) Warn(msg string, fields ...Field)  {}
func (l *noopLogger) Error(msg string, fields ...Field) {}
func (l *noopLogger) Fatal(msg string, fields ...Field) {}
func (l *noopLogger) With(fields ...Field) Logger       { return l }
func (l *noopLogger) Sync() error                       { return nil }
