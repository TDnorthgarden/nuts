package log

import (
	"testing"
)

func TestNewZapLogger(t *testing.T) {
	logger, err := NewZapLogger("debug")
	if err != nil {
		t.Fatalf("NewZapLogger() failed: %v", err)
	}
	if logger == nil {
		t.Error("NewZapLogger() should return non-nil logger")
	}
}

func TestNewZapLoggerWithConfig(t *testing.T) {
	config := ZapConfig{
		Level:     "info",
		Encoding:  "json",
	}
	logger, err := NewZapLoggerWithConfig(config)
	if err != nil {
		t.Fatalf("NewZapLoggerWithConfig() failed: %v", err)
	}
	if logger == nil {
		t.Error("NewZapLoggerWithConfig() should return non-nil logger")
	}
}

func TestNewZapLogger_InvalidLevel(t *testing.T) {
	_, err := NewZapLogger("invalid")
	if err == nil {
		t.Error("NewZapLogger() should fail with invalid level")
	}
}
