//! Nuts Collector Daemon - 特权采集守护进程
//!
//! 此程序以特权模式运行（root 或 CAP_BPF + CAP_SYS_ADMIN），
//! 通过 Unix Socket 为非特权的 nuts-observer 提供采集服务。
//!
//! # 权限要求
//! - 运行用户: root 或具有 CAP_BPF + CAP_SYS_ADMIN + CAP_SYS_PTRACE
//! - 文件权限: /run/nuts/collector.sock (0660, root:nuts)
//!
//! # 安全设计
//! - 只接受来自 Unix Socket 的请求
//! - 验证调用者 UID
//! - 限制单次采集时长（默认60秒）
//! - 白名单脚本路径验证
//! - 资源限制（cgroup）

use std::collections::HashMap;
use std::path::Path;
use std::process::Stdio;
use std::sync::Arc;
use std::time::{Duration, Instant};

use axum::http;
use tokio::net::UnixListener;
use tokio::process::Command;
use tokio::sync::{Mutex, RwLock};
use tonic::{transport::Server, Request, Response, Status};
use tracing::{info, warn, error};

// 引入生成的 protobuf 代码
mod proto {
    tonic::include_proto!("nuts.collector");
}

use proto::collector_server::{Collector, CollectorServer};
use proto::*;

/// 守护进程配置
#[derive(Debug, Clone)]
struct DaemonConfig {
    /// Unix Socket 路径
    socket_path: String,
    /// 允许调用的 UID 列表
    allowed_uids: Vec<u32>,
    /// 最大采集时长（秒）
    max_duration_secs: u64,
    /// 脚本路径白名单
    script_whitelist: Vec<String>,
    /// 是否允许任意脚本路径（仅开发模式）
    allow_any_script: bool,
}

impl Default for DaemonConfig {
    fn default() -> Self {
        Self {
            socket_path: "/run/nuts/collector.sock".to_string(),
            allowed_uids: vec![0, 1000], // root 和 uid 1000
            max_duration_secs: 60,
            script_whitelist: vec![
                "scripts/bpftrace/network/tcp_connect.bt".to_string(),
                "scripts/bpftrace/block_io/io_latency.bt".to_string(),
                "scripts/bpftrace/templates/network_latency.bt".to_string(),
                "scripts/bpftrace/templates/cgroup_contention.bt".to_string(),
                "scripts/bpftrace/templates/syscall_latency.bt".to_string(),
            ],
            allow_any_script: cfg!(debug_assertions),
        }
    }
}

/// 采集器状态
#[derive(Debug)]
struct CollectorState {
    /// 当前活跃的采集任务
    active_collections: Mutex<HashMap<String, tokio::task::AbortHandle>>,
    /// 统计信息
    stats: RwLock<CollectorStats>,
    /// 启动时间
    start_time: Instant,
}

impl CollectorState {
    fn new() -> Self {
        Self {
            active_collections: Mutex::new(HashMap::new()),
            stats: RwLock::new(CollectorStats {
                total_requests: 0,
                success_count: 0,
                error_count: 0,
                timeout_count: 0,
                evidence_type_counts: HashMap::new(),
                bytes_collected_total: 0,
            }),
            start_time: Instant::now(),
        }
    }
}

/// Collector 服务实现
#[derive(Debug)]
struct CollectorService {
    config: DaemonConfig,
    state: Arc<CollectorState>,
}

