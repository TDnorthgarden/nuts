# NRI gRPC/Unix Socket 集成方案

## 背景
当前 HTTP Webhook 仅用于测试/模拟，真实 NRI 插件应使用：
- **gRPC**: NRI 官方协议
- **Unix Socket**: 本地进程通信，更低延迟

## 架构对比

```
┌─────────────────────────────────────────────────────────────┐
│                        当前实现 (HTTP)                        │
├─────────────────────────────────────────────────────────────┤
│  Container Runtime ──> NRI Plugin ──> HTTP POST ──> nuts     │
│                              (Webhook 端口 8080)              │
└─────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│                      真实 NRI (gRPC/Unix Socket)              │
├─────────────────────────────────────────────────────────────┤
│  Container Runtime ──> NRI Plugin ──> gRPC/UnixSocket ──> nuts│
│                              (本地高效通信)                    │
└─────────────────────────────────────────────────────────────┘
```

## 实现方案

### 方案 A: Unix Socket 适配器（推荐）

```rust
use tokio::net::UnixListener;
use std::os::unix::fs::PermissionsExt;

/// Unix Socket NRI 适配器
pub struct NriUnixSocketAdapter {
    socket_path: String,
    table: Arc<NriMappingTable>,
}

impl NriUnixSocketAdapter {
    pub async fn start(&self) -> Result<(), Error> {
        // 清理旧 socket
        let _ = tokio::fs::remove_file(&self.socket_path).await;
        
        // 创建 socket
        let listener = UnixListener::bind(&self.socket_path)?;
        
        // 设置权限（仅 root/containerd 可访问）
        let perms = std::fs::Permissions::from_mode(0o660);
        std::fs::set_permissions(&self.socket_path, perms)?;
        
        tracing::info!("NRI Unix Socket listening at {}", self.socket_path);
        
        loop {
            let (socket, _) = listener.accept().await?;
            let table = Arc::clone(&self.table);
            
            tokio::spawn(async move {
                Self::handle_connection(socket, table).await;
            });
        }
    }
    
    async fn handle_connection(
        mut socket: tokio::net::UnixStream,
        table: Arc<NriMappingTable>,
    ) {
        use tokio::io::AsyncReadExt;
        
        let mut buf = vec![0u8; 65536];
        
        loop {
            match socket.read(&mut buf).await {
                Ok(0) => break, // 连接关闭
                Ok(n) => {
                    // 解析 NRI 二进制帧
                    if let Ok(event) = Self::parse_nri_frame(&buf[..n]) {
                        if let Err(e) = table.update_from_nri(event).await {
                            tracing::error!("Failed to process NRI frame: {:?}", e);
                        }
                    }
                }
                Err(e) => {
                    tracing::error!("Socket read error: {}", e);
                    break;
                }
            }
        }
    }
}
```

### 方案 B: gRPC 适配器（NRI 原生协议）

```rust
use tonic::{transport::Server, Request, Response, Status};

/// NRI gRPC 服务定义
pub mod nri_proto {
    tonic::include_proto!("nri");  // 从 NRI 官方 protobuf 生成
}

use nri_proto::{
    Event, EventResponse,
    RegistrationRequest, RegistrationResponse,
    nri_server::{Nri, NriServer},
};

#[derive(Debug)]
pub struct NriGrpcService {
    table: Arc<NriMappingTable>,
}

#[tonic::async_trait]
impl Nri for NriGrpcService {
    /// 注册插件
    async fn register(
        &self,
        request: Request<RegistrationRequest>,
    ) -> Result<Response<RegistrationResponse>, Status> {
        let req = request.into_inner();
        tracing::info!("NRI plugin registered: {} (version {})", 
            req.plugin_name, req.plugin_version);
        
        Ok(Response::new(RegistrationResponse {
            accepted: true,
            message: "nuts-observer NRI adapter ready".to_string(),
        }))
    }
    
    /// 接收 NRI 事件流
    async fn send_events(
        &self,
        request: Request<tonic::Streaming<Event>>,
    ) -> Result<Response<EventResponse>, Status> {
        let mut stream = request.into_inner();
        let mut processed = 0;
        
        while let Some(event) = stream.message().await? {
            // 转换 NRI gRPC 事件为内部格式
            let internal_event = Self::convert_event(event);
            
            match self.table.update_from_nri(internal_event).await {
                Ok(()) => processed += 1,
                Err(e) => {
                    tracing::error!("Failed to process NRI event: {:?}", e);
                    // 继续处理，不中断流
                }
            }
        }
        
        Ok(Response::new(EventResponse {
            processed_count: processed,
            status: "success".to_string(),
        }))
    }
}

/// 启动 gRPC NRI 服务
pub async fn start_nri_grpc_server(
    table: Arc<NriMappingTable>,
    addr: &str,
) -> Result<(), Error> {
    let service = NriGrpcService { table };
    
    Server::builder()
        .add_service(NriServer::new(service))
        .serve(addr.parse()?)
        .await?;
    
    Ok(())
}
```

### 方案 C: 双模式兼容（推荐实现）

```rust
/// NRI 接入模式
#[derive(Debug, Clone, Copy)]
pub enum NriMode {
    /// HTTP Webhook（测试/兼容模式）
    Http { port: u16 },
    /// Unix Socket（本地高效模式）
    UnixSocket { path: String },
    /// gRPC（NRI 原生模式）
    Grpc { addr: String },
}

/// 统一 NRI 适配器入口
pub struct NriAdapter {
    mode: NriMode,
    table: Arc<NriMappingTable>,
}

impl NriAdapter {
    pub async fn run(&self) -> Result<(), Error> {
        match &self.mode {
            NriMode::Http { port } => {
                // 复用现有 HTTP 实现
                self.run_http(*port).await
            }
            NriMode::UnixSocket { path } => {
                // Unix Socket 实现
                self.run_unix_socket(path).await
            }
            NriMode::Grpc { addr } => {
                // gRPC 实现
                self.run_grpc(addr).await
            }
        }
    }
}

/// 配置示例 (nuts.yaml)
/// 
/// ```yaml
/// nri:
///   mode: unix_socket  # http | unix_socket | grpc
///   http_port: 8080    # mode=http 时使用
///   socket_path: /run/nuts/nri.sock  # mode=unix_socket 时使用
///   grpc_addr: 0.0.0.0:50051  # mode=grpc 时使用
/// ```
```

## 部署建议

| 场景 | 推荐模式 | 原因 |
|-----|---------|------|
| 开发/测试 | HTTP | 调试方便，无需 root |
| 生产单节点 | Unix Socket | 最低延迟，无网络栈开销 |
| 生产多节点 | gRPC | 支持 NRI 标准协议，可跨节点 |
| Kubernetes | Unix Socket + DaemonSet | 每个节点部署 nuts-observer |

## 实现优先级

1. **P0**: Unix Socket 模式（性能最优，实现简单）
2. **P1**: gRPC 模式（标准兼容）
3. **P2**: 三模式运行时切换
