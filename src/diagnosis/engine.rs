use crate::types::diagnosis::*;
use crate::types::evidence::Evidence;
use crate::diagnosis::correlation_rule::{create_default_correlation_rules, CorrelationRule};
use crate::diagnosis::statistical_rule::{create_default_statistical_rules, StatisticalRule};
use crate::diagnosis::trend_rule::{create_default_trend_rules, TrendRule};

/// 规则引擎 - 基于阈值型规则生成诊断结论（第 1 周 PoC）
pub struct RuleEngine {
    rules: Vec<Box<dyn Rule>>,
}

/// 规则 trait
pub trait Rule: Send + Sync {
    fn name(&self) -> &str;
    fn evaluate(&self, evidence: &Evidence) -> Option<Conclusion>;
}

/// 阈值型规则
pub struct ThresholdRule {
    pub name: String,
    pub evidence_type: String,
    pub metric_name: String,
    pub threshold: f64,
    pub operator: ThresholdOperator,
    pub conclusion_title: String,
    pub severity: u8,
}

#[derive(Clone, Copy, Debug)]
pub enum ThresholdOperator {
    GreaterThan,
    LessThan,
    GreaterThanOrEqual,
    LessThanOrEqual,
}

impl ThresholdRule {
    pub fn new(
        name: &str,
        evidence_type: &str,
        metric_name: &str,
        threshold: f64,
        operator: ThresholdOperator,
        conclusion_title: &str,
        severity: u8,
    ) -> Self {
        Self {
            name: name.to_string(),
            evidence_type: evidence_type.to_string(),
            metric_name: metric_name.to_string(),
            threshold,
            operator,
            conclusion_title: conclusion_title.to_string(),
            severity,
        }
    }
}

impl Rule for ThresholdRule {
    fn name(&self) -> &str {
        &self.name
    }

    fn evaluate(&self, evidence: &Evidence) -> Option<Conclusion> {
        // 只处理匹配的 evidence_type
        if evidence.evidence_type != self.evidence_type {
            return None;
        }

        // 获取指标值
        let metric_value = evidence.metric_summary.get(&self.metric_name)?;

        // 比较阈值
        let triggered = match self.operator {
            ThresholdOperator::GreaterThan => *metric_value > self.threshold,
            ThresholdOperator::LessThan => *metric_value < self.threshold,
            ThresholdOperator::GreaterThanOrEqual => *metric_value >= self.threshold,
            ThresholdOperator::LessThanOrEqual => *metric_value <= self.threshold,
        };

        if triggered {
            // 计算置信度（距离阈值的距离作为置信度因子）
            let confidence = calculate_confidence(*metric_value, self.threshold, self.operator);

            Some(Conclusion {
                conclusion_id: format!("con-{}-{}", self.name, &evidence.evidence_id[..8.min(evidence.evidence_id.len())]),
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
                    "metric": self.metric_name,
                    "value": metric_value,
                    "threshold": self.threshold,
                    "operator": format!("{:?}", self.operator),
                })),
            })
        } else {
            None
        }
    }
}

/// 计算置信度
fn calculate_confidence(value: f64, threshold: f64, op: ThresholdOperator) -> f64 {
    let ratio = match op {
        ThresholdOperator::GreaterThan | ThresholdOperator::GreaterThanOrEqual => {
            if value <= threshold {
                return 0.0;
            }
            value / threshold
        }
        ThresholdOperator::LessThan | ThresholdOperator::LessThanOrEqual => {
            if value >= threshold {
                return 0.0;
            }
            threshold / value
        }
    };

    // 将 ratio 映射到 0.5-1.0 的置信度
    let confidence = 0.5 + (ratio - 1.0).min(1.0) * 0.5;
    confidence.min(1.0)
}

impl RuleEngine {
    pub fn new() -> Self {
        let mut engine = Self { rules: Vec::new() };
        engine.register_default_rules();
        engine
    }

    /// 创建空的规则引擎（用于动态规则管理）
    pub fn new_empty() -> Self {
        Self { rules: Vec::new() }
    }

