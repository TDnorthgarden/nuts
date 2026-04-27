//! Prometheus 指标导出模块
//!
//! 提供 NRI 系统的可观测性指标：
//! - 映射表大小和命中率
//! - 事件处理速率和延迟
//! - 归属查询性能
//! - 持久化状态

use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;

/// 指标收集器
#[derive(Debug)]
pub struct NriMetrics {
    /// 事件计数器
    events_total: AtomicU64,
    events_by_type: dashmap::DashMap<String, AtomicU64>,

    /// 处理延迟统计（微秒）
    event_processing_duration_us: AtomicU64,
    event_processing_count: AtomicU64,

    /// 归属查询统计
    attribution_queries_total: AtomicU64,
    attribution_cache_hits: AtomicU64,
    attribution_cache_misses: AtomicU64,

    /// 查询延迟（微秒）
    attribution_duration_us: AtomicU64,
    attribution_query_count: AtomicU64,

    /// 映射表统计
    mapping_table_pods: AtomicU64,
    mapping_table_containers: AtomicU64,
    mapping_table_cgroups: AtomicU64,
    mapping_table_pids: AtomicU64,

    /// 持久化统计
    persistence_snapshots_total: AtomicU64,
    persistence_snapshot_duration_ms: AtomicU64,
    persistence_restores_total: AtomicU64,

    /// 版本控制统计
    version_checks_total: AtomicU64,
    version_rejected_stale: AtomicU64,
    version_accepted: AtomicU64,

    /// 批量处理统计
    batch_flushes_total: AtomicU64,
    batch_events_processed: AtomicU64,
    batch_queue_depth: AtomicU64,
}

impl Default for NriMetrics {
    fn default() -> Self {
        Self::new()
    }
}

impl NriMetrics {
    /// 创建新的指标收集器
    pub fn new() -> Self {
        Self {
            events_total: AtomicU64::new(0),
            events_by_type: dashmap::DashMap::new(),
            event_processing_duration_us: AtomicU64::new(0),
            event_processing_count: AtomicU64::new(0),
            attribution_queries_total: AtomicU64::new(0),
            attribution_cache_hits: AtomicU64::new(0),
            attribution_cache_misses: AtomicU64::new(0),
            attribution_duration_us: AtomicU64::new(0),
            attribution_query_count: AtomicU64::new(0),
            mapping_table_pods: AtomicU64::new(0),
            mapping_table_containers: AtomicU64::new(0),
            mapping_table_cgroups: AtomicU64::new(0),
            mapping_table_pids: AtomicU64::new(0),
            persistence_snapshots_total: AtomicU64::new(0),
            persistence_snapshot_duration_ms: AtomicU64::new(0),
            persistence_restores_total: AtomicU64::new(0),
            version_checks_total: AtomicU64::new(0),
            version_rejected_stale: AtomicU64::new(0),
            version_accepted: AtomicU64::new(0),
            batch_flushes_total: AtomicU64::new(0),
            batch_events_processed: AtomicU64::new(0),
            batch_queue_depth: AtomicU64::new(0),
        }
    }

    /// 记录事件（按类型）
    pub fn record_event(&self, event_type: &str, duration_us: u64) {
        self.events_total.fetch_add(1, Ordering::Relaxed);

        // 按类型统计
        let counter = self
            .events_by_type
            .entry(event_type.to_string())
            .or_insert_with(|| AtomicU64::new(0));
        counter.fetch_add(1, Ordering::Relaxed);

        // 处理延迟
        self.event_processing_duration_us
            .fetch_add(duration_us, Ordering::Relaxed);
        self.event_processing_count.fetch_add(1, Ordering::Relaxed);
    }

    /// 记录归属查询
    pub fn record_attribution_query(&self, hit: bool, duration_us: u64) {
        self.attribution_queries_total.fetch_add(1, Ordering::Relaxed);

        if hit {
            self.attribution_cache_hits.fetch_add(1, Ordering::Relaxed);
        } else {
            self.attribution_cache_misses.fetch_add(1, Ordering::Relaxed);
        }

        self.attribution_duration_us
            .fetch_add(duration_us, Ordering::Relaxed);
        self.attribution_query_count.fetch_add(1, Ordering::Relaxed);
    }

