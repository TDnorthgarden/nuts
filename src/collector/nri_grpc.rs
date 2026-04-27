//! NRI gRPC 适配器 - 标准协议兼容 (实验性功能)
//!
//! 实现 NRI (Node Resource Interface) 官方 gRPC 协议
//! 参考: https://github.com/containerd/nri
//!
//! ⚠️ 实验性功能：需要启用 `nri-grpc` feature 才能使用
//!
//! 相比 Unix Socket：
//! - 标准化协议，兼容 containerd/CRI-O
//! - 支持插件注册、配置同步
//! - 更好的生态兼容性
#![cfg(feature = "nri-grpc")]

use std::sync::Arc;
use tokio::sync::mpsc;
use tonic::{Request, Response, Status, Streaming};

use super::nri_mapping::{NriContainerInfo, NriEvent, NriPodEvent};
use super::nri_mapping_v2::NriMappingTableV2;

/// 从 NRI 官方 protobuf 生成的代码
/// 注意：实际使用时需要通过 build.rs 生成
pub mod nri_proto {
    /// 插件注册请求
    #[derive(Debug, Clone)]
    pub struct RegisterPluginRequest {
        pub plugin_name: String,
        pub plugin_version: String,
        pub supported_events: Vec<EventType>,
    }

    /// 插件注册响应
    #[derive(Debug, Clone)]
    pub struct RegisterPluginResponse {
        pub accepted: bool,
        pub message: String,
        pub config: Option<PluginConfig>,
    }

    /// 插件配置
    #[derive(Debug, Clone)]
    pub struct PluginConfig {
        pub log_level: String,
        pub config_data: Vec<u8>,
    }

    /// NRI 事件类型
    #[derive(Debug, Clone, Copy, PartialEq, Eq)]
    #[repr(i32)]
    pub enum EventType {
        Unknown = 0,
        RunPodSandbox = 1,
        StopPodSandbox = 2,
        CreateContainer = 3,
        UpdateContainer = 4,
        StopContainer = 5,
        RemoveContainer = 6,
    }

    /// Pod 事件
    #[derive(Debug, Clone)]
    pub struct PodEvent {
        pub pod_uid: String,
        pub pod_name: String,
        pub namespace: String,
        pub linux: Option<LinuxPod>,
        pub containers: Vec<ContainerEvent>,
    }

    /// Linux Pod 信息
    #[derive(Debug, Clone)]
    pub struct LinuxPod {
        pub pod_overhead: Option<LinuxResources>,
        pub pod_resources: Option<LinuxResources>,
        pub cgroups_path: String,
    }

    /// Linux 资源限制
    #[derive(Debug, Clone)]
    pub struct LinuxResources {
        pub cpu_period: i64,
        pub cpu_quota: i64,
        pub cpu_shares: u64,
        pub memory_limit: i64,
    }

    /// 容器事件
    #[derive(Debug, Clone)]
    pub struct ContainerEvent {
        pub container_id: String,
        pub pod_uid: String,
        pub linux: Option<LinuxContainer>,
        pub cgroup_ids: Vec<String>,
        pub pids: Vec<u32>,
    }

    /// Linux 容器信息
    #[derive(Debug, Clone)]
    pub struct LinuxContainer {
        pub namespaces: Vec<LinuxNamespace>,
        pub resources: Option<LinuxResources>,
        pub cgroups_path: String,
    }

    /// Linux 命名空间
    #[derive(Debug, Clone)]
    pub struct LinuxNamespace {
        pub nstype: String,
        pub path: String,
    }

    /// NRI 事件消息
    #[derive(Debug, Clone)]
    pub struct EventMessage {
        pub event_type: EventType,
        pub pod: Option<PodEvent>,
        pub container: Option<ContainerEvent>,
    }

    /// 事件处理响应
    #[derive(Debug, Clone)]
    pub struct EventResponse {
        pub processed: bool,
        pub error: Option<String>,
        pub updates: Vec<ContainerUpdate>,
    }

    /// 容器更新指令
    #[derive(Debug, Clone)]
    pub struct ContainerUpdate {
        pub container_id: String,
        pub linux_resources: Option<LinuxResources>,
    }
}

/// gRPC 服务配置
#[derive(Debug, Clone)]
pub struct GrpcServiceConfig {
    /// 监听地址
    pub listen_addr: String,
    /// TLS 配置（可选）
    pub tls_config: Option<TlsConfig>,
    /// 最大并发流
    pub max_concurrent_streams: u32,
    /// 连接超时（秒）
    pub connection_timeout_secs: u64,
}

