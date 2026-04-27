//! Nuts Observer - HTTP 服务主程序
//!
//! 纯服务端二进制，通过 HTTP API 提供诊断服务。
//! CLI 客户端请使用独立的 nuts-observer-cli。

use nuts_observer::api::condition::{ConditionTrigger};
use nuts_observer::api::nri::router as nri_router;
use nuts_observer::api::nri_v3_enhanced::{router as nri_v3_enhanced_router, NriV3ApiState};
use nuts_observer::api::nri_v3::router as nri_v3_router;
use nuts_observer::api::trigger::router as trigger_router;
use nuts_observer::api::health::{router as health_router, AppState};
use nuts_observer::api::diagnosis::{router as diagnosis_router, DiagnosisApiState};
use axum::Router;
use nuts_observer::collector::nri_v3::{create_nri_v3, NriV3Config, NriV3};
use nuts_observer::collector::nri_mapping::NriMappingTable;
use nuts_observer::collector::nri_mapping_v2::NriMappingTableV2;
use nuts_observer::collector::oom_events::{OomEventListener, OomListenerConfig};
use nuts_observer::config::{Config, ConfigError};
use nuts_observer::ai::async_bridge::{start_ai_system, AiWorker, AiWorkerConfig, AiCompletionNotification, AiResultStore};
use nuts_observer::publisher::ResultPublisher;
use std::sync::Arc;
use tokio::net::TcpListener;
use tokio::signal::unix::{signal, SignalKind};
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() {
    // 服务器模式运行
    run_server().await;
}

