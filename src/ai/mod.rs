//! AI 适配层 - 将诊断证据转换为 AI 可理解的输入格式，并处理 AI 输出回填
//!
//! 核心功能：
//! 1. 将 Evidence + DiagnosisResult 转换为 AI 入参（结构化提示词）
//! 2. 解析 AI 输出并回填到诊断结果
//! 3. 支持降级策略（AI 不可用时保持核心链路）
//! 4. 异步AI增强（后台处理，不阻塞主链路）

pub mod async_bridge;
pub mod llm_client;

// 重新导出常用类型（AiEnhancedDiagnosis 在本模块定义）
pub use async_bridge::{AiResultStore, AiTask, AiTaskQueue};

use crate::types::diagnosis::{DiagnosisResult, Recommendation, AiStatus};
use crate::types::evidence::Evidence;
use serde::{Deserialize, Serialize};
use serde_json::json;
use std::time::Duration;

/// AI 适配器配置
#[derive(Debug, Clone)]
pub struct AiAdapterConfig {
    /// AI 服务端点
    pub endpoint: String,
    /// API 密钥
    pub api_key: Option<String>,
    /// 请求超时（秒）
    pub timeout_secs: u64,
    /// 最大重试次数
    pub max_retries: u32,
    /// 降级模式：当 AI 不可用时如何处理
    pub fallback_mode: AiFallbackMode,
    /// 模型名称
    pub model: String,
}

impl Default for AiAdapterConfig {
    fn default() -> Self {
        Self {
            endpoint: "http://localhost:8000/v1/chat/completions".to_string(),
            api_key: None,
            timeout_secs: 60,
            max_retries: 2,
            fallback_mode: AiFallbackMode::KeepOriginal,
            model: "nuts-ai-diagnosis".to_string(),
        }
    }
}

/// AI 降级模式
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum AiFallbackMode {
    /// 保留原始诊断结果，仅添加 AI 解释（推荐）
    KeepOriginal,
    /// 降低置信度标记
    ReduceConfidence,
    /// 标记为待人工审核
    MarkForReview,
}

/// AI 适配器
#[derive(Clone)]
pub struct AiAdapter {
    config: AiAdapterConfig,
}

/// AI 输入（提示词上下文）
#[derive(Debug, Serialize)]
pub struct AiInput {
    /// 系统提示词
    pub system_prompt: String,
    /// 用户提示词（结构化证据和诊断）
    pub user_prompt: String,
    /// 证据列表（JSON 格式）
    pub evidence_context: serde_json::Value,
    /// 诊断结果（JSON 格式）
    pub diagnosis_context: serde_json::Value,
    /// 任务元数据
    pub metadata: AiInputMetadata,
}

/// AI 输入元数据
#[derive(Debug, Serialize)]
pub struct AiInputMetadata {
    pub task_id: String,
    pub schema_version: String,
    pub evidence_types: Vec<String>,
    pub target_pod: Option<String>,
    pub time_window_ms: i64,
}

/// AI 输出结果
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AiOutput {
    /// AI 生成的解释
    pub explanation: String,
    /// 建议的排查路径
    pub troubleshooting_steps: Vec<String>,
    /// 根因分析
    pub root_cause_analysis: String,
    /// 置信度评估（AI 对结论的置信度）
    pub ai_confidence: f64,
    /// 需要关注的额外指标
    pub suggested_metrics: Vec<String>,
    /// 推荐工具或命令
    pub suggested_commands: Vec<String>,
}

/// 聊天完成响应结构（支持 OpenAI 和本地模型格式）
#[derive(Debug, Deserialize)]
struct ChatCompletionResponse {
    #[serde(skip_serializing_if = "Option::is_none")]
    id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    object: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    created: Option<i64>,
    model: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    choices: Option<Vec<ChatCompletionChoice>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    output: Option<String>,  // 本地模型可能直接返回 output
    #[serde(skip_serializing_if = "Option::is_none")]
    content: Option<String>, // 本地模型可能直接返回 content
    #[serde(skip_serializing_if = "Option::is_none")]
    usage: Option<ChatCompletionUsage>,
}

#[derive(Debug, Deserialize)]
struct ChatCompletionChoice {
    index: Option<i32>,
    message: Option<ChatCompletionMessage>,
    #[serde(skip_serializing_if = "Option::is_none")]
    finish_reason: Option<String>,
}

#[derive(Debug, Deserialize)]
struct ChatCompletionMessage {
    #[serde(skip_serializing_if = "Option::is_none")]
    role: Option<String>,
    content: Option<String>,
}