    /// 注册默认规则集（第 1 周 PoC 最小规则集）
    fn register_default_rules(&mut self) {
        // Network 规则
        self.add_rule(Box::new(ThresholdRule::new(
            "network_latency_p99_high",
            "network",
            "latency_p99_ms",
            100.0,
            ThresholdOperator::GreaterThan,
            "网络延迟 P99 超过 100ms，存在延迟异常",
            7,
        )));

        self.add_rule(Box::new(ThresholdRule::new(
            "network_connectivity_low",
            "network",
            "connectivity_success_rate",
            0.95,
            ThresholdOperator::LessThan,
            "网络连通成功率低于 95%，存在连通性问题",
            8,
        )));

        // Block IO 规则
        self.add_rule(Box::new(ThresholdRule::new(
            "block_io_latency_p99_high",
            "block_io",
            "io_latency_p99_ms",
            100.0,
            ThresholdOperator::GreaterThan,
            "块设备 I/O 延迟 P99 超过 100ms，存在 I/O 延迟异常",
            8,
        )));

        self.add_rule(Box::new(ThresholdRule::new(
            "block_io_timeout_exists",
            "block_io",
            "timeout_count",
            0.0,
            ThresholdOperator::GreaterThan,
            "检测到 I/O 超时，可能存在存储设备问题",
            9,
        )));

        // 启用 GreaterThanOrEqual 和 LessThanOrEqual 操作符的规则
        self.add_rule(Box::new(ThresholdRule::new(
            "block_io_latency_p90_high",
            "block_io",
            "io_latency_p90_ms",
            50.0,
            ThresholdOperator::GreaterThanOrEqual,
            "块设备 I/O 延迟 P90 超过 50ms，I/O 压力增大",
            6,
        )));

        self.add_rule(Box::new(ThresholdRule::new(
            "network_connectivity_critical",
            "network",
            "connectivity_success_rate",
            0.90,
            ThresholdOperator::LessThanOrEqual,
            "网络连通成功率低于 90%，连通性严重异常",
            9,
        )));

        // Syscall Latency 规则
        self.add_rule(Box::new(ThresholdRule::new(
            "syscall_latency_p99_high",
            "syscall_latency",
            "syscall_latency_p99_us",
            100000.0, // 100ms
            ThresholdOperator::GreaterThan,
            "系统调用延迟 P99 超过 100ms，存在内核态性能问题",
            7,
        )));

        self.add_rule(Box::new(ThresholdRule::new(
            "syscall_frequency_high",
            "syscall_latency",
            "syscall_count_per_sec",
            100000.0, // 10万/秒
            ThresholdOperator::GreaterThan,
            "系统调用频率过高，可能存在系统调用风暴",
            6,
        )));

        // FS Stall 规则
        self.add_rule(Box::new(ThresholdRule::new(
            "fs_stall_p99_high",
            "fs_stall",
            "fs_stall_p99_ms",
            100.0,
            ThresholdOperator::GreaterThan,
            "文件系统延迟 P99 超过 100ms，存在文件系统阻塞",
            8,
        )));

        self.add_rule(Box::new(ThresholdRule::new(
            "fs_stall_p90_high",
            "fs_stall",
            "fs_stall_p90_ms",
            50.0,
            ThresholdOperator::GreaterThanOrEqual,
            "文件系统延迟 P90 超过 50ms，文件系统压力增大",
            6,
        )));

        // cgroup_contention 规则
        self.add_rule(Box::new(ThresholdRule::new(
            "cpu_throttle_high",
            "cgroup_contention",
            "cpu_throttle_rate",
            10.0, // 10% throttle rate
            ThresholdOperator::GreaterThan,
            "CPU throttle 率超过 10%，存在 CPU 资源争抢",
            8,
        )));

        self.add_rule(Box::new(ThresholdRule::new(
            "cpu_usage_critical",
            "cgroup_contention",
            "cpu_usage_percent",
            95.0,
            ThresholdOperator::GreaterThanOrEqual,
            "CPU 使用率超过 95%，接近资源上限",
            7,
        )));

        self.add_rule(Box::new(ThresholdRule::new(
            "memory_pressure_high",
            "cgroup_contention",
            "memory_usage_percent",
            90.0,
            ThresholdOperator::GreaterThanOrEqual,
            "内存使用率超过 90%，存在内存争抢风险",
            8,
        )));

        self.add_rule(Box::new(ThresholdRule::new(
            "memory_pressure_score_high",
            "cgroup_contention",
            "memory_pressure_score",
            50.0,
            ThresholdOperator::GreaterThanOrEqual,
            "内存压力分数超过 50，内核正在积极回收内存",
            9,
        )));

        self.add_rule(Box::new(ThresholdRule::new(
            "io_wait_high",
            "cgroup_contention",
            "io_wait_time_ms",
            100.0,
            ThresholdOperator::GreaterThanOrEqual,
            "IO 等待时间超过 100ms，存在 IO 资源争抢",
            7,
        )));

        self.add_rule(Box::new(ThresholdRule::new(
            "contention_score_high",
            "cgroup_contention",
            "contention_score",
            70.0,
            ThresholdOperator::GreaterThanOrEqual,
            "综合资源争抢评分超过 70，多维度资源紧张",
            8,
        )));

        // 注册关联型规则
        for rule in create_default_correlation_rules() {
            self.add_rule(rule);
        }

        // 注册统计异常型规则
        for rule in create_default_statistical_rules() {
            self.add_rule(rule);
        }

        // 注册趋势分析规则
        for rule in create_default_trend_rules() {
            self.add_rule(rule);
        }
    }

