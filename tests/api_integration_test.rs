//! 集成测试 - API 端点测试
//!
//! 测试内容：
//! 1. 手动触发诊断端点 (/v1/diagnostics:trigger)
//! 2. NRI Webhook 端点 (/v1/nri/events)
//! 3. 完整链路：NRI 事件 -> 触发诊断 -> 验证输出

use axum::body::Body;
use axum::http::{Request, StatusCode};
use std::sync::Arc;
use tower::util::ServiceExt;

// 引入被测模块
use nuts_observer::api::nri::router as nri_router;
use nuts_observer::api::trigger::router as trigger_router;
use nuts_observer::collector::nri_mapping::{NriMappingTable, NriPodEvent, NriContainerInfo, NriEvent};

/// 测试手动触发诊断端点
#[tokio::test]
async fn test_trigger_endpoint() {
    // 创建共享的 NRI 映射表
    let nri_table = Arc::new(NriMappingTable::new());
    // 构建应用路由（传入 NRI 映射表）
    let app = trigger_router(Arc::clone(&nri_table), None);

    // 构建触发请求
    let request_body = serde_json::json!({
        "trigger_type": "manual",
        "target": {
            "pod_uid": "test-pod-001",
            "namespace": "default",
            "pod_name": "test-pod",
            "cgroup_id": "cgroup-test-001"
        },
        "time_window": {
            "start_time_ms": 1700000000000_i64,
            "end_time_ms": 1700000050000_i64
        },
        "collection_options": {
            "requested_evidence_types": ["block_io", "network"],
            "requested_metrics_by_type": {
                "block_io": {
                    "requested_metrics": ["io_latency_p99_ms", "io_ops_per_s"]
                }
            }
        },
        "idempotency_key": "test-key-001"
    });

    let request = Request::builder()
        .method("POST")
        .uri("/v1/diagnostics:trigger")
        .header("Content-Type", "application/json")
        .body(Body::from(request_body.to_string()))
        .unwrap();

    // 发送请求
    let response = app.oneshot(request).await.unwrap();

    // 验证响应
    assert_eq!(response.status(), StatusCode::OK);

    // 解析响应体
    let body = axum::body::to_bytes(response.into_body(), usize::MAX).await.unwrap();
    let json: serde_json::Value = serde_json::from_slice(&body).unwrap();

    // 验证响应结构
    assert!(json.get("task_id").is_some());
    assert!(json.get("status").is_some());
    assert_eq!(json["status"], "done");
    assert!(json.get("evidence_count").is_some());
    assert!(json.get("diagnosis_preview").is_some());
}

/// 测试 NRI Webhook 端点 - ADD 事件
#[tokio::test]
async fn test_nri_webhook_add_event() {
    // 创建共享的 NRI 映射表
    let nri_table = Arc::new(NriMappingTable::new());
    let app = nri_router(Arc::clone(&nri_table));

    // 构建 NRI 事件请求
    let request_body = serde_json::json!({
        "event_type": "ADD",
        "pod_uid": "nri-test-pod-001",
        "pod_name": "nri-test-pod",
        "namespace": "default",
        "containers": [
            {
                "container_id": "container-nri-001",
                "container_name": "main",
                "cgroup_ids": ["cgroup-nri-001"],
                "pids": [1001, 1002],
                "runtime": "runc"
            }
        ],
        "node_name": "node-1",
        "labels": {
            "app": "test-app"
        }
    });

    let request = Request::builder()
        .method("POST")
        .uri("/v1/nri/events")
        .header("Content-Type", "application/json")
        .body(Body::from(request_body.to_string()))
        .unwrap();

    // 发送请求
    let response = app.oneshot(request).await.unwrap();

    // 验证响应
    assert_eq!(response.status(), StatusCode::OK);

    // 解析响应体
    let body = axum::body::to_bytes(response.into_body(), usize::MAX).await.unwrap();
    let json: serde_json::Value = serde_json::from_slice(&body).unwrap();

    // 验证响应结构
    assert_eq!(json["status"], "success");
    assert!(json.get("stats").is_some());
    assert_eq!(json["stats"]["pod_count"], 1);
    assert_eq!(json["stats"]["container_count"], 1);
    assert_eq!(json["stats"]["cgroup_count"], 1);
    assert_eq!(json["stats"]["pid_count"], 2);

    // 验证映射表已更新
    assert_eq!(nri_table.pod_count(), 1);
    assert_eq!(nri_table.container_count(), 1);
}

