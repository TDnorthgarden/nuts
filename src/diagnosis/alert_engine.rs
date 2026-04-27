//! 告警规则引擎
//!
//! 基于诊断结果评估告警规则，生成告警实例

use crate::types::alert::*;
use crate::types::diagnosis::{Conclusion, DiagnosisResult, DiagnosisStatus};
use crate::types::evidence::Evidence;
use std::collections::HashMap;
use std::sync::{Arc, RwLock};
use tracing::{debug, info, warn};
use uuid::Uuid;

/// 告警规则引擎
pub struct AlertRuleEngine {
    /// 规则配置
    rules: Vec<AlertRule>,
    /// 活跃告警缓存（用于去重和抑制）
    active_alerts: Arc<RwLock<HashMap<String, AlertInstance>>>,
    /// 告警历史（用于恢复检测）
    alert_history: Arc<RwLock<Vec<AlertInstance>>>,
    /// 最大历史记录数
    max_history_size: usize,
}

impl AlertRuleEngine {
    /// 创建新的规则引擎
    pub fn new(rules: Vec<AlertRule>) -> Self {
        Self {
            rules,
            active_alerts: Arc::new(RwLock::new(HashMap::new())),
            alert_history: Arc::new(RwLock::new(Vec::new())),
            max_history_size: 1000,
        }
    }

    /// 从配置创建引擎
    pub fn from_config(config: AlertRuleConfig) -> Self {
        Self::new(config.rules)
    }

    /// 更新规则（热更新支持）
    pub fn update_rules(&mut self, rules: Vec<AlertRule>) {
        self.rules = rules;
        info!("Alert rules updated: {} rules", self.rules.len());
    }

    /// 评估诊断结果，生成告警
    pub fn evaluate(&self, diagnosis: &DiagnosisResult, evidences: &[Evidence]) -> Vec<AlertEvaluationResult> {
        let mut results = Vec::new();

        // 获取启用的规则
        let enabled_rules: Vec<&AlertRule> = self
            .rules
            .iter()
            .filter(|r| r.enabled)
            .collect();

        debug!(
            "Evaluating diagnosis {} against {} rules",
            diagnosis.task_id,
            enabled_rules.len()
        );

        for rule in enabled_rules {
            match self.evaluate_rule(rule, diagnosis, evidences) {
                AlertEvaluationResult::Firing(alert) => {
                    info!(
                        "Alert firing: {} for task {}",
                        alert.alert_id, diagnosis.task_id
                    );
                    results.push(AlertEvaluationResult::Firing(alert));
                }
                AlertEvaluationResult::Suppressed(key) => {
                    debug!("Alert suppressed: {} for task {}", key, diagnosis.task_id);
                    results.push(AlertEvaluationResult::Suppressed(key));
                }
                AlertEvaluationResult::Error(e) => {
                    warn!("Alert evaluation error: {}", e);
                    results.push(AlertEvaluationResult::Error(e));
                }
                AlertEvaluationResult::NotFiring => {
                    // 不触发，不添加到结果
                }
            }
        }

        results
    }

    /// 评估单个规则
    fn evaluate_rule(
        &self,
        rule: &AlertRule,
        diagnosis: &DiagnosisResult,
        evidences: &[Evidence],
    ) -> AlertEvaluationResult {
        // 检查条件是否满足
        let condition_met = self.evaluate_condition(&rule.condition, diagnosis, evidences);

        if !condition_met {
            return AlertEvaluationResult::NotFiring;
        }

        // 生成去重键
        let dedup_key = AlertInstance::generate_dedup_key(&rule.rule_id, &diagnosis.task_id);

        // 检查是否已有活跃告警（抑制）
        {
            let active = self.active_alerts.read().unwrap();
            if let Some(existing) = active.get(&dedup_key) {
                if existing.is_in_suppress_window(rule.suppress_window_secs) {
                    return AlertEvaluationResult::Suppressed(dedup_key);
                }
            }
        }

        // 生成告警实例
        let alert = self.create_alert_instance(rule, diagnosis, evidences, &dedup_key);

        // 添加到活跃告警缓存
        {
            let mut active = self.active_alerts.write().unwrap();
            active.insert(dedup_key.clone(), alert.clone());
        }

        // 添加到历史
        self.add_to_history(alert.clone());

        AlertEvaluationResult::Firing(alert)
    }