#[derive(Debug, Deserialize)]
struct ChatCompletionUsage {
    #[serde(skip_serializing_if = "Option::is_none")]
    prompt_tokens: Option<i32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    completion_tokens: Option<i32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    total_tokens: Option<i32>,
}

/// AI 增强后的诊断结果
#[derive(Debug, Clone)]
pub struct AiEnhancedDiagnosis {
    /// 原始诊断结果
    pub original: DiagnosisResult,
    /// AI 输出
    pub ai_output: Option<AiOutput>,
    /// 增强后的诊断（合并 AI 建议）
    pub enhanced: DiagnosisResult,
    /// AI 调用状态
    pub ai_status: AiStatus,
    /// 处理耗时（毫秒）
    pub processing_ms: i64,
    /// 创建时间戳（用于 TTL）
    pub created_at: std::time::Instant,
}

impl AiAdapter {
    /// 创建新的 AI 适配器
    pub fn new(config: AiAdapterConfig) -> Self {
        Self { config }
    }

    /// 构建 AI 输入（提示词工程）
    /// 
    /// 将证据和诊断结果转换为 AI 可理解的结构化提示词
    pub fn build_input(&self, diagnosis: &DiagnosisResult, evidences: &[Evidence]) -> AiInput {
        let system_prompt = self.build_system_prompt();
        let user_prompt = self.build_user_prompt(diagnosis, evidences);

        let evidence_context = serde_json::to_value(evidences)
            .unwrap_or_else(|_| json!({"error": "serialization failed"}));
        
        let diagnosis_context = serde_json::to_value(diagnosis)
            .unwrap_or_else(|_| json!({"error": "serialization failed"}));

        let evidence_types: Vec<String> = evidences
            .iter()
            .map(|e| e.evidence_type.clone())
            .collect();

        // 注意：time_window 在 Evidence 中，不在 DiagnosisResult 中
        // 这里使用默认值或从证据中计算
        let time_window_ms = 5000; // 默认 5 秒

        let metadata = AiInputMetadata {
            task_id: diagnosis.task_id.clone(),
            schema_version: "ai.v0.1".to_string(),
            evidence_types,
            target_pod: diagnosis.evidence_refs.first()
                .and_then(|r| r.scope_key.clone()),
            time_window_ms,
        };

        AiInput {
            system_prompt,
            user_prompt,
            evidence_context,
            diagnosis_context,
            metadata,
        }
    }

    /// 构建系统提示词
    fn build_system_prompt(&self) -> String {
        r#"你是一个专业的容器故障诊断专家。你的任务是分析系统采集的观测证据和诊断引擎的初步结论，提供详细的故障解释、根因分析和排查建议。

你需要：
1. 解释当前的诊断结论（为什么这些证据指向这些结论）
2. 分析可能的根因（从系统调用、I/O、网络等多维度）
3. 提供可执行的排查步骤（具体的命令或工具）
4. 指出需要额外关注的指标或日志
5. 评估当前结论的可信度

输出格式必须是 JSON：
{
    "explanation": "详细的故障解释",
    "troubleshooting_steps": ["步骤1", "步骤2"],
    "root_cause_analysis": "根因分析",
    "ai_confidence": 0.85,
    "suggested_metrics": ["metric1", "metric2"],
    "suggested_commands": ["command1", "command2"]
}

注意：
- ai_confidence 必须在 0.0 到 1.0 之间
- troubleshooting_steps 必须可执行
- suggested_commands 应该是 Linux 命令行可运行的命令
"#.to_string()
    }