/// 测试 NRI Webhook 端点 - DELETE 事件
#[tokio::test]
async fn test_nri_webhook_delete_event() {
    // 创建共享的 NRI 映射表
    let nri_table = Arc::new(NriMappingTable::new());
    
    // 先添加一个 Pod
    let add_event = NriPodEvent {
        pod_uid: "pod-to-delete".to_string(),
        pod_name: "delete-me".to_string(),
        namespace: "default".to_string(),
        containers: vec![
            NriContainerInfo {
                container_id: "container-del".to_string(),
                cgroup_ids: vec!["cgroup-del".to_string()],
                pids: vec![2001],
            },
        ],
    };
    nri_table.update_from_nri(NriEvent::AddOrUpdate(add_event)).unwrap();
    
    assert_eq!(nri_table.pod_count(), 1);

    // 然后删除它
    let app = nri_router(Arc::clone(&nri_table));
    let request_body = serde_json::json!({
        "event_type": "DELETE",
        "pod_uid": "pod-to-delete",
        "pod_name": "delete-me",
        "namespace": "default",
        "containers": []
    });

    let request = Request::builder()
        .method("POST")
        .uri("/v1/nri/events")
        .header("Content-Type", "application/json")
        .body(Body::from(request_body.to_string()))
        .unwrap();

    let response = app.oneshot(request).await.unwrap();
    assert_eq!(response.status(), StatusCode::OK);

    // 验证映射表已删除
    assert_eq!(nri_table.pod_count(), 0);
    assert_eq!(nri_table.container_count(), 0);
}

/// 测试 NRI Webhook 端点 - 未知事件类型
#[tokio::test]
async fn test_nri_webhook_unknown_event() {
    let nri_table = Arc::new(NriMappingTable::new());
    let app = nri_router(nri_table);

    let request_body = serde_json::json!({
        "event_type": "UNKNOWN_EVENT",
        "pod_uid": "test-pod",
        "pod_name": "test",
        "namespace": "default",
        "containers": []
    });

    let request = Request::builder()
        .method("POST")
        .uri("/v1/nri/events")
        .header("Content-Type", "application/json")
        .body(Body::from(request_body.to_string()))
        .unwrap();

    let response = app.oneshot(request).await.unwrap();
    assert_eq!(response.status(), StatusCode::OK);

    let body = axum::body::to_bytes(response.into_body(), usize::MAX).await.unwrap();
    let json: serde_json::Value = serde_json::from_slice(&body).unwrap();

    assert_eq!(json["status"], "error");
    assert!(json["message"].as_str().unwrap().contains("Unknown event type"));
}

/// 测试完整链路：NRI 事件 -> 归属查询 -> 触发诊断
#[tokio::test]
async fn test_full_pipeline_nri_to_diagnosis() {
    // 步骤 1: 通过 NRI Webhook 添加 Pod 信息
    let nri_table = Arc::new(NriMappingTable::new());
    let nri_app = nri_router(Arc::clone(&nri_table));

    let nri_request = serde_json::json!({
        "event_type": "ADD",
        "pod_uid": "pipeline-test-pod",
        "pod_name": "pipeline-pod",
        "namespace": "default",
        "containers": [
            {
                "container_id": "pipeline-container",
                "cgroup_ids": ["pipeline-cgroup"],
                "pids": [3001]
            }
        ]
    });

    let request = Request::builder()
        .method("POST")
        .uri("/v1/nri/events")
        .header("Content-Type", "application/json")
        .body(Body::from(nri_request.to_string()))
        .unwrap();

    let response = nri_app.oneshot(request).await.unwrap();
    assert_eq!(response.status(), StatusCode::OK);

    // 验证映射表已更新
    assert_eq!(nri_table.pod_count(), 1);

    // 步骤 2: 触发诊断（使用相同的 cgroup_id）
    // 使用同一个 nri_table，确保诊断能访问到之前添加的 Pod 信息
    let trigger_app = trigger_router(Arc::clone(&nri_table), None);
    let trigger_request = serde_json::json!({
        "trigger_type": "manual",
        "target": {
            "pod_uid": "pipeline-test-pod",
            "namespace": "default",
            "pod_name": "pipeline-pod",
            "cgroup_id": "pipeline-cgroup"  // 与 NRI 事件中的 cgroup 匹配
        },
        "time_window": {
            "start_time_ms": 1700000000000_i64,
            "end_time_ms": 1700000050000_i64
        },
        "collection_options": {
            "requested_evidence_types": ["block_io"]
        },
        "idempotency_key": "pipeline-test-001"
    });

    let request = Request::builder()
        .method("POST")
        .uri("/v1/diagnostics:trigger")
        .header("Content-Type", "application/json")
        .body(Body::from(trigger_request.to_string()))
        .unwrap();

    let response = trigger_app.oneshot(request).await.unwrap();
    assert_eq!(response.status(), StatusCode::OK);

    // 解析诊断响应
    let body = axum::body::to_bytes(response.into_body(), usize::MAX).await.unwrap();
    let json: serde_json::Value = serde_json::from_slice(&body).unwrap();

    // 验证诊断结果结构
    assert!(json.get("task_id").is_some());
    assert!(json.get("diagnosis_preview").is_some());
    
    let diagnosis = &json["diagnosis_preview"];
    assert!(diagnosis.get("conclusions").is_some());
    assert!(diagnosis.get("evidence_refs").is_some());
}

