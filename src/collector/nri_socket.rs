//! NRI Unix Socket 适配器
//!
//! 提供本地进程间通信能力，相比 HTTP Webhook：
//! - 更低延迟（无 TCP/IP 协议栈开销）
//! - 更高吞吐（无需序列化 HTTP 头）
//! - 更好安全（文件系统权限控制）
//! - 更低资源占用

use std::os::unix::fs::PermissionsExt;
use std::path::Path;
use std::sync::Arc;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::{UnixListener, UnixStream};
use tokio::sync::mpsc;

use super::nri_mapping::{NriEvent, NriPodEvent, NriContainerInfo};
use super::nri_mapping_v2::NriMappingTableV2;

/// Unix Socket 配置
#[derive(Debug, Clone)]
pub struct UnixSocketConfig {
    /// Socket 文件路径
    pub socket_path: String,
    /// 文件权限（模式，如 0o660）
    pub permissions: u32,
    /// 连接队列长度
    pub backlog: i32,
    /// 接收缓冲区大小
    pub recv_buffer_size: usize,
}

impl Default for UnixSocketConfig {
    fn default() -> Self {
        Self {
            socket_path: "/run/nuts/nri.sock".to_string(),
            permissions: 0o660, // owner+group 读写权限
            backlog: 128,
            recv_buffer_size: 65536,
        }
    }
}

/// Unix Socket NRI 适配器
pub struct NriUnixSocketAdapter {
    config: UnixSocketConfig,
    table: Arc<NriMappingTableV2>,
    event_tx: mpsc::Sender<NriEvent>,
}

impl NriUnixSocketAdapter {
    /// 创建新的适配器
    pub fn new(
        config: UnixSocketConfig,
        table: Arc<NriMappingTableV2>,
        event_tx: mpsc::Sender<NriEvent>,
    ) -> Self {
        Self {
            config,
            table,
            event_tx,
        }
    }

    /// 启动 Unix Socket 服务
    pub async fn start(&self) -> Result<(), SocketError> {
        let path = Path::new(&self.config.socket_path);

        // 清理旧 socket 文件
        if path.exists() {
            tracing::info!("[NriSocket] Removing old socket file: {}", self.config.socket_path);
            tokio::fs::remove_file(path).await?;
        }

        // 确保目录存在
        if let Some(parent) = path.parent() {
            tokio::fs::create_dir_all(parent).await?;
        }

        // 创建 socket
        let listener = UnixListener::bind(path)?;
        tracing::info!(
            "[NriSocket] Listening at {} (backlog: {})",
            self.config.socket_path, self.config.backlog
        );

        // 设置权限
        std::fs::set_permissions(path, std::fs::Permissions::from_mode(self.config.permissions))?;
        tracing::info!("[NriSocket] Socket permissions set to {:o}", self.config.permissions);

        // 接受连接循环
        loop {
            match listener.accept().await {
                Ok((stream, _)) => {
                    let event_tx = self.event_tx.clone();
                    let table = Arc::clone(&self.table);
                    let buffer_size = self.config.recv_buffer_size;

                    tokio::spawn(async move {
                        if let Err(e) = handle_connection(stream, event_tx, table, buffer_size).await {
                            tracing::error!("[NriSocket] Connection handler error: {}", e);
                        }
                    });
                }
                Err(e) => {
                    tracing::error!("[NriSocket] Accept error: {}", e);
                }
            }
        }
    }
}

