use crate::types::evidence::*;
use crate::collector::nri_mapping::{AttributionSource, NriMappingTable};
use serde::Deserialize;
use serde_json::json;
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::io::{BufRead, BufReader};
use std::process::{Command, Stdio};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

pub struct BlockIoCollectorConfig {
    pub task_id: String,
    pub time_window: TimeWindow,
    pub pod: Option<PodInfo>,
    pub container_id: Option<String>,
    pub cgroup_id: Option<String>,
    pub requested_metrics: Vec<String>,
    pub requested_events: Vec<String>,
    /// NRI 映射表引用，用于查询归属
    pub nri_table: Option<Arc<NriMappingTable>>,
    /// 目标 PID 列表（BPFtrace 采集时进行 PID 过滤）
    pub target_pids: Option<Vec<u32>>,
}

#[derive(Debug, Clone, Deserialize)]
struct BpftraceBlockIoEvent {
    #[serde(rename = "type")]
    event_type: String,
    pid: Option<u32>,
    comm: Option<String>,
    dev: Option<String>,
    bytes: Option<u64>,
    rw: Option<String>,
    latency_us: Option<u64>,
    ts_ms: Option<u64>,
    #[serde(flatten)]
    extra: HashMap<String, serde_json::Value>,
}

fn make_scope_key(pod_uid: Option<&str>, cgroup_id: Option<&str>) -> String {
    let u = pod_uid.unwrap_or("");
    let c = cgroup_id.unwrap_or("");
    let mut hasher = Sha256::new();
    hasher.update(format!("{u}|{c}"));
    format!("{:x}", hasher.finalize())
}

fn make_evidence_id(task_id: &str, evidence_type: &str, collection_id: &str, scope_key: &str) -> String {
    let mut hasher = Sha256::new();
    hasher.update(format!("{task_id}|{evidence_type}|{collection_id}|{scope_key}"));
    format!("{:x}", hasher.finalize())
}

/// 运行真实的 bpftrace block_io 采集（第 1 周 PoC）
pub fn run_block_io_collect_poc(cfg: BlockIoCollectorConfig) -> Evidence {
    let scope_key = make_scope_key(
        cfg.pod.as_ref().and_then(|p| p.uid.as_deref()),
        cfg.cgroup_id.as_deref(),
    );
    
    let collection_id = uuid::Uuid::new_v4().to_string();
    let probe_id = "block_io_latency.bt";
    
    // 计算采集持续时间
    let duration_ms = cfg.time_window.end_time_ms - cfg.time_window.start_time_ms;
    let duration_sec = (duration_ms / 1000).clamp(1, 60) as u64;
    
    let script_path = "scripts/bpftrace/block_io/io_latency.bt";
    
    // 存储采集结果
    let latencies = Arc::new(Mutex::new(Vec::<u64>::new()));
    let bytes_total = Arc::new(Mutex::new(0u64));
    let io_count = Arc::new(Mutex::new(0u64));
    let timeout_count = Arc::new(Mutex::new(0u64));
    let events = Arc::new(Mutex::new(Vec::<BpftraceBlockIoEvent>::new()));
    let errors = Arc::new(Mutex::new(Vec::<CollectionError>::new()));
    
    let latencies_clone = latencies.clone();
    let events_clone = events.clone();
    
    // 构建 bpftrace 命令
    let mut cmd = Command::new("sudo");
    cmd.arg("bpftrace").arg(script_path);
    
    // 添加目标 PID 过滤（如果指定了）
    // bpftrace 支持 -p PID 参数进行进程过滤
    if let Some(ref pids) = cfg.target_pids {
        for pid in pids {
            cmd.arg("-p").arg(pid.to_string());
        }
    }
    
    // 启动 bpftrace 采集
    let mut child = match cmd
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .spawn()
    {
        Ok(c) => c,
        Err(e) => {
            let mut errors_guard = errors.lock().unwrap();
            errors_guard.push(CollectionError {
                code: "BPFTRACE_SCRIPT_LOAD_FAILED".into(),
                message: format!("Failed to start bpftrace: {}", e),
                retryable: Some(false),
                detail: None,
            });
            drop(errors_guard);
            return build_evidence(
                cfg, scope_key, collection_id, probe_id,
                Arc::try_unwrap(latencies).unwrap().into_inner().unwrap(),
                *bytes_total.lock().unwrap(),
                *io_count.lock().unwrap(),
                *timeout_count.lock().unwrap(),
                Arc::try_unwrap(events).unwrap().into_inner().unwrap(),
                Arc::try_unwrap(errors).unwrap().into_inner().unwrap(),
                "failed",
            );
        }
    };
    
    let stdout = child.stdout.take().expect("Failed to capture stdout");
    let reader = BufReader::new(stdout);
    
    // 采集超时控制
    let start_time = Instant::now();
    let timeout = Duration::from_secs(duration_sec);
    
    // 解析 bpftrace 输出
    for line in reader.lines() {
        if start_time.elapsed() > timeout {
            break;
        }
        
        let line = match line {
            Ok(l) => l,
            Err(_) => continue,
        };
        
        // 解析 JSON 输出
        if let Ok(event) = serde_json::from_str::<BpftraceBlockIoEvent>(&line) {
            match event.event_type.as_str() {
                "io_complete" => {
                    if let Some(latency) = event.latency_us {
                        let mut latencies = latencies_clone.lock().unwrap();
                        latencies.push(latency);
                    }
                    if let Some(b) = event.bytes {
                        let mut bytes_total = bytes_total.lock().unwrap();
                        *bytes_total += b;
                    }
                    {
                        let mut io_count = io_count.lock().unwrap();
                        *io_count += 1;
                    }
                    let mut events = events_clone.lock().unwrap();
                    events.push(event);
                }
                "io_timeout" => {
                    let mut timeout_count = timeout_count.lock().unwrap();
                    *timeout_count += 1;
                    let mut events = events_clone.lock().unwrap();
                    events.push(event);
                }
                _ => {}
            }
        }
    }
    
    // 停止 bpftrace
    let _ = child.kill();
    
    // 收集结果（使用 lock 获取数据，避免 Arc::try_unwrap 因引用计数失败）
    let latencies: Vec<u64> = Arc::try_unwrap(latencies)
        .map(|m| m.into_inner().unwrap_or_default())
        .unwrap_or_else(|arc| arc.lock().map(|m| m.clone()).unwrap_or_default());
    let bytes_total: u64 = Arc::try_unwrap(bytes_total)
        .map(|m| m.into_inner().unwrap_or(0))
        .unwrap_or_else(|arc| arc.lock().map(|g| *g).unwrap_or(0));
    let io_count: u64 = Arc::try_unwrap(io_count)
        .map(|m| m.into_inner().unwrap_or(0))
        .unwrap_or_else(|arc| arc.lock().map(|g| *g).unwrap_or(0));
    let timeout_count: u64 = Arc::try_unwrap(timeout_count)
        .map(|m| m.into_inner().unwrap_or(0))
        .unwrap_or_else(|arc| arc.lock().map(|g| *g).unwrap_or(0));
    let events: Vec<BpftraceBlockIoEvent> = Arc::try_unwrap(events)
        .map(|m| m.into_inner().unwrap_or_default())
        .unwrap_or_else(|arc| arc.lock().map(|m| m.clone()).unwrap_or_default());
    let errors: Vec<CollectionError> = Arc::try_unwrap(errors)
        .map(|m| m.into_inner().unwrap_or_default())
        .unwrap_or_else(|arc| arc.lock().map(|m| m.clone()).unwrap_or_default());
    
    let collection_status = if errors.is_empty() { "success" } else { "partial" };
    
    build_evidence(
        cfg, scope_key, collection_id, probe_id,
        latencies, bytes_total, io_count, timeout_count, events, errors, collection_status,
    )
}