    /// 构建用户提示词
    fn build_user_prompt(&self, diagnosis: &DiagnosisResult, evidences: &[Evidence]) -> String {
        let mut prompt = format!(
            "## 诊断任务\n\n任务 ID: {}\n",
            diagnosis.task_id
        );

        // 添加证据概览
        prompt.push_str("\n### 采集的证据\n\n");
        for evidence in evidences {
            prompt.push_str(&format!(
                "- **{}** (scope: {})\n",
                evidence.evidence_type,
                evidence.scope.scope_key
            ));
            
            // 关键指标
            if !evidence.metric_summary.is_empty() {
                prompt.push_str("  - 指标: ");
                let metrics: Vec<String> = evidence.metric_summary
                    .iter()
                    .map(|(k, v)| format!("{}={:.2}", k, v))
                    .collect();
                prompt.push_str(&metrics.join(", "));
                prompt.push('\n');
            }

            // 事件
            if !evidence.events_topology.is_empty() {
                prompt.push_str("  - 事件: ");
                let events: Vec<String> = evidence.events_topology
                    .iter()
                    .map(|e| format!("{}(severity={})", e.event_type, e.severity.unwrap_or(0)))
                    .collect();
                prompt.push_str(&events.join(", "));
                prompt.push('\n');
            }
        }

        // 添加诊断引擎结论
        prompt.push_str("\n### 诊断引擎结论\n\n");
        for (i, conclusion) in diagnosis.conclusions.iter().enumerate() {
            prompt.push_str(&format!(
                "{}. {} (置信度: {:.2})\n",
                i + 1,
                conclusion.title,
                conclusion.confidence
            ));
            if let Some(details) = &conclusion.details {
                prompt.push_str(&format!("   详情: {}\n", details));
            }
        }

        // 添加建议
        prompt.push_str("\n### 建议\n\n");
        for (i, rec) in diagnosis.recommendations.iter().enumerate() {
            prompt.push_str(&format!(
                "{}. {} (优先级: {})\n",
                i + 1,
                rec.action,
                rec.priority
            ));
        }

        prompt.push_str("\n---\n\n请基于以上信息，提供你的分析和建议。");
        prompt
    }

    /// 调用 AI 服务（OpenAI 格式兼容）
    /// 
    /// 支持 OpenAI API 格式及兼容的 LLM 服务（如 vLLM、LocalAI 等）
    /// 
    /// 注意：本地模型（如 llama.cpp、text-generation-inference）
    /// 通常不需要 API key，此时 api_key 应为 None
    pub async fn call_ai(&self, input: &AiInput) -> Result<AiOutput, AiError> {
        // 构建 HTTP 客户端
        let client = reqwest::Client::builder()
            .timeout(Duration::from_secs(self.config.timeout_secs))
            .build()
            .map_err(|e| AiError::HttpError(format!("Failed to create HTTP client: {}", e)))?;

        // 构建请求体（本地模型格式 - input/system_prompt）
        let request_body = json!({
            "model": self.config.model,
            "system_prompt": &input.system_prompt,
            "input": &input.user_prompt,
            "temperature": 0.3,
        });

        // 构建请求（本地模型不需要 API key）
        let mut request_builder = client
            .post(&self.config.endpoint)
            .header("Content-Type", "application/json")
            .json(&request_body);
        
        // 如果配置了 API key，添加认证头
        if let Some(ref api_key) = self.config.api_key {
            request_builder = request_builder.header("Authorization", format!("Bearer {}", api_key));
        }

        // 发送请求
        let response = request_builder
            .send()
            .await
            .map_err(|e| {
                if e.is_timeout() {
                    AiError::Timeout
                } else {
                    AiError::HttpError(format!("Request failed: {}", e))
                }
            })?;

        // 检查响应状态
        let status = response.status();
        if !status.is_success() {
            let error_text = response.text().await.unwrap_or_default();
            return Err(AiError::HttpError(format!(
                "AI service returned error (HTTP {}): {}",
                status, error_text
            )));
        }

        // 解析响应
        let chat_response: ChatCompletionResponse = response
            .json()
            .await
            .map_err(|e| AiError::SerializationError(format!("Failed to parse response: {}", e)))?;

        // 提取 AI 回复内容（支持多种响应格式）
        let content = chat_response
            .output  // 本地模型格式：直接返回 output
            .or(chat_response.content)  // 或者 content 字段
            .or_else(|| {
                // OpenAI 格式：从 choices[0].message.content 提取
                chat_response.choices.as_ref().and_then(|choices| {
                    choices.first().and_then(|c| {
                        c.message.as_ref().and_then(|m| m.content.clone())
                    })
                })
            })
            .ok_or_else(|| AiError::InvalidResponse("Empty response from AI".to_string()))?;

        // 解析 JSON 格式的 AI 输出
        let ai_output: AiOutput = serde_json::from_str(&content)
            .map_err(|e| AiError::InvalidResponse(format!(
                "AI response is not valid JSON: {}. Raw content: {}",
                e, content
            )))?;

        tracing::info!(
            "AI call successful for task {} (confidence: {})",
            input.metadata.task_id,
            ai_output.ai_confidence
        );

        Ok(ai_output)
    }

