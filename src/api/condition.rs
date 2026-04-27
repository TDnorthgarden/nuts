use crate::collector::block_io::{run_block_io_collect_poc, BlockIoCollectorConfig};
use crate::collector::cgroup_contention::{run_cgroup_contention_collect_poc, CgroupContentionConfig};
use crate::collector::network::{run_network_collect_poc, NetworkCollectorConfig};
use crate::collector::syscall_latency::{run_syscall_collect_poc, SyscallCollectorConfig};
use crate::collector::fs_stall::{run_fs_stall_collect_poc, FsStallCollectorConfig};
use crate::collector::nri_mapping::NriMappingTable;
use crate::diagnosis::engine::RuleEngine;
use crate::publisher::ResultPublisher;
use crate::types::evidence::{PodInfo, TimeWindow, Evidence};
use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::Duration;
use tokio::time::interval;

/// 条件触发配置
#[derive(Debug, Clone)]
pub struct ConditionTriggerConfig {
    /// 触发器 ID
    pub trigger_id: String,
    /// 条件名称
    pub name: String,
    /// 目标 Pod UID
    pub pod_uid: String,
    /// 目标 cgroup ID（可选）
    pub cgroup_id: Option<String>,
    /// 目标命名空间
    pub namespace: String,
    /// Pod 名称
    pub pod_name: String,
    /// 需要监控的证据类型
    pub evidence_types: Vec<String>,
    /// 阈值规则列表
    pub thresholds: Vec<ThresholdRule>,
    /// 检查间隔（秒）
    pub check_interval_sec: u64,
    /// 采集时间窗长度（毫秒）
    pub collection_window_ms: i64,
    /// 幂等键前缀
    pub idempotency_prefix: String,
}

/// 阈值规则
#[derive(Debug, Clone)]
pub struct ThresholdRule {
    /// 指标名称，如 "io_latency_p99_ms"
    pub metric_name: String,
    /// 证据类型，如 "block_io"
    pub evidence_type: String,
    /// 操作符：">", "<", ">=", "<="
    pub operator: ComparisonOperator,
    /// 阈值
    pub threshold: f64,
    /// 触发描述
    pub description: String,
}

/// 比较操作符
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ComparisonOperator {
    GreaterThan,
    LessThan,
    GreaterThanOrEqual,
    LessThanOrEqual,
}

impl ComparisonOperator {
    /// 从字符串解析操作符
    pub fn from_str(s: &str) -> Option<Self> {
        match s {
            ">" => Some(ComparisonOperator::GreaterThan),
            "<" => Some(ComparisonOperator::LessThan),
            ">=" => Some(ComparisonOperator::GreaterThanOrEqual),
            "<=" => Some(ComparisonOperator::LessThanOrEqual),
            _ => None,
        }
    }

    /// 评估比较
    pub fn eval(&self, value: f64, threshold: f64) -> bool {
        match self {
            ComparisonOperator::GreaterThan => value > threshold,
            ComparisonOperator::LessThan => value < threshold,
            ComparisonOperator::GreaterThanOrEqual => value >= threshold,
            ComparisonOperator::LessThanOrEqual => value <= threshold,
        }
    }
}

/// 条件触发器
pub struct ConditionTrigger {
    config: ConditionTriggerConfig,
    /// NRI 映射表
    nri_table: Option<Arc<NriMappingTable>>,
    /// 已触发记录（用于幂等）
    triggered_records: Arc<Mutex<HashMap<String, i64>>>,
    /// 冷却期（毫秒）- 同一条件在冷却期内不重复触发
    cooldown_ms: i64,
}

impl ConditionTrigger {
    /// 创建新的条件触发器
    pub fn new(config: ConditionTriggerConfig, nri_table: Option<Arc<NriMappingTable>>) -> Self {
        Self {
            config,
            nri_table,
            triggered_records: Arc::new(Mutex::new(HashMap::new())),
            cooldown_ms: 60000, // 默认 60 秒冷却期
        }
    }

    /// 设置冷却期
    pub fn with_cooldown(mut self, cooldown_ms: i64) -> Self {
        self.cooldown_ms = cooldown_ms;
        self
    }