/// 处理单个连接
async fn handle_connection(
    mut stream: UnixStream,
    event_tx: mpsc::Sender<NriEvent>,
    _table: Arc<NriMappingTableV2>,
    buffer_size: usize,
) -> Result<(), SocketError> {
    let peer = stream.peer_addr()?;
    tracing::debug!("[NriSocket] New connection from {:?}", peer);

    let mut buffer = vec![0u8; buffer_size];

    loop {
        match stream.read(&mut buffer).await {
            Ok(0) => {
                // 连接关闭
                tracing::debug!("[NriSocket] Connection closed by peer");
                break;
            }
            Ok(n) => {
                // 解析帧
                match parse_nri_frame(&buffer[..n]) {
                    Ok(events) => {
                        for event in events {
                            // 发送事件到处理通道
                            if let Err(e) = event_tx.send(event).await {
                                tracing::error!("[NriSocket] Failed to send event: {}", e);
                                return Err(SocketError::ChannelClosed);
                            }
                        }

                        // 发送确认
                        let ack = b"OK\n";
                        if let Err(e) = stream.write_all(ack).await {
                            tracing::warn!("[NriSocket] Failed to send ack: {}", e);
                        }
                    }
                    Err(e) => {
                        tracing::error!("[NriSocket] Frame parse error: {}", e);
                        let err = format!("ERR: {}\n", e);
                        let _ = stream.write_all(err.as_bytes()).await;
                    }
                }
            }
            Err(e) => {
                tracing::error!("[NriSocket] Read error: {}", e);
                return Err(e.into());
            }
        }
    }

    Ok(())
}

/// NRI 帧格式
/// 
/// 简单二进制帧格式：
/// - 4 bytes: 魔数 (0x4E524900 = "NRI\0")
/// - 4 bytes: 事件数量 (u32, big-endian)
/// - N 个事件：
///   - 4 bytes: 事件类型 (1=ADD, 2=UPDATE, 3=DELETE)
///   - 4 bytes: Pod UID 长度
///   - N bytes: Pod UID (UTF-8)
///   - 4 bytes: Pod 名称长度
///   - N bytes: Pod 名称
///   - 4 bytes: 命名空间长度
///   - N bytes: 命名空间
///   - 4 bytes: 容器数量
///   - 对每个容器：
///     - 4 bytes: 容器 ID 长度
///     - N bytes: 容器 ID
///     - 4 bytes: cgroup ID 数量
///     - 对每个 cgroup ID：
///       - 4 bytes: ID 长度
///       - N bytes: ID
///     - 4 bytes: PID 数量
///     - N * 4 bytes: PID 列表 (u32)
///
/// 简化版：使用 JSON Lines 格式（更易调试）
#[derive(Debug, Clone, serde::Deserialize, serde::Serialize)]
struct NriFrame {
    version: u32,
    events: Vec<NriFrameEvent>,
}

#[derive(Debug, Clone, serde::Deserialize, serde::Serialize)]
struct NriFrameEvent {
    event_type: String, // "ADD", "UPDATE", "DELETE"
    pod_uid: String,
    pod_name: String,
    namespace: String,
    containers: Vec<NriFrameContainer>,
}

#[derive(Debug, Clone, serde::Deserialize, serde::Serialize)]
struct NriFrameContainer {
    container_id: String,
    cgroup_ids: Vec<String>,
    pids: Vec<u32>,
}

/// 解析 NRI 帧（JSON Lines 格式）
/// 
/// 每行一个 JSON 对象，支持批量传输
fn parse_nri_frame(data: &[u8]) -> Result<Vec<NriEvent>, String> {
    let text = String::from_utf8_lossy(data);
    let mut events = Vec::new();

    for line in text.lines() {
        let line = line.trim();
        if line.is_empty() {
            continue;
        }

        // 尝试解析为帧事件
        let frame_event: NriFrameEvent = serde_json::from_str(line)
            .map_err(|e| format!("JSON parse error: {}", e))?;

        // 转换为内部事件格式
        let event = match frame_event.event_type.as_str() {
            "ADD" | "UPDATE" | "Add" | "Update" => {
                let containers: Vec<NriContainerInfo> = frame_event
                    .containers
                    .into_iter()
                    .map(|c| NriContainerInfo {
                        container_id: c.container_id,
                        cgroup_ids: c.cgroup_ids,
                        pids: c.pids,
                    })
                    .collect();

                NriEvent::AddOrUpdate(NriPodEvent {
                    pod_uid: frame_event.pod_uid,
                    pod_name: frame_event.pod_name,
                    namespace: frame_event.namespace,
                    containers,
                })
            }
            "DELETE" | "Delete" => {
                NriEvent::Delete {
                    pod_uid: frame_event.pod_uid,
                }
            }
            other => {
                return Err(format!("Unknown event type: {}", other));
            }
        };

        events.push(event);
    }

    Ok(events)
}

