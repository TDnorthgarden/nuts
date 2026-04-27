//! cgroup 资源争抢检测采集器
//!
//! 监控 cgroup 的 CPU/内存/IO 资源争抢迹象，包括：
//! - CPU throttle（被限制次数/时间）
//! - 内存压力（memory.pressure）
//! - IO 等待时间（io.stat）

use crate::collector::nri_mapping::{NriMappingTable, AttributionSource};
use crate::types::evidence::*;
use serde::{Deserialize, Serialize};
use serde_json::json;
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::fs;
use std::io;
use std::path::Path;
use std::sync::Arc;

/// cgroup 资源争抢采集配置
pub struct CgroupContentionConfig {
    pub task_id: String,
    pub time_window: TimeWindow,
    pub pod: Option<PodInfo>,
    pub container_id: Option<String>,
    pub cgroup_id: Option<String>,
    pub requested_metrics: Vec<String>,
    pub requested_events: Vec<String>,
    /// NRI 映射表引用
    pub nri_table: Option<Arc<NriMappingTable>>,
}

/// cgroup 资源争抢统计（用于诊断展示）
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CgroupContentionStats {
    pub cpu: Option<CpuContentionStats>,
    pub memory: Option<MemoryContentionStats>,
    pub io: Option<IoContentionStats>,
    pub contention_score: f64,
    pub primary_contention_type: String,
}

/// CPU 争抢统计
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CpuContentionStats {
    /// CPU 使用配额（微秒）
    pub cpu_usage_us: u64,
    /// CPU 限制配额（微秒）
    pub cpu_limit_us: u64,
    /// Throttle 次数（周期被限制）
    pub nr_throttled: u64,
    /// Throttle 时间（纳秒）
    pub throttled_time_ns: u64,
    /// CPU 使用率百分比
    pub usage_percent: f64,
    /// Throttle 率（被限制周期占比）
    pub throttle_rate: f64,
    /// 是否高 CPU 争抢
    pub is_high_contention: bool,
}

/// 内存争抢统计
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MemoryContentionStats {
    /// 当前内存使用（字节）
    pub memory_usage_bytes: u64,
    /// 内存限制（字节）
    pub memory_limit_bytes: u64,
    /// 内存使用百分比
    pub usage_percent: f64,
    /// 内存压力 - 等待时间（微秒）
    pub memory_pressure_wait_us: u64,
    /// 内存压力 - 压力分数（0-100）
    pub memory_pressure_score: f64,
    /// 页面回收次数
    pub pgpgin: u64,
    /// 页面换出次数
    pub pgpgout: u64,
    /// 是否高内存争抢
    pub is_high_contention: bool,
}

/// IO 争抢统计
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IoContentionStats {
    /// IO 等待时间（微秒）
    pub io_wait_time_us: u64,
    /// IO 服务时间（微秒）
    pub io_service_time_us: u64,
    /// IO 队列深度
    pub queue_depth: u32,
    /// 设备 IO 时间（毫秒）
    pub device_io_time_ms: u64,
    /// IO 权重（cgroup 权重配置）
    pub io_weight: u32,
    /// 是否高 IO 争抢
    pub is_high_contention: bool,
}

/// 从 cgroup fs 读取 CPU 统计
fn read_cpu_stats(cgroup_path: &Path) -> Option<CpuContentionStats> {
    let cpu_stat_path = cgroup_path.join("cpu.stat");
    let cpu_max_path = cgroup_path.join("cpu.max");
    
    if !cpu_stat_path.exists() {
        return None;
    }

    let content = fs::read_to_string(&cpu_stat_path).ok()?;
    let mut usage_us = 0u64;
    let mut nr_throttled = 0u64;
    let mut throttled_time_ns = 0u64;

    for line in content.lines() {
        let parts: Vec<&str> = line.split_whitespace().collect();
        if parts.len() >= 2 {
            match parts[0] {
                "usage_usec" => usage_us = parts[1].parse().unwrap_or(0),
                "nr_throttled" => nr_throttled = parts[1].parse().unwrap_or(0),
                "throttled_usec" => throttled_time_ns = parts[1].parse::<u64>().unwrap_or(0) * 1000,
                _ => {}
            }
        }
    }

    // 读取 CPU 限制
    let (limit_us, period_us) = if cpu_max_path.exists() {
        let max_content = fs::read_to_string(&cpu_max_path).ok()?;
        let parts: Vec<&str> = max_content.split_whitespace().collect();
        if parts.len() >= 2 {
            let limit = if parts[0] == "max" {
                u64::MAX
            } else {
                parts[0].parse().unwrap_or(u64::MAX)
            };
            let period = parts[1].parse().unwrap_or(100000u64);
            (limit, period)
        } else {
            (u64::MAX, 100000u64)
        }
    } else {
        (u64::MAX, 100000u64)
    };

    // 计算使用率（简单估算）
    let usage_percent = if period_us > 0 {
        (usage_us as f64 / period_us as f64) * 100.0
    } else {
        0.0
    };

    // 计算 throttle 率
    let total_periods = nr_throttled.saturating_add(1);
    let throttle_rate = (nr_throttled as f64 / total_periods as f64) * 100.0;

    // 判断是否高争抢
    let is_high_contention = nr_throttled > 100 || throttle_rate > 10.0 || usage_percent > 95.0;

    Some(CpuContentionStats {
        cpu_usage_us: usage_us,
        cpu_limit_us: limit_us,
        nr_throttled,
        throttled_time_ns,
        usage_percent,
        throttle_rate,
        is_high_contention,
    })
}