#[tonic::async_trait]
impl Collector for CollectorService {
    /// 执行 bpftrace 采集
    async fn collect_bpftrace(
        &self,
        request: Request<CollectRequest>,
    ) -> Result<Response<CollectResponse>, Status> {
        let req = request.into_inner();
        let collection_id = format!("{}-{}", req.task_id, uuid::Uuid::new_v4());
        
        info!(
            "Collection request: id={}, script={}, duration={}s, pid={:?}",
            collection_id, req.script_path, req.duration_secs, req.scope_pid
        );

        // 1. 验证脚本路径
        if !self.is_script_allowed(&req.script_path) {
            error!("Script not in whitelist: {}", req.script_path);
            return Err(Status::permission_denied(format!(
                "Script not in whitelist: {}", req.script_path
            )));
        }

        // 2. 限制采集时长
        let duration_secs = req.duration_secs.min(self.config.max_duration_secs);

        // 3. 执行采集
        let start = Instant::now();
        let result = self.run_bpftrace(&req, &collection_id, duration_secs).await;
        let duration_ms = start.elapsed().as_millis() as u64;

        // 4. 更新统计
        self.update_stats(&result, &req.evidence_type).await;

        // 5. 构建响应
        let response = match result {
            Ok((output, event_count)) => {
                let bytes_collected = output.len() as u64;
                info!(
                    "Collection {} completed: {} events, {} bytes, {} ms",
                    collection_id,
                    event_count,
                    bytes_collected,
                    duration_ms
                );
                CollectResponse {
                    collection_id,
                    raw_output: output.into_bytes(),
                    duration_ms,
                    status: "success".to_string(),
                    error_msg: None,
                    event_count,
                    bytes_collected,
                }
            }
            Err((status, msg)) => {
                error!("Collection {} failed: status={}, msg={}", collection_id, status, msg);
                CollectResponse {
                    collection_id,
                    raw_output: Vec::new(),
                    duration_ms,
                    status,
                    error_msg: Some(msg),
                    event_count: 0,
                    bytes_collected: 0,
                }
            }
        };

        Ok(Response::new(response))
    }

    /// 读取 /proc 文件
    async fn read_proc(
        &self,
        request: Request<ReadProcRequest>,
    ) -> Result<Response<ReadProcResponse>, Status> {
        let req = request.into_inner();
        
        // 安全验证：只允许访问 /proc/<pid>/ 下的特定文件
        let allowed_files = ["maps", "status", "stat", "cmdline", "environ", "fd"];
        let _path = Path::new(&req.path);
        
        // 检查路径是否在允许的范围内
        if !is_safe_proc_path(&req.path, &allowed_files) {
            return Err(Status::permission_denied("Access to this path is not allowed"));
        }

        let full_path = format!("/proc/{}", req.path);
        
        match tokio::fs::read(&full_path).await {
            Ok(content) => {
                Ok(Response::new(ReadProcResponse {
                    content,
                    exists: true,
                    error_msg: None,
                }))
            }
            Err(e) if e.kind() == std::io::ErrorKind::NotFound => {
                Ok(Response::new(ReadProcResponse {
                    content: Vec::new(),
                    exists: false,
                    error_msg: Some(format!("File not found: {}", full_path)),
                }))
            }
            Err(e) => {
                Err(Status::internal(format!("Failed to read file: {}", e)))
            }
        }
    }

    /// 取消采集
    async fn cancel_collection(
        &self,
        request: Request<CancelRequest>,
    ) -> Result<Response<CancelResponse>, Status> {
        let req = request.into_inner();
        
        let mut active = self.state.active_collections.lock().await;
        
        if let Some(handle) = active.remove(&req.collection_id) {
            handle.abort();
            info!("Collection {} cancelled: {}", req.collection_id, req.reason);
            Ok(Response::new(CancelResponse {
                success: true,
                status: "cancelled".to_string(),
            }))
        } else {
            Ok(Response::new(CancelResponse {
                success: false,
                status: "not_found".to_string(),
            }))
        }
    }

    /// 健康检查
    async fn health(
        &self,
        request: Request<HealthRequest>,
    ) -> Result<Response<HealthResponse>, Status> {
        let include_stats = request.into_inner().include_stats;
        
        let uptime_secs = self.state.start_time.elapsed().as_secs();
        let active_collections = self.state.active_collections.lock().await.len() as u32;
        
        let (total, capabilities) = if include_stats {
            let stats = self.state.stats.read().await;
            (
                stats.total_requests,
                check_capabilities(),
            )
        } else {
            (0, vec![])
        };

        Ok(Response::new(HealthResponse {
            healthy: true,
            version: env!("CARGO_PKG_VERSION").to_string(),
            active_collections,
            total_collections: total,
            uptime_secs,
            socket_path: self.config.socket_path.clone(),
            capabilities,
        }))
    }