fn build_evidence(
    cfg: BlockIoCollectorConfig,
    scope_key: String,
    collection_id: String,
    probe_id: &str,
    latencies: Vec<u64>,
    bytes_total: u64,
    io_count: u64,
    timeout_count: u64,
    raw_events: Vec<BpftraceBlockIoEvent>,
    errors: Vec<CollectionError>,
    collection_status: &str,
) -> Evidence {
    let mut metric_summary = HashMap::new();
    
    let window_duration_sec = ((cfg.time_window.end_time_ms - cfg.time_window.start_time_ms) as f64) / 1000.0;
    
    // 计算延迟分位
    if !latencies.is_empty() {
        let mut sorted = latencies.clone();
        sorted.sort();
        
        let len = sorted.len();
        let p50 = sorted[len * 50 / 100] as f64 / 1000.0; // us -> ms
        let p90 = sorted[len * 90 / 100] as f64 / 1000.0;
        let p99 = sorted[len * 99 / 100] as f64 / 1000.0;
        
        let is_requested = |m: &str| {
            cfg.requested_metrics.is_empty() || cfg.requested_metrics.contains(&m.to_string())
        };
        
        if is_requested("io_latency_p50_ms") {
            metric_summary.insert("io_latency_p50_ms".into(), p50);
        }
        if is_requested("io_latency_p90_ms") {
            metric_summary.insert("io_latency_p90_ms".into(), p90);
        }
        if is_requested("io_latency_p99_ms") {
            metric_summary.insert("io_latency_p99_ms".into(), p99);
        }
        
        // 计算吞吐和 IOPS
        if window_duration_sec > 0.0 {
            if is_requested("throughput_bytes_per_s") {
                metric_summary.insert("throughput_bytes_per_s".into(), bytes_total as f64 / window_duration_sec);
            }
            if is_requested("io_ops_per_s") {
                metric_summary.insert("io_ops_per_s".into(), io_count as f64 / window_duration_sec);
            }
        }
        
        // 超时计数
        if timeout_count > 0 && is_requested("timeout_count") {
            metric_summary.insert("timeout_count".into(), timeout_count as f64);
        }
        
        // 队列深度（简化：用 io_count / window_duration 作为近似）
        if is_requested("queue_depth") {
            metric_summary.insert("queue_depth".into(), io_count as f64 / window_duration_sec.max(1.0));
        }
    }
    
    // 构建 events_topology
    let mut events_topology = Vec::new();
    
    // 检测 I/O 延迟突增（阈值 100ms）
    if let Some(p99) = metric_summary.get("io_latency_p99_ms") {
        if *p99 > 100.0 {
            let is_requested = cfg.requested_events.is_empty() || cfg.requested_events.contains(&"io_latency_spike".to_string());
            if is_requested {
                events_topology.push(Event {
                    event_type: "io_latency_spike".into(),
                    event_time_ms: cfg.time_window.start_time_ms + (cfg.time_window.end_time_ms - cfg.time_window.start_time_ms) / 2,
                    severity: Some(8),
                    details: Some(json!({
                        "io_latency_ms_at_spike": p99,
                        "delta_p99_ms": p99 - 50.0, // 简化基线
                    })),
                });
            }
        }
    }
    
    // I/O 超时事件
    if timeout_count > 0 {
        let is_requested = cfg.requested_events.is_empty() || cfg.requested_events.contains(&"io_timeout".to_string());
        if is_requested {
            events_topology.push(Event {
                event_type: "io_timeout".into(),
                event_time_ms: cfg.time_window.start_time_ms,
                severity: Some(9),
                details: Some(json!({
                    "timeout_count": timeout_count,
                })),
            });
        }
    }
    
    let collected_metrics: Vec<String> = metric_summary.keys().cloned().collect();
    let collected_events: Vec<String> = events_topology.iter().map(|e| e.event_type.clone()).collect();
    
    let selection = Selection {
        requested_metrics: cfg.requested_metrics.clone(),
        collected_metrics,
        requested_events: cfg.requested_events.clone(),
        collected_events,
    };
    
    // 保存 cgroup_id 存在状态（用于后续 fallback）
    let has_cgroup_id = cfg.cgroup_id.is_some();
    
    // 从 bpftrace 事件中提取唯一 PID 列表
    let collected_pids: Vec<u32> = raw_events
        .iter()
        .filter_map(|e| e.pid)
        .collect::<std::collections::HashSet<_>>()
        .into_iter()
        .collect();
    
    // 查询 NRI 映射表获取归属信息
    let attribution_info = if let Some(ref table) = cfg.nri_table {
        let pod_uid = cfg.pod.as_ref().and_then(|p| p.uid.as_deref());
        let cgroup_id = cfg.cgroup_id.as_deref();
        
        // 优先使用 bpftrace 采集到的实际 PID 进行查询
        let pid_result = if !collected_pids.is_empty() {
            collected_pids.iter()
                .filter_map(|&pid| table.resolve_attribution(pod_uid, cgroup_id, Some(pid)).ok())
                .next()
        } else {
            None
        };
        
        // 如果 PID 查询失败，回退到仅使用 cgroup/pod 查询
        pid_result.or_else(|| table.resolve_attribution(pod_uid, cgroup_id, None).ok())
    } else {
        None
    };
    
    let scope = Scope {
        pod: cfg.pod,
        container_id: cfg.container_id,
        cgroup_id: cfg.cgroup_id,
        pid_scope: None,
        scope_key: scope_key.clone(),
        network_target: None,
    };
    
    // 根据 NRI 映射结果构建归因信息
    let attribution = if let Some(ref info) = attribution_info {
        Attribution {
            status: info.status.to_string(),
            confidence: Some(info.confidence),
            source: Some(match info.source {
                AttributionSource::Nri => "nri".into(),
                AttributionSource::PidMap => "pid_map".into(),
                AttributionSource::Uncertain => "uncertain".into(),
            }),
            mapping_version: Some(info.mapping_version.clone()),
        }
    } else {
        // NRI 映射表不可用时的兜底
        Attribution {
            status: if has_cgroup_id { "nri_mapped".into() } else { "pid_cgroup_fallback".into() },
            confidence: Some(if has_cgroup_id { 0.9 } else { 0.6 }),
            source: if has_cgroup_id { Some("nri".into()) } else { Some("pid_map".into()) },
            mapping_version: None,
        }
    };
    
    let collection = CollectionMeta {
        collection_id: collection_id.clone(),
        collection_status: collection_status.into(),
        probe_id: probe_id.into(),
        errors,
    };
    
    let evidence_id = make_evidence_id(&cfg.task_id, "block_io", &collection_id, &scope_key);
    
    Evidence {
        schema_version: "evidence.v0.2".into(),
        task_id: cfg.task_id,
        evidence_id,
        evidence_type: "block_io".into(),
        collection,
        time_window: cfg.time_window,
        scope,
        selection: Some(selection),
        metric_summary,
        events_topology,
        top_calls: None,
        attribution,
    }
}
