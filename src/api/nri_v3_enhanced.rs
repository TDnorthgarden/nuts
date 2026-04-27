//! NRI V3 增强版 API 路由
//!
//! 提供对 NRI V3 优化功能的 HTTP 访问：
//! - 高性能映射表查询 (DashMap)
//! - 批量事件提交
//! - 实时指标获取

use axum::{
    extract::{State, Json},
    routing::{get, post},
    Router,
};
use std::sync::Arc;
use serde::{Deserialize, Serialize};
use crate::collector::nri_v3::NriV3;
use crate::collector::nri_mapping::{PodInfo, ContainerMapping, NriEvent, NriPodEvent, NriContainerInfo};

/// API 状态
#[derive(Clone)]
pub struct NriV3ApiState {
    nri_v3: Arc<NriV3>,
}

impl NriV3ApiState {
    pub fn new(nri_v3: Arc<NriV3>) -> Self {
        Self { nri_v3 }
    }
}

/// 查询 Pod 请求
#[derive(Debug, Deserialize)]
pub struct QueryPodRequest {
    pod_uid: String,
}

/// Pod 查询响应
#[derive(Debug, Serialize)]
pub struct PodQueryResponse {
    found: bool,
    pod: Option<PodInfo>,
    containers: Vec<ContainerMapping>,
    query_time_ms: u64,
}

/// 批量事件提交请求
#[derive(Debug, Deserialize)]
pub struct BatchEventRequest {
    events: Vec<EventRequest>,
}

#[derive(Debug, Deserialize)]
pub struct EventRequest {
    pod_uid: String,
    pod_name: String,
    namespace: String,
    containers: Vec<ContainerRequest>,
}

#[derive(Debug, Deserialize)]
pub struct ContainerRequest {
    container_id: String,
    cgroup_ids: Vec<String>,
    pids: Vec<u32>,
}

/// 批量提交响应
#[derive(Debug, Serialize)]
pub struct BatchSubmitResponse {
    submitted: usize,
    failed: usize,
    errors: Vec<String>,
}

/// V3 状态响应
#[derive(Debug, Serialize)]
pub struct V3StatusResponse {
    version: String,
    features: Vec<String>,
    pod_count: usize,
    container_count: usize,
    cgroup_count: usize,
    pid_count: usize,
}

/// 创建路由
pub fn router(state: Arc<NriV3ApiState>) -> Router {
    Router::new()
        .route("/api/v3/nri/status", get(get_v3_status))
        .route("/api/v3/nri/pod", get(query_pod))
        .route("/api/v3/nri/batch", post(submit_batch_events))
        .with_state(state)
}

/// 获取 V3 状态
async fn get_v3_status(
    State(state): State<Arc<NriV3ApiState>>,
) -> Json<V3StatusResponse> {
    let table = state.nri_v3.table();
    
    let pod_count = table.pod_map.len();
    let container_count = table.container_map.len();
    let cgroup_count = table.cgroup_map.len();
    let pid_count = table.pid_map.len();
    
    Json(V3StatusResponse {
        version: "3.0.0-optimized".to_string(),
        features: vec![
            "dashmap-concurrent".to_string(),
            "batch-processing".to_string(),
            "version-control".to_string(),
        ],
        pod_count,
        container_count,
        cgroup_count,
        pid_count,
    })
}

/// 查询 Pod
async fn query_pod(
    State(state): State<Arc<NriV3ApiState>>,
    Json(req): Json<QueryPodRequest>,
) -> Json<PodQueryResponse> {
    let start = std::time::Instant::now();
    let table = state.nri_v3.table();
    
    let result = table.get_pod_details(&req.pod_uid);
    
    let (found, pod, containers) = match result {
        Some((info, containers)) => {
            (true, Some(info), containers)
        }
        None => (false, None, vec![]),
    };
    
    Json(PodQueryResponse {
        found,
        pod,
        containers,
        query_time_ms: start.elapsed().as_millis() as u64,
    })
}

/// 批量提交事件
async fn submit_batch_events(
    State(state): State<Arc<NriV3ApiState>>,
    Json(req): Json<BatchEventRequest>,
) -> Json<BatchSubmitResponse> {
    let mut submitted = 0;
    let mut failed = 0;
    let mut errors = vec![];
    
    for event_req in req.events {
        let containers: Vec<NriContainerInfo> = event_req
            .containers
            .into_iter()
            .map(|c| NriContainerInfo {
                container_id: c.container_id,
                cgroup_ids: c.cgroup_ids,
                pids: c.pids,
            })
            .collect();
        
        let event = NriEvent::AddOrUpdate(NriPodEvent {
            pod_uid: event_req.pod_uid,
            pod_name: event_req.pod_name,
            namespace: event_req.namespace,
            containers,
        });
        
        match state.nri_v3.submit_event(event).await {
            Ok(_) => submitted += 1,
            Err(e) => {
                failed += 1;
                errors.push(e.to_string());
            }
        }
    }
    
    Json(BatchSubmitResponse {
        submitted,
        failed,
        errors,
    })
}
