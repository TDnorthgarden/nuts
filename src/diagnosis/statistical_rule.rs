//! 统计异常型规则 - 基于统计方法检测异常
//!
//! 支持以下异常检测算法：
//! - 突发异常检测（Sudden Spike/Drop）
//! - 方差异常（Variance Increase）
//! - 分布偏移（Distribution Shift）
//! - 离群值检测（3-sigma）

use crate::types::diagnosis::*;
use crate::types::evidence::Evidence;
use crate::diagnosis::engine::Rule;
use std::collections::VecDeque;
use std::sync::{Arc, Mutex};

/// 异常类型
#[derive(Clone, Copy, Debug, PartialEq)]
pub enum AnomalyType {
    /// 突发异常（突然跃迁）
    SuddenSpike,
    /// 骤降异常（突然下降）
    SuddenDrop,
    /// 方差异常（不稳定性增加）
    VarianceIncrease,
    /// 分布偏移（基线变化）
    DistributionShift,
    /// 离群值检测（3-sigma）
    OutlierDetection,
}

/// 统计异常型规则
pub struct StatisticalRule {
    pub name: String,
    pub evidence_type: String,
    pub metric_name: String,
    pub anomaly_type: AnomalyType,
    /// 统计窗口大小（秒）
    pub window_secs: u64,
    /// 阈值倍数（如3-sigma中的3.0）
    pub threshold: f64,
    pub conclusion_title: String,
    pub severity: u8,
    /// 历史数据缓存（用于计算统计量）
    history: Arc<Mutex<VecDeque<f64>>>,
    /// 最大缓存大小
    max_history_size: usize,
}

impl StatisticalRule {
    pub fn new(
        name: &str,
        evidence_type: &str,
        metric_name: &str,
        anomaly_type: AnomalyType,
        window_secs: u64,
        threshold: f64,
        conclusion_title: &str,
        severity: u8,
    ) -> Self {
        // 根据窗口计算最大缓存（假设每秒一个样本）
        let max_history_size = window_secs as usize;
        
        Self {
            name: name.to_string(),
            evidence_type: evidence_type.to_string(),
            metric_name: metric_name.to_string(),
            anomaly_type,
            window_secs,
            threshold,
            conclusion_title: conclusion_title.to_string(),
            severity,
            history: Arc::new(Mutex::new(VecDeque::with_capacity(max_history_size))),
            max_history_size,
        }
    }

    /// 添加历史数据点
    pub fn add_history(&self, value: f64) {
        if let Ok(mut history) = self.history.lock() {
            if history.len() >= self.max_history_size {
                history.pop_front();
            }
            history.push_back(value);
        }
    }

    /// 计算历史统计量（均值、标准差）
    fn calculate_statistics(&self) -> Option<(f64, f64)> {
        let history = self.history.lock().ok()?;
        if history.len() < 2 {
            return None;
        }

        let mean: f64 = history.iter().sum::<f64>() / history.len() as f64;
        let variance: f64 = history
            .iter()
            .map(|x| (x - mean).powi(2))
            .sum::<f64>() / (history.len() - 1) as f64;
        let std_dev = variance.sqrt();

        Some((mean, std_dev))
    }

    /// 检测突发异常
    fn detect_sudden_change(&self, current: f64, prev: f64) -> bool {
        if prev.abs() < f64::EPSILON {
            return false;
        }
        let change_ratio = (current - prev).abs() / prev;
        change_ratio > self.threshold
    }

    /// 检测方差异常
    fn detect_variance_anomaly(&self, current: f64, mean: f64, std_dev: f64) -> bool {
        if std_dev < f64::EPSILON {
            return false;
        }
        let z_score = (current - mean).abs() / std_dev;
        z_score > self.threshold
    }

    /// 检测离群值（3-sigma）
    fn detect_outlier(&self, current: f64, mean: f64, std_dev: f64) -> bool {
        if std_dev < f64::EPSILON {
            return false;
        }
        let z_score = (current - mean).abs() / std_dev;
        z_score > self.threshold
    }

    /// 计算置信度
    fn calculate_confidence(&self, current: f64, mean: f64, std_dev: f64) -> f64 {
        if std_dev < f64::EPSILON {
            return 0.5;
        }
        let z_score = (current - mean).abs() / std_dev;
        let confidence = 0.5 + (z_score / self.threshold).min(1.0) * 0.5;
        confidence.min(1.0)
    }
}

impl Rule for StatisticalRule {
    fn name(&self) -> &str {
        &self.name
    }