    /// 启动条件触发服务（后台任务）
    pub async fn start(self) {
        let check_interval = Duration::from_secs(self.config.check_interval_sec);
        let mut ticker = interval(check_interval);

        tracing::info!(
            "Condition trigger '{}' started, checking every {} seconds",
            self.config.name,
            self.config.check_interval_sec
        );

        loop {
            ticker.tick().await;
            
            if let Err(e) = self.check_and_trigger().await {
                tracing::warn!("Condition trigger check failed: {:?}", e);
            }
        }
    }

    /// 检查条件并触发诊断（单次）
    async fn check_and_trigger(&self) -> Result<(), TriggerError> {
        let now = chrono::Utc::now().timestamp_millis();
        let window_start = now - self.config.collection_window_ms;

        // 创建临时采集配置
        let time_window = TimeWindow {
            start_time_ms: window_start,
            end_time_ms: now,
            collection_interval_ms: None,
        };

        let pod_info = PodInfo {
            uid: Some(self.config.pod_uid.clone()),
            name: Some(self.config.pod_name.clone()),
            namespace: Some(self.config.namespace.clone()),
        };

        // 采集证据
        let mut evidences: Vec<Evidence> = Vec::new();

        for evidence_type in &self.config.evidence_types {
            match evidence_type.as_str() {
                "network" => {
                    let network_cfg = NetworkCollectorConfig {
                        task_id: format!("{}-{}", self.config.idempotency_prefix, now),
                        time_window: time_window.clone(),
                        pod: Some(pod_info.clone()),
                        container_id: None,
                        cgroup_id: self.config.cgroup_id.clone(),
                        network_target: None,
                        requested_metrics: vec![
                            "latency_p99_ms".into(),
                            "connectivity_success_rate".into(),
                        ],
                        requested_events: vec!["latency_spike".into(), "connectivity_failure_burst".into()],
                        nri_table: self.nri_table.clone(),
                        target_pids: None,
                    };
                    let evidence = run_network_collect_poc(network_cfg);
                    evidences.push(evidence);
                }
                "block_io" => {
                    let block_io_cfg = BlockIoCollectorConfig {
                        task_id: format!("{}-{}", self.config.idempotency_prefix, now),
                        time_window: time_window.clone(),
                        pod: Some(pod_info.clone()),
                        container_id: None,
                        cgroup_id: self.config.cgroup_id.clone(),
                        requested_metrics: vec![
                            "io_latency_p99_ms".into(),
                            "io_latency_p90_ms".into(),
                            "timeout_count".into(),
                        ],
                        requested_events: vec!["io_latency_spike".into(), "io_timeout".into()],
                        nri_table: self.nri_table.clone(),
                        target_pids: None,
                    };
                    let evidence = run_block_io_collect_poc(block_io_cfg);
                    evidences.push(evidence);
                }
                "syscall_latency" => {
                    let syscall_cfg = SyscallCollectorConfig {
                        task_id: format!("{}-{}", self.config.idempotency_prefix, now),
                        time_window: time_window.clone(),
                        pod: Some(pod_info.clone()),
                        container_id: None,
                        cgroup_id: self.config.cgroup_id.clone(),
                        requested_metrics: vec!["syscall_latency_p99_ms".into()],
                        requested_events: vec!["syscall_latency_spike".into()],
                        nri_table: self.nri_table.clone(),
                    };
                    let evidence = run_syscall_collect_poc(syscall_cfg);
                    evidences.push(evidence);
                }
                "fs_stall" => {
                    let fs_stall_cfg = FsStallCollectorConfig {
                        task_id: format!("{}-{}", self.config.idempotency_prefix, now),
                        time_window: time_window.clone(),
                        pod: Some(pod_info.clone()),
                        container_id: None,
                        cgroup_id: self.config.cgroup_id.clone(),
                        requested_metrics: vec![
                            "fs_stall_p99_ms".into(),
                            "fs_stall_p90_ms".into(),
                        ],
                        requested_events: vec!["fs_stall_spike".into()],
                        nri_table: self.nri_table.clone(),
                    };
                    let evidence = run_fs_stall_collect_poc(fs_stall_cfg);
                    evidences.push(evidence);
                }
                "cgroup_contention" => {
                    let cgroup_cfg = CgroupContentionConfig {
                        task_id: format!("{}-{}", self.config.idempotency_prefix, now),
                        time_window: TimeWindow {
                            start_time_ms: now - 10000, // 10秒窗口
                            end_time_ms: now,
                            collection_interval_ms: None,
                        },
                        pod: Some(PodInfo {
                            uid: Some(self.config.pod_uid.clone()),
                            name: Some(self.config.pod_uid.clone()),
                            namespace: Some("default".to_string()),
                        }),
                        container_id: None,
                        cgroup_id: None, // 条件触发时通过 pod_uid 解析
                        requested_metrics: vec![
                            "cpu_usage_percent".into(),
                            "cpu_throttle_rate".into(),
                            "memory_usage_percent".into(),
                            "memory_pressure_score".into(),
                            "contention_score".into(),
                        ],
                        requested_events: vec!["cpu_throttle_high".into(), "memory_pressure_high".into()],
                        nri_table: self.nri_table.clone(),
                    };
                    match run_cgroup_contention_collect_poc(&cgroup_cfg).await {
                        Ok(evidence) => evidences.push(evidence),
                        Err(e) => tracing::warn!("Failed to collect cgroup_contention: {:?}", e),
                    }
                }
                _ => {
                    tracing::warn!("Unknown evidence type: {}", evidence_type);
                }
            }
        }

        // 评估阈值条件
        let triggered_rules = self.evaluate_thresholds(&evidences);

        if !triggered_rules.is_empty() {
            // 检查冷却期
            let trigger_key = format!("{}-{}", self.config.trigger_id, self.config.pod_uid);
            
            let should_trigger = {
                let records = self.triggered_records.lock().map_err(|_| TriggerError::LockError)?;
                if let Some(last_trigger) = records.get(&trigger_key) {
                    now - last_trigger > self.cooldown_ms
                } else {
                    true
                }
            };

            if should_trigger {
                // 运行诊断引擎
                let engine = RuleEngine::new();
                let diagnosis = engine.diagnose(&evidences);

                // 发布结果
                let publisher = ResultPublisher::new("/tmp/nuts");
                for evidence in &evidences {
                    if let Err(e) = publisher.publish_evidence(evidence) {
                        tracing::warn!("Failed to publish evidence: {:?}", e);
                    }
                }
                if let Err(e) = publisher.publish_diagnosis(&diagnosis) {
                    tracing::warn!("Failed to publish diagnosis: {:?}", e);
                }

                // 更新触发记录
                {
                    let mut records = self.triggered_records.lock().map_err(|_| TriggerError::LockError)?;
                    records.insert(trigger_key, now);
                }

                tracing::info!(
                    "Condition trigger '{}' triggered for pod {}, {} rules matched",
                    self.config.name,
                    self.config.pod_uid,
                    triggered_rules.len()
                );
            } else {
                tracing::debug!(
                    "Condition trigger '{}' in cooldown period for pod {}",
                    self.config.name,
                    self.config.pod_uid
                );
            }
        }

        Ok(())
    }

