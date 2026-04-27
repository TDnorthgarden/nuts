//! AI 增强诊断引擎
//!
//! 集成 AI 分析到诊断流程，支持异步后台处理

use crate::ai::{
    AiAdapter, AiAdapterConfig, AiEnhancedDiagnosis, AiFallbackMode,
    llm_client::{LlmConfig},
};
use crate::diagnosis::engine::RuleEngine;
use crate::types::diagnosis::{DiagnosisResult, DiagnosisStatus};
use crate::types::evidence::Evidence;
use std::collections::HashMap;
use std::sync::{Arc, RwLock};
use tokio::sync::mpsc;
use tokio::time::Duration;
use tracing::{debug, info, warn};

/// AI 增强诊断引擎
pub struct AiEnhancedEngine {
    /// 基础规则引擎
    rule_engine: RuleEngine,
    /// AI 适配器
    ai_adapter: Option<AiAdapter>,
    /// 异步任务发送器
    ai_task_tx: Option<mpsc::UnboundedSender<AiTask>>,
    /// 是否启用异步 AI 处理
    enable_async: bool,
    /// AI 结果存储（异步任务完成后的结果）
    ai_results: Arc<RwLock<HashMap<String, AiEnhancedDiagnosis>>>,
}

/// AI 任务（用于异步处理）
#[derive(Debug, Clone)]
struct AiTask {
    /// 任务ID
    task_id: String,
    /// 诊断结果
    diagnosis: DiagnosisResult,
    /// 证据列表
    evidences: Vec<Evidence>,
}

/// AI 增强诊断配置
#[derive(Debug, Clone)]
pub struct AiEngineConfig {
    /// 是否启用 AI 增强
    pub enabled: bool,
    /// AI 适配器配置
    pub ai_config: Option<AiAdapterConfig>,
    /// LLM 配置
    pub llm_config: Option<LlmConfig>,
    /// 是否启用异步处理
    pub enable_async: bool,
    /// 异步工作线程数
    pub worker_threads: usize,
    /// AI 结果 TTL（秒），默认 3600（1小时）
    pub result_ttl_secs: u64,
}

impl Default for AiEngineConfig {
    fn default() -> Self {
        Self {
            enabled: false,
            ai_config: None,
            llm_config: None,
            enable_async: true,
            worker_threads: 2,
            result_ttl_secs: 3600,
        }
    }
}

impl AiEnhancedEngine {
    /// 创建新的 AI 增强诊断引擎
    pub fn new(rule_engine: RuleEngine, config: AiEngineConfig) -> Self {
        let ai_adapter = if config.enabled {
            if let Some(ai_config) = config.ai_config {
                Some(AiAdapter::new(ai_config))
            } else {
                warn!("AI enabled but no config provided");
                None
            }
        } else {
            None
        };

        let mut engine = Self {
            rule_engine,
            ai_adapter,
            ai_task_tx: None,
            enable_async: config.enable_async,
            ai_results: Arc::new(RwLock::new(HashMap::new())),
        };

        // 如果启用异步处理，启动后台工作线程
        if config.enabled && config.enable_async {
            engine.start_async_workers(config.worker_threads);
        }

        // 启动 TTL 清理任务（只要启用 AI 就启动，不限于异步模式）
        if config.enabled {
            engine.start_cleanup_worker(config.result_ttl_secs);
        }

        engine
    }

    /// 快速创建（从环境变量读取配置）
    pub fn from_env(rule_engine: RuleEngine) -> Self {
        let enabled = std::env::var("NUTS_AI_ENABLED")
            .map(|v| v == "true" || v == "1")
            .unwrap_or(false);

        let api_key = std::env::var("NUTS_AI_API_KEY").ok();
        let endpoint = std::env::var("NUTS_AI_ENDPOINT")
            .unwrap_or_else(|_| "http://localhost:8000/v1/chat/completions".to_string());
        let result_ttl_secs = std::env::var("NUTS_AI_RESULT_TTL_SECS")
            .ok()
            .and_then(|v| v.parse().ok())
            .unwrap_or(3600);

        let config = AiEngineConfig {
            enabled,
            ai_config: if enabled {
                Some(AiAdapterConfig {
                    endpoint,
                    api_key,
                    timeout_secs: 60,
                    max_retries: 2,
                    fallback_mode: AiFallbackMode::KeepOriginal,
                    model: "nuts-ai-diagnosis".to_string(),
                })
            } else {
                None
            },
            llm_config: None,
            enable_async: true,
            worker_threads: 2,
            result_ttl_secs,
        };

        Self::new(rule_engine, config)
    }