/// 从 cgroup fs 读取内存统计
fn read_memory_stats(cgroup_path: &Path) -> Option<MemoryContentionStats> {
    let memory_current_path = cgroup_path.join("memory.current");
    let memory_max_path = cgroup_path.join("memory.max");
    let memory_stat_path = cgroup_path.join("memory.stat");
    let memory_pressure_path = cgroup_path.join("memory.pressure");

    if !memory_current_path.exists() {
        return None;
    }

    // 读取当前内存使用
    let memory_usage = fs::read_to_string(&memory_current_path)
        .ok()?
        .trim()
        .parse::<u64>()
        .unwrap_or(0);

    // 读取内存限制
    let memory_limit = if memory_max_path.exists() {
        let max_str = fs::read_to_string(&memory_max_path).ok()?;
        if max_str.trim() == "max" {
            u64::MAX
        } else {
            max_str.trim().parse::<u64>().unwrap_or(u64::MAX)
        }
    } else {
        u64::MAX
    };

    // 计算内存使用百分比
    let usage_percent = if memory_limit > 0 && memory_limit != u64::MAX {
        (memory_usage as f64 / memory_limit as f64) * 100.0
    } else {
        0.0
    };

    // 读取内存统计详情
    let mut pgpgin = 0u64;
    let mut pgpgout = 0u64;
    if memory_stat_path.exists() {
        if let Ok(content) = fs::read_to_string(&memory_stat_path) {
            for line in content.lines() {
                let parts: Vec<&str> = line.split_whitespace().collect();
                if parts.len() >= 2 {
                    match parts[0] {
                        "pgpgin" => pgpgin = parts[1].parse().unwrap_or(0),
                        "pgpgout" => pgpgout = parts[1].parse().unwrap_or(0),
                        _ => {}
                    }
                }
            }
        }
    }

    // 读取内存压力（cgroup v2 memory.pressure）
    let (pressure_wait, pressure_score) = if memory_pressure_path.exists() {
        read_memory_pressure(&memory_pressure_path).unwrap_or((0, 0.0))
    } else {
        (0, 0.0)
    };

    // 判断是否高内存争抢
    let is_high_contention = usage_percent > 90.0 || pressure_score > 50.0;

    Some(MemoryContentionStats {
        memory_usage_bytes: memory_usage,
        memory_limit_bytes: memory_limit,
        usage_percent,
        memory_pressure_wait_us: pressure_wait,
        memory_pressure_score: pressure_score,
        pgpgin,
        pgpgout,
        is_high_contention,
    })
}

/// 解析 memory.pressure 文件
fn read_memory_pressure(path: &Path) -> Option<(u64, f64)> {
    let content = fs::read_to_string(path).ok()?;
    
    // 格式示例: "some avg10=0.00 avg60=0.00 avg300=0.00 total=12345\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=67890"
    let mut total_wait = 0u64;
    let mut avg10_score = 0.0f64;

    for line in content.lines() {
        if line.starts_with("some") || line.starts_with("full") {
            // 提取 total=xxx（微秒）
            if let Some(total_start) = line.find("total=") {
                let total_str = &line[total_start + 6..];
                let total_val = total_str.split_whitespace().next()
                    .and_then(|s| s.parse::<u64>().ok())
                    .unwrap_or(0);
                total_wait += total_val;
            }
            
            // 提取 avg10 分数
            if let Some(avg_start) = line.find("avg10=") {
                let avg_str = &line[avg_start + 6..];
                let avg_val = avg_str.split_whitespace().next()
                    .and_then(|s| s.parse::<f64>().ok())
                    .unwrap_or(0.0);
                avg10_score = avg10_score.max(avg_val);
            }
        }
    }

    Some((total_wait, avg10_score))
}