    /// 评估阈值规则
    fn evaluate_thresholds(&self, evidences: &[Evidence]) -> Vec<&ThresholdRule> {
        let mut triggered = Vec::new();

        for rule in &self.config.thresholds {
            // 查找对应证据
            if let Some(evidence) = evidences.iter().find(|e| e.evidence_type == rule.evidence_type) {
                // 查找指标
                if let Some(value) = evidence.metric_summary.get(&rule.metric_name) {
                    if rule.operator.eval(*value, rule.threshold) {
                        triggered.push(rule);
                        tracing::debug!(
                            "Threshold triggered: {} {} {} (value: {})",
                            rule.metric_name,
                            format!("{:?}", rule.operator),
                            rule.threshold,
                            value
                        );
                    }
                }
            }
        }

        triggered
    }

    /// 手动触发一次检查（用于测试或强制触发）
    pub async fn trigger_once(&self) -> Result<(), TriggerError> {
        self.check_and_trigger().await
    }
}

/// 触发错误
#[derive(Debug)]
pub enum TriggerError {
    LockError,
    CollectionFailed(String),
    PublishFailed(String),
}

impl std::fmt::Display for TriggerError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            TriggerError::LockError => write!(f, "Failed to acquire lock"),
            TriggerError::CollectionFailed(s) => write!(f, "Collection failed: {}", s),
            TriggerError::PublishFailed(s) => write!(f, "Publish failed: {}", s),
        }
    }
}