    /// 增强诊断结果（合并 AI 建议）
    /// 
    /// 将 AI 输出合并到原始诊断结果中
    pub fn enhance_diagnosis(&self, original: &DiagnosisResult, ai_output: &AiOutput) -> DiagnosisResult {
        let mut enhanced = original.clone();

        // 更新每个结论，添加 AI 解释
        for conclusion in &mut enhanced.conclusions {
            // 将 AI 解释添加到 details
            let ai_explanation = json!({
                "ai_explanation": ai_output.explanation,
                "ai_confidence": ai_output.ai_confidence,
            });
            
            if let Some(existing) = &conclusion.details {
                // 合并现有详情和 AI 解释
                let mut merged = existing.clone();
                if let Some(obj) = merged.as_object_mut() {
                    obj.insert("ai_enhancement".to_string(), ai_explanation);
                }
                conclusion.details = Some(merged);
            } else {
                // 创建新的 details 对象，包含 ai_enhancement 键
                let wrapper = json!({
                    "ai_enhancement": ai_explanation
                });
                conclusion.details = Some(wrapper);
            }
        }

        // 添加 AI 推荐的排查步骤作为建议
        for step in &ai_output.troubleshooting_steps {
            enhanced.recommendations.push(Recommendation {
                priority: 5, // 中等优先级
                action: step.clone(),
                expected_impact: Some("辅助排查".to_string()),
                verification: Some("按照步骤执行后观察指标变化".to_string()),
            });
        }

        enhanced
    }

    /// 处理诊断（带 AI 增强）
    /// 
    /// 完整的 AI 增强流程：
    /// 1. 构建输入 -> 2. 调用 AI -> 3. 合并结果 -> 4. 返回增强结果
    pub async fn process(&self, diagnosis: &DiagnosisResult, evidences: &[Evidence]) -> AiEnhancedDiagnosis {
        let start = chrono::Utc::now().timestamp_millis();

        // 构建输入
        let input = self.build_input(diagnosis, evidences);

        // 调用 AI
        match self.call_ai(&input).await {
            Ok(ai_output) => {
                // 成功：合并结果
                let enhanced = self.enhance_diagnosis(diagnosis, &ai_output);
                let processing_ms = chrono::Utc::now().timestamp_millis() - start;
                
                AiEnhancedDiagnosis {
                    original: diagnosis.clone(),
                    ai_output: Some(ai_output),
                    enhanced,
                    ai_status: AiStatus::Ok,
                    processing_ms,
                    created_at: std::time::Instant::now(),
                }
            }
            Err(_) => {
                // 失败：降级处理
                let enhanced = self.apply_fallback(diagnosis);
                let processing_ms = chrono::Utc::now().timestamp_millis() - start;
                
                AiEnhancedDiagnosis {
                    original: diagnosis.clone(),
                    ai_output: None,
                    enhanced,
                    ai_status: AiStatus::Unavailable,
                    processing_ms,
                    created_at: std::time::Instant::now(),
                }
            }
        }
    }

    /// 应用降级策略
    pub fn apply_fallback(&self, diagnosis: &DiagnosisResult) -> DiagnosisResult {
        let mut fallback = diagnosis.clone();

        match self.config.fallback_mode {
            AiFallbackMode::KeepOriginal => {
                // 什么都不做，保留原始结果
            }
            AiFallbackMode::ReduceConfidence => {
                // 降低所有结论的置信度
                for conclusion in &mut fallback.conclusions {
                    conclusion.confidence *= 0.8; // 降低 20%
                }
            }
            AiFallbackMode::MarkForReview => {
                // 添加标记到 traceability
                // 实际实现可以添加一个标记字段
            }
        }

        fallback
    }
}

/// AI 错误
#[derive(Debug)]
pub enum AiError {
    HttpError(String),
    Timeout,
    InvalidResponse(String),
    SerializationError(String),
}

impl std::fmt::Display for AiError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            AiError::HttpError(s) => write!(f, "HTTP error: {}", s),
            AiError::Timeout => write!(f, "Request timeout"),
            AiError::InvalidResponse(s) => write!(f, "Invalid response: {}", s),
            AiError::SerializationError(s) => write!(f, "Serialization error: {}", s),
        }
    }
}