/// 从 cgroup fs 读取 IO 统计
fn read_io_stats(cgroup_path: &Path) -> Option<IoContentionStats> {
    let io_stat_path = cgroup_path.join("io.stat");
    let io_weight_path = cgroup_path.join("io.weight");

    if !io_stat_path.exists() {
        return None;
    }

    let content = fs::read_to_string(&io_stat_path).ok()?;
    let mut io_wait_time = 0u64;
    let io_service_time = 0u64;

    for line in content.lines() {
        // 格式: "8:0 rbytes=... wbytes=... rios=... wios=... dbytes=... dios=..."
        // 或 "8:0 rbytes=... wbytes=... rios=... wios=..."
        if line.contains(':') {
            // 这是一个设备行
            let parts: Vec<&str> = line.split_whitespace().collect();
            for part in parts.iter().skip(1) {
                if let Some(idx) = part.find('=') {
                    let key = &part[..idx];
                    let val = &part[idx + 1..];
                    // 查找可能的等待/服务时间字段
                    if key.contains("wait") || key.contains("delay") {
                        io_wait_time += val.parse::<u64>().unwrap_or(0);
                    }
                }
            }
        }
    }

    // 读取 IO 权重
    let io_weight = if io_weight_path.exists() {
        fs::read_to_string(&io_weight_path)
            .ok()?
            .trim()
            .parse::<u32>()
            .unwrap_or(100)
    } else {
        100
    };

    // 估算队列深度（简化）
    let queue_depth = if io_wait_time > 0 { 1 } else { 0 };

    // 判断是否高 IO 争抢（等待时间较长）
    let is_high_contention = io_wait_time > 1000000; // > 1秒

    Some(IoContentionStats {
        io_wait_time_us: io_wait_time,
        io_service_time_us: io_service_time,
        queue_depth,
        device_io_time_ms: io_wait_time / 1000,
        io_weight,
        is_high_contention,
    })
}

/// 查找 cgroup 路径
fn find_cgroup_path(cgroup_id: &str, pod_uid: Option<&str>, container_id: Option<&str>) -> Option<std::path::PathBuf> {
    // 尝试常见的 cgroup v2 路径
    let possible_paths = vec![
        format!("/sys/fs/cgroup{}", cgroup_id),
        format!("/sys/fs/cgroup/kubepods/{}", cgroup_id),
        format!("/sys/fs/cgroup/kubepods/pod{}/{}", pod_uid.unwrap_or(""), container_id.unwrap_or("")),
        format!("/sys/fs/cgroup/system.slice/{}" , cgroup_id),
    ];

    for path_str in possible_paths {
        let path = std::path::PathBuf::from(&path_str);
        if path.exists() {
            return Some(path);
        }
    }

    None
}

