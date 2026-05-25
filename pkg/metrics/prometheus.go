package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// PrometheusMetrics 实现 common.MetricsRecorder 接口的 Prometheus 适配器
type PrometheusMetrics struct {
	taskCreated  *prometheus.CounterVec
	stateTrans   *prometheus.CounterVec
	taskTimeout  prometheus.Counter
	taskRetry    prometheus.Counter
	taskArchived prometheus.Counter
	taskDeleted  prometheus.Counter
	taskError    prometheus.Counter
}

// NewPrometheusMetrics 创建 Prometheus 指标实例
// namespace 为指标前缀，如 "nuts"
func NewPrometheusMetrics(namespace string) *PrometheusMetrics {
	reg := promauto.With(prometheus.DefaultRegisterer)
	return &PrometheusMetrics{
		taskCreated: reg.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "task_created_total",
			Help:      "Total number of tasks created",
		}, []string{}),
		stateTrans: reg.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "task_state_transition_total",
			Help:      "Total number of task state transitions",
		}, []string{"from", "to"}),
		taskTimeout: reg.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "task_timeout_total",
			Help:      "Total number of task timeouts",
		}),
		taskRetry: reg.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "task_retry_total",
			Help:      "Total number of task retries",
		}),
		taskArchived: reg.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "task_archived_total",
			Help:      "Total number of tasks archived",
		}),
		taskDeleted: reg.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "task_deleted_total",
			Help:      "Total number of archived tasks deleted by cleaner",
		}),
		taskError: reg.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "task_error_total",
			Help:      "Total number of task errors",
		}),
	}
}

func (p *PrometheusMetrics) TaskCreated() {
	p.taskCreated.WithLabelValues().Inc()
}

func (p *PrometheusMetrics) TaskStateTransition(from, to string) {
	p.stateTrans.WithLabelValues(from, to).Inc()
}

func (p *PrometheusMetrics) TaskTimeout()  { p.taskTimeout.Inc() }
func (p *PrometheusMetrics) TaskRetry()    { p.taskRetry.Inc() }
func (p *PrometheusMetrics) TaskArchived() { p.taskArchived.Inc() }
func (p *PrometheusMetrics) TaskDeleted()  { p.taskDeleted.Inc() }
func (p *PrometheusMetrics) TaskError()    { p.taskError.Inc() }
