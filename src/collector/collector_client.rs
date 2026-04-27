//! Collector Client - 连接到特权采集守护进程
//!
//! 为非特权的 nuts-observer 提供访问 collector daemon 的接口。
//! 使用 gRPC over Unix Socket 进行通信。
//! 
//! 注意: 当前版本使用开发模式（直接执行），gRPC over Unix Socket 功能待完善

use std::path::Path;

use tracing::{info, warn};

// 引入生成的 protobuf 代码用于类型定义
#[cfg(feature = "nri-grpc")]
use crate::collector::proto;

/// 采集器客户端错误
#[derive(Debug)]
pub enum CollectorClientError {
    /// 连接失败
    ConnectionError(String),
    /// 请求被拒绝（权限不足）
    PermissionDenied(String),
    /// 采集超时
    Timeout,
    /// 守护进程不可用
    DaemonUnavailable,
    /// 其他错误
    Other(String),
}

impl std::fmt::Display for CollectorClientError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::ConnectionError(msg) => write!(f, "Connection error: {}", msg),
            Self::PermissionDenied(msg) => write!(f, "Permission denied: {}", msg),
            Self::Timeout => write!(f, "Collection timed out"),
            Self::DaemonUnavailable => write!(f, "Collector daemon not available"),
            Self::Other(msg) => write!(f, "{}", msg),
        }
    }
}

impl std::error::Error for CollectorClientError {}

/// 采集器客户端
/// 
/// 注意: 当前版本使用简化实现，gRPC over Unix Socket 待完善
pub struct CollectorClient {
    socket_path: String,
    connected: bool,
}

impl CollectorClient {
    /// 连接到 collector daemon
    /// 
    /// # Arguments
    /// * `socket_path` - Unix Socket 路径，默认为 "/run/nuts/collector.sock"
    pub async fn connect(socket_path: &str) -> Result<Self, CollectorClientError> {
        // 检查 socket 文件是否存在
        if !Path::new(socket_path).exists() {
            return Err(CollectorClientError::DaemonUnavailable);
        }

        // TODO: 实现 gRPC over Unix Socket
        // 当前版本使用开发模式回退
        info!("Collector daemon socket found at {}, but using dev mode fallback", socket_path);
        
        Ok(Self {
            socket_path: socket_path.to_string(),
            connected: false,
        })
    }

    /// 尝试连接，如果失败则返回 None（用于检测 daemon 是否可用）
    pub async fn try_connect(socket_path: &str) -> Option<Self> {
        match Self::connect(socket_path).await {
            Ok(client) => Some(client),
            Err(e) => {
                warn!("Failed to connect to collector daemon: {}", e);
                None
            }
        }
    }

    /// 执行 bpftrace 采集
    /// 
    /// 注意: 当前版本回退到开发模式执行
    pub async fn collect_bpftrace(
        &mut self,
        _task_id: &str,
        _script_path: &str,
        _duration_secs: u64,
        _scope_pid: Option<u32>,
        _evidence_type: &str,
    ) -> Result<proto::CollectResponse, CollectorClientError> {
        // 当前版本使用开发模式回退
        Err(CollectorClientError::DaemonUnavailable)
    }

    /// 读取 /proc 文件
    pub async fn read_proc(
        &mut self,
        _task_id: &str,
        _path: &str,
        _pid: Option<u32>,
    ) -> Result<proto::ReadProcResponse, CollectorClientError> {
        Err(CollectorClientError::DaemonUnavailable)
    }

    /// 取消正在进行的采集
    pub async fn cancel_collection(
        &mut self,
        _collection_id: &str,
        _reason: &str,
    ) -> Result<proto::CancelResponse, CollectorClientError> {
        Err(CollectorClientError::DaemonUnavailable)
    }

    /// 健康检查
    #[cfg(feature = "nri-grpc")]
    pub async fn health(&mut self, _include_stats: bool) -> Result<proto::HealthResponse, CollectorClientError> {
        Ok(proto::HealthResponse {
            healthy: self.connected,
            version: env!("CARGO_PKG_VERSION").to_string(),
            active_collections: 0,
            total_collections: 0,
            uptime_secs: 0,
            socket_path: self.socket_path.clone(),
            capabilities: vec![],
        })
    }

    /// 检查当前 UID 的权限
    #[cfg(feature = "nri-grpc")]
    pub async fn check_permission(&mut self, uid: u32) -> Result<proto::PermissionCheckResponse, CollectorClientError> {
        Ok(proto::PermissionCheckResponse {
            allowed: uid == 0 || uid == 1000,
            granted_permissions: vec![],
            message: "Using dev mode".to_string(),
        })
    }

    /// 获取 socket 路径
    pub fn socket_path(&self) -> &str {
        &self.socket_path
    }
}

/// 自动回退的采集器
/// 
/// 优先使用 daemon，如果不可用则回退到开发模式（直接执行）
pub struct AutoFallbackCollector {
    client: Option<CollectorClient>,
    socket_path: String,
    allow_dev_mode: bool,
}

impl AutoFallbackCollector {
    /// 创建新的自动回退采集器
    pub async fn new(socket_path: &str, allow_dev_mode: bool) -> Self {
        let client = CollectorClient::try_connect(socket_path).await;
        
        if client.is_none() && allow_dev_mode {
            warn!("Collector daemon not available, will use dev mode (direct execution)");
        }

        Self {
            client,
            socket_path: socket_path.to_string(),
            allow_dev_mode,
        }
    }

    /// 检查是否使用 daemon 模式
    pub fn is_daemon_mode(&self) -> bool {
        // 当前版本总是使用开发模式
        false
    }

    /// 执行采集
    pub async fn collect(
        &mut self,
        task_id: &str,
        script_path: &str,
        duration_secs: u64,
        scope_pid: Option<u32>,
        evidence_type: &str,
    ) -> Result<proto::CollectResponse, CollectorClientError> {
        // 当前版本直接使用开发模式
        self.collect_dev_mode(task_id, script_path, duration_secs, scope_pid, evidence_type).await
    }

    /// 开发模式采集（直接执行 bpftrace）
    async fn collect_dev_mode(
        &mut self,
        _task_id: &str,
        script_path: &str,
        duration_secs: u64,
        _scope_pid: Option<u32>,
        _evidence_type: &str,
    ) -> Result<proto::CollectResponse, CollectorClientError> {
        use tokio::process::Command;
        use tokio::time::{timeout, Duration};

        warn!("Using dev mode: executing bpftrace directly with sudo");

        let output = timeout(
            Duration::from_secs(duration_secs + 5), // 额外5秒缓冲
            Command::new("sudo")
                .args(["bpftrace", script_path])
                .output()
        )
        .await
        .map_err(|_| CollectorClientError::Timeout)?
        .map_err(|e| CollectorClientError::Other(format!("Failed to execute: {}", e)))?;

        let stdout = String::from_utf8_lossy(&output.stdout);
        let event_count = stdout.lines().count() as u32;

        Ok(proto::CollectResponse {
            collection_id: format!("dev-mode-{}", uuid::Uuid::new_v4()),
            raw_output: stdout.as_bytes().to_vec(),
            duration_ms: duration_secs * 1000, // 估算
            status: if output.status.success() { "success".to_string() } else { "error".to_string() },
            error_msg: if output.status.success() {
                None
            } else {
                Some(String::from_utf8_lossy(&output.stderr).to_string())
            },
            event_count,
            bytes_collected: stdout.len() as u64,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_collector_error_display() {
        let err = CollectorClientError::PermissionDenied("test".to_string());
        assert_eq!(err.to_string(), "Permission denied: test");
    }
}