    /// 评估条件
    fn evaluate_condition(
        &self,
        condition: &AlertCondition,
        diagnosis: &DiagnosisResult,
        evidences: &[Evidence],
    ) -> bool {
        match condition {
            AlertCondition::ConclusionMatch {
                conclusion_pattern,
                min_confidence,
            } => {
                self.evaluate_conclusion_match(diagnosis, conclusion_pattern, *min_confidence)
            }
            AlertCondition::MetricThreshold {
                evidence_type,
                metric_name,
                operator,
                threshold,
                duration_secs: _,
            } => self.evaluate_metric_threshold(evidences, evidence_type, metric_name, *operator, *threshold),
            AlertCondition::DiagnosisStatus {
                status,
                min_evidence_count,
            } => self.evaluate_diagnosis_status(diagnosis, status, *min_evidence_count),
            AlertCondition::And { conditions } => {
                conditions.iter().all(|c| self.evaluate_condition(c, diagnosis, evidences))
            }
            AlertCondition::Or { conditions } => {
                conditions.iter().any(|c| self.evaluate_condition(c, diagnosis, evidences))
            }
        }
    }

    /// 评估结论匹配
    fn evaluate_conclusion_match(
        &self,
        diagnosis: &DiagnosisResult,
        pattern: &str,
        min_confidence: f64,
    ) -> bool {
        // 简单的通配符匹配
        let pattern_lower = pattern.to_lowercase();
        
        diagnosis.conclusions.iter().any(|c| {
            let matches_pattern = if pattern.contains('*') {
                // 通配符匹配
                let parts: Vec<&str> = pattern.split('*').collect();
                let title_lower = c.title.to_lowercase();
                if parts.len() == 2 {
                    title_lower.starts_with(parts[0]) && title_lower.ends_with(parts[1])
                } else {
                    title_lower.contains(&pattern_lower.replace('*', ""))
                }
            } else {
                c.title.to_lowercase().contains(&pattern_lower)
            };
            
            matches_pattern && c.confidence >= min_confidence
        })
    }

    /// 评估指标阈值
    fn evaluate_metric_threshold(
        &self,
        evidences: &[Evidence],
        evidence_type: &str,
        metric_name: &str,
        operator: ThresholdOperator,
        threshold: f64,
    ) -> bool {
        evidences.iter().any(|e| {
            if e.evidence_type != evidence_type {
                return false;
            }
            
            e.metric_summary.get(metric_name).map_or(false, |value| {
                operator.evaluate(*value, threshold)
            })
        })
    }

    /// 评估诊断状态
    fn evaluate_diagnosis_status(
        &self,
        diagnosis: &DiagnosisResult,
        status: &str,
        _min_evidence_count: usize,
    ) -> bool {
        let status_matches = match status.to_lowercase().as_str() {
            "done" => matches!(diagnosis.status, DiagnosisStatus::Done),
            "failed" => matches!(diagnosis.status, DiagnosisStatus::Failed),
            "partial" => matches!(diagnosis.status, DiagnosisStatus::Partial),
            "running" => matches!(diagnosis.status, DiagnosisStatus::Running),
            _ => false,
        };

        status_matches && diagnosis.evidence_refs.len() >= _min_evidence_count
    }

    /// 创建告警实例
    fn create_alert_instance(
        &self,
        rule: &AlertRule,
        diagnosis: &DiagnosisResult,
        evidences: &[Evidence],
        dedup_key: &str,
    ) -> AlertInstance {
        let now = chrono::Utc::now().timestamp();
        let alert_id = format!("alert-{}-{}-{}-{}-XXX", 
            Uuid::new_v4().to_string(),
            &rule.rule_id,
            &diagnosis.task_id,
            now
        );

        // 生成告警标题和描述
        let title = self.generate_alert_title(rule, diagnosis);
        let description = self.generate_alert_description(rule, diagnosis, evidences);
        let root_cause = self.generate_root_cause(diagnosis);
        let suggestion = self.generate_suggestion(rule, diagnosis);
        
        // 提取 pod 信息（从第一个结论的 references 中）
        let _pod_info = diagnosis.traceability.references.first()
            .and_then(|r| r.reasoning_summary.as_ref())
            .cloned()
            .unwrap_or_else(|| "unknown".to_string());

        // 收集关联证据
        let evidence_refs: Vec<String> = diagnosis.evidence_refs.iter()
            .map(|e| e.evidence_id.clone())
            .collect();

        // 合并标签
        let mut labels = rule.labels.clone();
        labels.insert("rule_id".to_string(), rule.rule_id.clone());
        labels.insert("task_id".to_string(), diagnosis.task_id.clone());
        labels.insert("severity".to_string(), rule.severity.to_string());

        AlertInstance {
            alert_id,
            rule_id: rule.rule_id.clone(),
            task_id: diagnosis.task_id.clone(),
            severity: rule.severity,
            status: AlertStatus::Firing,
            title,
            description,
            root_cause,
            suggestion,
            triggered_at: now,
            resolved_at: None,
            acknowledged_at: None,
            labels,
            evidence_refs,
            dedup_key: dedup_key.to_string(),
        }
    }