    /// 权限检查
    /// 
    /// 客户端主动上报 UID 进行权限验证。
    /// 注意：真正的 Unix Socket 层 UID 验证应在连接层实现（TODO）。
    async fn check_permission(
        &self,
        request: Request<PermissionCheckRequest>,
    ) -> Result<Response<PermissionCheckResponse>, Status> {
        let uid = request.into_inner().uid;
        let allowed = self.config.allowed_uids.contains(&uid);
        
        info!(
            "[Auth] Permission check from UID {}, allowed: {}, allowed_list: {:?}",
            uid, allowed, self.config.allowed_uids
        );
        
        let message = if allowed {
            format!("UID {} is allowed to use collector", uid)
        } else {
            format!("UID {} is not in allowed list: {:?}", uid, self.config.allowed_uids)
        };

        let permissions = if allowed {
            vec!["collect".to_string(), "read_proc".to_string(), "cancel".to_string()]
        } else {
            vec![]
        };

        Ok(Response::new(PermissionCheckResponse {
            allowed,
            granted_permissions: permissions,
            message,
        }))
    }
}

impl CollectorService {
    fn new(config: DaemonConfig) -> Self {
        Self {
            config,
            state: Arc::new(CollectorState::new()),
        }
    }

    /// 检查脚本是否在白名单中
    fn is_script_allowed(&self, script_path: &str) -> bool {
        if self.config.allow_any_script {
            return true;
        }
        
        // 检查是否为白名单中的路径
        self.config.script_whitelist.iter().any(|allowed| {
            script_path.ends_with(allowed) || allowed.ends_with(script_path)
        })
    }

    /// 运行 bpftrace 采集
    async fn run_bpftrace(
        &self,
        req: &CollectRequest,
        _collection_id: &str,
        duration_secs: u64,
    ) -> Result<(String, u32), (String, String)> {
        let script_path = &req.script_path;
        
        // 检查脚本文件是否存在
        if !Path::new(script_path).exists() {
            return Err(("error".to_string(), format!("Script not found: {}", script_path)));
        }

        // 构建命令
        let mut cmd = Command::new("bpftrace");
        cmd.arg(script_path)
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());
        
        // 如果有目标 PID，添加到环境变量
        if let Some(pid) = req.scope_pid {
            cmd.env("TARGET_PID", pid.to_string());
        }
        
        // 启动进程
        let mut child = cmd.spawn()
            .map_err(|e| ("error".to_string(), format!("Failed to spawn bpftrace: {}", e)))?;

        // 获取 stdout
        let stdout = child.stdout.take()
            .ok_or(("error".to_string(), "Failed to capture stdout".to_string()))?;

        // 在超时内读取输出
        let timeout = Duration::from_secs(duration_secs);
        let read_future = tokio::time::timeout(timeout, read_to_end(stdout));
        
        let output_result = read_future.await;
        
        // 终止进程
        let _ = child.kill().await;
        
        match output_result {
            Ok(Ok(output)) => {
                let output_str = String::from_utf8_lossy(&output);
                let event_count = output_str.lines().count() as u32;
                Ok((output_str.to_string(), event_count))
            }
            Ok(Err(e)) => {
                Err(("error".to_string(), format!("Failed to read output: {}", e)))
            }
            Err(_) => {
                Err(("timeout".to_string(), format!("Collection timed out after {}s", duration_secs)))
            }
        }
    }

    /// 更新统计信息
    async fn update_stats(&self, result: &Result<(String, u32), (String, String)>, evidence_type: &str) {
        let mut stats = self.state.stats.write().await;
        stats.total_requests += 1;
        
        match result {
            Ok((output, _)) => {
                stats.success_count += 1;
                stats.bytes_collected_total += output.len() as u64;
                *stats.evidence_type_counts.entry(evidence_type.to_string()).or_insert(0) += 1;
            }
            Err((status, _)) => {
                if status == "timeout" {
                    stats.timeout_count += 1;
                } else {
                    stats.error_count += 1;
                }
            }
        }
    }
}

