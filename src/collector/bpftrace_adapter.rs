//! BPFTrace Adapter - 标准化的bpftrace输出解析与字段映射
//!
//! 提供通用接口，将不同bpftrace脚本的输出转换为标准化的Evidence Schema。
//! 支持配置驱动的字段映射，以适应客户的自定义脚本。

use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::HashMap;
use std::io::{BufRead, BufReader};
use std::process::{Child, Command, Stdio};
use std::time::{Duration, Instant};

/// BPFTrace事件类型
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum BpftraceEventType {
    /// 采集开始
    Start,
    /// 采集结束
    End,
    /// I/O完成事件
    IoComplete,
    /// I/O超时事件
    IoTimeout,
    /// TCP连接事件
    TcpConnect,
    /// TCP重置事件
    TcpReset,
    /// 网络丢包事件
    PacketDrop,
    /// 系统调用事件
    Syscall,
    /// 文件系统stall事件
    FsStall,
    /// OOM事件
    OomEvent,
    /// 聚合统计
    Stats,
    /// 通用数据事件
    Data,
    /// 未知类型
    Unknown(String),
}

impl From<&str> for BpftraceEventType {
    fn from(s: &str) -> Self {
        match s {
            "start" => Self::Start,
            "end" => Self::End,
            "io_complete" => Self::IoComplete,
            "io_timeout" => Self::IoTimeout,
            "tcp_connect" => Self::TcpConnect,
            "tcp_reset" => Self::TcpReset,
            "packet_drop" => Self::PacketDrop,
            "syscall" => Self::Syscall,
            "fs_stall" => Self::FsStall,
            "oom_event" => Self::OomEvent,
            "stats" => Self::Stats,
            "data" => Self::Data,
            _ => Self::Unknown(s.to_string()),
        }
    }
}

/// BPFTrace原始事件
#[derive(Debug, Clone)]
pub struct BpftraceRawEvent {
    pub event_type: BpftraceEventType,
    pub timestamp_ms: Option<i64>,
    pub pid: Option<u32>,
    pub comm: Option<String>,
    pub fields: HashMap<String, Value>,
}

/// 字段映射配置 - 用于适配客户脚本的字段命名差异
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct FieldMappingConfig {
    /// 事件类型字段名（默认："type"）
    #[serde(default = "default_type_field")]
    pub event_type_field: String,
    /// 时间戳字段名（默认："ts_ms"）
    #[serde(default = "default_timestamp_field")]
    pub timestamp_field: String,
    /// PID字段名（默认："pid"）
    #[serde(default = "default_pid_field")]
    pub pid_field: String,
    /// 命令名字段名（默认："comm"）
    #[serde(default = "default_comm_field")]
    pub comm_field: String,
    /// 延迟字段名映射（用于网络/IO类）
    #[serde(default)]
    pub latency_field_aliases: Vec<String>,
    /// 字节数字段名映射（用于IO类）
    #[serde(default)]
    pub bytes_field_aliases: Vec<String>,
    /// 自定义字段映射：脚本字段名 -> 标准字段名
    #[serde(default)]
    pub custom_mappings: HashMap<String, String>,
}

fn default_type_field() -> String { "type".to_string() }
fn default_timestamp_field() -> String { "ts_ms".to_string() }
fn default_pid_field() -> String { "pid".to_string() }
fn default_comm_field() -> String { "comm".to_string() }

impl Default for FieldMappingConfig {
    fn default() -> Self {
        Self {
            event_type_field: default_type_field(),
            timestamp_field: default_timestamp_field(),
            pid_field: default_pid_field(),
            comm_field: default_comm_field(),
            latency_field_aliases: vec!["latency_us".to_string(), "latency_ms".to_string(), "latency".to_string()],
            bytes_field_aliases: vec!["bytes".to_string(), "size".to_string(), "len".to_string()],
            custom_mappings: HashMap::new(),
        }
    }
}

