use axum::{extract::{Json, State}, routing::post, Router};
use serde::Deserialize;
use std::sync::Arc;

use crate::ai::async_bridge::{AiTaskQueue, AiTask, AiTaskPriority};
use crate::collector::block_io::{run_block_io_collect_poc, BlockIoCollectorConfig};
use crate::collector::cgroup_contention::{run_cgroup_contention_collect_poc, CgroupContentionConfig};
use crate::collector::network::{run_network_collect_poc, NetworkCollectorConfig};
use crate::collector::nri_mapping::NriMappingTable;
use crate::collector::syscall_latency::{run_syscall_collect_poc, SyscallCollectorConfig};
use crate::collector::fs_stall::{run_fs_stall_collect_poc, FsStallCollectorConfig};
use crate::diagnosis::engine::RuleEngine;
use crate::publisher::ResultPublisher;
use crate::types::diagnosis::{DiagnosisResult, Conclusion, EvidenceStrength, DiagnosisStatus, Traceability};
use crate::types::evidence::{NetworkTarget, PodInfo, TimeWindow, Evidence};
use serde_json::json;

#[derive(Debug, Deserialize)]
pub struct TriggerRequest {
    pub trigger_type: String,
    pub target: Option<TriggerTarget>,
    pub time_window: TriggerTimeWindow,
    pub collection_options: Option<CollectionOptions>,
    pub idempotency_key: String,
}

#[derive(Debug, Deserialize)]
pub struct TriggerTarget {
    pub pod_uid: Option<String>,
    pub namespace: Option<String>,
    pub pod_name: Option<String>,
    pub cgroup_id: Option<String>,
    pub node: Option<String>,
    pub all: Option<bool>,
    pub network_target: Option<NetworkTarget>,
}

#[derive(Debug, Deserialize)]
pub struct TriggerTimeWindow {
    pub start_time_ms: i64,
    pub end_time_ms: i64,
}

#[derive(Debug, Deserialize)]
pub struct CollectionOptions {
    pub requested_evidence_types: Option<Vec<String>>,
    pub requested_metrics_by_type: Option<serde_json::Map<String, serde_json::Value>>,
    pub requested_events_by_type: Option<serde_json::Map<String, serde_json::Value>>,
    /// 目标 PID 列表（BPFtrace 采集时进行 PID 过滤）
    pub target_pids: Option<Vec<u32>>,
}

/// 创建触发器路由
/// 
/// 需要传入共享的 NriMappingTable 用于证据采集
/// 可选传入 AI 任务队列用于异步 AI 增强
pub fn router(nri_table: Arc<NriMappingTable>, ai_queue: Option<Arc<AiTaskQueue>>) -> Router {
    Router::new()
        .route("/v1/diagnostics:trigger", post(trigger_handler))
        .with_state((nri_table, ai_queue))
}