    fn evaluate(&self, evidence: &Evidence) -> Option<Conclusion> {
        // 只处理匹配的证据类型
        if evidence.evidence_type != self.evidence_type {
            return None;
        }

        // 获取当前指标值
        let current_value = evidence.metric_summary.get(&self.metric_name)?;

        // 添加到历史
        self.add_history(*current_value);

        // 获取历史统计量
        let stats = self.calculate_statistics()?;
        let (mean, std_dev) = stats;

        let triggered = match self.anomaly_type {
            AnomalyType::SuddenSpike => {
                // 检测突增
                if let Some(prev_value) = {
                    let history = self.history.lock().ok()?;
                    history.iter().rev().nth(1).copied()
                } {
                    *current_value > mean && self.detect_sudden_change(*current_value, prev_value)
                } else {
                    false
                }
            }
            AnomalyType::SuddenDrop => {
                // 检测突降
                if let Some(prev_value) = {
                    let history = self.history.lock().ok()?;
                    history.iter().rev().nth(1).copied()
                } {
                    *current_value < mean && self.detect_sudden_change(*current_value, prev_value)
                } else {
                    false
                }
            }
            AnomalyType::VarianceIncrease => {
                // 检测方差异常（当前值偏离均值超过阈值倍标准差）
                self.detect_variance_anomaly(*current_value, mean, std_dev)
            }
            AnomalyType::DistributionShift => {
                // 检测分布偏移（需要更复杂算法，简化为3-sigma检测）
                self.detect_outlier(*current_value, mean, std_dev)
            }
            AnomalyType::OutlierDetection => {
                // 离群值检测（3-sigma）
                self.detect_outlier(*current_value, mean, std_dev)
            }
        };

        if triggered {
            let confidence = self.calculate_confidence(*current_value, mean, std_dev);

            Some(Conclusion {
                conclusion_id: format!(
                    "stat-{}-{}-{:?}",
                    self.name,
                    &evidence.evidence_id[..8.min(evidence.evidence_id.len())],
                    self.anomaly_type
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
                details: Some(serde_json::json!({
                    "rule_type": "statistical",
                    "anomaly_type": format!("{:?}", self.anomaly_type),
                    "metric": self.metric_name,
                    "current_value": current_value,
                    "mean": mean,
                    "std_dev": std_dev,
                    "threshold": self.threshold,
                    "window_secs": self.window_secs,
                    "z_score": (current_value - mean).abs() / std_dev,
                })),
            })
        } else {
            None
        }
    }
}

/// 创建默认统计规则集
pub fn create_default_statistical_rules() -> Vec<Box<dyn Rule>> {
    vec![
        Box::new(StatisticalRule::new(
            "network_latency_sudden_spike",
            "network",
            "latency_p99_ms",
            AnomalyType::SuddenSpike,
            60,   // 60秒窗口
            3.0,  // 超过基线3倍
            "网络延迟突发性跃迁，可能存在网络拥塞",
            8,
        )),
        Box::new(StatisticalRule::new(
            "io_latency_outlier",
            "block_io",
            "io_latency_ms",
            AnomalyType::OutlierDetection,
            300,  // 5分钟窗口
            3.0,  // 3-sigma
            "I/O 延迟出现离群值，可能存在存储设备异常",
            7,
        )),
        Box::new(StatisticalRule::new(
            "cpu_usage_variance_high",
            "cgroup_contention",
            "cpu_usage_percent",
            AnomalyType::VarianceIncrease,
            120,  // 2分钟窗口
            2.5,  // 2.5-sigma
            "CPU使用率波动异常，存在不稳定性",
            6,
        )),
    ]
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;
    use crate::types::diagnosis::Conclusion;
    use crate::types::evidence::{CollectionMeta, TimeWindow, Scope, Attribution};

    fn create_test_evidence(value: f64) -> Evidence {
        let mut metric_summary = HashMap::new();
        metric_summary.insert("latency_p99_ms".to_string(), value);
        let now = chrono::Utc::now().timestamp_millis();

        Evidence {
            schema_version: "evidence.v0.2".to_string(),
            task_id: "task-001".to_string(),
            evidence_id: "test-001".to_string(),
            evidence_type: "network".to_string(),
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
            scope: Scope::default(),
            selection: None,
            metric_summary,
            events_topology: vec![],
            top_calls: None,
            attribution: Attribution::default(),
        }
    }

    #[test]
    fn test_statistical_rule_outlier_detection() {
        let rule = StatisticalRule::new(
            "test_outlier",
            "network",
            "latency_p99_ms",
            AnomalyType::OutlierDetection,
            10,
            2.0,
            "Test outlier",
            5,
        );

        // 添加正常历史数据（均值~10）
        for i in 0..5 {
            rule.add_history(10.0 + i as f64 * 0.1);
        }

        // 测试异常值
        let evidence = create_test_evidence(50.0); // 5倍于均值
        let result = rule.evaluate(&evidence);
        assert!(result.is_some());
    }
}
