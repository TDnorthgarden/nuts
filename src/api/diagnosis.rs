//! 诊断查询 API
//!
//! 提供 AI 增强诊断结果的查询接口：
//! - GET /v1/diagnosis/:id/ai - 查询指定诊断的 AI 增强结果
//! - GET /v1/diagnosis/ai-results - 列出所有 AI 增强结果

use axum::{
    extract::{Path, State, Query},
    routing::get,
    Json, Router,
};
use serde::{Deserialize, Serialize};
use std::sync::Arc;

use crate::ai::AiResultStore;

/// 诊断查询 API 状态
#[derive(Clone)]
pub struct DiagnosisApiState {
    /// AI 增强结果存储
    ai_store: Arc<AiResultStore>,
}

impl DiagnosisApiState {
    pub fn new(ai_store: Arc<AiResultStore>) -> Self {
        Self { ai_store }
    }
}

/// 创建诊断查询路由
pub fn router(state: Arc<DiagnosisApiState>) -> Router {
    Router::new()
        .route("/v1/diagnosis/:diagnosis_id/ai", get(get_ai_enhanced_handler))
        .route("/v1/diagnosis/ai-results", get(list_ai_results_handler))
        .with_state(state)
}

/// 查询 AI 增强诊断结果请求
#[derive(Debug, Deserialize)]
pub struct GetAiEnhancedQuery {
    /// 是否包含原始诊断详情
    include_original: Option<bool>,
}

/// AI 增强诊断结果响应
#[derive(Debug, Serialize)]
pub struct AiEnhancedResponse {
    pub diagnosis_id: String,
    pub ai_status: String,
    pub ai_output: Option<serde_json::Value>,
    pub enhanced_summary: String,
    pub processing_ms: i64,
    pub created_at: Option<String>,
}

/// 查询单个诊断的 AI 增强结果
async fn get_ai_enhanced_handler(
    State(state): State<Arc<DiagnosisApiState>>,
    Path(diagnosis_id): Path<String>,
    Query(query): Query<GetAiEnhancedQuery>,
) -> Json<serde_json::Value> {
    // 查询 AI 增强结果
    let enhanced = state.ai_store.get(&diagnosis_id).await;

    match enhanced {
        Some(enhanced) => {
            let include_original = query.include_original.unwrap_or(false);
            
            let mut response = serde_json::json!({
                "diagnosis_id": diagnosis_id,
                "ai_status": format!("{:?}", enhanced.ai_status),
                "ai_output": enhanced.ai_output.as_ref().map(|o| serde_json::json!({
                    "explanation": o.explanation,
                    "troubleshooting_steps": o.troubleshooting_steps,
                    "root_cause_analysis": o.root_cause_analysis,
                    "confidence": o.ai_confidence,
                    "suggested_metrics": o.suggested_metrics,
                    "suggested_commands": o.suggested_commands,
                })),
                "processing_ms": enhanced.processing_ms,
                "created_at": format!("{:?}", enhanced.created_at),
            });

            if include_original {
                response["original"] = serde_json::json!({
                    "task_id": enhanced.original.task_id,
                    "status": format!("{:?}", enhanced.original.status),
                });
            }

            Json(response)
        }
        None => {
            Json(serde_json::json!({
                "error": "AI enhanced result not found",
                "diagnosis_id": diagnosis_id,
                "status": "not_found"
            }))
        }
    }
}

/// 列出 AI 结果查询参数
#[derive(Debug, Deserialize)]
pub struct ListAiResultsQuery {
    /// 按状态过滤：ok, error, unavailable, processing
    status: Option<String>,
    /// 限制返回数量
    limit: Option<usize>,
}

/// AI 结果列表响应
#[derive(Debug, Serialize)]
pub struct AiResultsListResponse {
    pub results: Vec<AiEnhancedSummary>,
    pub total: usize,
}

/// AI 增强结果摘要
#[derive(Debug, Serialize)]
pub struct AiEnhancedSummary {
    pub diagnosis_id: String,
    pub ai_status: String,
    pub processing_ms: i64,
    pub created_at: String,
}

/// 列出所有 AI 增强结果
async fn list_ai_results_handler(
    State(state): State<Arc<DiagnosisApiState>>,
    Query(query): Query<ListAiResultsQuery>,
) -> Json<serde_json::Value> {
    let all_results = state.ai_store.list_all().await;
    
    let results: Vec<_> = all_results
        .into_iter()
        .filter(|(_, v)| {
            // 如果指定了状态过滤
            if let Some(ref status_filter) = query.status {
                let status_str = format!("{:?}", v.ai_status).to_lowercase();
                status_str == status_filter.to_lowercase()
            } else {
                true
            }
        })
        .map(|(k, v)| {
            serde_json::json!({
                "diagnosis_id": k,
                "ai_status": format!("{:?}", v.ai_status),
                "processing_ms": v.processing_ms,
                "created_at": format!("{:?}", v.created_at),
            })
        })
        .collect();

    let total = results.len();
    let limit = query.limit.unwrap_or(100);
    let limited_results: Vec<_> = results.into_iter().take(limit).collect();

    Json(serde_json::json!({
        "results": limited_results,
        "total": total,
        "returned": limited_results.len(),
    }))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ai::AiResultStore;

    fn create_test_state() -> Arc<DiagnosisApiState> {
        let ai_store = Arc::new(AiResultStore::new());
        Arc::new(DiagnosisApiState::new(ai_store))
    }
}