impl std::error::Error for TriggerError {}

/// 从字符串解析阈值表达式
/// 格式: "<evidence_type>.<metric_name> <operator> <value>"
/// 例如: "block_io.io_latency_p99_ms > 100"
pub fn parse_threshold_expression(expr: &str) -> Result<ThresholdRule, String> {
    let parts: Vec<&str> = expr.trim().split_whitespace().collect();
    
    if parts.len() != 3 {
        return Err(format!(
            "Invalid expression format: '{}'. Expected: '<evidence_type>.<metric_name> <operator> <value>'",
            expr
        ));
    }

    // 解析 evidence_type.metric_name
    let metric_parts: Vec<&str> = parts[0].split('.').collect();
    if metric_parts.len() != 2 {
        return Err(format!(
            "Invalid metric format: '{}'. Expected: '<evidence_type>.<metric_name>'",
            parts[0]
        ));
    }

    let evidence_type = metric_parts[0].to_string();
    let metric_name = metric_parts[1].to_string();

    // 解析操作符
    let operator = ComparisonOperator::from_str(parts[1])
        .ok_or_else(|| format!("Invalid operator: '{}'. Supported: >, <, >=, <=", parts[1]))?;

    // 解析阈值
    let threshold = parts[2]
        .parse::<f64>()
        .map_err(|_| format!("Invalid threshold value: '{}'", parts[2]))?;

    let description = format!(
        "{} {} {} triggers",
        metric_name,
        parts[1],
        threshold
    );

    Ok(ThresholdRule {
        metric_name,
        evidence_type,
        operator,
        threshold,
        description,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_comparison_operator_eval() {
        assert!(ComparisonOperator::GreaterThan.eval(101.0, 100.0));
        assert!(!ComparisonOperator::GreaterThan.eval(99.0, 100.0));
        
        assert!(ComparisonOperator::LessThan.eval(99.0, 100.0));
        assert!(!ComparisonOperator::LessThan.eval(101.0, 100.0));
        
        assert!(ComparisonOperator::GreaterThanOrEqual.eval(100.0, 100.0));
        assert!(ComparisonOperator::GreaterThanOrEqual.eval(101.0, 100.0));
        
        assert!(ComparisonOperator::LessThanOrEqual.eval(100.0, 100.0));
        assert!(ComparisonOperator::LessThanOrEqual.eval(99.0, 100.0));
    }

    #[test]
    fn test_parse_threshold_expression() {
        let rule = parse_threshold_expression("block_io.io_latency_p99_ms > 100").unwrap();
        assert_eq!(rule.evidence_type, "block_io");
        assert_eq!(rule.metric_name, "io_latency_p99_ms");
        assert_eq!(rule.operator, ComparisonOperator::GreaterThan);
        assert_eq!(rule.threshold, 100.0);

        let rule = parse_threshold_expression("network.connectivity_success_rate < 0.95").unwrap();
        assert_eq!(rule.evidence_type, "network");
        assert_eq!(rule.metric_name, "connectivity_success_rate");
        assert_eq!(rule.operator, ComparisonOperator::LessThan);
        assert_eq!(rule.threshold, 0.95);

        let rule = parse_threshold_expression("syscall_latency.syscall_latency_p99_ms >= 10").unwrap();
        assert_eq!(rule.evidence_type, "syscall_latency");
        assert_eq!(rule.operator, ComparisonOperator::GreaterThanOrEqual);
    }

    #[test]
    fn test_parse_invalid_expression() {
        assert!(parse_threshold_expression("invalid").is_err());
        assert!(parse_threshold_expression("block_io.metric unknown_op 100").is_err());
        assert!(parse_threshold_expression("block_io.metric > abc").is_err());
    }
}