    pub fn add_rule(&mut self, rule: Box<dyn Rule>) {
        self.rules.push(rule);
    }

    /// 运行诊断引擎
    pub fn diagnose(&self, evidence_list: &[Evidence]) -> DiagnosisResult {
        let mut conclusions = Vec::new();
        let mut evidence_refs = Vec::new();
        let mut recommendations = Vec::new();

        for evidence in evidence_list {
            // 添加证据引用
            evidence_refs.push(EvidenceRef {
                evidence_id: evidence.evidence_id.clone(),
                evidence_type: Some(evidence.evidence_type.clone()),
                scope_key: Some(evidence.scope.scope_key.clone()),
                role: Some("primary".to_string()),
            });

            // 评估所有规则
            for rule in &self.rules {
                if let Some(conclusion) = rule.evaluate(evidence) {
                    // 生成建议
                    let recommendation = generate_recommendation(&conclusion, evidence);
                    recommendations.push(recommendation);

                    conclusions.push(conclusion);
                }
            }

            // 特殊处理：OOM 事件检测（基于 events_topology）
            if evidence.evidence_type == "oom_events" {
                for event in &evidence.events_topology {
                    if event.event_type == "oom_kill" {
                        let conclusion = Conclusion {
                            conclusion_id: uuid::Uuid::new_v4().to_string(),
                            title: "检测到 OOM Kill 事件，进程因内存耗尽被内核终止".to_string(),
                            severity: Some(10),
                            confidence: 1.0,
                            evidence_strength: EvidenceStrength::High,
                            details: Some(serde_json::json!({
                                "pod": evidence.scope.pod.as_ref().and_then(|p| p.name.as_ref()),
                                "event": "oom_kill",
                            })),
                        };
                        let recommendation = generate_recommendation(&conclusion, evidence);
                        recommendations.push(recommendation);
                        conclusions.push(conclusion);
                    }
                }
            }

            // 特殊处理：cgroup_contention 事件检测
            if evidence.evidence_type == "cgroup_contention" {
                for event in &evidence.events_topology {
                    let (title, severity) = match event.event_type.as_str() {
                        "cpu_throttle_high" => ("CPU throttle 事件触发，进程被限制 CPU 使用", 8),
                        "memory_pressure_high" => ("内存压力事件触发，内核正在积极回收内存", 7),
                        _ => continue,
                    };
                    let conclusion = Conclusion {
                        conclusion_id: uuid::Uuid::new_v4().to_string(),
                        title: title.to_string(),
                        severity: Some(severity),
                        confidence: 0.85,
                        evidence_strength: EvidenceStrength::Medium,
                        details: Some(serde_json::json!({
                            "event_type": event.event_type,
                            "contention_score": evidence.metric_summary.get("contention_score"),
                            "cpu_throttle_rate": evidence.metric_summary.get("cpu_throttle_rate"),
                            "memory_usage_percent": evidence.metric_summary.get("memory_usage_percent"),
                        })),
                    };
                    let recommendation = generate_recommendation(&conclusion, evidence);
                    recommendations.push(recommendation);
                    conclusions.push(conclusion);
                }
            }
        }

        // 按置信度排序
        conclusions.sort_by(|a, b| b.confidence.partial_cmp(&a.confidence).unwrap());
        recommendations.sort_by(|a, b| a.priority.cmp(&b.priority));

        // 构建可追溯性
        let traceability = build_traceability(&conclusions, &evidence_refs);

        // 确定状态
        let status = if conclusions.is_empty() {
            DiagnosisStatus::Done
        } else {
            DiagnosisStatus::Done
        };

        DiagnosisResult {
            schema_version: "diagnosis.v0.2".to_string(),
            task_id: evidence_list.first().map(|e| e.task_id.clone()).unwrap_or_default(),
            status,
            runtime: None,
            trigger: TriggerInfo {
                trigger_type: "manual".to_string(),
                trigger_reason: "用户手动触发诊断任务".to_string(),
                trigger_time_ms: chrono::Utc::now().timestamp_millis(),
                matched_condition: None,
                event_type: None,
            },
            evidence_refs,
            conclusions,
            recommendations,
            traceability,
            ai: None,
        }
    }
}