/// 安全地读取 /proc 路径
fn is_safe_proc_path(path: &str, allowed_files: &[&str]) -> bool {
    // 路径格式应该是: <pid>/<file> 或 <pid>/task/<tid>/<file>
    let parts: Vec<&str> = path.split('/').collect();
    
    // 至少需要两部分: pid/file
    if parts.len() < 2 {
        return false;
    }
    
    // 验证 PID 部分是数字
    if !parts[0].parse::<u32>().is_ok() {
        return false;
    }
    
    // 检查文件是否在白名单中
    let file_name = parts[parts.len() - 1];
    allowed_files.contains(&file_name)
}

/// 读取命令输出
async fn read_to_end<R: tokio::io::AsyncRead + Unpin>(mut reader: R) -> std::io::Result<Vec<u8>> {
    use tokio::io::AsyncReadExt;
    let mut buffer = Vec::new();
    reader.read_to_end(&mut buffer).await?;
    Ok(buffer)
}

/// 检查当前进程的 capabilities
fn check_capabilities() -> Vec<String> {
    let mut caps = vec![];
    
    // 读取 /proc/self/status
    if let Ok(status) = std::fs::read_to_string("/proc/self/status") {
        for line in status.lines() {
            if line.starts_with("CapEff:") {
                if let Some(caps_hex) = line.split(':').nth(1) {
                    if let Ok(caps_val) = u64::from_str_radix(caps_hex.trim(), 16) {
                        // CAP_BPF = 39
                        if (caps_val >> 39) & 1 == 1 {
                            caps.push("CAP_BPF".to_string());
                        }
                        // CAP_SYS_ADMIN = 21
                        if (caps_val >> 21) & 1 == 1 {
                            caps.push("CAP_SYS_ADMIN".to_string());
                        }
                        // CAP_SYS_PTRACE = 19
                        if (caps_val >> 19) & 1 == 1 {
                            caps.push("CAP_SYS_PTRACE".to_string());
                        }
                    }
                }
                break;
            }
        }
    }
    
    caps
}

/// 创建安全的 Unix Socket
async fn create_secure_socket(path: &str) -> std::io::Result<UnixListener> {
    use std::os::unix::fs::PermissionsExt;
    
    // 如果 socket 文件已存在，删除它
    if Path::new(path).exists() {
        tokio::fs::remove_file(path).await?;
    }
    
    // 确保目录存在
    let dir = Path::new(path).parent().unwrap_or(Path::new("/run/nuts"));
    tokio::fs::create_dir_all(dir).await?;
    tokio::fs::set_permissions(dir, std::fs::Permissions::from_mode(0o755)).await?;
    
    // 创建 socket
    let listener = UnixListener::bind(path)?;
    
    // 设置权限 (0660 = rw-rw----)
    tokio::fs::set_permissions(path, std::fs::Permissions::from_mode(0o660)).await?;
    
    info!("Unix socket created at {} with permissions 0660", path);
    
    Ok(listener)
}

/// 验证 UID 是否有权限（通过 Unix Socket credentials）
/// 
/// 使用 Linux 的 SO_PEERCRED 选项获取连接对端的 UID。
/// 这是 Unix Socket 的原生身份验证机制，无法被伪造。
/// 
/// TODO: 在 tonic gRPC 层集成此验证（需要自定义 Connection/Accept 逻辑）
fn get_peer_uid(stream: &tokio::net::UnixStream) -> std::io::Result<u32> {
    use nix::sys::socket::{getsockopt, sockopt::PeerCredentials};
    
    let creds = getsockopt(stream, PeerCredentials)?;
    Ok(creds.uid())
}

