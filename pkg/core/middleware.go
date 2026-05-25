package core

import (
	"net/http"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/log"
)

// responseWriter 包装 http.ResponseWriter，捕获状态码
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// loggingMiddleware HTTP 请求日志中间件
// 记录 method、path、status、latency、remote
func loggingMiddleware(logger log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, statusCode: 200}

			next.ServeHTTP(rw, r)

			latency := time.Since(start)
			fields := []log.Field{
				log.String("method", r.Method),
				log.String("path", r.URL.Path),
				log.Int("status", rw.statusCode),
				log.String("latency", latency.String()),
				log.String("remote", r.RemoteAddr),
			}

			switch {
			case rw.statusCode >= 500:
				logger.Error("HTTP request", fields...)
			case rw.statusCode >= 400:
				logger.Warn("HTTP request", fields...)
			default:
				logger.Info("HTTP request", fields...)
			}
		})
	}
}