/// 根据结论生成建议
fn generate_recommendation(conclusion: &Conclusion, evidence: &Evidence) -> Recommendation {
    let action = match evidence.evidence_type.as_str() {
        "network" => {
            if conclusion.title.contains("延迟") {
                "检查网络链路质量、目标服务端负载、DNS 解析延迟"
            } else if conclusion.title.contains("连通性") {
                "检查防火墙规则、网络策略、目标服务健康状态"
            } else {
                "检查网络配置和链路状态"
            }
        }
        "block_io" => {
            if conclusion.title.contains("超时") {
                "检查存储设备健康状态、磁盘空间、I/O 调度器配置"
            } else {
                "检查存储性能、磁盘负载、是否有其他进程占用 I/O 带宽"
            }
        }
        "syscall_latency" => {
            if conclusion.title.contains("延迟") {
                "检查内核版本、系统调用频率、是否有大量上下文切换"
            } else {
                "优化系统调用模式，考虑使用缓存或减少不必要的调用"
            }
        }
        "fs_stall" => {
            "检查文件系统类型、挂载参数、底层存储性能"
        }
        "cgroup_contention" => {
            if conclusion.title.contains("CPU") {
                "调整 CPU limit/request 配比，考虑横向扩容或优化代码效率"
            } else if conclusion.title.contains("内存") {
                "检查内存泄漏、调整 memory limit、考虑使用内存分析工具"
            } else if conclusion.title.contains("IO") {
                "检查 IO 密集型进程、调整 IO 权重或限制"
            } else {
                "综合分析资源使用模式，优化容器资源配置"
            }
        }
        "oom_events" => {
            "紧急扩容内存限制、检查内存泄漏、优化内存使用模式"
        }
        _ => "查看相关监控指标，定位异常根因",
    };

    let priority = (10 - conclusion.severity.unwrap_or(5)) as u32;

    Recommendation {
        priority,
        action: action.to_string(),
        expected_impact: Some("降低延迟/恢复服务正常".to_string()),
        verification: Some("重新运行诊断任务，确认指标恢复正常".to_string()),
    }
}

/// 构建可追溯性信息
fn build_traceability(conclusions: &[Conclusion], evidence_refs: &[EvidenceRef]) -> Traceability {
    let mut references = Vec::new();

    for conclusion in conclusions {
        // 找到支持该结论的证据
        let supporting_evidence: Vec<String> = evidence_refs
            .iter()
            .filter(|e| e.role.as_deref() == Some("primary"))
            .map(|e| e.evidence_id.clone())
            .collect();

        references.push(TraceabilityRef {
            conclusion_id: conclusion.conclusion_id.clone(),
            evidence_ids: supporting_evidence,
            reasoning_summary: Some(format!(
                "基于 {} 的阈值规则触发",
                conclusion.title
            )),
        });
    }

    Traceability {
        references,
        engine_version: Some("rules.v0.1.poc".to_string()),
    }
}

impl Default for RuleEngine {
    fn default() -> Self {
        Self::new()
    }
}
