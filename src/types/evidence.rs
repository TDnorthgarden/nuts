use serde::{Deserialize, Serialize};
use std::collections::HashMap;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Evidence {
    pub schema_version: String, // e.g. "evidence.v0.2"
    pub task_id: String,
    pub evidence_id: String,
    pub evidence_type: String,
    pub collection: CollectionMeta,
    pub time_window: TimeWindow,
    pub scope: Scope,
    #[serde(default)]
    pub selection: Option<Selection>,
    #[serde(default)]
    pub metric_summary: HashMap<String, f64>,
    #[serde(default)]
    pub events_topology: Vec<Event>,
    #[serde(default)]
    pub top_calls: Option<TopCalls>,
    pub attribution: Attribution,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CollectionMeta {
    pub collection_id: String,
    pub collection_status: String,
    #[serde(default)]
    pub probe_id: String,
    #[serde(default)]
    pub errors: Vec<CollectionError>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CollectionError {
    pub code: String,
    pub message: String,
    #[serde(default)]
    pub retryable: Option<bool>,
    #[serde(default)]
    pub detail: Option<serde_json::Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TimeWindow {
    pub start_time_ms: i64,
    pub end_time_ms: i64,
    #[serde(default)]
    pub collection_interval_ms: Option<i64>,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct Scope {
    #[serde(default)]
    pub pod: Option<PodInfo>,
    #[serde(default)]
    pub container_id: Option<String>,
    #[serde(default)]
    pub cgroup_id: Option<String>,
    #[serde(default)]
    pub pid_scope: Option<PidScope>,
    #[serde(default)]
    pub scope_key: String,
    #[serde(default)]
    pub network_target: Option<NetworkTarget>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PodInfo {
    pub uid: Option<String>,
    pub name: Option<String>,
    pub namespace: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PidScope {
    pub pids: Vec<i32>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NetworkTarget {
    pub target_id: Option<String>,
    pub dst_ip: Option<String>,
    pub dst_port: Option<u16>,
    pub protocol: Option<String>,
    pub endpoint: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct Selection {
    pub requested_metrics: Vec<String>,
    pub collected_metrics: Vec<String>,
    pub requested_events: Vec<String>,
    pub collected_events: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Event {
    pub event_type: String,
    pub event_time_ms: i64,
    #[serde(default)]
    pub severity: Option<u8>,
    #[serde(default)]
    pub details: Option<serde_json::Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TopCalls {
    pub by_call: Vec<TopCall>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TopCall {
    pub call_name: String,
    pub count: u64,
    #[serde(default)]
    pub p95_latency_ms: Option<f64>,
    #[serde(default)]
    pub p99_latency_ms: Option<f64>,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct Attribution {
    #[serde(default)]
    pub status: String,
    #[serde(default)]
    pub confidence: Option<f64>,
    #[serde(default)]
    pub source: Option<String>,
    #[serde(default)]
    pub mapping_version: Option<String>,
}