    /// 更新映射表大小
    pub fn update_mapping_table_size(
        &self,
        pods: usize,
        containers: usize,
        cgroups: usize,
        pids: usize,
    ) {
        self.mapping_table_pods.store(pods as u64, Ordering::Relaxed);
        self.mapping_table_containers
            .store(containers as u64, Ordering::Relaxed);
        self.mapping_table_cgroups.store(cgroups as u64, Ordering::Relaxed);
        self.mapping_table_pids.store(pids as u64, Ordering::Relaxed);
    }

    /// 记录持久化快照
    pub fn record_persistence_snapshot(&self, duration_ms: u64) {
        self.persistence_snapshots_total.fetch_add(1, Ordering::Relaxed);
        self.persistence_snapshot_duration_ms
            .fetch_add(duration_ms, Ordering::Relaxed);
    }

    /// 记录持久化恢复
    pub fn record_persistence_restore(&self) {
        self.persistence_restores_total.fetch_add(1, Ordering::Relaxed);
    }

    /// 记录版本检查
    pub fn record_version_check(&self, accepted: bool) {
        self.version_checks_total.fetch_add(1, Ordering::Relaxed);

        if accepted {
            self.version_accepted.fetch_add(1, Ordering::Relaxed);
        } else {
            self.version_rejected_stale.fetch_add(1, Ordering::Relaxed);
        }
    }

    /// 记录批量处理
    pub fn record_batch_flush(&self, events_count: usize) {
        self.batch_flushes_total.fetch_add(1, Ordering::Relaxed);
        self.batch_events_processed
            .fetch_add(events_count as u64, Ordering::Relaxed);
    }

    /// 更新队列深度
    pub fn update_batch_queue_depth(&self, depth: usize) {
        self.batch_queue_depth.store(depth as u64, Ordering::Relaxed);
    }

    /// 获取事件处理平均延迟（微秒）
    pub fn avg_event_processing_us(&self) -> f64 {
        let total = self.event_processing_duration_us.load(Ordering::Relaxed);
        let count = self.event_processing_count.load(Ordering::Relaxed);

        if count == 0 {
            0.0
        } else {
            total as f64 / count as f64
        }
    }

    /// 获取归属查询平均延迟（微秒）
    pub fn avg_attribution_query_us(&self) -> f64 {
        let total = self.attribution_duration_us.load(Ordering::Relaxed);
        let count = self.attribution_query_count.load(Ordering::Relaxed);

        if count == 0 {
            0.0
        } else {
            total as f64 / count as f64
        }
    }

    /// 获取缓存命中率
    pub fn cache_hit_rate(&self) -> f64 {
        let hits = self.attribution_cache_hits.load(Ordering::Relaxed);
        let misses = self.attribution_cache_misses.load(Ordering::Relaxed);
        let total = hits + misses;

        if total == 0 {
            0.0
        } else {
            hits as f64 / total as f64
        }
    }