    /// 生成告警标题
    fn generate_alert_title(&self, rule: &AlertRule, diagnosis: &DiagnosisResult) -> String {
        // 如果有主要结论，使用结论信息
        if let Some(top_conclusion) = diagnosis.conclusions.first() {
            format!("[{}] {}: {}", 
                rule.severity.to_string().to_uppercase(),
                rule.name,
                &top_conclusion.title[..top_conclusion.title.len().min(50)]
            )
        } else {
            format!("[{}] {}", 
                rule.severity.to_string().to_uppercase(),
                rule.name
            )
        }
    }

    /// 生成告警描述
    fn generate_alert_description(
        &self,
        rule: &AlertRule,
        diagnosis: &DiagnosisResult,
        _evidences: &[Evidence],
    ) -> String {
        let mut desc = rule.description.clone();
        desc.push_str("\n\n诊断详情:\n");
        desc.push_str(&format!("- 任务ID: {}\n", diagnosis.task_id));
        let pod_info = diagnosis.traceability.references.first()
            .and_then(|r| r.reasoning_summary.as_ref())
            .cloned()
            .unwrap_or_else(|| "unknown".to_string());
        desc.push_str(&format!("- 目标Pod: {}\n", pod_info));
        desc.push_str(&format!("- 证据数量: {}\n", diagnosis.evidence_refs.len()));
        
        if !diagnosis.conclusions.is_empty() {
            desc.push_str("\n诊断结论:\n");
            for (i, c) in diagnosis.conclusions.iter().enumerate() {
                desc.push_str(&format!("{}. {} (置信度: {:.0}%)\n", 
                    i + 1, c.title, c.confidence * 100.0));
            }
        }

        desc
    }

    /// 生成根因分析
    fn generate_root_cause(&self, diagnosis: &DiagnosisResult) -> String {
        diagnosis.conclusions.first()
            .map(|c| c.title.clone())
            .unwrap_or_else(|| "根因分析进行中".to_string())
    }

    /// 生成处理建议
    fn generate_suggestion(&self, rule: &AlertRule, _diagnosis: &DiagnosisResult) -> String {
        // 优先使用规则注释中的建议
        if let Some(playbook) = rule.annotations.get("playbook") {
            return format!("参考处理手册: {}", playbook);
        }
        if let Some(runbook) = rule.annotations.get("runbook") {
            return format!("参考运维手册: {}", runbook);
        }

        // 默认建议
        match rule.severity {
            AlertSeverity::Critical => "立即处理：检查资源使用情况，可能需要扩容或重启服务",
            AlertSeverity::High => "尽快处理：分析趋势，准备应对措施",
            AlertSeverity::Medium => "计划处理：持续观察，排查潜在问题",
            AlertSeverity::Low => "观察处理：记录异常，分析模式",
            AlertSeverity::Info => "仅记录：无需处理",
        }.to_string()
    }

    /// 添加到历史
    fn add_to_history(&self, alert: AlertInstance) {
        let mut history = self.alert_history.write().unwrap();
        history.push(alert);
        
        // 限制历史大小
        if history.len() > self.max_history_size {
            history.remove(0);
        }
    }

    /// 获取活跃告警
    pub fn get_active_alerts(&self) -> Vec<AlertInstance> {
        let active = self.active_alerts.read().unwrap();
        active.values().cloned().collect()
    }

    /// 获取告警历史
    pub fn get_alert_history(&self) -> Vec<AlertInstance> {
        let history = self.alert_history.read().unwrap();
        history.clone()
    }

    /// 解决告警
    pub fn resolve_alert(&self, dedup_key: &str) -> bool {
        let mut active = self.active_alerts.write().unwrap();
        if let Some(alert) = active.get_mut(dedup_key) {
            alert.resolve();
            info!("Alert resolved: {}", dedup_key);
            true
        } else {
            false
        }
    }

    /// 确认告警
    pub fn acknowledge_alert(&self, dedup_key: &str) -> bool {
        let mut active = self.active_alerts.write().unwrap();
        if let Some(alert) = active.get_mut(dedup_key) {
            alert.acknowledge();
            info!("Alert acknowledged: {}", dedup_key);
            true
        } else {
            false
        }
    }

    /// 清理过期告警
    pub fn cleanup_expired_alerts(&self, max_age_secs: u64) -> usize {
        let now = chrono::Utc::now().timestamp();
        let mut active = self.active_alerts.write().unwrap();
        let initial_count = active.len();
        
        active.retain(|_, alert| {
            let age = now - alert.triggered_at;
            age < max_age_secs as i64
        });
        
        let removed = initial_count - active.len();
        if removed > 0 {
            info!("Cleaned up {} expired alerts", removed);
        }
        removed
    }
}

