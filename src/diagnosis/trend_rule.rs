//! 趋势分析规则 - 基于时间序列的趋势检测和预测
//!
//! 支持以下趋势分析：
//! - 线性趋势检测（上升/下降）
//! - 趋势速率计算
//! - 预测性告警（基于趋势预测未来状态）
//! - 趋势变化点检测

use crate::types::diagnosis::*;
use crate::types::evidence::Evidence;
use crate::diagnosis::engine::Rule;
use std::collections::VecDeque;
use std::sync::{Arc, Mutex};

/// 趋势方向
#[derive(Clone, Copy, Debug, PartialEq)]
pub enum TrendDirection {
    /// 上升趋势
    Increasing,
    /// 下降趋势
    Decreasing,
    /// 平稳趋势
    Stable,
    /// 波动趋势
    Fluctuating,
}

/// 趋势类型
#[derive(Clone, Copy, Debug, PartialEq)]
pub enum TrendType {
    /// 持续增长趋势
    SustainedGrowth,
    /// 持续下降趋势
    SustainedDecline,
    /// 加速增长
    AcceleratingGrowth,
    /// 减速下降
    DeceleratingDecline,
    /// 趋势反转（上升转下降或反之）
    TrendReversal,
    /// 周期性波动
    CyclicalPattern,
}

/// 趋势规则配置
#[derive(Clone, Debug)]
pub struct TrendRuleConfig {
    /// 趋势方向
    pub direction: TrendDirection,
    /// 趋势类型
    pub trend_type: TrendType,
    /// 最小斜率阈值（变化速率）
    pub min_slope: f64,
    /// 预测时间窗口（秒）
    pub forecast_window_secs: u64,
    /// 预测阈值（触发告警的预测值）
    pub forecast_threshold: f64,
    /// 检测窗口大小
    pub window_size: usize,
}

impl Default for TrendRuleConfig {
    fn default() -> Self {
        Self {
            direction: TrendDirection::Increasing,
            trend_type: TrendType::SustainedGrowth,
            min_slope: 1.0,
            forecast_window_secs: 300,
            forecast_threshold: 100.0,
            window_size: 10,
        }
    }
}

/// 趋势分析规则
pub struct TrendRule {
    pub name: String,
    pub evidence_type: String,
    pub metric_name: String,
    pub config: TrendRuleConfig,
    pub conclusion_title: String,
    pub severity: u8,
    /// 时间序列数据点（时间戳，值）
    time_series: Arc<Mutex<VecDeque<(i64, f64)>>>,
    /// 最大缓存大小
    max_cache_size: usize,
}

/// 趋势分析结果
#[derive(Debug)]
pub struct TrendAnalysisResult {
    /// 当前趋势方向
    pub direction: TrendDirection,
    /// 当前趋势类型
    pub trend_type: TrendType,
    /// 线性回归斜率
    pub slope: f64,
    /// 截距
    pub intercept: f64,
    /// R² 决定系数（拟合优度）
    pub r_squared: f64,
    /// 预测值（forecast_window后）
    pub forecast_value: f64,
    /// 趋势置信度
    pub confidence: f64,
    /// 是否触发告警
    pub should_alert: bool,
}

impl TrendRule {
    pub fn new(
        name: &str,
        evidence_type: &str,
        metric_name: &str,
        config: TrendRuleConfig,
        conclusion_title: &str,
        severity: u8,
    ) -> Self {
        let max_cache_size = config.window_size * 2;
        
        Self {
            name: name.to_string(),
            evidence_type: evidence_type.to_string(),
            metric_name: metric_name.to_string(),
            config,
            conclusion_title: conclusion_title.to_string(),
            severity,
            time_series: Arc::new(Mutex::new(VecDeque::with_capacity(max_cache_size))),
            max_cache_size,
        }
    }

    /// 添加数据点
    pub fn add_data_point(&self, timestamp: i64, value: f64) {
        if let Ok(mut series) = self.time_series.lock() {
            if series.len() >= self.max_cache_size {
                series.pop_front();
            }
            series.push_back((timestamp, value));
        }
    }

