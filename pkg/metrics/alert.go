package metrics

import (
	"time"

	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/log"
)

// AlertConfig 告警配置
type AlertConfig struct {
	TimeoutRateThreshold float64       // 超时率阈值（次/秒）
	ErrorRateThreshold   float64       // 错误率阈值（次/秒）
	WindowSize           time.Duration // 滑动窗口大小
	CheckInterval        time.Duration // 检查间隔
}

// DefaultAlertConfig 默认告警配置
func DefaultAlertConfig() AlertConfig {
	return AlertConfig{
		TimeoutRateThreshold: 10.0,
		ErrorRateThreshold:   20.0,
		WindowSize:           time.Minute,
		CheckInterval:        10 * time.Second,
	}
}

// AlertMetrics 告警装饰器
// 包装 MetricsRecorder，在指标回调中检测阈值并输出告警日志
type AlertMetrics struct {
	inner  common.MetricsRecorder // 被装饰的实际实现
	logger log.Logger
	config AlertConfig

	// 滑动窗口计数器
	timeoutWindow *slidingWindow
	errorWindow   *slidingWindow
	retryWindow   *slidingWindow
}

// NewAlertMetrics 创建告警装饰器
func NewAlertMetrics(inner common.MetricsRecorder, logger log.Logger, cfg AlertConfig) *AlertMetrics {
	return &AlertMetrics{
		inner:         inner,
		logger:        logger,
		config:        cfg,
		timeoutWindow: newSlidingWindow(cfg.WindowSize, 0),
		errorWindow:   newSlidingWindow(cfg.WindowSize, 0),
		retryWindow:   newSlidingWindow(cfg.WindowSize, 0),
	}
}

func (a *AlertMetrics) TaskCreated() {
	a.inner.TaskCreated()
}

func (a *AlertMetrics) TaskStateTransition(from, to string) {
	a.inner.TaskStateTransition(from, to)
}

func (a *AlertMetrics) TaskTimeout() {
	a.inner.TaskTimeout()
	a.timeoutWindow.Add(1)
	if rate := a.timeoutWindow.Rate(); rate > a.config.TimeoutRateThreshold {
		a.logger.Error("ALERT: task timeout rate exceeded threshold",
			log.Any("current_rate", rate),
			log.Any("threshold", a.config.TimeoutRateThreshold),
		)
	}
}

func (a *AlertMetrics) TaskRetry() {
	a.inner.TaskRetry()
	a.retryWindow.Add(1)
}

func (a *AlertMetrics) TaskArchived() {
	a.inner.TaskArchived()
}

func (a *AlertMetrics) TaskDeleted() {
	a.inner.TaskDeleted()
}

func (a *AlertMetrics) TaskError() {
	a.inner.TaskError()
	a.errorWindow.Add(1)
	if rate := a.errorWindow.Rate(); rate > a.config.ErrorRateThreshold {
		a.logger.Error("ALERT: task error rate exceeded threshold",
			log.Any("current_rate", rate),
			log.Any("threshold", a.config.ErrorRateThreshold),
		)
	}
}

// TimeoutRate 返回当前超时速率（次/秒）
func (a *AlertMetrics) TimeoutRate() float64 {
	return a.timeoutWindow.Rate()
}

// ErrorRate 返回当前错误速率（次/秒）
func (a *AlertMetrics) ErrorRate() float64 {
	return a.errorWindow.Rate()
}

// RetryRate 返回当前重试速率（次/秒）
func (a *AlertMetrics) RetryRate() float64 {
	return a.retryWindow.Rate()
}