/// BPFTrace采集配置
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct BpftraceCollectionConfig {
    /// bpftrace脚本路径
    pub script_path: String,
    /// 采集持续时间（秒）
    pub duration_sec: u64,
    /// 字段映射配置
    pub field_mapping: FieldMappingConfig,
    /// 额外的脚本参数
    #[serde(default)]
    pub extra_args: Vec<String>,
    /// 是否使用sudo
    #[serde(default = "default_use_sudo")]
    pub use_sudo: bool,
    /// 指标白名单（仅采集这些指标，空=不过滤）
    #[serde(default)]
    pub metric_whitelist: Vec<String>,
    /// 指标黑名单（不采集这些指标，空=不过滤）
    #[serde(default)]
    pub metric_blacklist: Vec<String>,
    /// 目标 PID 列表（仅采集这些进程的事件，空=不过滤）
    #[serde(default)]
    pub target_pids: Vec<u32>,
}

fn default_use_sudo() -> bool { true }

impl Default for BpftraceCollectionConfig {
    fn default() -> Self {
        Self {
            script_path: String::new(),
            duration_sec: 5,
            field_mapping: FieldMappingConfig::default(),
            extra_args: Vec::new(),
            use_sudo: true,
            metric_whitelist: Vec::new(),
            metric_blacklist: Vec::new(),
            target_pids: Vec::new(),
        }
    }
}

/// 采集结果
#[derive(Debug, Clone)]
pub struct BpftraceCollectionResult {
    /// 原始事件列表
    pub events: Vec<BpftraceRawEvent>,
    /// 指标聚合结果
    pub metrics: HashMap<String, f64>,
    /// 错误列表
    pub errors: Vec<BpftraceAdapterError>,
    /// 采集状态
    pub status: CollectionStatus,
    /// 统计信息
    pub stats: CollectionStats,
}

/// 采集状态
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum CollectionStatus {
    Success,
    Partial,
    Failed,
}

/// 采集统计
#[derive(Debug, Clone, Default)]
pub struct CollectionStats {
    pub total_events: u64,
    pub parsed_events: u64,
    pub dropped_lines: u64,
    pub duration_ms: u64,
}

/// BPFTrace Adapter错误
#[derive(Debug, Clone)]
pub enum BpftraceAdapterError {
    ScriptLoadFailed { message: String },
    ParseError { line: String, reason: String },
    Timeout,
    ProcessError { code: Option<i32>, message: String },
}

impl std::fmt::Display for BpftraceAdapterError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::ScriptLoadFailed { message } => write!(f, "Script load failed: {}", message),
            Self::ParseError { line, reason } => write!(f, "Parse error: {} (line: {})", reason, line),
            Self::Timeout => write!(f, "Collection timeout"),
            Self::ProcessError { code, message } => {
                write!(f, "Process error: {} (code: {:?})", message, code)
            }
        }
    }
}

impl std::error::Error for BpftraceAdapterError {}

/// BPFTrace Adapter - 标准化的bpftrace采集接口
pub struct BpftraceAdapter {
    config: BpftraceCollectionConfig,
}

impl BpftraceAdapter {
    /// 创建新的Adapter
    pub fn new(config: BpftraceCollectionConfig) -> Self {
        Self { config }
    }