    /// 执行线性回归分析
    fn linear_regression(&self, points: &[(i64, f64)]) -> Option<(f64, f64, f64)> {
        if points.len() < 2 {
            return None;
        }

        let n = points.len() as f64;
        
        // 标准化时间戳（从第一个点开始）
        let base_time = points[0].0 as f64;
        let x_values: Vec<f64> = points
            .iter()
            .map(|(t, _)| (*t as f64 - base_time) / 1000.0)
            .collect(); // 转换为秒
        let y_values: Vec<f64> = points.iter().map(|(_, v)| *v).collect();

        let sum_x: f64 = x_values.iter().sum();
        let sum_y: f64 = y_values.iter().sum();
        let sum_xy: f64 = x_values.iter().zip(y_values.iter()).map(|(x, y)| x * y).sum();
        let sum_x2: f64 = x_values.iter().map(|x| x * x).sum();
        let sum_y2: f64 = y_values.iter().map(|y| y * y).sum();

        let denominator = n * sum_x2 - sum_x * sum_x;
        if denominator.abs() < f64::EPSILON {
            return None;
        }

        let slope = (n * sum_xy - sum_x * sum_y) / denominator;
        let intercept = (sum_y - slope * sum_x) / n;

        // 计算 R²
        let y_mean = sum_y / n;
        let ss_total: f64 = y_values.iter().map(|y| (y - y_mean).powi(2)).sum();
        let ss_residual: f64 = y_values
            .iter()
            .zip(x_values.iter())
            .map(|(y, x)| {
                let y_pred = slope * x + intercept;
                (y - y_pred).powi(2)
            })
            .sum();

        let r_squared = if ss_total < f64::EPSILON {
            1.0
        } else {
            1.0 - (ss_residual / ss_total)
        };

        Some((slope, intercept, r_squared))
    }

    /// 检测趋势方向
    fn detect_trend_direction(&self, slope: f64) -> TrendDirection {
        if slope.abs() < self.config.min_slope {
            TrendDirection::Stable
        } else if slope > 0.0 {
            TrendDirection::Increasing
        } else {
            TrendDirection::Decreasing
        }
    }

    /// 检测趋势类型
    fn detect_trend_type(
        &self,
        direction: TrendDirection,
        slope: f64,
        r_squared: f64,
    ) -> TrendType {
        // 如果拟合度差，认为是波动
        if r_squared < 0.5 {
            return TrendType::CyclicalPattern;
        }

        match direction {
            TrendDirection::Increasing => {
                if slope > self.config.min_slope * 2.0 {
                    TrendType::AcceleratingGrowth
                } else {
                    TrendType::SustainedGrowth
                }
            }
            TrendDirection::Decreasing => {
                if slope.abs() > self.config.min_slope * 2.0 {
                    TrendType::SustainedDecline
                } else {
                    TrendType::DeceleratingDecline
                }
            }
            TrendDirection::Stable => TrendType::CyclicalPattern,
            TrendDirection::Fluctuating => TrendType::CyclicalPattern,
        }
    }

    /// 预测未来值
    fn forecast_value(&self, intercept: f64, slope: f64) -> f64 {
        let forecast_time = self.config.forecast_window_secs as f64;
        intercept + slope * forecast_time
    }

    /// 执行趋势分析
    fn analyze_trend(&self) -> Option<TrendAnalysisResult> {
        let series = self.time_series.lock().ok()?;
        
        if series.len() < self.config.window_size {
            return None;
        }

        // 获取最近的窗口数据
        let points: Vec<_> = series.iter().rev().take(self.config.window_size).rev().copied().collect();

        let (slope, intercept, r_squared) = self.linear_regression(&points)?;
        
        let direction = self.detect_trend_direction(slope);
        let trend_type = self.detect_trend_type(direction, slope, r_squared);
        
        let forecast_value = self.forecast_value(intercept, slope);
        
        // 计算置信度（基于R²和趋势清晰度）
        let confidence = r_squared * 0.8 + 0.2;
        
        // 判断是否应触发告警
        let should_alert = match self.config.direction {
            TrendDirection::Increasing => {
                direction == TrendDirection::Increasing 
                    && forecast_value > self.config.forecast_threshold
            }
            TrendDirection::Decreasing => {
                direction == TrendDirection::Decreasing
                    && forecast_value < self.config.forecast_threshold
            }
            TrendDirection::Stable => {
                forecast_value.abs() < self.config.forecast_threshold
            }
            TrendDirection::Fluctuating => true,
        };

        Some(TrendAnalysisResult {
            direction,
            trend_type,
            slope,
            intercept,
            r_squared,
            forecast_value,
            confidence,
            should_alert,
        })
    }

    /// 构建趋势详情
    fn build_trend_details(&self, result: &TrendAnalysisResult) -> serde_json::Value {
        serde_json::json!({
            "rule_type": "trend",
            "metric": self.metric_name,
            "direction": format!("{:?}", result.direction),
            "trend_type": format!("{:?}", result.trend_type),
            "slope": result.slope,
            "r_squared": result.r_squared,
            "forecast_value": result.forecast_value,
            "forecast_threshold": self.config.forecast_threshold,
            "forecast_window_secs": self.config.forecast_window_secs,
            "confidence": result.confidence,
            "min_slope_config": self.config.min_slope,
        })
    }
}