    /// 导出为 Prometheus 格式文本
    pub fn export_prometheus(&self) -> String {
        let mut output = String::with_capacity(4096);

        // 帮助信息
        output.push_str("# HELP nri_events_total Total number of NRI events processed\n");
        output.push_str("# TYPE nri_events_total counter\n");
        output.push_str(&format!(
            "nri_events_total {}\n",
            self.events_total.load(Ordering::Relaxed)
        ));

        // 按类型的事件计数
        for entry in self.events_by_type.iter() {
            output.push_str(&format!(
                "nri_events_total{{type=\"{}\"}} {}\n",
                entry.key(),
                entry.value().load(Ordering::Relaxed)
            ));
        }

        // 处理延迟
        output.push_str("\n# HELP nri_event_processing_duration_microseconds Average event processing latency\n");
        output.push_str("# TYPE nri_event_processing_duration_microseconds gauge\n");
        output.push_str(&format!(
            "nri_event_processing_duration_microseconds {:.2}\n",
            self.avg_event_processing_us()
        ));

        // 归属查询统计
        output.push_str("\n# HELP nri_attribution_queries_total Total attribution queries\n");
        output.push_str("# TYPE nri_attribution_queries_total counter\n");
        output.push_str(&format!(
            "nri_attribution_queries_total {}\n",
            self.attribution_queries_total.load(Ordering::Relaxed)
        ));

        output.push_str(&format!(
            "nri_attribution_queries_total{{result=\"hit\"}} {}\n",
            self.attribution_cache_hits.load(Ordering::Relaxed)
        ));
        output.push_str(&format!(
            "nri_attribution_queries_total{{result=\"miss\"}} {}\n",
            self.attribution_cache_misses.load(Ordering::Relaxed)
        ));

        // 缓存命中率
        output.push_str("\n# HELP nri_attribution_cache_hit_rate Attribution cache hit rate\n");
        output.push_str("# TYPE nri_attribution_cache_hit_rate gauge\n");
        output.push_str(&format!(
            "nri_attribution_cache_hit_rate {:.4}\n",
            self.cache_hit_rate()
        ));

        // 查询延迟
        output.push_str("\n# HELP nri_attribution_query_duration_microseconds Average attribution query latency\n");
        output.push_str("# TYPE nri_attribution_query_duration_microseconds gauge\n");
        output.push_str(&format!(
            "nri_attribution_query_duration_microseconds {:.2}\n",
            self.avg_attribution_query_us()
        ));

        // 映射表大小
        output.push_str("\n# HELP nri_mapping_table_size Current size of mapping tables\n");
        output.push_str("# TYPE nri_mapping_table_size gauge\n");
        output.push_str(&format!(
            "nri_mapping_table_size{{type=\"pods\"}} {}\n",
            self.mapping_table_pods.load(Ordering::Relaxed)
        ));
        output.push_str(&format!(
            "nri_mapping_table_size{{type=\"containers\"}} {}\n",
            self.mapping_table_containers.load(Ordering::Relaxed)
        ));
        output.push_str(&format!(
            "nri_mapping_table_size{{type=\"cgroups\"}} {}\n",
            self.mapping_table_cgroups.load(Ordering::Relaxed)
        ));
        output.push_str(&format!(
            "nri_mapping_table_size{{type=\"pids\"}} {}\n",
            self.mapping_table_pids.load(Ordering::Relaxed)
        ));

        // 持久化统计
        output.push_str("\n# HELP nri_persistence_snapshots_total Total persistence snapshots\n");
        output.push_str("# TYPE nri_persistence_snapshots_total counter\n");
        output.push_str(&format!(
            "nri_persistence_snapshots_total {}\n",
            self.persistence_snapshots_total.load(Ordering::Relaxed)
        ));

        output.push_str("\n# HELP nri_persistence_restores_total Total persistence restores\n");
        output.push_str("# TYPE nri_persistence_restores_total counter\n");
        output.push_str(&format!(
            "nri_persistence_restores_total {}\n",
            self.persistence_restores_total.load(Ordering::Relaxed)
        ));

        // 版本控制统计
        output.push_str("\n# HELP nri_version_checks_total Total version checks\n");
        output.push_str("# TYPE nri_version_checks_total counter\n");
        output.push_str(&format!(
            "nri_version_checks_total {}\n",
            self.version_checks_total.load(Ordering::Relaxed)
        ));
        output.push_str(&format!(
            "nri_version_checks_total{{result=\"accepted\"}} {}\n",
            self.version_accepted.load(Ordering::Relaxed)
        ));
        output.push_str(&format!(
            "nri_version_checks_total{{result=\"rejected\"}} {}\n",
            self.version_rejected_stale.load(Ordering::Relaxed)
        ));

        // 批量处理统计
        output.push_str("\n# HELP nri_batch_flushes_total Total batch flushes\n");
        output.push_str("# TYPE nri_batch_flushes_total counter\n");
        output.push_str(&format!(
            "nri_batch_flushes_total {}\n",
            self.batch_flushes_total.load(Ordering::Relaxed)
        ));

        output.push_str("\n# HELP nri_batch_events_processed_total Total events processed in batches\n");
        output.push_str("# TYPE nri_batch_events_processed_total counter\n");
        output.push_str(&format!(
            "nri_batch_events_processed_total {}\n",
            self.batch_events_processed.load(Ordering::Relaxed)
        ));

        output.push_str("\n# HELP nri_batch_queue_depth Current batch queue depth\n");
        output.push_str("# TYPE nri_batch_queue_depth gauge\n");
        output.push_str(&format!(
            "nri_batch_queue_depth {}\n",
            self.batch_queue_depth.load(Ordering::Relaxed)
        ));

        output
    }