impl Default for GrpcServiceConfig {
    fn default() -> Self {
        Self {
            listen_addr: "0.0.0.0:50051".to_string(),
            tls_config: None,
            max_concurrent_streams: 100,
            connection_timeout_secs: 30,
        }
    }
}

/// TLS 配置
#[derive(Debug, Clone)]
pub struct TlsConfig {
    pub cert_path: String,
    pub key_path: String,
    pub ca_path: Option<String>,
}

/// NRI gRPC 服务实现
pub struct NriGrpcService {
    table: Arc<NriMappingTableV2>,
    event_tx: mpsc::Sender<nri_proto::EventMessage>,
    plugin_info: Arc<std::sync::RwLock<Option<PluginInfo>>>,
}

#[derive(Debug, Clone)]
struct PluginInfo {
    name: String,
    version: String,
    supported_events: Vec<nri_proto::EventType>,
}

impl NriGrpcService {
    /// 创建新的 gRPC 服务
    pub fn new(
        table: Arc<NriMappingTableV2>,
        event_tx: mpsc::Sender<nri_proto::EventMessage>,
    ) -> Self {
        Self {
            table,
            event_tx,
            plugin_info: Arc::new(std::sync::RwLock::new(None)),
        }
    }

    /// 转换 gRPC 事件为内部事件
    fn convert_event(&self, msg: &nri_proto::EventMessage) -> Option<NriEvent> {
        match msg.event_type {
            nri_proto::EventType::RunPodSandbox => {
                // Pod 创建/启动
                msg.pod.as_ref().map(|pod| {
                    let containers: Vec<NriContainerInfo> = pod
                        .containers
                        .iter()
                        .map(|c| super::nri_mapping::NriContainerInfo {
                            container_id: c.container_id.clone(),
                            cgroup_ids: c.cgroup_ids.clone(),
                            pids: vec![], // 从 container 提取或使用默认值
                        })
                        .collect();

                    NriEvent::AddOrUpdate(NriPodEvent {
                        pod_uid: pod.pod_uid.clone(),
                        pod_name: pod.pod_name.clone(),
                        namespace: pod.namespace.clone(),
                        containers,
                    })
                })
            }
            nri_proto::EventType::StopPodSandbox => {
                // Pod 停止
                msg.pod.as_ref().map(|pod| NriEvent::Delete {
                    pod_uid: pod.pod_uid.clone(),
                })
            }
            nri_proto::EventType::CreateContainer | nri_proto::EventType::UpdateContainer => {
                // 容器创建/更新 - 需要获取完整 Pod 信息
                msg.container.as_ref().and_then(|container| {
                    // 通过 Pod UID 查找现有 Pod，更新容器列表
                    self.update_container_in_pod(&container.pod_uid, container)
                })
            }
            nri_proto::EventType::StopContainer | nri_proto::EventType::RemoveContainer => {
                // 容器停止/移除
                msg.container.as_ref().and_then(|container| {
                    self.remove_container_from_pod(&container.pod_uid, &container.container_id)
                })
            }
            _ => {
                tracing::debug!("[NriGrpc] Unknown or unsupported event type: {:?}", msg.event_type);
                None
            }
        }
    }

    /// 更新 Pod 中的容器信息
    fn update_container_in_pod(
        &self,
        pod_uid: &str,
        container: &nri_proto::ContainerEvent,
    ) -> Option<NriEvent> {
        // 查找现有 Pod
        if let Some((mut pod, _)) = self.table.get_pod_details(pod_uid) {
            // 检查容器是否已存在
            if let Some(existing) = pod.containers.iter_mut().find(|c| {
                c.container_id == container.container_id
            }) {
                // 更新现有容器
                existing.cgroup_ids = extract_cgroup_ids(&container.linux);
            } else {
                // 添加新容器
                pod.containers.push(super::nri_mapping::ContainerMapping {
                    container_id: container.container_id.clone(),
                    pod_uid: pod_uid.to_string(),
                    cgroup_ids: extract_cgroup_ids(&container.linux),
                });
            }

            Some(NriEvent::AddOrUpdate(NriPodEvent {
                pod_uid: pod.pod_uid,
                pod_name: pod.pod_name,
                namespace: pod.namespace,
                containers: pod.containers.iter().map(|c| super::nri_mapping::NriContainerInfo {
                    container_id: c.container_id.clone(),
                    cgroup_ids: c.cgroup_ids.clone(),
                    pids: vec![],
                }).collect(),
            }))
        } else {
            None
        }
    }