impl std::error::Error for AiError {}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::types::diagnosis::Conclusion;
    use crate::types::evidence::{PodInfo, TimeWindow, Scope, Attribution, CollectionMeta};
    use crate::types::diagnosis::EvidenceStrength;

    fn create_test_evidence() -> Evidence {
        Evidence {
            schema_version: "evidence.v0.2".to_string(),
            task_id: "test-task".to_string(),
            evidence_id: "test-evidence".to_string(),
            evidence_type: "block_io".to_string(),
            collection: CollectionMeta {
                collection_id: "test-collection".to_string(),
                collection_status: "success".to_string(),
                probe_id: "test-probe".to_string(),
                errors: vec![],
            },
            time_window: TimeWindow {
                start_time_ms: 1000,
                end_time_ms: 2000,
                collection_interval_ms: None,
            },
            scope: Scope {
                pod: Some(PodInfo {
                    uid: Some("pod-123".to_string()),
                    name: Some("test-pod".to_string()),
                    namespace: Some("default".to_string()),
                }),
                container_id: None,
                cgroup_id: Some("cgroup-123".to_string()),
                pid_scope: None,
                scope_key: "test-scope".to_string(),
                network_target: None,
            },
            selection: None,
            metric_summary: {
                let mut m = std::collections::HashMap::new();
                m.insert("io_latency_p99_ms".to_string(), 150.0);
                m
            },
            events_topology: vec![],
            top_calls: None,
            attribution: Attribution {
                status: "nri_mapped".to_string(),
                confidence: Some(0.9),
                source: Some("nri".to_string()),
                mapping_version: None,
            },
        }
    }

    #[test]
    fn test_build_input() {
        let config = AiAdapterConfig::default();
        let adapter = AiAdapter::new(config);

        let evidence = create_test_evidence();
        let diagnosis = DiagnosisResult {
            schema_version: "diagnosis.v0.2".to_string(),
            task_id: "test-task".to_string(),
            status: crate::types::diagnosis::DiagnosisStatus::Done,
            runtime: None,
            trigger: crate::types::diagnosis::TriggerInfo {
                trigger_type: "manual".to_string(),
                trigger_reason: "test".to_string(),
                trigger_time_ms: 2000,
                matched_condition: None,
                event_type: None,
            },
            evidence_refs: vec![],
            conclusions: vec![Conclusion {
                conclusion_id: "con-1".to_string(),
                title: "I/O 延迟异常".to_string(),
                confidence: 0.85,
                evidence_strength: EvidenceStrength::High,
                severity: Some(8),
                details: None,
            }],
            recommendations: vec![],
            traceability: crate::types::diagnosis::Traceability {
                references: vec![],
                engine_version: None,
            },
            ai: None,
        };

        let input = adapter.build_input(&diagnosis, &[evidence]);

        assert_eq!(input.metadata.task_id, "test-task");
        assert!(input.system_prompt.contains("故障诊断"));
        assert!(input.user_prompt.contains("诊断任务"));
        assert!(!input.metadata.evidence_types.is_empty());
    }

    #[test]
    fn test_enhance_diagnosis() {
        let config = AiAdapterConfig::default();
        let adapter = AiAdapter::new(config);

        let ai_output = AiOutput {
            explanation: "AI 解释".to_string(),
            troubleshooting_steps: vec!["步骤1".to_string(), "步骤2".to_string()],
            root_cause_analysis: "根因".to_string(),
            ai_confidence: 0.8,
            suggested_metrics: vec![],
            suggested_commands: vec![],
        };

        let diagnosis = DiagnosisResult {
            schema_version: "diagnosis.v0.2".to_string(),
            task_id: "test-task".to_string(),
            status: crate::types::diagnosis::DiagnosisStatus::Done,
            runtime: None,
            trigger: crate::types::diagnosis::TriggerInfo {
                trigger_type: "manual".to_string(),
                trigger_reason: "test".to_string(),
                trigger_time_ms: 2000,
                matched_condition: None,
                event_type: None,
            },
            evidence_refs: vec![],
            conclusions: vec![Conclusion {
                conclusion_id: "con-1".to_string(),
                title: "I/O 延迟异常".to_string(),
                confidence: 0.85,
                evidence_strength: EvidenceStrength::High,
                severity: Some(8),
                details: None,
            }],
            recommendations: vec![],
            traceability: crate::types::diagnosis::Traceability {
                references: vec![],
                engine_version: None,
            },
            ai: None,
        };

        let enhanced = adapter.enhance_diagnosis(&diagnosis, &ai_output);

        // 验证 AI 建议被添加
        assert_eq!(enhanced.recommendations.len(), 2);
        assert_eq!(enhanced.recommendations[0].action, "步骤1");

        // 验证结论被增强
        let details = enhanced.conclusions[0].details.as_ref().unwrap();
        assert!(details.get("ai_enhancement").is_some());
    }
}
