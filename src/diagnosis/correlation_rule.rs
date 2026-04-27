//! 关联型规则 - 检测多指标/多证据之间的关联关系
//!
//! 支持以下关联模式：
//! - 多指标同时异常（AND 条件）
//! - 指标间相关性（如延迟上升伴随丢包增加）
//! - 时间序列关联（因果推断）
//! - 跨证据类型关联（网络 + 存储）

use crate::types::diagnosis::*;
use crate::types::evidence::Evidence;
use crate::diagnosis::engine::Rule;
use std::collections::HashMap;
use std::sync::{Arc, Mutex};

/// 关联条件
#[derive(Clone, Debug)]
pub enum CorrelationCondition {
    /// 单一指标阈值条件
    MetricThreshold {
        metric_name: String,
        threshold: f64,
        operator: ComparisonOperator,
    },
    /// 多个指标同时满足（AND）
    All(Vec<CorrelationCondition>),
    /// 任一指标满足（OR）
    Any(Vec<CorrelationCondition>),
    /// 指标比率条件
    Ratio {
        numerator: String,
        denominator: String,
        threshold: f64,
        operator: ComparisonOperator,
    },
}

/// 比较操作符
#[derive(Clone, Copy, Debug, PartialEq)]
pub enum ComparisonOperator {
    GreaterThan,
    LessThan,
    GreaterThanOrEqual,
    LessThanOrEqual,
    Equal,
}

/// 关联型规则
pub struct CorrelationRule {
    pub name: String,
    /// 主证据类型（触发规则的首要条件）
    pub primary_evidence_type: String,
    /// 关联条件
    pub conditions: CorrelationCondition,
    pub conclusion_title: String,
    pub severity: u8,
    /// 关联证据类型列表（用于跨证据关联）
    pub related_evidence_types: Vec<String>,
    /// 时间窗口（毫秒，用于时间关联）
    pub time_window_ms: i64,
    /// 关联证据缓存
    evidence_cache: Arc<Mutex<Vec<Evidence>>>,
}

/// 关联分析结果
#[derive(Debug)]
pub struct CorrelationResult {
    pub primary_match: bool,
    pub related_matches: Vec<(String, bool)>,
    pub correlation_score: f64,
    pub matched_evidence_ids: Vec<String>,
}

impl CorrelationRule {
    pub fn new(
        name: &str,
        primary_evidence_type: &str,
        conditions: CorrelationCondition,
        conclusion_title: &str,
        severity: u8,
    ) -> Self {
        Self {
            name: name.to_string(),
            primary_evidence_type: primary_evidence_type.to_string(),
            conditions,
            conclusion_title: conclusion_title.to_string(),
            severity,
            related_evidence_types: vec![],
            time_window_ms: 60000, // 默认60秒窗口
            evidence_cache: Arc::new(Mutex::new(Vec::new())),
        }
    }

    /// 添加关联证据类型
    pub fn with_related_types(mut self, types: Vec<&str>) -> Self {
        self.related_evidence_types = types.iter().map(|s| s.to_string()).collect();
        self
    }

    /// 设置时间窗口
    pub fn with_time_window(mut self, window_ms: i64) -> Self {
        self.time_window_ms = window_ms;
        self
    }

    /// 缓存证据（用于跨证据关联）
    pub fn cache_evidence(&self, evidence: &Evidence) {
        if let Ok(mut cache) = self.evidence_cache.lock() {
            cache.push(evidence.clone());
            
            // 清理过期证据
            let now = chrono::Utc::now().timestamp_millis();
            cache.retain(|e| now - e.time_window.end_time_ms < self.time_window_ms);
        }
    }

    /// 评估单一条件
    fn evaluate_condition(
        &self,
        condition: &CorrelationCondition,
        evidence: &Evidence,
    ) -> bool {
        match condition {
            CorrelationCondition::MetricThreshold {
                metric_name,
                threshold,
                operator,
            } => {
                if let Some(value) = evidence.metric_summary.get(metric_name) {
                    match operator {
                        ComparisonOperator::GreaterThan => *value > *threshold,
                        ComparisonOperator::LessThan => *value < *threshold,
                        ComparisonOperator::GreaterThanOrEqual => *value >= *threshold,
                        ComparisonOperator::LessThanOrEqual => *value <= *threshold,
                        ComparisonOperator::Equal => (*value - *threshold).abs() < f64::EPSILON,
                    }
                } else {
                    false
                }
            }
            CorrelationCondition::All(conditions) => {
                conditions.iter().all(|c| self.evaluate_condition(c, evidence))
            }
            CorrelationCondition::Any(conditions) => {
                conditions.iter().any(|c| self.evaluate_condition(c, evidence))
            }
            CorrelationCondition::Ratio {
                numerator,
                denominator,
                threshold,
                operator,
            } => {
                if let (Some(num_val), Some(den_val)) = (
                    evidence.metric_summary.get(numerator),
                    evidence.metric_summary.get(denominator),
                ) {
                    if *den_val < f64::EPSILON {
                        return false;
                    }
                    let ratio = *num_val / *den_val;
                    match operator {
                        ComparisonOperator::GreaterThan => ratio > *threshold,
                        ComparisonOperator::LessThan => ratio < *threshold,
                        ComparisonOperator::GreaterThanOrEqual => ratio >= *threshold,
                        ComparisonOperator::LessThanOrEqual => ratio <= *threshold,
                        ComparisonOperator::Equal => (ratio - *threshold).abs() < f64::EPSILON,
                    }
                } else {
                    false
                }
            }
        }
    }