    /// 从 Pod 中移除容器
    fn remove_container_from_pod(
        &self,
        pod_uid: &str,
        container_id: &str,
    ) -> Option<NriEvent> {
        if let Some((mut pod, _)) = self.table.get_pod_details(pod_uid) {
            pod.containers.retain(|c| c.container_id != container_id);

            if pod.containers.is_empty() {
                // 如果没有容器了，删除 Pod
                Some(NriEvent::Delete {
                    pod_uid: pod_uid.to_string(),
                })
            } else {
                Some(NriEvent::AddOrUpdate(NriPodEvent {
                    pod_uid: pod.pod_uid,
                    pod_name: pod.pod_name,
                    namespace: pod.namespace,
                    containers: pod.containers.iter().map(|c| super::nri_mapping::NriContainerInfo {
                        container_id: c.container_id.clone(),
                        cgroup_ids: c.cgroup_ids.clone(),
                        pids: vec![],
                    }).collect(),
                }))
            }
        } else {
            None
        }
    }

    /// 处理事件并返回响应
    async fn handle_event(&self, msg: nri_proto::EventMessage) -> nri_proto::EventResponse {
        let start = std::time::Instant::now();

        // 发送到处理通道
        if let Err(e) = self.event_tx.send(msg.clone()).await {
            return nri_proto::EventResponse {
                processed: false,
                error: Some(format!("Failed to queue event: {}", e)),
                updates: vec![],
            };
        }

        // 转换并更新映射表
        if let Some(event) = self.convert_event(&msg) {
            if let Err(e) = self.table.update_from_nri(event) {
                return nri_proto::EventResponse {
                    processed: false,
                    error: Some(format!("Failed to update mapping: {:?}", e)),
                    updates: vec![],
                };
            }
        }

        let elapsed = start.elapsed();
        tracing::debug!("[NriGrpc] Event processed in {:?}", elapsed);

        nri_proto::EventResponse {
            processed: true,
            error: None,
            updates: vec![], // 可以根据策略返回更新指令
        }
    }
}

/// 从 LinuxContainer 提取 cgroup IDs
fn extract_cgroup_ids(linux: &Option<nri_proto::LinuxContainer>) -> Vec<String> {
    linux
        .as_ref()
        .map(|l| vec![l.cgroups_path.clone()])
        .unwrap_or_default()
}

/// 模拟 gRPC 服务 trait（实际使用时从 protobuf 生成）
#[tonic::async_trait]
pub trait NriPlugin {
    async fn register_plugin(
        &self,
        request: Request<nri_proto::RegisterPluginRequest>,
    ) -> Result<Response<nri_proto::RegisterPluginResponse>, Status>;

    async fn send_events(
        &self,
        request: Request<Streaming<nri_proto::EventMessage>>,
    ) -> Result<Response<nri_proto::EventResponse>, Status>;
}

#[tonic::async_trait]
impl NriPlugin for NriGrpcService {
    async fn register_plugin(
        &self,
        request: Request<nri_proto::RegisterPluginRequest>,
    ) -> Result<Response<nri_proto::RegisterPluginResponse>, Status> {
        let req = request.into_inner();
        tracing::info!(
            "[NriGrpc] Plugin registration: {} v{} (events: {:?})",
            req.plugin_name, req.plugin_version, req.supported_events
        );

        // 保存插件信息
        let plugin_info = PluginInfo {
            name: req.plugin_name,
            version: req.plugin_version,
            supported_events: req.supported_events,
        };

        if let Ok(mut guard) = self.plugin_info.write() {
            *guard = Some(plugin_info);
        }

        let response = nri_proto::RegisterPluginResponse {
            accepted: true,
            message: "nuts-observer NRI plugin registered successfully".to_string(),
            config: Some(nri_proto::PluginConfig {
                log_level: "info".to_string(),
                config_data: vec![], // 可以返回具体配置
            }),
        };

        Ok(Response::new(response))
    }

    async fn send_events(
        &self,
        request: Request<Streaming<nri_proto::EventMessage>>,
    ) -> Result<Response<nri_proto::EventResponse>, Status> {
        let mut stream = request.into_inner();
        let mut processed = 0;
        let mut errors = 0;

        while let Some(msg) = stream.message().await? {
            let response = self.handle_event(msg).await;
            if response.processed {
                processed += 1;
            } else {
                errors += 1;
                if let Some(ref err) = response.error {
                    tracing::warn!("[NriGrpc] Event processing error: {}", err);
                }
            }
        }

        tracing::info!(
            "[NriGrpc] Event stream closed. Processed: {}, Errors: {}",
            processed, errors
        );

        let final_response = nri_proto::EventResponse {
            processed: errors == 0,
            error: if errors > 0 {
                Some(format!("{} events failed", errors))
            } else {
                None
            },
            updates: vec![],
        };

        Ok(Response::new(final_response))
    }
}

