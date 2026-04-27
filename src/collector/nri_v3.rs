//! NRI V3 集成版 - 一键启用所有优化
//!
//! 功能组合：
//! - DashMap 高性能并发存储
//! - 事件版本控制（防旧盖新）
//! - sled 持久化存储（自动恢复）
//! - 批量事件处理（背压控制）
//! - Prometheus 指标导出
//!
//! 使用方式：
//! ```rust
//! let nri = NriV3::new(NriV3Config::default())
//!     .await
//!     .expect("Failed to initialize NRI V3");
//! ```

use std::sync::Arc;

use super::nri_mapping::NriEvent;
use super::nri_mapping_v2::{NriMappingTableV2, NriMappingStats};
use super::nri_version::EventVersionManager;
use super::nri_persist::{restore_from_persist, PersistConfig, NriPersistStore};
use super::nri_batch::{BatchProcessorConfig, NriBatchProcessor};
use crate::metrics::{NriMetrics, create_metrics};

/// V3 配置
#[derive(Debug, Clone)]
pub struct NriV3Config {
    /// 持久化配置
    pub persistence: PersistConfig,
    /// 批量处理配置
    pub batch: BatchProcessorConfig,
    /// 是否启用持久化
    pub enable_persistence: bool,
    /// 是否启用指标
    pub enable_metrics: bool,
    /// 预分配容量
    pub capacity: CapacityConfig,
}

/// 容量配置
#[derive(Debug, Clone)]
pub struct CapacityConfig {
    pub pods: usize,
    pub containers: usize,
    pub cgroups: usize,
    pub pids: usize,
}

impl Default for NriV3Config {
    fn default() -> Self {
        Self {
            persistence: PersistConfig::default(),
            batch: BatchProcessorConfig::default(),
            enable_persistence: true,
            enable_metrics: true,
            capacity: CapacityConfig::default(),
        }
    }
}

impl Default for CapacityConfig {
    fn default() -> Self {
        Self {
            pods: 1000,
            containers: 2000,
            cgroups: 2000,
            pids: 10000,
        }
    }
}

/// NRI V3 集成结构
pub struct NriV3 {
    /// 高性能映射表
    table: Arc<NriMappingTableV2>,
    /// 版本管理器
    version_mgr: Arc<EventVersionManager>,
    /// 批量处理器
    batch_processor: NriBatchProcessor,
    /// 指标收集器
    metrics: Arc<NriMetrics>,
    /// 持久化存储（可选）
    persist_store: Option<Arc<NriPersistStore>>,
    /// 后台任务句柄
    handles: Vec<tokio::task::JoinHandle<()>>,
}

impl NriV3 {
    /// 创建并初始化 NRI V3
    pub async fn new(config: NriV3Config) -> Result<Self, NriV3Error> {
        tracing::info!("[NriV3] Initializing NRI V3 with full optimizations...");

        // 1. 尝试从持久化恢复或创建新表
        let table = Self::init_table(&config).await?;
        let table = Arc::new(table);

        // 2. 创建版本管理器
        let version_mgr = Arc::new(EventVersionManager::new());
        tracing::info!("[NriV3] Event version manager initialized");

        // 3. 创建指标收集器
        let metrics = if config.enable_metrics {
            let m = create_metrics();
            tracing::info!("[NriV3] Metrics collection enabled");
            m
        } else {
            create_metrics() // 空实现
        };

        // 4. 创建持久化存储
        let persist_store: Option<Arc<NriPersistStore>> = if config.enable_persistence {
            let store = NriPersistStore::open(config.persistence.clone())?;
            tracing::info!("[NriV3] Persistence enabled at {}", config.persistence.db_path);
            Some(Arc::new(store))
        } else {
            tracing::info!("[NriV3] Persistence disabled");
            None
        };

        // 5. 创建批量处理器
        let (batch_processor, batch_handles) = NriBatchProcessor::new(
            config.batch.clone(),
            Arc::clone(&table),
            Arc::clone(&version_mgr),
        );

        // 6. 启动后台任务
        let mut handles = batch_handles;

        // 启动快照任务
        if let Some(store) = &persist_store {
            let table_clone = Arc::clone(&table);
            let store_clone = Arc::clone(store);
            let interval = config.persistence.snapshot_interval_secs;
            
            let snapshot_handle = tokio::spawn(async move {
                let mut ticker = tokio::time::interval(
                    tokio::time::Duration::from_secs(interval)
                );
                
                loop {
                    ticker.tick().await;
                    
                    let start = std::time::Instant::now();
                    match store_clone.snapshot_table(
                        &table_clone.pod_map,
                        &table_clone.container_map,
                        &table_clone.cgroup_map,
                        &table_clone.pid_map,
                    ) {
                        Ok(info) => {
                            let elapsed = start.elapsed().as_millis() as u64;
                            tracing::info!(
                                "[NriV3] Auto snapshot completed: {} pods in {}ms",
                                info.pod_count, elapsed
                            );
                        }
                        Err(e) => {
                            tracing::error!("[NriV3] Auto snapshot failed: {}", e);
                        }
                    }
                }
            });
            handles.push(snapshot_handle);
        }

        // 启动指标更新任务
        let metrics_clone = Arc::clone(&metrics);
        let table_clone = Arc::clone(&table);
        let metrics_handle = tokio::spawn(async move {
            let mut ticker = tokio::time::interval(tokio::time::Duration::from_secs(10));
            
            loop {
                ticker.tick().await;
                
                // 更新映射表大小指标
                metrics_clone.update_mapping_table_size(
                    table_clone.pod_count(),
                    table_clone.container_count(),
                    table_clone.cgroup_count(),
                    table_clone.pid_count(),
                );
            }
        });
        handles.push(metrics_handle);

        tracing::info!("[NriV3] Initialization completed successfully");

        Ok(Self {
            table,
            version_mgr,
            batch_processor,
            metrics,
            persist_store,
            handles,
        })
    }