    /// 执行关联分析
    fn perform_correlation_analysis(
        &self,
        primary_evidence: &Evidence,
    ) -> Option<CorrelationResult> {
        // 检查主条件是否匹配
        let primary_match = self.evaluate_condition(&self.conditions, primary_evidence);
        
        if !primary_match {
            return None;
        }

        let mut related_matches = Vec::new();
        let mut matched_ids = vec![primary_evidence.evidence_id.clone()];
        
        // 检查关联证据
        if let Ok(cache) = self.evidence_cache.lock() {
            for related_type in &self.related_evidence_types {
                let has_match = cache.iter().any(|e| {
                    e.evidence_type == *related_type 
                        && (e.time_window.end_time_ms - primary_evidence.time_window.end_time_ms).abs() < self.time_window_ms
                });
                
                related_matches.push((related_type.clone(), has_match));
                
                if let Some(matched) = cache.iter().find(|e| {
                    e.evidence_type == *related_type 
                        && (e.time_window.end_time_ms - primary_evidence.time_window.end_time_ms).abs() < self.time_window_ms
                }) {
                    matched_ids.push(matched.evidence_id.clone());
                }
            }
        }

        // 计算关联分数
        let total_related = self.related_evidence_types.len();
        let matched_related = related_matches.iter().filter(|(_, m)| *m).count();
        let correlation_score = if total_related > 0 {
            matched_related as f64 / total_related as f64
        } else {
            1.0 // 无线索关联时，主条件匹配即满分
        };

        Some(CorrelationResult {
            primary_match: true,
            related_matches,
            correlation_score,
            matched_evidence_ids: matched_ids,
        })
    }

    /// 构建关联结论详情
    fn build_correlation_details(
        &self,
        primary_evidence: &Evidence,
        result: &CorrelationResult,
    ) -> serde_json::Value {
        let related_info: Vec<_> = result
            .related_matches
            .iter()
            .map(|(ev_type, matched)| {
                serde_json::json!({
                    "evidence_type": ev_type,
                    "matched": matched,
                })
            })
            .collect();

        serde_json::json!({
            "rule_type": "correlation",
            "primary_evidence_type": self.primary_evidence_type,
            "primary_evidence_id": primary_evidence.evidence_id,
            "correlation_score": result.correlation_score,
            "matched_evidence_count": result.matched_evidence_ids.len(),
            "related_matches": related_info,
            "time_window_ms": self.time_window_ms,
            "primary_metrics": primary_evidence.metric_summary,
        })
    }
}

impl Rule for CorrelationRule {
    fn name(&self) -> &str {
        &self.name
    }

    fn evaluate(&self, evidence: &Evidence) -> Option<Conclusion> {
        // 缓存证据用于关联分析
        self.cache_evidence(evidence);

        // 执行关联分析
        let result = self.perform_correlation_analysis(evidence)?;
        
        // 主条件必须匹配
        if !result.primary_match {
            return None;
        }

        // 计算置信度（基于关联分数）
        let confidence = 0.5 + result.correlation_score * 0.5;

        Some(Conclusion {
            conclusion_id: format!(
                "corr-{}-{}-{}-{:?}",
                self.name,
                &evidence.evidence_id[..8.min(evidence.evidence_id.len())],
                evidence.evidence_type,
                std::time::SystemTime::now()
                    .duration_since(std::time::UNIX_EPOCH)
                    .unwrap_or_default()
                    .as_millis()
            ),
            title: self.conclusion_title.clone(),
            confidence,
            evidence_strength: if confidence > 0.8 {
                EvidenceStrength::High
            } else if confidence > 0.5 {
                EvidenceStrength::Medium
            } else {
                EvidenceStrength::Low
            },
            severity: Some(self.severity),
            details: Some(self.build_correlation_details(evidence, &result)),
        })
    }
}