    /// 执行采集并解析结果
    pub fn collect(&self) -> BpftraceCollectionResult {
        let start_time = Instant::now();
        let mut result = BpftraceCollectionResult {
            events: Vec::new(),
            metrics: HashMap::new(),
            errors: Vec::new(),
            status: CollectionStatus::Success,
            stats: CollectionStats::default(),
        };

        // 启动bpftrace进程
        let mut child = match self.spawn_bpftrace() {
            Ok(c) => c,
            Err(e) => {
                result.errors.push(e);
                result.status = CollectionStatus::Failed;
                return result;
            }
        };

        let stdout = child.stdout.take().expect("Failed to capture stdout");
        let reader = BufReader::new(stdout);

        // 解析输出
        let timeout = Duration::from_secs(self.config.duration_sec);
        for line in reader.lines() {
            if start_time.elapsed() > timeout {
                break;
            }

            let line = match line {
                Ok(l) => l,
                Err(e) => {
                    result.stats.dropped_lines += 1;
                    result.errors.push(BpftraceAdapterError::ParseError {
                        line: String::new(),
                        reason: format!("Read error: {}", e),
                    });
                    continue;
                }
            };

            result.stats.total_events += 1;

            // 解析事件
            match self.parse_line(&line) {
                Ok(Some(event)) => {
                    result.events.push(event);
                    result.stats.parsed_events += 1;
                }
                Ok(None) => {
                    // 忽略非数据事件（start/end等）
                }
                Err(e) => {
                    result.stats.dropped_lines += 1;
                    result.errors.push(e);
                }
            }
        }

        // 停止进程
        let _ = child.kill();

        // 计算聚合指标
        result.metrics = self.aggregate_metrics(&result.events);
        result.stats.duration_ms = start_time.elapsed().as_millis() as u64;

        // 确定状态
        result.status = if result.errors.is_empty() {
            CollectionStatus::Success
        } else if result.events.is_empty() {
            CollectionStatus::Failed
        } else {
            CollectionStatus::Partial
        };

        result
    }

    /// 启动bpftrace进程
    fn spawn_bpftrace(&self) -> Result<Child, BpftraceAdapterError> {
        let mut cmd = if self.config.use_sudo {
            let mut c = Command::new("sudo");
            c.arg("bpftrace");
            c
        } else {
            Command::new("bpftrace")
        };

        cmd.arg(&self.config.script_path)
            .args(&self.config.extra_args);
        
        // 添加目标 PID 过滤（如果指定了 target_pids）
        // bpftrace 支持 -p PID 参数进行进程过滤
        for pid in &self.config.target_pids {
            cmd.arg("-p").arg(pid.to_string());
        }
        
        cmd.stdout(Stdio::piped())
            .stderr(Stdio::piped());

        cmd.spawn().map_err(|e| BpftraceAdapterError::ScriptLoadFailed {
            message: format!("Failed to spawn bpftrace: {}", e),
        })
    }

    /// 解析单行输出
    fn parse_line(&self, line: &str) -> Result<Option<BpftraceRawEvent>, BpftraceAdapterError> {
        // 跳过空行
        if line.trim().is_empty() {
            return Ok(None);
        }

        // 解析JSON
        let json: Value = serde_json::from_str(line).map_err(|e| BpftraceAdapterError::ParseError {
            line: line.to_string(),
            reason: format!("JSON parse error: {}", e),
        })?;

        // 确保是对象
        let obj = json.as_object().ok_or_else(|| BpftraceAdapterError::ParseError {
            line: line.to_string(),
            reason: "Expected JSON object".to_string(),
        })?;

        // 提取事件类型
        let event_type = obj
            .get(&self.config.field_mapping.event_type_field)
            .and_then(|v| v.as_str())
            .map(BpftraceEventType::from)
            .unwrap_or_else(|| BpftraceEventType::Unknown("missing".to_string()));

        // 跳过控制事件
        match event_type {
            BpftraceEventType::Start | BpftraceEventType::End | BpftraceEventType::Stats => {
                return Ok(None);
            }
            _ => {}
        }

        // 提取时间戳
        let timestamp_ms = obj
            .get(&self.config.field_mapping.timestamp_field)
            .and_then(|v| v.as_i64())
            .or_else(|| {
                // 尝试从其他常见字段提取
                obj.get("timestamp").and_then(|v| v.as_i64())
            });

        // 提取PID
        let pid = obj
            .get(&self.config.field_mapping.pid_field)
            .and_then(|v| v.as_u64())
            .map(|v| v as u32);

        // 提取comm
        let comm = obj
            .get(&self.config.field_mapping.comm_field)
            .and_then(|v| v.as_str())
            .map(|s| s.to_string());

        // 构建字段map（排除已提取的标准字段）
        let mut fields = HashMap::new();
        for (key, value) in obj {
            let standard_key = match key.as_str() {
                k if k == self.config.field_mapping.event_type_field => continue,
                k if k == self.config.field_mapping.timestamp_field => continue,
                k if k == self.config.field_mapping.pid_field => continue,
                k if k == self.config.field_mapping.comm_field => continue,
                _ => key.clone(),
            };
            fields.insert(standard_key, value.clone());
        }

        // 应用自定义映射
        for (script_field, standard_field) in &self.config.field_mapping.custom_mappings {
            if let Some(value) = obj.get(script_field) {
                fields.insert(standard_field.clone(), value.clone());
            }
        }

        Ok(Some(BpftraceRawEvent {
            event_type,
            timestamp_ms,
            pid,
            comm,
            fields,
        }))
    }