/// 将内部事件序列化为帧
fn serialize_nri_event(event: &NriEvent) -> Result<String, serde_json::Error> {
    let frame_event = match event {
        NriEvent::AddOrUpdate(pod) => {
            let containers: Vec<NriFrameContainer> = pod
                .containers
                .iter()
                .map(|c| NriFrameContainer {
                    container_id: c.container_id.clone(),
                    cgroup_ids: c.cgroup_ids.clone(),
                    pids: vec![], // V1 结构体没有 pids
                })
                .collect();

            NriFrameEvent {
                event_type: "UPDATE".to_string(),
                pod_uid: pod.pod_uid.clone(),
                pod_name: pod.pod_name.clone(),
                namespace: pod.namespace.clone(),
                containers,
            }
        }
        NriEvent::Delete { pod_uid } => {
            NriFrameEvent {
                event_type: "DELETE".to_string(),
                pod_uid: pod_uid.clone(),
                pod_name: String::new(),
                namespace: String::new(),
                containers: vec![],
            }
        }
    };

    serde_json::to_string(&frame_event)
}

/// Socket 错误类型
#[derive(Debug)]
pub enum SocketError {
    Io(std::io::Error),
    Parse(String),
    ChannelClosed,
    InvalidFrame(String),
}

impl From<std::io::Error> for SocketError {
    fn from(e: std::io::Error) -> Self {
        SocketError::Io(e)
    }
}

impl std::fmt::Display for SocketError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            SocketError::Io(e) => write!(f, "IO error: {}", e),
            SocketError::Parse(msg) => write!(f, "Parse error: {}", msg),
            SocketError::ChannelClosed => write!(f, "Event channel closed"),
            SocketError::InvalidFrame(msg) => write!(f, "Invalid frame: {}", msg),
        }
    }
}

impl std::error::Error for SocketError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            SocketError::Io(e) => Some(e),
            _ => None,
        }
    }
}

/// 启动事件处理任务
/// 
/// 从 channel 接收事件并批量处理
pub fn start_event_processor(
    mut event_rx: mpsc::Receiver<NriEvent>,
    table: Arc<NriMappingTableV2>,
    version_mgr: Option<Arc<super::nri_version::EventVersionManager>>,
    batch_size: usize,
    flush_interval_ms: u64,
) -> tokio::task::JoinHandle<()> {

    tokio::spawn(async move {
        let mut batch = Vec::with_capacity(batch_size);
        let mut flush_interval = tokio::time::interval(
            tokio::time::Duration::from_millis(flush_interval_ms)
        );

        loop {
            tokio::select! {
                Some(event) = event_rx.recv() => {
                    batch.push(event);

                    if batch.len() >= batch_size {
                        process_batch(&batch, &table, version_mgr.as_deref()).await;
                        batch.clear();
                    }
                }
                _ = flush_interval.tick() => {
                    if !batch.is_empty() {
                        process_batch(&batch, &table, version_mgr.as_deref()).await;
                        batch.clear();
                    }
                }
                else => {
                    // Channel 关闭
                    tracing::info!("[NriSocket] Event channel closed, processor exiting");
                    break;
                }
            }
        }

        // 处理剩余事件
        if !batch.is_empty() {
            process_batch(&batch, &table, version_mgr.as_deref()).await;
        }
    })
}

/// 批量处理事件
async fn process_batch(
    batch: &[NriEvent],
    table: &NriMappingTableV2,
    version_mgr: Option<&super::nri_version::EventVersionManager>,
) {
    for event in batch {
        // 版本检查
        if let Some(vm) = version_mgr {
            let pod_uid = match event {
                NriEvent::AddOrUpdate(pod) => &pod.pod_uid,
                NriEvent::Delete { pod_uid } => pod_uid,
            };

            let version = vm.generate_version();
            match vm.try_update(pod_uid, version) {
                Ok(true) => {
                    // 版本检查通过，处理事件
                    if let Err(e) = table.update_from_nri(event.clone()) {
                        tracing::error!("[NriSocket] Failed to update table: {:?}", e);
                    }
                }
                Ok(false) => {
                    tracing::debug!("[NriSocket] Stale event dropped for pod {}", pod_uid);
                }
                Err(e) => {
                    tracing::error!("[NriSocket] Version check error: {}", e);
                }
            }
        } else {
            // 无版本控制，直接处理
            if let Err(e) = table.update_from_nri(event.clone()) {
                tracing::error!("[NriSocket] Failed to update table: {:?}", e);
            }
        }
    }

    tracing::debug!("[NriSocket] Processed batch of {} events", batch.len());
}