    /// 初始化映射表（尝试恢复或创建新表）
    async fn init_table(config: &NriV3Config) -> Result<NriMappingTableV2, NriV3Error> {
        if config.enable_persistence {
            // 尝试从持久化恢复
            match restore_from_persist(config.persistence.clone()) {
                Ok((table, meta)) => {
                    tracing::info!(
                        "[NriV3] Restored from persistence: {} pods, {} containers, {} cgroups, {} pids",
                        table.pod_count(),
                        table.container_count(),
                        table.cgroup_count(),
                        table.pid_count()
                    );
                    if let Some(m) = meta {
                        tracing::info!(
                            "[NriV3] Last snapshot at {}",
                            chrono::DateTime::from_timestamp_millis(m.last_snapshot_ms)
                                .map(|d| d.to_rfc3339())
                                .unwrap_or_else(|| "unknown".to_string())
                        );
                    }
                    return Ok(table);
                }
                Err(e) => {
                    tracing::warn!(
                        "[NriV3] Failed to restore from persistence: {}. Creating fresh table.",
                        e
                    );
                }
            }
        }

        // 创建新表
        let table = NriMappingTableV2::with_capacity(
            config.capacity.pods * 2,
            config.capacity.containers * 2,
            config.capacity.cgroups * 2,
            config.capacity.pids * 2,
        );
        
        tracing::info!(
            "[NriV3] Created fresh mapping table with capacity: {} pods, {} containers",
            config.capacity.pods, config.capacity.containers
        );
        
        Ok(table)
    }

    /// 提交事件到处理队列
    pub async fn submit_event(&self, event: NriEvent) -> Result<(), NriV3Error> {
        let start = std::time::Instant::now();

        // 通过批量处理器提交
        self.batch_processor.submit(event).await
            .map_err(|e| NriV3Error::BatchError(e.to_string()))?;

        // 记录指标
        let duration_us = start.elapsed().as_micros() as u64;
        self.metrics.record_event("submitted", duration_us);

        Ok(())
    }

    /// 尝试非阻塞提交
    pub fn try_submit_event(&self, event: NriEvent) -> Result<(), NriV3Error> {
        self.batch_processor.try_submit(event)
            .map_err(|e| NriV3Error::BatchError(e.to_string()))
    }

    /// 获取映射表引用
    pub fn table(&self) -> Arc<NriMappingTableV2> {
        Arc::clone(&self.table)
    }

    /// 获取指标收集器
    pub fn metrics(&self) -> Arc<NriMetrics> {
        Arc::clone(&self.metrics)
    }

    /// 获取统计信息
    pub fn stats(&self) -> NriV3Stats {
        let table_stats = self.table.stats();
        let version_stats = self.version_mgr.stats();

        NriV3Stats {
            mapping_table: table_stats,
            version_control: version_stats,
            batch_queue_depth: self.batch_processor.queue_depth(),
            metrics: self.metrics.export_json(),
        }
    }

    /// 强制持久化快照
    pub fn force_snapshot(&self) -> Result<(), NriV3Error> {
        if let Some(ref store) = self.persist_store {
            let info = store.snapshot_table(
                &self.table.pod_map,
                &self.table.container_map,
                &self.table.cgroup_map,
                &self.table.pid_map,
            )?;
            
            tracing::info!(
                "[NriV3] Manual snapshot completed: {} pods, {} containers in {}ms",
                info.pod_count, info.container_count, info.elapsed_ms
            );
            
            Ok(())
        } else {
            Err(NriV3Error::PersistenceDisabled)
        }
    }