impl Rule for TrendRule {
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

        // 添加数据点
        self.add_data_point(evidence.time_window.end_time_ms, *current_value);

        // 执行趋势分析
        let result = self.analyze_trend()?;

        // 检查是否应该告警
        if !result.should_alert {
            return None;
        }

        Some(Conclusion {
            conclusion_id: format!(
                "trend-{}-{}-{:?}",
                self.name,
                &evidence.evidence_id[..8.min(evidence.evidence_id.len())],
                result.trend_type
            ),
            title: self.conclusion_title.clone(),
            confidence: result.confidence,
            evidence_strength: if result.confidence > 0.8 {
                EvidenceStrength::High
            } else if result.confidence > 0.5 {
                EvidenceStrength::Medium
            } else {
                EvidenceStrength::Low
            },
            severity: Some(self.severity),
            details: Some(self.build_trend_details(&result)),
        })
    }
}

/// 创建默认趋势规则集
pub fn create_default_trend_rules() -> Vec<Box<dyn Rule>> {
    vec![
        // 内存使用持续增长趋势
        Box::new(TrendRule::new(
            "memory_usage_growth_trend",
            "cgroup_contention",
            "memory_usage_percent",
            TrendRuleConfig {
                direction: TrendDirection::Increasing,
                trend_type: TrendType::SustainedGrowth,
                min_slope: 0.5,  // 每秒增长0.5%
                forecast_window_secs: 300,  // 5分钟预测
                forecast_threshold: 90.0,   // 预测超过90%告警
                window_size: 20,
            },
            "内存使用率呈持续上升趋势，预测5分钟后将超过90%，存在OOM风险",
            8,
        )),
        // 网络延迟上升趋势
        Box::new(TrendRule::new(
            "network_latency_growth_trend",
            "network",
            "latency_p99_ms",
            TrendRuleConfig {
                direction: TrendDirection::Increasing,
                trend_type: TrendType::SustainedGrowth,
                min_slope: 2.0,  // 每秒增长2ms
                forecast_window_secs: 180,  // 3分钟预测
                forecast_threshold: 200.0,    // 预测超过200ms告警
                window_size: 15,
            },
            "网络延迟呈上升趋势，预测3分钟后将超过200ms，可能存在网络拥塞",
            7,
        )),
        // CPU使用率加速增长
        Box::new(TrendRule::new(
            "cpu_usage_accelerating_trend",
            "cgroup_contention",
            "cpu_usage_percent",
            TrendRuleConfig {
                direction: TrendDirection::Increasing,
                trend_type: TrendType::AcceleratingGrowth,
                min_slope: 1.0,
                forecast_window_secs: 120,
                forecast_threshold: 95.0,
                window_size: 15,
            },
            "CPU使用率加速增长，预测2分钟后将超过95%，存在CPU耗尽风险",
            9,
        )),
    ]
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;
    use crate::types::diagnosis::Conclusion;
    use crate::types::evidence::{CollectionMeta, TimeWindow, Scope, Attribution};

    fn create_test_evidence_with_time(
        timestamp: i64,
        value: f64,
    ) -> Evidence {
        let mut metric_summary = HashMap::new();
        metric_summary.insert("memory_usage_percent".to_string(), value);

        Evidence {
            schema_version: "evidence.v0.2".to_string(),
            task_id: "task-001".to_string(),
            evidence_id: "test-001".to_string(),
            evidence_type: "cgroup_contention".to_string(),
            collection: CollectionMeta {
                collection_id: "test-collection".to_string(),
                collection_status: "completed".to_string(),
                probe_id: "test".to_string(),
                errors: vec![],
            },
            time_window: TimeWindow {
                start_time_ms: timestamp - 60000,
                end_time_ms: timestamp,
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
    fn test_trend_rule_linear_regression() {
        let rule = TrendRule::new(
            "test_trend",
            "cgroup_contention",
            "memory_usage_percent",
            TrendRuleConfig::default(),
            "Test trend",
            5,
        );

        // 添加线性增长的数据点 (每秒增长1%)
        let base_time = chrono::Utc::now().timestamp_millis();
        for i in 0..15 {
            rule.add_data_point(base_time + i * 1000, 50.0 + i as f64 * 1.0);
        }

        // 验证斜率计算
        let series = rule.time_series.lock().unwrap();
        let points: Vec<_> = series.iter().copied().collect();
        let result = rule.linear_regression(&points).unwrap();
        
        // 斜率应该接近1.0
        assert!(result.0 > 0.8 && result.0 < 1.2, "Slope should be ~1.0, got {}", result.0);
    }
}