/// 便捷的启动函数
pub async fn start_unix_socket_nri(
    table: Arc<NriMappingTableV2>,
    config: UnixSocketConfig,
    batch_size: usize,
    flush_interval_ms: u64,
) -> Result<(tokio::task::JoinHandle<()>, tokio::task::JoinHandle<()>), SocketError> {
    // 创建事件通道
    let (event_tx, event_rx) = mpsc::channel(10000);

    // 创建版本管理器（可选）
    let version_mgr = Arc::new(super::nri_version::EventVersionManager::new());

    // 启动 socket 适配器
    let adapter = NriUnixSocketAdapter::new(config, Arc::clone(&table), event_tx);
    let socket_handle = tokio::spawn(async move {
        if let Err(e) = adapter.start().await {
            tracing::error!("[NriSocket] Adapter error: {}", e);
        }
    });

    // 启动批量处理器
    let processor_handle = start_event_processor(
        event_rx,
        table,
        Some(version_mgr),
        batch_size,
        flush_interval_ms,
    );

    Ok((socket_handle, processor_handle))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_nri_frame_parse() {
        let json = r#"{"event_type":"ADD","pod_uid":"pod-123","pod_name":"test-pod","namespace":"default","containers":[{"container_id":"container-1","cgroup_ids":["cg-1","cg-2"],"pids":[1234,5678]}]}"#;

        let events = parse_nri_frame(json.as_bytes()).unwrap();
        assert_eq!(events.len(), 1);

        match &events[0] {
            NriEvent::AddOrUpdate(pod) => {
                assert_eq!(pod.pod_uid, "pod-123");
                assert_eq!(pod.containers.len(), 1);
            }
            _ => panic!("Expected AddOrUpdate event"),
        }
    }

    #[test]
    fn test_nri_frame_parse_batch() {
        let json = r#"{"event_type":"ADD","pod_uid":"pod-1","pod_name":"p1","namespace":"ns1","containers":[]}
{"event_type":"DELETE","pod_uid":"pod-2","pod_name":"","namespace":"","containers":[]}"#;

        let events = parse_nri_frame(json.as_bytes()).unwrap();
        assert_eq!(events.len(), 2);
    }

    #[tokio::test]
    async fn test_unix_socket_communication() {
        use tokio::io::AsyncWriteExt;

        let socket_path = "/tmp/test_nri.sock";
        let _ = std::fs::remove_file(socket_path);

        let table = Arc::new(NriMappingTableV2::new());
        let (event_tx, mut event_rx) = mpsc::channel(100);
        let adapter = NriUnixSocketAdapter::new(
            UnixSocketConfig {
                socket_path: socket_path.to_string(),
                permissions: 0o666,
                backlog: 10,
                recv_buffer_size: 4096,
            },
            Arc::clone(&table),
            event_tx,
        );

        // 启动服务端
        let server_handle = tokio::spawn(async move {
            let _ = adapter.start().await;
        });

        // 等待 socket 创建
        tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;

        // 客户端连接
        let mut client = UnixStream::connect(socket_path).await.unwrap();

        // 发送事件
        let event_json = r#"{"event_type":"ADD","pod_uid":"test-uid","pod_name":"test","namespace":"default","containers":[]}
"#;
        client.write_all(event_json.as_bytes()).await.unwrap();

        // 等待服务端处理
        tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;

        // 验证事件接收
        assert!(event_rx.try_recv().is_ok());

        // 清理
        drop(client);
        drop(server_handle);
        let _ = std::fs::remove_file(socket_path);
    }
}