async fn trigger_handler(
    State((nri_table, ai_queue)): State<(Arc<NriMappingTable>, Option<Arc<AiTaskQueue>>)>,
    Json(req): Json<TriggerRequest>
) -> Json<serde_json::Value> {
    let task_id = uuid::Uuid::new_v4().to_string();
    let start_time = chrono::Utc::now().timestamp_millis();

    // 确定需要采集的证据类型
    let evidence_types = req.collection_options
        .as_ref()
        .and_then(|o| o.requested_evidence_types.clone())
        .unwrap_or_else(|| vec!["network".to_string(), "block_io".to_string()]);

    let mut evidences: Vec<Evidence> = Vec::new();

    // NRI 映射表已接入：通过 axum State 从全局状态传入
    let nri_table: Option<Arc<NriMappingTable>> = Some(nri_table);

    // 采集 network 证据
    if evidence_types.contains(&"network".to_string()) {
        let network_cfg = NetworkCollectorConfig {
            task_id: task_id.clone(),
            time_window: TimeWindow {
                start_time_ms: req.time_window.start_time_ms,
                end_time_ms: req.time_window.end_time_ms,
                collection_interval_ms: None,
            },
            pod: Some(PodInfo {
                uid: req.target.as_ref().and_then(|t| t.pod_uid.clone()),
                name: req.target.as_ref().and_then(|t| t.pod_name.clone()),
                namespace: req.target.as_ref().and_then(|t| t.namespace.clone()),
            }),
            container_id: None,
            cgroup_id: req.target.as_ref().and_then(|t| t.cgroup_id.clone()),
            network_target: req.target.as_ref().and_then(|t| t.network_target.clone()),
            requested_metrics: extract_string_list(&req.collection_options, "network", "requested_metrics_by_type"),
            requested_events: extract_string_list(&req.collection_options, "network", "requested_events_by_type"),
            nri_table: nri_table.clone(),
            target_pids: req.collection_options.as_ref().and_then(|o| o.target_pids.clone()),
        };

        let evidence = run_network_collect_poc(network_cfg);
        evidences.push(evidence);
    }

    // 采集 block_io 证据
    if evidence_types.contains(&"block_io".to_string()) {
        let block_io_cfg = BlockIoCollectorConfig {
            task_id: task_id.clone(),
            time_window: TimeWindow {
                start_time_ms: req.time_window.start_time_ms,
                end_time_ms: req.time_window.end_time_ms,
                collection_interval_ms: None,
            },
            pod: Some(PodInfo {
                uid: req.target.as_ref().and_then(|t| t.pod_uid.clone()),
                name: req.target.as_ref().and_then(|t| t.pod_name.clone()),
                namespace: req.target.as_ref().and_then(|t| t.namespace.clone()),
            }),
            container_id: None,
            cgroup_id: req.target.as_ref().and_then(|t| t.cgroup_id.clone()),
            requested_metrics: extract_string_list(&req.collection_options, "block_io", "requested_metrics_by_type"),
            requested_events: extract_string_list(&req.collection_options, "block_io", "requested_events_by_type"),
            nri_table: nri_table.clone(),
            target_pids: req.collection_options.as_ref().and_then(|o| o.target_pids.clone()),
        };

        let evidence = run_block_io_collect_poc(block_io_cfg);
        evidences.push(evidence);
    }

    // 采集 syscall_latency 证据
    if evidence_types.contains(&"syscall_latency".to_string()) {
        let syscall_cfg = SyscallCollectorConfig {
            task_id: task_id.clone(),
            time_window: TimeWindow {
                start_time_ms: req.time_window.start_time_ms,
                end_time_ms: req.time_window.end_time_ms,
                collection_interval_ms: None,
            },
            pod: Some(PodInfo {
                uid: req.target.as_ref().and_then(|t| t.pod_uid.clone()),
                name: req.target.as_ref().and_then(|t| t.pod_name.clone()),
                namespace: req.target.as_ref().and_then(|t| t.namespace.clone()),
            }),
            container_id: None,
            cgroup_id: req.target.as_ref().and_then(|t| t.cgroup_id.clone()),
            requested_metrics: extract_string_list(&req.collection_options, "syscall_latency", "requested_metrics_by_type"),
            requested_events: extract_string_list(&req.collection_options, "syscall_latency", "requested_events_by_type"),
            nri_table: nri_table.clone(),
        };

        let evidence = run_syscall_collect_poc(syscall_cfg);
        evidences.push(evidence);
    }

    // 采集 fs_stall 证据
    if evidence_types.contains(&"fs_stall".to_string()) {
        let fs_stall_cfg = FsStallCollectorConfig {
            task_id: task_id.clone(),
            time_window: TimeWindow {
                start_time_ms: req.time_window.start_time_ms,
                end_time_ms: req.time_window.end_time_ms,
                collection_interval_ms: None,
            },
            pod: Some(PodInfo {
                uid: req.target.as_ref().and_then(|t| t.pod_uid.clone()),
                name: req.target.as_ref().and_then(|t| t.pod_name.clone()),
                namespace: req.target.as_ref().and_then(|t| t.namespace.clone()),
            }),
            container_id: None,
            cgroup_id: req.target.as_ref().and_then(|t| t.cgroup_id.clone()),
            requested_metrics: extract_string_list(&req.collection_options, "fs_stall", "requested_metrics_by_type"),
            requested_events: extract_string_list(&req.collection_options, "fs_stall", "requested_events_by_type"),
            nri_table: nri_table.clone(),
        };

        let evidence = run_fs_stall_collect_poc(fs_stall_cfg);
        evidences.push(evidence);
    }

    // 采集 cgroup 资源争抢证据
    if evidence_types.contains(&"cgroup_contention".to_string()) {
        let cgroup_cfg = CgroupContentionConfig {
            task_id: task_id.clone(),
            time_window: TimeWindow {
                start_time_ms: req.time_window.start_time_ms,
                end_time_ms: req.time_window.end_time_ms,
                collection_interval_ms: None,
            },
            pod: Some(PodInfo {
                uid: req.target.as_ref().and_then(|t| t.pod_uid.clone()),
                name: req.target.as_ref().and_then(|t| t.pod_name.clone()),
                namespace: req.target.as_ref().and_then(|t| t.namespace.clone()),
            }),
            container_id: None,
            cgroup_id: req.target.as_ref().and_then(|t| t.cgroup_id.clone()),
            requested_metrics: extract_string_list(&req.collection_options, "cgroup_contention", "requested_metrics_by_type"),
            requested_events: extract_string_list(&req.collection_options, "cgroup_contention", "requested_events_by_type"),
            nri_table: nri_table.clone(),
        };

        match run_cgroup_contention_collect_poc(&cgroup_cfg).await {
            Ok(evidence) => evidences.push(evidence),
            Err(e) => tracing::warn!("Failed to collect cgroup_contention: {:?}", e),
        }
    }

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
    let _payload = publisher.generate_alert_payload(&diagnosis);

    // 提交 AI 增强任务（异步处理）
    if let Some(ref queue) = ai_queue {
        let ai_task = AiTask::new(
            task_id.clone(),
            diagnosis.clone(),
            evidences.clone(),
            AiTaskPriority::Normal,
        );
        match queue.submit(ai_task).await {
            Ok(_) => tracing::info!("[Trigger] AI enhancement task submitted: {}", task_id),
            Err(e) => tracing::warn!("[Trigger] Failed to submit AI task: {}", e),
        }
    } else {
        tracing::debug!("[Trigger] AI enhancement skipped (queue not available)");
    }

    let end_time = chrono::Utc::now().timestamp_millis();
    let duration_ms = end_time - start_time;

    Json(serde_json::json!({
        "task_id": task_id,
        "status": "done",
        "duration_ms": duration_ms,
        "evidence_count": evidences.len(),
        "conclusion_count": diagnosis.conclusions.len(),
        "diagnosis_preview": diagnosis,
    }))
}

fn extract_string_list(
    options: &Option<CollectionOptions>,
    etype: &str,
    field: &str,
) -> Vec<String> {
    let map_opt = match (options, field) {
        (Some(o), "requested_metrics_by_type") => o.requested_metrics_by_type.as_ref(),
        (Some(o), "requested_events_by_type") => o.requested_events_by_type.as_ref(),
        _ => None,
    };

    map_opt
        .and_then(|m| m.get(etype))
        .and_then(|v| v.as_array())
        .map(|arr| {
            arr.iter()
                .filter_map(|x| x.as_str().map(|s| s.to_string()))
                .collect()
        })
        .unwrap_or_default()
}