    /// 导出为 JSON 格式
    pub fn export_json(&self) -> serde_json::Value {
        use serde_json::json;

        let mut events_by_type = serde_json::Map::new();
        for entry in self.events_by_type.iter() {
            events_by_type.insert(
                entry.key().clone(),
                json!(entry.value().load(Ordering::Relaxed)),
            );
        }

        json!({
            "events": {
                "total": self.events_total.load(Ordering::Relaxed),
                "by_type": events_by_type,
                "avg_processing_us": self.avg_event_processing_us(),
            },
            "attribution": {
                "queries_total": self.attribution_queries_total.load(Ordering::Relaxed),
                "cache_hit_rate": self.cache_hit_rate(),
                "avg_query_us": self.avg_attribution_query_us(),
            },
            "batch_processing": {
                "flushes": self.batch_flushes_total.load(Ordering::Relaxed),
                "events_processed": self.batch_events_processed.load(Ordering::Relaxed),
                "queue_depth": self.batch_queue_depth.load(Ordering::Relaxed),
            },
        })
    }
}

/// 创建全局指标收集器
pub fn create_metrics() -> Arc<NriMetrics> {
    Arc::new(NriMetrics::new())
}

/// Axum handler for Prometheus metrics endpoint
pub async fn metrics_handler_prometheus(
    metrics: axum::extract::State<Arc<NriMetrics>>,
) -> impl axum::response::IntoResponse {
    let body = metrics.export_prometheus();
    (
        axum::http::StatusCode::OK,
        [(axum::http::header::CONTENT_TYPE, "text/plain; charset=utf-8")],
        body,
    )
}

/// Axum handler for JSON metrics endpoint
pub async fn metrics_handler_json(
    metrics: axum::extract::State<Arc<NriMetrics>>,
) -> impl axum::response::IntoResponse {
    let json = metrics.export_json();
    axum::Json(json)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_metrics_recording() {
        let metrics = NriMetrics::new();

        // 记录事件
        metrics.record_event("ADD", 100);
        metrics.record_event("ADD", 200);
        metrics.record_event("DELETE", 50);

        assert_eq!(metrics.events_total.load(Ordering::Relaxed), 3);
        assert_eq!(
            metrics.avg_event_processing_us(),
            116.66666666666667 // (100 + 200 + 50) / 3
        );
    }

    #[test]
    fn test_cache_hit_rate() {
        let metrics = NriMetrics::new();

        metrics.record_attribution_query(true, 10);  // hit
        metrics.record_attribution_query(true, 20);  // hit
        metrics.record_attribution_query(false, 30); // miss

        assert_eq!(metrics.cache_hit_rate(), 2.0 / 3.0);
    }

    #[test]
    fn test_prometheus_export() {
        let metrics = NriMetrics::new();

        metrics.record_event("ADD", 100);
        metrics.update_mapping_table_size(10, 20, 30, 40);
        metrics.record_version_check(true);
        metrics.record_version_check(false);

        let export = metrics.export_prometheus();

        assert!(export.contains("nri_events_total"));
        assert!(export.contains("nri_mapping_table_size"));
        assert!(export.contains("nri_version_checks_total"));
    }

    #[test]
    fn test_json_export() {
        let metrics = NriMetrics::new();

        metrics.record_event("ADD", 100);
        metrics.update_mapping_table_size(5, 10, 15, 20);

        let json = metrics.export_json();

        assert!(json.get("events").is_some());
        assert!(json.get("mapping_table").is_some());
    }
}
