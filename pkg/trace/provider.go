package trace

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Config 追踪配置
type Config struct {
	Enabled     bool    `toml:"enabled"`
	Endpoint    string  `toml:"endpoint"`
	ServiceName string  `toml:"service_name"`
	SampleRate  float64 `toml:"sample_rate"`
}

// InitTracer 初始化 OpenTelemetry TracerProvider
func InitTracer(ctx context.Context, cfg Config) (*sdktrace.TracerProvider, error) {
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("create OTLP exporter: %w", err)
	}

	serviceName := cfg.ServiceName
	if serviceName == "" {
		serviceName = "nuts"
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		)),
		sdktrace.WithSampler(sdktrace.ParentBased(
			sdktrace.TraceIDRatioBased(cfg.SampleRate),
		)),
	)
	otel.SetTracerProvider(tp)
	return tp, nil
}

// GetTracer 获取命名 Tracer
func GetTracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// SpanAttrs 创建 span 属性的便捷函数
func SpanAttrs(attrs ...attribute.KeyValue) []attribute.KeyValue {
	return attrs
}