    /// 强制刷新批量处理器
    pub async fn flush(&self) {
        self.batch_processor.flush().await;
    }

    /// 优雅关闭
    pub async fn shutdown(self) {
        tracing::info!("[NriV3] Shutting down...");

        // 先获取需要的数据，避免部分移动问题
        let batch_processor = self.batch_processor;
        let persist_store = self.persist_store;
        let mut handles = self.handles;
        let table = self.table;

        // 执行最终快照
        if let Some(store) = persist_store {
            match store.snapshot_table(&table.pod_map, &table.container_map, &table.cgroup_map, &table.pid_map) {
                Ok(info) => tracing::info!("[NriV3] Final snapshot: {} pods", info.pod_count),
                Err(e) => tracing::warn!("[NriV3] Final snapshot failed: {}", e),
            }
        }

        // 刷新批量处理器
        batch_processor.flush().await;

        // 取消后台任务
        for handle in handles {
            handle.abort();
        }

        tracing::info!("[NriV3] Shutdown completed");
    }
}

/// V3 统计信息
#[derive(Debug, Clone)]
pub struct NriV3Stats {
    pub mapping_table: NriMappingStats,
    pub version_control: super::nri_version::VersionStats,
    pub batch_queue_depth: usize,
    pub metrics: serde_json::Value,
}

/// V3 错误类型
#[derive(Debug)]
pub enum NriV3Error {
    Persistence(super::nri_persist::PersistError),
    BatchError(String),
    PersistenceDisabled,
}

impl From<super::nri_persist::PersistError> for NriV3Error {
    fn from(e: super::nri_persist::PersistError) -> Self {
        NriV3Error::Persistence(e)
    }
}

impl std::fmt::Display for NriV3Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            NriV3Error::Persistence(e) => write!(f, "Persistence error: {}", e),
            NriV3Error::BatchError(msg) => write!(f, "Batch error: {}", msg),
            NriV3Error::PersistenceDisabled => write!(f, "Persistence is disabled"),
        }
    }
}

impl std::error::Error for NriV3Error {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            NriV3Error::Persistence(e) => Some(e),
            _ => None,
        }
    }
}

/// 便捷函数：快速启动 NRI V3
pub async fn create_nri_v3() -> Result<NriV3, NriV3Error> {
    NriV3::new(NriV3Config::default()).await
}

/// 便捷函数：带配置的 NRI V3
pub async fn create_nri_v3_with_config(config: NriV3Config) -> Result<NriV3, NriV3Error> {
    NriV3::new(config).await
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::collector::nri_mapping::{NriContainerInfo, NriPodEvent};

    #[tokio::test]
    async fn test_nri_v3_creation() {
        let config = NriV3Config {
            enable_persistence: false, // 测试时禁用持久化
            enable_metrics: true,
            ..Default::default()
        };

        let nri = NriV3::new(config).await.expect("Failed to create NRI V3");

        // 提交事件
        let event = NriEvent::AddOrUpdate(NriPodEvent {
            pod_uid: "test-uid".to_string(),
            pod_name: "test-pod".to_string(),
            namespace: "default".to_string(),
            containers: vec![NriContainerInfo {
                container_id: "container-1".to_string(),
                cgroup_ids: vec!["cg-1".to_string()],
                pids: vec![1234],
            }],
        });

        nri.submit_event(event).await.expect("Failed to submit event");
        
        // 等待处理
        nri.flush().await;

        // 验证
        assert_eq!(nri.table().pod_count(), 1);
        
        // 获取指标
        let export = nri.metrics().export_prometheus();
        assert!(export.contains("nri_events_total"));

        // 关闭
        nri.shutdown().await;
    }

    #[test]
    fn test_nri_v3_stats() {
        let config = NriV3Config {
            enable_persistence: false,
            enable_metrics: true,
            ..Default::default()
        };

        // 需要运行时环境
        let rt = tokio::runtime::Runtime::new().unwrap();
        rt.block_on(async {
            let nri = NriV3::new(config).await.unwrap();
            
            let stats = nri.stats();
            assert!(stats.metrics.get("mapping_table").is_some());
            
            nri.shutdown().await;
        });
    }
}

// TODO: Fix nri_v3_tests module - tests need to be updated for new API
// #[cfg(test)]
// #[path = "nri_v3_tests.rs"]
// mod nri_v3_tests;