/// 测试 NRI 映射表查询功能
#[tokio::test]
async fn test_nri_mapping_query() {
    let nri_table = Arc::new(NriMappingTable::new());
    
    // 添加多个 Pod
    let pod1 = NriPodEvent {
        pod_uid: "pod-001".to_string(),
        pod_name: "web-server".to_string(),
        namespace: "production".to_string(),
        containers: vec![
            NriContainerInfo {
                container_id: "ctr-web".to_string(),
                cgroup_ids: vec!["/kubepods/pod-001/ctr-web".to_string()],
                pids: vec![1001, 1002],
            },
        ],
    };
    
    let pod2 = NriPodEvent {
        pod_uid: "pod-002".to_string(),
        pod_name: "db-server".to_string(),
        namespace: "production".to_string(),
        containers: vec![
            NriContainerInfo {
                container_id: "ctr-db".to_string(),
                cgroup_ids: vec!["/kubepods/pod-002/ctr-db".to_string()],
                pids: vec![2001, 2002],
            },
        ],
    };
    
    nri_table.update_from_nri(NriEvent::AddOrUpdate(pod1)).unwrap();
    nri_table.update_from_nri(NriEvent::AddOrUpdate(pod2)).unwrap();
    
    // 验证列表查询
    let all_pods = nri_table.list_all_pods();
    assert_eq!(all_pods.len(), 2);
    
    // 验证通过 UID 查询
    let (pod, containers) = nri_table.get_pod_details("pod-001").unwrap();
    assert_eq!(pod.pod_name, "web-server");
    assert_eq!(containers.len(), 1);
    
    // 验证通过 cgroup 归属查询
    let info = nri_table.resolve_attribution(
        None,
        Some("/kubepods/pod-002/ctr-db"),
        None,
    ).unwrap();
    assert_eq!(info.pod_uid, Some("pod-002".to_string()));
    
    // 验证通过 PID 归属查询
    let info_by_pid = nri_table.resolve_attribution(None, None, Some(1001)).unwrap();
    assert_eq!(info_by_pid.pod_uid, Some("pod-001".to_string()));
    
    // 验证统计
    assert_eq!(nri_table.pod_count(), 2);
    assert_eq!(nri_table.container_count(), 2);
    assert_eq!(nri_table.cgroup_count(), 2);
    assert_eq!(nri_table.pid_count(), 4);
}

/// 测试 NRI 映射表在诊断触发中的使用
#[tokio::test]
async fn test_nri_mapping_in_diagnosis() {
    // 创建 NRI 映射表并添加 Pod
    let nri_table = Arc::new(NriMappingTable::new());
    
    let pod = NriPodEvent {
        pod_uid: "diagnosis-test-pod".to_string(),
        pod_name: "test-app".to_string(),
        namespace: "default".to_string(),
        containers: vec![
            NriContainerInfo {
                container_id: "test-container".to_string(),
                cgroup_ids: vec!["test-cgroup-123".to_string()],
                pids: vec![1234],
            },
        ],
    };
    nri_table.update_from_nri(NriEvent::AddOrUpdate(pod)).unwrap();
    
    // 使用相同的 nri_table 创建触发路由
    let trigger_app = trigger_router(Arc::clone(&nri_table), None);
    
    // 触发诊断请求，引用已存在的 cgroup
    let request_body = serde_json::json!({
        "trigger_type": "manual",
        "target": {
            "pod_uid": "diagnosis-test-pod",
            "namespace": "default",
            "pod_name": "test-app",
            "cgroup_id": "test-cgroup-123"  // NRI 表中已存在
        },
        "time_window": {
            "start_time_ms": 1700000000000_i64,
            "end_time_ms": 1700000050000_i64
        },
        "collection_options": {
            "requested_evidence_types": ["network"]
        },
        "idempotency_key": "nri-diagnosis-test-001"
    });
    
    let request = Request::builder()
        .method("POST")
        .uri("/v1/diagnostics:trigger")
        .header("Content-Type", "application/json")
        .body(Body::from(request_body.to_string()))
        .unwrap();
    
    let response = trigger_app.oneshot(request).await.unwrap();
    assert_eq!(response.status(), StatusCode::OK);
    
    let body = axum::body::to_bytes(response.into_body(), usize::MAX).await.unwrap();
    let json: serde_json::Value = serde_json::from_slice(&body).unwrap();
    
    // 验证响应
    assert!(json.get("task_id").is_some());
    assert_eq!(json["status"], "done");
    
    // 验证诊断预览包含证据引用
    let preview = &json["diagnosis_preview"];
    assert!(preview.get("evidence_refs").is_some());
}