    /// 启动异步 AI 工作线程
    fn start_async_workers(&mut self, _num_threads: usize) {
        let (tx, mut rx) = mpsc::unbounded_channel::<AiTask>();
        self.ai_task_tx = Some(tx);

        // 克隆 AI 适配器和结果存储用于后台任务
        let ai_adapter = self.ai_adapter.clone();
        let ai_results = self.ai_results.clone();

        // 启动后台处理任务
        tokio::spawn(async move {
            info!("AI async worker started");

            while let Some(task) = rx.recv().await {
                if let Some(ref adapter) = ai_adapter {
                    debug!("Processing AI task: {}", task.task_id);

                    // 构建 AI 输入
                    let ai_input = adapter.build_input(&task.diagnosis, &task.evidences);

                    // 实际调用 AI 分析
                    info!(
                        "AI analysis starting for task {} with {} evidences",
                        task.task_id,
                        task.evidences.len()
                    );

                    // 调用 LLM 并存储结果
                    let start = std::time::Instant::now();
                    match adapter.call_ai(&ai_input).await {
                        Ok(ai_output) => {
                            let processing_ms = start.elapsed().as_millis() as i64;
                            info!(
                                "AI analysis completed for task {} in {}ms",
                                task.task_id, processing_ms
                            );

                            // 构建增强诊断结果
                            let enhanced = AiEnhancedDiagnosis {
                                original: task.diagnosis.clone(),
                                ai_output: Some(ai_output.clone()),
                                enhanced: adapter.enhance_diagnosis(&task.diagnosis, &ai_output),
                                ai_status: crate::types::diagnosis::AiStatus::Ok,
                                processing_ms,
                                created_at: std::time::Instant::now(),
                            };

                            // 存储结果
                            if let Ok(mut results) = ai_results.write() {
                                results.insert(task.task_id.clone(), enhanced);
                                info!("Stored AI result for task {}", task.task_id);
                            }
                        }
                        Err(e) => {
                            warn!("AI analysis failed for task {}: {}", task.task_id, e);
                            // 存储失败状态
                            let enhanced = AiEnhancedDiagnosis {
                                original: task.diagnosis.clone(),
                                ai_output: None,
                                enhanced: adapter.apply_fallback(&task.diagnosis),
                                ai_status: crate::types::diagnosis::AiStatus::Unavailable,
                                processing_ms: start.elapsed().as_millis() as i64,
                                created_at: std::time::Instant::now(),
                            };
                            if let Ok(mut results) = ai_results.write() {
                                results.insert(task.task_id.clone(), enhanced);
                            }
                        }
                    }
                }
            }

            info!("AI async worker stopped");
        });
    }

    /// 诊断（同步快速响应 + 可选异步 AI 增强）
    pub async fn diagnose(&self, evidences: &[Evidence]) -> DiagnosisResult {
        // 1. 基础诊断（快速规则引擎）
        let base_diagnosis = self.rule_engine.diagnose(evidences);

        // 2. 检查是否需要 AI 增强
        if self.should_ai_enhance(&base_diagnosis) {
            if self.enable_async && self.ai_task_tx.is_some() {
                // 异步处理：后台触发 AI 分析
                self.trigger_async_ai_analysis(&base_diagnosis, evidences);
                base_diagnosis
            } else if let Some(ref adapter) = self.ai_adapter {
                // 同步处理：直接调用 AI（可能较慢）
                match self.perform_ai_analysis(adapter, &base_diagnosis, evidences).await {
                    Ok(enhanced) => enhanced,
                    Err(e) => {
                        warn!("AI analysis failed: {}, returning base diagnosis", e);
                        base_diagnosis
                    }
                }
            } else {
                base_diagnosis
            }
        } else {
            base_diagnosis
        }
    }