/// 启动 gRPC NRI 服务
pub async fn start_nri_grpc_server(
    table: Arc<NriMappingTableV2>,
    config: GrpcServiceConfig,
) -> Result<tokio::task::JoinHandle<()>, GrpcError> {
    // 创建事件通道
    let (event_tx, mut event_rx) = mpsc::channel(10000);

    // 创建服务
    let _service = NriGrpcService::new(table, event_tx);

    // 启动后台事件处理器
    let _processor_handle = tokio::spawn(async move {
        while let Some(_event) = event_rx.recv().await {
            // 事件已在 service.handle_event 中处理
            // 这里可以用于额外的事件日志/监控
        }
    });

    // 配置服务器
    let _addr: std::net::SocketAddr = config.listen_addr.parse().map_err(|e| GrpcError::Io(std::io::Error::new(std::io::ErrorKind::InvalidInput, e)))?;

    // 构建服务（实际使用时应从 protobuf 生成）
    // 这里简化处理，直接使用自定义实现
    let server_handle = tokio::spawn(async move {
        tracing::info!("[NriGrpc] Server starting at {}", config.listen_addr);

        // 注意：实际实现需要使用 tonic-build 生成的代码
        // Server::builder()
        //     .max_concurrent_streams(config.max_concurrent_streams)
        //     .add_service(NriPluginServer::new(service))
        //     .serve(addr)
        //     .await

        tracing::warn!("[NriGrpc] Full gRPC server requires protobuf generation. Use nri_socket for now.");

        // 保持存活直到取消
        tokio::signal::ctrl_c().await.ok();
    });

    Ok(server_handle)
}

/// gRPC 错误类型
#[derive(Debug)]
pub enum GrpcError {
    Tonic(tonic::transport::Error),
    Io(std::io::Error),
    InvalidAddress(String),
}

impl From<tonic::transport::Error> for GrpcError {
    fn from(e: tonic::transport::Error) -> Self {
        GrpcError::Tonic(e)
    }
}

impl From<std::io::Error> for GrpcError {
    fn from(e: std::io::Error) -> Self {
        GrpcError::Io(e)
    }
}

impl std::fmt::Display for GrpcError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            GrpcError::Tonic(e) => write!(f, "Tonic error: {}", e),
            GrpcError::Io(e) => write!(f, "IO error: {}", e),
            GrpcError::InvalidAddress(addr) => write!(f, "Invalid address: {}", addr),
        }
    }
}

impl std::error::Error for GrpcError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            GrpcError::Tonic(e) => Some(e),
            GrpcError::Io(e) => Some(e),
            GrpcError::InvalidAddress(_) => None,
        }
    }
}

/// 使用说明
///
/// 要启用完整的 gRPC 支持，需要：
///
/// 1. 在 build.rs 中添加 tonic-build：
/// ```rust
/// use std::io::Result;
/// fn main() -> Result<()> {
///     tonic_build::compile_protos("proto/nri.proto")?;
///     Ok(())
/// }
/// ```
///
/// 2. 添加 protobuf 文件到 proto/nri.proto（从 containerd/nri 复制）
///
/// 3. 更新 Cargo.toml：
/// ```toml
/// [dependencies]
/// tonic-build = "0.12"
///
/// [build-dependencies]
/// tonic-build = "0.12"
/// ```
///
/// 4. 重新编译生成代码
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_event_conversion() {
        let table = Arc::new(NriMappingTableV2::new());
        let (tx, _rx) = mpsc::channel(10);
        let service = NriGrpcService::new(table, tx);

        let msg = nri_proto::EventMessage {
            event_type: nri_proto::EventType::RunPodSandbox,
            pod: Some(nri_proto::PodEvent {
                pod_uid: "test-uid".to_string(),
                pod_name: "test-pod".to_string(),
                namespace: "default".to_string(),
                linux: None,
                containers: vec![],
            }),
            container: None,
        };

        let event = service.convert_event(&msg);
        assert!(event.is_some());

        match event.unwrap() {
            NriEvent::AddOrUpdate(pod) => {
                assert_eq!(pod.pod_uid, "test-uid");
            }
            _ => panic!("Expected AddOrUpdate event"),
        }
    }

    #[tokio::test]
    async fn test_plugin_registration() {
        let table = Arc::new(NriMappingTableV2::new());
        let (tx, _rx) = mpsc::channel(10);
        let service = NriGrpcService::new(table, tx);

        let request = nri_proto::RegisterPluginRequest {
            plugin_name: "test-plugin".to_string(),
            plugin_version: "1.0.0".to_string(),
            supported_events: vec![nri_proto::EventType::RunPodSandbox],
        };

        let response = service
            .register_plugin(Request::new(request))
            .await
            .unwrap()
            .into_inner();

        assert!(response.accepted);
        assert!(response.config.is_some());
    }
}