/// 服务器模式运行
async fn run_server() {
    // 加载配置文件（使用 Arc<Mutex> 支持热重载）
    let config = match load_config() {
        Ok(c) => c,
        Err(e) => {
            eprintln!("Failed to load config: {}. Using default.", e);
            Config::default()
        }
    };
    let config = std::sync::Arc::new(tokio::sync::RwLock::new(config));

    // 基础日志初始化（使用默认级别，避免初始化时的锁竞争）
    let initial_log_level = config.read().await.log_level.clone();
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::new(&initial_log_level)
        )
        .init();

    // 获取初始配置读取锁
    let config_read = config.read().await;
    
    tracing::info!("Configuration loaded: log_level={}, ai_enabled={}, alert_enabled={}",
        config_read.log_level, config_read.ai.enabled, config_read.alert.enabled);

    // 初始化 NRI 映射表 (V1 - 兼容现有API)
    let nri_table = Arc::new(NriMappingTable::new());
    
    // 初始化 NRI V3 (优化版本 - 包含DashMap、版本控制、持久化等)
    let nri_v3 = create_nri_v3()
        .await
        .expect("Failed to initialize NRI V3");
    let nri_v3 = Arc::new(nri_v3);
    tracing::info!("NRI mapping table initialized");

    // 启动条件触发服务（从配置读取）
    let condition_triggers: Vec<_> = config_read.condition_triggers.clone();
    for trigger_config in condition_triggers {
        let nri_table_clone = Arc::clone(&nri_table);
        let config_def = trigger_config.clone();
        let cooldown_ms = config_def.cooldown_ms;
        tokio::spawn(async move {
            let trigger = ConditionTrigger::new(config_def.into(), Some(nri_table_clone))
                .with_cooldown(cooldown_ms);
            trigger.start().await;
        });
    }

    // 启动 OOM 事件监听器（异常联动触发）
    let server_bind = config_read.server.bind_address.clone();
    let server_port = config_read.server.port;
    drop(config_read);  // 释放读取锁
    
    let oom_config = OomListenerConfig {
        enabled: true,
        evidence_types: vec!["block_io".to_string(), "syscall_latency".to_string(), "network".to_string()],
        collection_window_secs: 10,
        cooldown_secs: 60,
        server_url: format!("http://{}:{}", server_bind, server_port),
    };
    let nri_table_for_oom = Arc::clone(&nri_table);
    tokio::spawn(async move {
        let oom_listener = OomEventListener::new(oom_config, nri_table_for_oom);
        oom_listener.start().await;
    });
    tracing::info!("OOM event listener started (oom_kill monitoring)");

    // 获取配置读取锁
    let config_read = config.read().await;
    
    // 初始化异步AI系统（如果启用）
    let (mut app, _ai_store) = if config_read.ai.enabled {
        let worker_config = AiWorkerConfig {
            adapter_config: config_read.ai.clone().into(),
            max_concurrent: 3,
            queue_timeout_ms: 300_000,
            retry_limit: 3,
            poll_interval_ms: 100,
            cleanup_interval_secs: 300,
        };
        let (queue, store, rx, mut notif_rx) = start_ai_system(worker_config.clone());
        
        // 启动AI Worker后台任务
        let worker = AiWorker::new(worker_config, rx, Arc::clone(&store), queue.get_pending_tasks(), None);
        tokio::spawn(async move {
            worker.run().await;
        });
        
        // 启动Publisher通知接收任务（增量触发）
        let _publisher = ResultPublisher::new("/var/log/nuts");
        let store_for_notif = Arc::clone(&store);
        tokio::spawn(async move {
            tracing::info!("[Publisher Notifier] Starting notification receiver...");
            loop {
                match notif_rx.recv().await {
                    Some(notification) => {
                        tracing::info!(
                            "[Publisher Notifier] Received AI completion for task {}",
                            notification.task_id
                        );
                        
                        // 查询AI增强结果
                        if let Some(enhanced) = store_for_notif.get(&notification.task_id).await {
                            // 这里简化处理，实际应该从原始存储中获取evidences
                            // 暂时只发布增强后的诊断
                            tracing::info!(
                                "[Publisher Notifier] AI enhanced result ready for task {} (processing: {}ms)",
                                notification.task_id,
                                enhanced.processing_ms
                            );
                            
                            // TODO: 获取原始证据并调用 publisher.publish_all()
                            // 当前仅记录日志，证据需要在AI worker中关联存储
                        }
                    }
                    None => {
                        tracing::warn!("[Publisher Notifier] Notification channel closed");
                        break;
                    }
                }
            }
        });
        
        tracing::info!("AI async enhancement system started with incremental publisher notifications (enabled=true)");
        
        // 创建应用状态
        let app_state = Arc::new(AppState::new(Arc::clone(&nri_table)));
        let nri_v3_api_state = Arc::new(NriV3ApiState::new(Arc::clone(&nri_v3)));
        
        // 构建路由：触发器 + NRI Webhook + NRI V3增强 + 健康检查 + 诊断查询
        let queue_for_router = Arc::new(queue);
        let mut app = Router::new()
            .merge(trigger_router(Arc::clone(&nri_table), Some(queue_for_router)))
            .merge(nri_router(Arc::clone(&nri_table)))
            .merge(nri_v3_enhanced_router(nri_v3_api_state))
            .merge(health_router(app_state));
        
        // 添加诊断查询路由（AI 启用时）
        let diagnosis_state = Arc::new(DiagnosisApiState::new(Arc::clone(&store)));
        app = app.merge(diagnosis_router(diagnosis_state));
        tracing::info!("AI diagnosis query API enabled");
        
        drop(config_read);  // 释放读取锁
        (app, Some(store))
    } else {
        tracing::info!("AI async enhancement system disabled");
        
        // 创建诊断存储（空）
        let store = Arc::new(AiResultStore::new());
        
        // 创建应用状态
        let app_state = Arc::new(AppState::new(Arc::clone(&nri_table)));
        let nri_v3_api_state = Arc::new(NriV3ApiState::new(Arc::clone(&nri_v3)));
        
        // 构建基础路由（含诊断查询，但 AI 未启用时返回空）
        let mut app = Router::new()
            .merge(trigger_router(Arc::clone(&nri_table), None))
            .merge(nri_router(Arc::clone(&nri_table)))
            .merge(nri_v3_enhanced_router(nri_v3_api_state))
            .merge(health_router(app_state));
        
        // 添加诊断查询路由（即使 AI 禁用也注册，返回 503 Service Unavailable）
        let diagnosis_state = Arc::new(DiagnosisApiState::new(Arc::clone(&store)));
        app = app.merge(diagnosis_router(diagnosis_state));
        
        drop(config_read);  // 释放读取锁
        (app, None)
    };

    // 获取服务器配置（注意：端口/绑定地址热重载需要重启服务）
    let config_read = config.read().await;
    let bind_address = config_read.server.bind_address.clone();
    let port = config_read.server.port;
    drop(config_read);
    
    let addr = std::net::SocketAddr::from((
        parse_bind_address(&bind_address),
        port,
    ));
    tracing::info!("nuts-observer listening on {addr}");

    // 启动 SIGHUP 信号监听器（配置热重载）
    let config_for_reload = Arc::clone(&config);
    tokio::spawn(async move {
        match signal(SignalKind::hangup()) {
            Ok(mut sig) => {
                tracing::info!("[Hot Reload] SIGHUP handler registered. Send SIGHUP to reload config.");
                loop {
                    sig.recv().await;
                    tracing::info!("[Hot Reload] Received SIGHUP signal, reloading configuration...");
                    
                    let mut config = config_for_reload.write().await;
                    match config.reload() {
                        Ok(_) => {
                            tracing::info!("[Hot Reload] Configuration reloaded successfully!");
                            tracing::info!("[Hot Reload] New config: {}", config.reload_summary());
                            // 注意：日志级别变化需要重启才能生效
                            // 端口、绑定地址等关键配置变化也需要重启
                        }
                        Err(e) => {
                            tracing::error!("[Hot Reload] Failed to reload configuration: {}", e);
                        }
                    }
                }
            }
            Err(e) => {
                tracing::warn!("[Hot Reload] Failed to register SIGHUP handler: {}", e);
            }
        }
    });

    let listener = TcpListener::bind(&addr).await.expect("failed to bind");
    tracing::info!("[Server] Starting HTTP server on {}. Send SIGHUP to reload config.", addr);
    axum::serve(listener, app)
        .await
        .expect("server failed");
}

/// 加载配置文件
fn load_config() -> Result<Config, ConfigError> {
    // 尝试从多个路径加载配置文件
    let config_paths = vec![
        "nuts.yaml",
        "/etc/nuts/config.yaml",
        "config/nuts.yaml",
    ];

    for path in &config_paths {
        if std::path::Path::new(path).exists() {
            tracing::info!("Loading config from: {}", path);
            return Config::from_file(path);
        }
    }

    // 如果没有找到配置文件，检查环境变量
    if let Ok(config_path) = std::env::var("NUTS_CONFIG") {
        tracing::info!("Loading config from NUTS_CONFIG: {}", config_path);
        return Config::from_file(config_path);
    }

    // 返回默认配置
    tracing::warn!("No config file found, using default configuration");
    Ok(Config::default())
}

/// 解析绑定地址
fn parse_bind_address(addr: &str) -> std::net::IpAddr {
    addr.parse().unwrap_or_else(|_| std::net::Ipv4Addr::new(0, 0, 0, 0).into())
}