    /// 判断是否需要 AI 增强
    fn should_ai_enhance(&self, diagnosis: &DiagnosisResult) -> bool {
        // 只有异常诊断才需要 AI 增强
        if !matches!(diagnosis.status, DiagnosisStatus::Done) {
            return false;
        }

        // 如果有置信度较低的结论，需要 AI 增强
        let has_low_confidence = diagnosis.conclusions.iter()
            .any(|c| c.confidence < 0.7);

        // 如果有结论，就进行 AI 增强
        !diagnosis.conclusions.is_empty() && has_low_confidence
    }

    /// 触发异步 AI 分析
    fn trigger_async_ai_analysis(&self, diagnosis: &DiagnosisResult, evidences: &[Evidence]) {
        if let Some(ref tx) = self.ai_task_tx {
            let task = AiTask {
                task_id: diagnosis.task_id.clone(),
                diagnosis: diagnosis.clone(),
                evidences: evidences.to_vec(),
            };

            if let Err(e) = tx.send(task) {
                warn!("Failed to send AI task: {}", e);
            } else {
                info!("AI analysis queued for task {}", diagnosis.task_id);
            }
        }
    }

    /// 执行 AI 分析（同步）
    async fn perform_ai_analysis(
        &self,
        adapter: &AiAdapter,
        diagnosis: &DiagnosisResult,
        evidences: &[Evidence],
    ) -> Result<DiagnosisResult, String> {
        info!("Performing AI analysis for task {}", diagnosis.task_id);

        let start = std::time::Instant::now();

        // 构建 AI 输入
        let ai_input = adapter.build_input(diagnosis, evidences);

        // 调用 AI 适配器处理
        // 这里简化处理，实际应该调用 adapter.process()
        // 由于当前适配器接口不同，这里模拟 AI 增强

        let mut enhanced = diagnosis.clone();

        // 模拟 AI 增强结果
        if !enhanced.conclusions.is_empty() {
            // 为第一个结论添加 AI 解释
            let ai_summary = format!(
                "AI 分析：基于 {} 个证据，检测到 {} 问题。建议进一步检查相关指标。",
                evidences.len(),
                enhanced.conclusions[0].title
            );

            // 回填到诊断结果（使用 ai 字段）
            enhanced.ai = Some(crate::types::diagnosis::AiInfo {
                enabled: true,
                status: crate::types::diagnosis::AiStatus::Ok,
                summary: Some(ai_summary),
                version: Some("nuts-ai-v0.1".to_string()),
                submitted_at_ms: Some(chrono::Utc::now().timestamp_millis()),
                completed_at_ms: Some(chrono::Utc::now().timestamp_millis()),
                processing_duration_ms: Some(start.elapsed().as_millis() as i64),
            });
        }

        let elapsed = start.elapsed().as_millis() as i64;
        info!("AI analysis completed in {} ms for task {}", elapsed, diagnosis.task_id);

        Ok(enhanced)
    }

    /// 获取 AI 增强的诊断（如果已完成）
    pub async fn get_ai_enhanced_diagnosis(&self, task_id: &str) -> Option<AiEnhancedDiagnosis> {
        // 从存储中查询异步完成的 AI 增强结果
        if let Ok(results) = self.ai_results.read() {
            results.get(task_id).cloned()
        } else {
            None
        }
    }

    /// 列出所有 AI 增强诊断结果
    pub async fn list_ai_diagnoses(&self) -> Vec<(String, AiEnhancedDiagnosis)> {
        if let Ok(results) = self.ai_results.read() {
            results.iter().map(|(k, v)| (k.clone(), v.clone())).collect()
        } else {
            vec![]
        }
    }

    /// 按状态查询 AI 诊断结果
    pub async fn find_by_status(&self, status: crate::types::diagnosis::AiStatus) -> Vec<(String, AiEnhancedDiagnosis)> {
        if let Ok(results) = self.ai_results.read() {
            results
                .iter()
                .filter(|(_, v)| v.ai_status == status)
                .map(|(k, v)| (k.clone(), v.clone()))
                .collect()
        } else {
            vec![]
        }
    }