    /// 检查指标是否允许通过过滤（白名单优先）
    fn is_metric_allowed(&self, metric_name: &str) -> bool {
        // 如果白名单非空，只允许白名单中的指标
        if !self.config.metric_whitelist.is_empty() {
            return self.config.metric_whitelist.iter()
                .any(|pattern| Self::match_pattern(metric_name, pattern));
        }
        
        // 如果黑名单非空，不允许黑名单中的指标
        if !self.config.metric_blacklist.is_empty() {
            return !self.config.metric_blacklist.iter()
                .any(|pattern| Self::match_pattern(metric_name, pattern));
        }
        
        // 无过滤，允许所有
        true
    }
    
    /// 简单的通配符匹配（支持*通配符）
    fn match_pattern(name: &str, pattern: &str) -> bool {
        if pattern == "*" || pattern == name {
            return true;
        }
        if pattern.ends_with("*") {
            let prefix = &pattern[..pattern.len()-1];
            return name.starts_with(prefix);
        }
        if pattern.starts_with("*") {
            let suffix = &pattern[1..];
            return name.ends_with(suffix);
        }
        name == pattern
    }

    /// 聚合指标
    fn aggregate_metrics(&self, events: &[BpftraceRawEvent]) -> HashMap<String, f64> {
        let mut metrics = HashMap::new();

        if events.is_empty() {
            return metrics;
        }

        // 计数
        metrics.insert("event_count".to_string(), events.len() as f64);

        // 提取延迟指标
        let latencies: Vec<f64> = events
            .iter()
            .filter_map(|e| {
                for alias in &self.config.field_mapping.latency_field_aliases {
                    if let Some(Value::Number(n)) = e.fields.get(alias) {
                        if let Some(v) = n.as_f64() {
                            return Some(v);
                        }
                    }
                }
                None
            })
            .collect();

        if !latencies.is_empty() {
            let sum: f64 = latencies.iter().sum();
            let avg = sum / latencies.len() as f64;
            let min = latencies.iter().cloned().fold(f64::INFINITY, f64::min);
            let max = latencies.iter().cloned().fold(0f64, f64::max);

            metrics.insert("latency_avg".to_string(), avg);
            metrics.insert("latency_min".to_string(), min);
            metrics.insert("latency_max".to_string(), max);
            metrics.insert("latency_p50".to_string(), percentile(&latencies, 50.0));
            metrics.insert("latency_p99".to_string(), percentile(&latencies, 99.0));
        }

        // 提取字节指标
        let bytes: Vec<f64> = events
            .iter()
            .filter_map(|e| {
                for alias in &self.config.field_mapping.bytes_field_aliases {
                    if let Some(Value::Number(n)) = e.fields.get(alias) {
                        if let Some(v) = n.as_f64() {
                            return Some(v);
                        }
                    }
                }
                None
            })
            .collect();

        if !bytes.is_empty() {
            let total: f64 = bytes.iter().sum();
            metrics.insert("bytes_total".to_string(), total);
            metrics.insert("bytes_avg".to_string(), total / bytes.len() as f64);
        }
        
        // 应用指标过滤
        if !self.config.metric_whitelist.is_empty() || !self.config.metric_blacklist.is_empty() {
            metrics = metrics.into_iter()
                .filter(|(name, _)| self.is_metric_allowed(name))
                .collect();
        }

        metrics
    }
}