/// 创建默认关联规则集
pub fn create_default_correlation_rules() -> Vec<Box<dyn Rule>> {
    vec![
        // 网络延迟 + 丢包关联
        Box::new(
            CorrelationRule::new(
                "network_latency_with_packet_loss",
                "network",
                CorrelationCondition::All(vec![
                    CorrelationCondition::MetricThreshold {
                        metric_name: "latency_p99_ms".to_string(),
                        threshold: 100.0,
                        operator: ComparisonOperator::GreaterThan,
                    },
                    CorrelationCondition::MetricThreshold {
                        metric_name: "packet_loss_rate".to_string(),
                        threshold: 0.01,
                        operator: ComparisonOperator::GreaterThan,
                    },
                ]),
                "网络延迟高且伴随丢包，可能存在网络拥塞或链路质量问题",
                8,
            )
            .with_related_types(vec!["network"]),
        ),
        // CPU节流 + 内存压力关联
        Box::new(
            CorrelationRule::new(
                "cpu_throttle_with_memory_pressure",
                "cgroup_contention",
                CorrelationCondition::All(vec![
                    CorrelationCondition::MetricThreshold {
                        metric_name: "cpu_throttle_rate".to_string(),
                        threshold: 10.0,
                        operator: ComparisonOperator::GreaterThan,
                    },
                    CorrelationCondition::MetricThreshold {
                        metric_name: "memory_pressure_score".to_string(),
                        threshold: 50.0,
                        operator: ComparisonOperator::GreaterThanOrEqual,
                    },
                ]),
                "CPU节流伴随内存压力，可能存在综合资源争抢",
                9,
            )
            .with_related_types(vec!["cgroup_contention"]),
        ),
        // IO延迟 + CPU等待关联
        Box::new(
            CorrelationRule::new(
                "io_wait_with_cpu_stall",
                "block_io",
                CorrelationCondition::All(vec![
                    CorrelationCondition::MetricThreshold {
                        metric_name: "io_latency_p99_ms".to_string(),
                        threshold: 100.0,
                        operator: ComparisonOperator::GreaterThan,
                    },
                    CorrelationCondition::MetricThreshold {
                        metric_name: "io_wait_time_ms".to_string(),
                        threshold: 50.0,
                        operator: ComparisonOperator::GreaterThan,
                    },
                ]),
                "I/O延迟高且CPU等待时间长，可能存在存储性能瓶颈",
                8,
            )
            .with_related_types(vec!["block_io", "cgroup_contention"]),
        ),
    ]
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::types::diagnosis::Conclusion;
    use crate::types::evidence::{CollectionMeta, TimeWindow, Scope, Attribution};
    use std::collections::HashMap;

    fn create_test_evidence(
        evidence_type: &str,
        metrics: HashMap<String, f64>,
    ) -> Evidence {
        let now = chrono::Utc::now().timestamp_millis();
        Evidence {
            schema_version: "evidence.v0.2".to_string(),
            task_id: "test-task".to_string(),
            evidence_id: format!("evidence-{}", now),
            evidence_type: evidence_type.to_string(),
            collection: CollectionMeta {
                collection_id: "test-collection".to_string(),
                collection_status: "completed".to_string(),
                probe_id: "test".to_string(),
                errors: vec![],
            },
            time_window: TimeWindow {
                start_time_ms: now - 60000,
                end_time_ms: now,
                collection_interval_ms: Some(1000),
            },
            scope: Scope {
                pod: None,
                container_id: None,
                cgroup_id: None,
                pid_scope: None,
                scope_key: String::new(),
                network_target: None,
            },
            selection: None,
            metric_summary: metrics,
            events_topology: vec![],
            top_calls: None,
            attribution: Attribution::default(),
        }
    }

    #[test]
    fn test_correlation_rule_and_condition() {
        let rule = CorrelationRule::new(
            "test_and",
            "network",
            CorrelationCondition::All(vec![
                CorrelationCondition::MetricThreshold {
                    metric_name: "latency".to_string(),
                    threshold: 100.0,
                    operator: ComparisonOperator::GreaterThan,
                },
                CorrelationCondition::MetricThreshold {
                    metric_name: "loss".to_string(),
                    threshold: 0.01,
                    operator: ComparisonOperator::GreaterThan,
                },
            ]),
            "Test correlation",
            5,
        );

        // 两个条件都满足
        let mut metrics = HashMap::new();
        metrics.insert("latency".to_string(), 150.0);
        metrics.insert("loss".to_string(), 0.02);
        let evidence = create_test_evidence("network", metrics);
        let result = rule.evaluate(&evidence);
        assert!(result.is_some());
    }
}