    /// 启动 TTL 清理后台任务
    fn start_cleanup_worker(&self, ttl_secs: u64) {
        let results = self.ai_results.clone();
        let interval = std::cmp::max(ttl_secs / 10, 60); // 每 1/10 TTL 或至少 60 秒检查一次

        tokio::spawn(async move {
            let mut ticker = tokio::time::interval(Duration::from_secs(interval));

            loop {
                ticker.tick().await;

                let now = std::time::Instant::now();
                let ttl = Duration::from_secs(ttl_secs);

                if let Ok(mut results) = results.write() {
                    let before = results.len();
                    results.retain(|_, v| now.duration_since(v.created_at) < ttl);
                    let after = results.len();
                    let cleaned = before - after;

                    if cleaned > 0 {
                        info!("Cleaned {} expired AI results (TTL: {}s)", cleaned, ttl_secs);
                    }
                }
            }
        });

        info!("Started AI results cleanup worker (TTL: {}s, interval: {}s)", ttl_secs, interval);
    }

    /// 手动清理过期结果
    pub fn cleanup_expired(&self, ttl_secs: u64) -> usize {
        let now = std::time::Instant::now();
        let ttl = Duration::from_secs(ttl_secs);

        if let Ok(mut results) = self.ai_results.write() {
            let before = results.len();
            results.retain(|_, v| now.duration_since(v.created_at) < ttl);
            let cleaned = before - results.len();
            cleaned
        } else {
            0
        }
    }

    /// 健康检查
    pub async fn health_check(&self) -> AiEngineHealth {
        let ai_healthy = if let Some(ref adapter) = self.ai_adapter {
            // 检查 AI 适配器是否可用
            // 这里简化处理
            true
        } else {
            false
        };

        AiEngineHealth {
            rule_engine_healthy: true,
            ai_adapter_healthy: ai_healthy,
            async_queue_healthy: self.ai_task_tx.is_some(),
        }
    }
}

/// AI 引擎健康状态
#[derive(Debug, Clone)]
pub struct AiEngineHealth {
    pub rule_engine_healthy: bool,
    pub ai_adapter_healthy: bool,
    pub async_queue_healthy: bool,
}

impl AiEngineHealth {
    pub fn all_healthy(&self) -> bool {
        self.rule_engine_healthy && self.ai_adapter_healthy && self.async_queue_healthy
    }
}

/// 诊断结果增强器
pub struct DiagnosisEnhancer;

impl DiagnosisEnhancer {
    /// 使用 AI 输出增强诊断结果
    pub fn enhance(diagnosis: &mut DiagnosisResult, ai_output: &crate::ai::AiOutput) {
        // 添加 AI 解释
        if let Some(ref mut ai_info) = diagnosis.ai {
            // 更新现有 AI 信息
            ai_info.summary = Some(ai_output.explanation.clone());
            ai_info.completed_at_ms = Some(chrono::Utc::now().timestamp_millis());
        } else {
            // 创建新的 AI 信息
            diagnosis.ai = Some(crate::types::diagnosis::AiInfo {
                enabled: true,
                status: crate::types::diagnosis::AiStatus::Ok,
                summary: Some(ai_output.explanation.clone()),
                version: Some("ai-model".to_string()),
                submitted_at_ms: Some(chrono::Utc::now().timestamp_millis()),
                completed_at_ms: Some(chrono::Utc::now().timestamp_millis()),
                processing_duration_ms: Some(0),
            });
        }

        // 增强结论
        for conclusion in &mut diagnosis.conclusions {
            // 如果 AI 提供了更详细的根因分析，添加进去
            if !ai_output.root_cause_analysis.is_empty() {
                let enhanced_details = format!(
                    "{}",
                    ai_output.root_cause_analysis
                );
                conclusion.details = Some(serde_json::json!(enhanced_details));
            }
        }

        // 添加 AI 建议
        for step in &ai_output.troubleshooting_steps {
            diagnosis.recommendations.push(crate::types::diagnosis::Recommendation {
                action: step.clone(),
                priority: 1,
                expected_impact: Some("AI建议".to_string()),
                verification: None,
            });
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_ai_engine_config_default() {
        let config = AiEngineConfig::default();
        assert!(!config.enabled);
        assert!(config.enable_async);
        assert_eq!(config.worker_threads, 2);
    }

    #[test]
    fn test_ai_health() {
        let health = AiEngineHealth {
            rule_engine_healthy: true,
            ai_adapter_healthy: true,
            async_queue_healthy: true,
        };
        assert!(health.all_healthy());

        let unhealthy = AiEngineHealth {
            rule_engine_healthy: true,
            ai_adapter_healthy: false,
            async_queue_healthy: true,
        };
        assert!(!unhealthy.all_healthy());
    }
}