/// 默认规则配置
pub fn default_alert_rules() -> AlertRuleConfig {
    let mut config = AlertRuleConfig::new();

    // CPU 资源争抢告警
    config.add_rule(
        AlertRule::new(
            "cpu-contention-p0",
            "CPU资源争抢严重",
            AlertCondition::ConclusionMatch {
                conclusion_pattern: "CPU*".to_string(),
                min_confidence: 0.8,
            },
            AlertSeverity::Critical,
        )
        .with_label("category", "resource")
        .with_label("team", "platform")
        .with_annotation("runbook", "https://wiki.example.com/runbooks/cpu-contention")
        .with_annotation("playbook", "https://wiki.example.com/playbooks/cpu-throttling"),
    );

    // 内存泄漏告警
    config.add_rule(
        AlertRule::new(
            "memory-leak-p1",
            "内存泄漏检测",
            AlertCondition::MetricThreshold {
                evidence_type: "memory".to_string(),
                metric_name: "growth_rate".to_string(),
                operator: ThresholdOperator::GreaterThan,
                threshold: 10.0,
                duration_secs: 300,
            },
            AlertSeverity::High,
        )
        .with_label("category", "memory")
        .with_annotation("runbook", "https://wiki.example.com/runbooks/memory-leak"),
    );

    // 网络延迟告警
    config.add_rule(
        AlertRule::new(
            "network-latency-p2",
            "网络延迟异常",
            AlertCondition::MetricThreshold {
                evidence_type: "network".to_string(),
                metric_name: "latency_p99".to_string(),
                operator: ThresholdOperator::GreaterThan,
                threshold: 100.0,
                duration_secs: 60,
            },
            AlertSeverity::Medium,
        )
        .with_label("category", "network"),
    );

    // I/O 延迟告警
    config.add_rule(
        AlertRule::new(
            "io-latency-p2",
            "I/O延迟过高",
            AlertCondition::MetricThreshold {
                evidence_type: "block_io".to_string(),
                metric_name: "io_latency".to_string(),
                operator: ThresholdOperator::GreaterThan,
                threshold: 50.0,
                duration_secs: 120,
            },
            AlertSeverity::Medium,
        )
        .with_label("category", "storage"),
    );

    config
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::types::diagnosis::{DiagnosisResult, DiagnosisStatus};

    fn create_test_diagnosis() -> DiagnosisResult {
        DiagnosisResult {
            schema_version: "diagnosis.v0.2".to_string(),
            task_id: "task-123".to_string(),
            status: DiagnosisStatus::Done,
            runtime: None,
            trigger: crate::types::diagnosis::TriggerInfo {
                trigger_type: "manual".to_string(),
                trigger_reason: "test".to_string(),
                trigger_time_ms: 1000,
                matched_condition: None,
                event_type: None,
            },
            evidence_refs: vec![
                crate::types::diagnosis::EvidenceRef {
                    evidence_id: "e1".to_string(),
                    evidence_type: Some("cpu".to_string()),
                    scope_key: None,
                    role: Some("primary".to_string()),
                },
            ],
            conclusions: vec![
                Conclusion {
                    conclusion_id: "c1".to_string(),
                    title: "CPU资源争抢严重".to_string(),
                    confidence: 0.92,
                    evidence_strength: crate::types::diagnosis::EvidenceStrength::High,
                    severity: Some(2),
                    details: Some(serde_json::json!({
                        "description": "CPU资源争抢严重",
                        "conclusion_type": "manual"
                    })),
                    },
            ],
            recommendations: vec![],
            traceability: crate::types::diagnosis::Traceability {
                references: vec![],
                engine_version: None,
            },
            ai: None,
        }
    }

    #[test]
    fn test_alert_engine_evaluation() {
        let rules = default_alert_rules();
        let engine = AlertRuleEngine::from_config(rules);
        
        let diagnosis = create_test_diagnosis();
        let evidences = vec![];
        
        let results = engine.evaluate(&diagnosis, &evidences);
        
        // 应该触发 CPU 告警
        assert!(!results.is_empty());
        let firing_count = results.iter()
            .filter(|r| matches!(r, AlertEvaluationResult::Firing(_)))
            .count();
        assert!(firing_count >= 1);
    }

    #[test]
    fn test_conclusion_match() {
        let rule = AlertRule::new(
            "test",
            "Test",
            AlertCondition::ConclusionMatch {
                conclusion_pattern: "CPU*".to_string(),
                min_confidence: 0.8,
            },
            AlertSeverity::High,
        );

        let engine = AlertRuleEngine::new(vec![rule]);
        let diagnosis = create_test_diagnosis();
        let evidences = vec![];

        let results = engine.evaluate(&diagnosis, &evidences);
        
        assert!(results.iter().any(|r| matches!(r, AlertEvaluationResult::Firing(_))));
    }
}