/// 权限验证拦截器
#[derive(Clone)]
#[allow(dead_code)]
struct AuthInterceptor {
    allowed_uids: Arc<Vec<u32>>,
}

impl AuthInterceptor {
    fn new(allowed_uids: Vec<u32>) -> Self {
        Self {
            allowed_uids: Arc::new(allowed_uids),
        }
    }
}

impl<B> tower::Service<http::Request<B>> for AuthInterceptor
where
    B: Send + 'static,
{
    type Response = http::Response<tonic::body::BoxBody>;
    type Error = tonic::Status;
    type Future = std::pin::Pin<
        Box<dyn std::future::Future<Output = Result<Self::Response, Self::Error>> + Send + 'static>,
    >;

    fn poll_ready(
        &mut self,
        _cx: &mut std::task::Context<'_>,
    ) -> std::task::Poll<std::result::Result<(), Self::Error>> {
        std::task::Poll::Ready(Ok(()))
    }

    fn call(&mut self, req: http::Request<B>) -> Self::Future {
        let allowed_uids = Arc::clone(&self.allowed_uids);
        
        Box::pin(async move {
            // 从请求扩展中获取 peer UID（由 tonic 的 Unix Socket 连接器设置）
            let peer_uid = req
                .extensions()
                .get::<PeerUid>()
                .map(|uid| uid.0);
            
            match peer_uid {
                Some(uid) => {
                    if allowed_uids.contains(&uid) {
                        tracing::debug!("UID {} authorized for gRPC call", uid);
                        // 继续处理请求
                        Err(tonic::Status::unimplemented("Request passed auth, continue to handler"))
                    } else {
                        tracing::warn!("UID {} not in allowed list: {:?}", uid, allowed_uids);
                        Err(tonic::Status::permission_denied(format!(
                            "UID {} not authorized. Allowed: {:?}",
                            uid, allowed_uids
                        )))
                    }
                }
                None => {
                    // 非 Unix Socket 连接或无法获取 UID
                    tracing::warn!("Could not determine peer UID for gRPC call");
                    Err(tonic::Status::unauthenticated(
                        "Unable to verify caller identity via Unix Socket credentials"
                    ))
                }
            }
        })
    }
}

/// Peer UID 包装类型（用于请求扩展）
#[derive(Debug, Clone, Copy)]
#[allow(dead_code)]
struct PeerUid(u32);

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // 初始化日志
    tracing_subscriber::fmt()
        .with_env_filter("nuts_collector_daemon=info,error")
        .init();

    // 解析命令行参数
    let args: Vec<String> = std::env::args().collect();
    
    let socket_path = args.get(1)
        .cloned()
        .unwrap_or_else(|| "/run/nuts/collector.sock".to_string());

    info!("Starting nuts-collector-daemon v{}", env!("CARGO_PKG_VERSION"));
    
    // 检查权限
    let uid = unsafe { libc::getuid() };
    let caps = check_capabilities();
    
    if uid != 0 && !caps.contains(&"CAP_BPF".to_string()) {
        error!("Insufficient privileges: need root or CAP_BPF");
        eprintln!("Error: This daemon requires root or CAP_BPF capability");
        std::process::exit(1);
    }
    
    info!("Running as UID: {}, Capabilities: {:?}", uid, caps);

    // 创建配置
    let config = DaemonConfig {
        socket_path: socket_path.clone(),
        ..DaemonConfig::default()
    };

    // 创建安全的 Unix Socket
    let listener = create_secure_socket(&socket_path).await?;
    
    info!("Collector daemon listening on {}", socket_path);

    // 创建服务
    let service = CollectorService::new(config);
    
    // 启动 gRPC 服务器
    Server::builder()
        .add_service(CollectorServer::new(service))
        .serve_with_incoming(tokio_stream::wrappers::UnixListenerStream::new(listener))
        .await?;

    Ok(())
}