/// 计算百分位数
fn percentile(sorted_data: &[f64], p: f64) -> f64 {
    if sorted_data.is_empty() {
        return 0.0;
    }
    let mut sorted = sorted_data.to_vec();
    sorted.sort_by(|a, b| a.partial_cmp(b).unwrap());
    
    let index = (p / 100.0) * (sorted.len() as f64 - 1.0);
    let lower = index.floor() as usize;
    let upper = index.ceil() as usize;
    
    if lower == upper {
        sorted[lower]
    } else {
        let weight = index - lower as f64;
        sorted[lower] * (1.0 - weight) + sorted[upper] * weight
    }
}

/// 便捷函数：执行标准block_io采集
pub fn collect_block_io(script_path: &str, duration_sec: u64) -> BpftraceCollectionResult {
    let config = BpftraceCollectionConfig {
        script_path: script_path.to_string(),
        duration_sec,
        field_mapping: FieldMappingConfig {
            latency_field_aliases: vec!["latency_us".to_string()],
            bytes_field_aliases: vec!["bytes".to_string()],
            ..Default::default()
        },
        ..Default::default()
    };
    let adapter = BpftraceAdapter::new(config);
    adapter.collect()
}

/// 便捷函数：执行标准network采集
pub fn collect_network(script_path: &str, duration_sec: u64) -> BpftraceCollectionResult {
    let config = BpftraceCollectionConfig {
        script_path: script_path.to_string(),
        duration_sec,
        field_mapping: FieldMappingConfig {
            latency_field_aliases: vec!["latency_us".to_string(), "latency_ms".to_string()],
            ..Default::default()
        },
        ..Default::default()
    };
    let adapter = BpftraceAdapter::new(config);
    adapter.collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn test_field_mapping_default() {
        let config = FieldMappingConfig::default();
        assert_eq!(config.event_type_field, "type");
        assert_eq!(config.timestamp_field, "ts_ms");
        assert_eq!(config.pid_field, "pid");
        assert_eq!(config.comm_field, "comm");
    }

    #[test]
    fn test_parse_event_type() {
        assert_eq!(BpftraceEventType::from("io_complete"), BpftraceEventType::IoComplete);
        assert_eq!(BpftraceEventType::from("tcp_connect"), BpftraceEventType::TcpConnect);
        assert_eq!(BpftraceEventType::from("unknown"), BpftraceEventType::Unknown("unknown".to_string()));
    }

    #[test]
    fn test_percentile() {
        let data = vec![1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0];
        assert_eq!(percentile(&data, 50.0), 5.5);
        assert_eq!(percentile(&data, 0.0), 1.0);
        assert_eq!(percentile(&data, 100.0), 10.0);
    }

    #[test]
    fn test_aggregate_metrics() {
        let config = BpftraceCollectionConfig::default();
        let adapter = BpftraceAdapter::new(config);

        let events = vec![
            BpftraceRawEvent {
                event_type: BpftraceEventType::IoComplete,
                timestamp_ms: Some(1000),
                pid: Some(1234),
                comm: Some("test".to_string()),
                fields: {
                    let mut m = HashMap::new();
                    m.insert("latency_us".to_string(), json!(100.0));
                    m.insert("bytes".to_string(), json!(4096));
                    m
                },
            },
            BpftraceRawEvent {
                event_type: BpftraceEventType::IoComplete,
                timestamp_ms: Some(1001),
                pid: Some(1234),
                comm: Some("test".to_string()),
                fields: {
                    let mut m = HashMap::new();
                    m.insert("latency_us".to_string(), json!(200.0));
                    m.insert("bytes".to_string(), json!(8192));
                    m
                },
            },
        ];

        let metrics = adapter.aggregate_metrics(&events);
        assert_eq!(metrics.get("event_count"), Some(&2.0));
        assert_eq!(metrics.get("bytes_total"), Some(&12288.0));
        assert_eq!(metrics.get("latency_avg"), Some(&150.0));
    }
}