/// 计算综合争抢评分 (0-100)
fn calculate_contention_score(
    cpu: &Option<CpuContentionStats>,
    memory: &Option<MemoryContentionStats>,
    io: &Option<IoContentionStats>,
) -> (f64, String) {
    let mut score = 0.0;
    let mut max_type = "none".to_string();
    let mut _max_subtype_score = 0.0;

    // CPU 评分 (权重 40%)
    if let Some(c) = cpu {
        let cpu_score = (c.throttle_rate * 0.5) + (c.usage_percent * 0.5);
        let weighted_cpu_score = cpu_score * 0.4;
        score += weighted_cpu_score;
        if cpu_score > _max_subtype_score {
            _max_subtype_score = cpu_score;
            max_type = "cpu".to_string();
        }
    }

    // 内存评分 (权重 35%)
    if let Some(m) = memory {
        let mem_score = (m.usage_percent * 0.7) + (m.memory_pressure_score * 0.3);
        let weighted_mem_score = mem_score * 0.35;
        score += weighted_mem_score;
        if mem_score > _max_subtype_score {
            _max_subtype_score = mem_score;
            max_type = "memory".to_string();
        }
    }

    // IO 评分 (权重 25%)
    if let Some(i) = io {
        // IO 等待时间转换为分数 (假设 > 100ms 为高分)
        let io_score = (i.io_wait_time_us as f64 / 100000.0).min(100.0);
        let weighted_io_score = io_score * 0.25;
        score += weighted_io_score;
        if io_score > _max_subtype_score {
            _max_subtype_score = io_score;
            max_type = "io".to_string();
        }
    }

    (score.min(100.0), max_type)
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

fn make_collection_id() -> String {
    uuid::Uuid::new_v4().to_string()
}

/// 运行 cgroup 资源争抢采集
pub async fn run_cgroup_contention_collect(
    cfg: &CgroupContentionConfig,
) -> Result<Evidence, CollectionError> {
    let collection_id = make_collection_id();
    let scope_key = make_scope_key(
        cfg.pod.as_ref().and_then(|p| p.uid.as_ref().map(|s| s.as_str())),
        cfg.cgroup_id.as_ref().map(|s| s.as_str()),
    );

    // 查找 cgroup 路径
    let cgroup_path = if let Some(ref cid) = cfg.cgroup_id {
        find_cgroup_path(cid, cfg.pod.as_ref().and_then(|p| p.uid.as_deref()), cfg.container_id.as_deref())
    } else {
        None
    };

    // 采集各项统计
    let cpu_stats = cgroup_path.as_ref().and_then(|p| read_cpu_stats(p));
    let memory_stats = cgroup_path.as_ref().and_then(|p| read_memory_stats(p));
    let io_stats = cgroup_path.as_ref().and_then(|p| read_io_stats(p));

    // 计算争抢评分
    let (contention_score, _primary_type) = calculate_contention_score(&cpu_stats, &memory_stats, &io_stats);

    // 构建归属信息
    let has_cgroup = cfg.cgroup_id.is_some();
    let attribution = Attribution {
        status: if has_cgroup { "nri_mapped".into() } else { "cgroup_fallback".into() },
        confidence: Some(if has_cgroup { 0.9 } else { 0.5 }),
        source: if has_cgroup { Some("nri".into()) } else { Some("cgroup_fs".into()) },
        mapping_version: None,
    };

    // 构建范围信息
    let scope = Scope {
        pod: cfg.pod.clone(),
        container_id: cfg.container_id.clone(),
        cgroup_id: cfg.cgroup_id.clone(),
        pid_scope: None,
        scope_key: scope_key.clone(),
        network_target: None,
    };

    // 构建元数据
    let collection = CollectionMeta {
        collection_id: collection_id.clone(),
        collection_status: if cpu_stats.is_some() || memory_stats.is_some() { "success".into() } else { "partial".into() },
        probe_id: "cgroup_contention_fs".into(),
        errors: vec![],
    };

    // 构建 metric_summary
    let mut metric_summary = HashMap::new();
    if let Some(ref cpu) = cpu_stats {
        metric_summary.insert("cpu_usage_percent".into(), cpu.usage_percent);
        metric_summary.insert("cpu_throttle_rate".into(), cpu.throttle_rate);
        metric_summary.insert("cpu_nr_throttled".into(), cpu.nr_throttled as f64);
    }
    if let Some(ref mem) = memory_stats {
        metric_summary.insert("memory_usage_percent".into(), mem.usage_percent);
        metric_summary.insert("memory_pressure_score".into(), mem.memory_pressure_score);
    }
    if let Some(ref io) = io_stats {
        metric_summary.insert("io_wait_time_ms".into(), io.io_wait_time_us as f64 / 1000.0);
    }
    metric_summary.insert("contention_score".into(), contention_score);

    // 构建 events_topology
    let mut events_topology = vec![];
    if let Some(ref cpu) = cpu_stats {
        if cpu.is_high_contention {
            events_topology.push(Event {
                event_type: "cpu_throttle_high".into(),
                event_time_ms: cfg.time_window.start_time_ms,
                severity: Some(8),
                details: Some(json!({
                    "throttle_rate": cpu.throttle_rate,
                    "nr_throttled": cpu.nr_throttled,
                })),
            });
        }
    }
    if let Some(ref mem) = memory_stats {
        if mem.is_high_contention {
            events_topology.push(Event {
                event_type: "memory_pressure_high".into(),
                event_time_ms: cfg.time_window.start_time_ms,
                severity: Some(7),
                details: Some(json!({
                    "usage_percent": mem.usage_percent,
                    "pressure_score": mem.memory_pressure_score,
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

    let evidence_id = make_evidence_id(&cfg.task_id, "cgroup_contention", &collection_id, &scope_key);

    Ok(Evidence {
        schema_version: "evidence.v0.2".into(),
        task_id: cfg.task_id.clone(),
        evidence_id,
        evidence_type: "cgroup_contention".into(),
        collection,
        time_window: cfg.time_window.clone(),
        scope,
        selection: Some(selection),
        metric_summary,
        events_topology,
        top_calls: None,
        attribution,
    })
}

/// POC 采集函数（简化版，用于集成测试）
pub async fn run_cgroup_contention_collect_poc(
    cfg: &CgroupContentionConfig,
) -> Result<Evidence, CollectionError> {
    let collection_id = make_collection_id();
    let scope_key = make_scope_key(
        cfg.pod.as_ref().and_then(|p| p.uid.as_ref().map(|s| s.as_str())),
        cfg.cgroup_id.as_ref().map(|s| s.as_str()),
    );

    // POC 模式：模拟采集数据
    let cpu_stats = CpuContentionStats {
        cpu_usage_us: 950000, // 95% 使用率
        cpu_limit_us: 1000000,
        nr_throttled: 50,
        throttled_time_ns: 500000000,
        usage_percent: 95.0,
        throttle_rate: 5.0,
        is_high_contention: true,
    };

    let memory_stats = MemoryContentionStats {
        memory_usage_bytes: 900_000_000,
        memory_limit_bytes: 1_000_000_000,
        usage_percent: 90.0,
        memory_pressure_wait_us: 10000,
        memory_pressure_score: 15.0,
        pgpgin: 1000,
        pgpgout: 500,
        is_high_contention: true,
    };

    let io_stats = IoContentionStats {
        io_wait_time_us: 500000,
        io_service_time_us: 2000000,
        queue_depth: 2,
        device_io_time_ms: 500,
        io_weight: 100,
        is_high_contention: false,
    };

    let cpu_opt = Some(cpu_stats.clone());
    let memory_opt = Some(memory_stats.clone());
    let io_opt = Some(io_stats.clone());
    let (contention_score, _primary_type) = calculate_contention_score(&cpu_opt, &memory_opt, &io_opt);

    // 查询 NRI 映射表获取归属信息
    let attribution_info = if let Some(ref table) = cfg.nri_table {
        let pod_uid = cfg.pod.as_ref().and_then(|p| p.uid.as_deref());
        let cgroup_id = cfg.cgroup_id.as_deref();
        table.resolve_attribution(pod_uid, cgroup_id, None).ok()
    } else {
        None
    };

    let scope = Scope {
        pod: cfg.pod.clone(),
        container_id: cfg.container_id.clone(),
        cgroup_id: cfg.cgroup_id.clone(),
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
        let has_cgroup = cfg.cgroup_id.is_some();
        Attribution {
            status: if has_cgroup { "nri_mapped".into() } else { "cgroup_fallback".into() },
            confidence: Some(if has_cgroup { 0.9 } else { 0.5 }),
            source: if has_cgroup { Some("nri".into()) } else { Some("cgroup_fs".into()) },
            mapping_version: None,
        }
    };

    let collection = CollectionMeta {
        collection_id: collection_id.clone(),
        collection_status: "success".into(),
        probe_id: "cgroup_contention_poc".into(),
        errors: vec![],
    };

    let evidence_id = make_evidence_id(&cfg.task_id, "cgroup_contention", &collection_id, &scope_key);

    // 构建 metric_summary
    let mut metric_summary = HashMap::new();
    metric_summary.insert("cpu_usage_percent".into(), cpu_stats.usage_percent);
    metric_summary.insert("cpu_throttle_rate".into(), cpu_stats.throttle_rate);
    metric_summary.insert("cpu_nr_throttled".into(), cpu_stats.nr_throttled as f64);
    metric_summary.insert("memory_usage_percent".into(), memory_stats.usage_percent);
    metric_summary.insert("memory_pressure_score".into(), memory_stats.memory_pressure_score);
    metric_summary.insert("io_wait_time_ms".into(), io_stats.io_wait_time_us as f64 / 1000.0);
    metric_summary.insert("contention_score".into(), contention_score);

    // 构建 events_topology
    let mut events_topology = vec![];
    if cpu_stats.is_high_contention {
        events_topology.push(Event {
            event_type: "cpu_throttle_high".into(),
            event_time_ms: cfg.time_window.start_time_ms,
            severity: Some(8),
            details: Some(json!({
                "throttle_rate": cpu_stats.throttle_rate,
                "nr_throttled": cpu_stats.nr_throttled,
            })),
        });
    }
    if memory_stats.is_high_contention {
        events_topology.push(Event {
            event_type: "memory_pressure_high".into(),
            event_time_ms: cfg.time_window.start_time_ms,
            severity: Some(7),
            details: Some(json!({
                "usage_percent": memory_stats.usage_percent,
                "pressure_score": memory_stats.memory_pressure_score,
            })),
        });
    }

    let collected_metrics: Vec<String> = metric_summary.keys().cloned().collect();
    let collected_events: Vec<String> = events_topology.iter().map(|e| e.event_type.clone()).collect();

    let selection = Selection {
        requested_metrics: cfg.requested_metrics.clone(),
        collected_metrics,
        requested_events: cfg.requested_events.clone(),
        collected_events,
    };

    Ok(Evidence {
        schema_version: "evidence.v0.2".into(),
        task_id: cfg.task_id.clone(),
        evidence_id,
        evidence_type: "cgroup_contention".into(),
        collection,
        time_window: cfg.time_window.clone(),
        scope,
        selection: Some(selection),
        metric_summary,
        events_topology,
        top_calls: None,
        attribution,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_contention_score_calculation() {
        let cpu = Some(CpuContentionStats {
            cpu_usage_us: 900000,
            cpu_limit_us: 1000000,
            nr_throttled: 100,
            throttled_time_ns: 1000000000,
            usage_percent: 90.0,
            throttle_rate: 20.0,
            is_high_contention: true,
        });

        let memory = Some(MemoryContentionStats {
            memory_usage_bytes: 950_000_000,
            memory_limit_bytes: 1_000_000_000,
            usage_percent: 95.0,
            memory_pressure_wait_us: 50000,
            memory_pressure_score: 60.0,
            pgpgin: 5000,
            pgpgout: 2000,
            is_high_contention: true,
        });

        let io = None;

        let (score, primary_type) = calculate_contention_score(&cpu, &memory, &io);
        
        // 验证分数在合理范围内
        assert!(score > 0.0 && score <= 100.0);
        // 验证主要类型
        assert!(primary_type == "cpu" || primary_type == "memory");
    }

    #[test]
    fn test_read_memory_pressure_parsing() {
        // 模拟 pressure 文件内容
        let test_content = "some avg10=5.23 avg60=2.10 avg300=1.00 total=123456\nfull avg10=3.00 avg60=1.50 avg300=0.50 total=78901";
        
        // 写入临时文件测试
        let temp_dir = std::env::temp_dir();
        let test_file = temp_dir.join("test_pressure");
        std::fs::write(&test_file, test_content).unwrap();
        
        let result = read_memory_pressure(&test_file);
        assert!(result.is_some());
        
        let (wait, score) = result.unwrap();
        assert_eq!(wait, 123456 + 78901);
        assert_eq!(score, 5.23); // max of avg10
        
        // 清理
        let _ = std::fs::remove_file(&test_file);
    }

    #[tokio::test]
    async fn test_poc_collection() {
        let cfg = CgroupContentionConfig {
            task_id: "test-task".to_string(),
            time_window: TimeWindow {
                start_time_ms: 1000,
                end_time_ms: 5000,
                collection_interval_ms: None,
            },
            pod: Some(PodInfo {
                uid: Some("test-pod".to_string()),
                name: Some("test-pod-name".to_string()),
                namespace: Some("default".to_string()),
            }),
            container_id: Some("ctr-001".to_string()),
            cgroup_id: Some("/kubepods/pod-001/ctr-001".to_string()),
            requested_metrics: vec![],
            requested_events: vec![],
            nri_table: None,
        };

        let result = run_cgroup_contention_collect_poc(&cfg).await;
        assert!(result.is_ok());
        
        let evidence = result.unwrap();
        assert_eq!(evidence.evidence_type, "cgroup_contention");
        assert!(evidence.metric_summary.contains_key("contention_score"));
        assert!(evidence.metric_summary.contains_key("cpu_usage_percent"));
        assert!(evidence.metric_summary.contains_key("memory_usage_percent"));
        assert!(!evidence.events_topology.is_empty());
        
        // 验证包含高争抢事件
        let has_cpu_event = evidence.events_topology.iter().any(|e| e.event_type == "cpu_throttle_high");
        let has_memory_event = evidence.events_topology.iter().any(|e| e.event_type == "memory_pressure_high");
        assert!(has_cpu_event || has_memory_event);
    }
}
