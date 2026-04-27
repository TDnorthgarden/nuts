//! NRI 映射表持久化存储模块
//!
//! 解决问题：服务重启后映射表丢失，冷启动期间查询失败
//!
//! 机制：
//! - 使用 sled 嵌入式数据库持久化映射数据
//! - 定期快照 + 事件日志双保险
//! - 启动时快速恢复映射表状态
//! - 支持增量同步和全量恢复

use dashmap::DashMap;
use serde::{Deserialize, Serialize};
use std::path::Path;
use std::sync::Arc;

/// 持久化配置
#[derive(Debug, Clone)]
pub struct PersistConfig {
    /// 数据库路径
    pub db_path: String,
    /// 自动快照间隔（秒）
    pub snapshot_interval_secs: u64,
    /// 是否启用异步刷盘
    pub flush_async: bool,
    /// 缓存大小（MB）
    pub cache_capacity_mb: usize,
}

impl Default for PersistConfig {
    fn default() -> Self {
        Self {
            db_path: "/var/lib/nuts/nri.db".to_string(),
            snapshot_interval_secs: 300, // 5分钟
            flush_async: true,
            cache_capacity_mb: 128,
        }
    }
}

/// NRI 持久化存储
pub struct NriPersistStore {
    /// sled 数据库实例
    db: sled::Db,
    /// 配置
    config: PersistConfig,
}

impl NriPersistStore {
    /// 打开或创建持久化存储
    pub fn open(config: PersistConfig) -> Result<Self, PersistError> {
        // 确保目录存在
        let db_path = Path::new(&config.db_path);
        if let Some(parent) = db_path.parent() {
            std::fs::create_dir_all(parent)?;
        }

        // 配置 sled
        let db = sled::Config::new()
            .path(&config.db_path)
            .cache_capacity((config.cache_capacity_mb * 1024 * 1024) as u64)
            .flush_every_ms(if config.flush_async { Some(500) } else { None })
            .open()?;

        tracing::info!(
            "[NriPersist] Database opened at {} (cache: {}MB)",
            config.db_path, config.cache_capacity_mb
        );

        Ok(Self { db, config })
    }

    /// 保存 Pod 信息
    pub fn save_pod(&self, pod: &PodInfoRecord) -> Result<(), PersistError> {
        let key = format!("pod:{}", pod.pod_uid);
        let value = serde_json::to_vec(pod)?;
        self.db.insert(key, value)?;
        
        if !self.config.flush_async {
            self.db.flush()?;
        }
        
        Ok(())
    }

    /// 加载所有 Pod
    pub fn load_all_pods(&self) -> Result<Vec<PodInfoRecord>, PersistError> {
        let mut pods = Vec::new();
        
        for item in self.db.scan_prefix("pod:") {
            let (_, value) = item?;
            let pod: PodInfoRecord = serde_json::from_slice(&value)?;
            pods.push(pod);
        }
        
        tracing::info!("[NriPersist] Loaded {} pods from database", pods.len());
        Ok(pods)
    }

    /// 删除 Pod
    pub fn delete_pod(&self, pod_uid: &str) -> Result<(), PersistError> {
        let key = format!("pod:{}", pod_uid);
        self.db.remove(key)?;
        Ok(())
    }

    /// 保存容器信息
    pub fn save_container(&self, container: &ContainerRecord) -> Result<(), PersistError> {
        let key = format!("container:{}", container.container_id);
        let value = serde_json::to_vec(container)?;
        self.db.insert(key, value)?;
        Ok(())
    }

    /// 加载所有容器
    pub fn load_all_containers(&self) -> Result<Vec<ContainerRecord>, PersistError> {
        let mut containers = Vec::new();
        
        for item in self.db.scan_prefix("container:") {
            let (_, value) = item?;
            let container: ContainerRecord = serde_json::from_slice(&value)?;
            containers.push(container);
        }
        
        Ok(containers)
    }

    /// 保存 cgroup 信息
    pub fn save_cgroup(&self, cgroup: &CgroupRecord) -> Result<(), PersistError> {
        let key = format!("cgroup:{}", cgroup.cgroup_id);
        let value = serde_json::to_vec(cgroup)?;
        self.db.insert(key, value)?;
        Ok(())
    }

    /// 加载所有 cgroup
    pub fn load_all_cgroups(&self) -> Result<Vec<CgroupRecord>, PersistError> {
        let mut cgroups = Vec::new();
        
        for item in self.db.scan_prefix("cgroup:") {
            let (_, value) = item?;
            let cgroup: CgroupRecord = serde_json::from_slice(&value)?;
            cgroups.push(cgroup);
        }
        
        Ok(cgroups)
    }

    /// 保存 PID 映射
    pub fn save_pid(&self, pid: u32, mapping: &PidRecord) -> Result<(), PersistError> {
        let key = format!("pid:{}", pid);
        let value = serde_json::to_vec(mapping)?;
        self.db.insert(key, value)?;
        Ok(())
    }

    /// 加载所有 PID 映射
    pub fn load_all_pids(&self) -> Result<Vec<(u32, PidRecord)>, PersistError> {
        let mut pids = Vec::new();
        
        for item in self.db.scan_prefix("pid:") {
            let (key, value) = item?;
            let key_str = String::from_utf8_lossy(&key);
            if let Some(pid_str) = key_str.strip_prefix("pid:") {
                if let Ok(pid) = pid_str.parse::<u32>() {
                    let mapping: PidRecord = serde_json::from_slice(&value)?;
                    pids.push((pid, mapping));
                }
            }
        }
        
        Ok(pids)
    }

    /// 保存元数据（最后更新时间、版本等）
    pub fn save_metadata(&self, meta: &PersistMetadata) -> Result<(), PersistError> {
        let value = serde_json::to_vec(meta)?;
        self.db.insert("meta:info", value)?;
        self.db.flush()?; // 元数据同步刷盘
        Ok(())
    }

    /// 加载元数据
    pub fn load_metadata(&self) -> Result<Option<PersistMetadata>, PersistError> {
        match self.db.get("meta:info")? {
            Some(value) => {
                let meta: PersistMetadata = serde_json::from_slice(&value)?;
                Ok(Some(meta))
            }
            None => Ok(None),
        }
    }

    /// 创建全量快照
    pub fn snapshot_table(
        &self,
        pods: &DashMap<String, super::nri_mapping_v2::PodInfo>,
        containers: &DashMap<String, super::nri_mapping::ContainerMapping>,
        cgroups: &DashMap<String, super::nri_mapping::CgroupMapping>,
        pids: &DashMap<u32, super::nri_mapping::PidMapping>,
    ) -> Result<SnapshotInfo, PersistError> {
        let start = std::time::Instant::now();
        let mut batch = sled::Batch::default();

        // 批量写入 Pod
        for entry in pods.iter() {
            let pod_record = PodInfoRecord::from(entry.value().clone());
            let key = format!("pod:{}", pod_record.pod_uid);
            let value = serde_json::to_vec(&pod_record)?;
            batch.insert(key.as_bytes(), value);
        }

        // 批量写入容器
        for entry in containers.iter() {
            let container_record = ContainerRecord::from(entry.value().clone());
            let key = format!("container:{}", container_record.container_id);
            let value = serde_json::to_vec(&container_record)?;
            batch.insert(key.as_bytes(), value);
        }

        // 批量写入 cgroup
        for entry in cgroups.iter() {
            let cgroup_record = CgroupRecord::from(entry.value().clone());
            let key = format!("cgroup:{}", cgroup_record.cgroup_id);
            let value = serde_json::to_vec(&cgroup_record)?;
            batch.insert(key.as_bytes(), value);
        }

        // 批量写入 PID
        for entry in pids.iter() {
            let pid = *entry.key();
            let pid_record = PidRecord::from(entry.value().clone());
            let key = format!("pid:{}", pid);
            let value = serde_json::to_vec(&pid_record)?;
            batch.insert(key.as_bytes(), value);
        }

        // 执行批量写入
        self.db.apply_batch(batch)?;
        self.db.flush()?;

        let elapsed = start.elapsed();
        let info = SnapshotInfo {
            pod_count: pods.len(),
            container_count: containers.len(),
            cgroup_count: cgroups.len(),
            pid_count: pids.len(),
            elapsed_ms: elapsed.as_millis() as u64,
            timestamp_ms: chrono::Utc::now().timestamp_millis(),
        };

        // 保存快照元数据
        self.save_metadata(&PersistMetadata {
            last_snapshot_ms: info.timestamp_ms,
            pod_count: info.pod_count,
            container_count: info.container_count,
            cgroup_count: info.cgroup_count,
            pid_count: info.pid_count,
        })?;

        tracing::info!(
            "[NriPersist] Snapshot completed: {} pods, {} containers, {} cgroups, {} pids in {}ms",
            info.pod_count, info.container_count, info.cgroup_count, info.pid_count, info.elapsed_ms
        );

        Ok(info)
    }

    /// 获取数据库统计
    pub fn db_export(&self) -> Vec<(Vec<u8>, Vec<u8>, impl Iterator<Item = Vec<Vec<u8>>>)> {
        self.db.export()
    }

    /// 关闭数据库（确保刷盘）
    pub fn close(self) -> Result<(), PersistError> {
        self.db.flush()?;
        drop(self.db);
        Ok(())
    }
}

/// 持久化 Pod 记录（序列化格式）
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PodInfoRecord {
    pub pod_uid: String,
    pub pod_name: String,
    pub namespace: String,
    pub containers: Vec<super::nri_mapping::ContainerMapping>,
    pub version: u64, // 版本号
    pub updated_at_ms: i64,
}

impl From<super::nri_mapping_v2::PodInfo> for PodInfoRecord {
    fn from(pod: super::nri_mapping_v2::PodInfo) -> Self {
        Self {
            pod_uid: pod.pod_uid,
            pod_name: pod.pod_name,
            namespace: pod.namespace,
            containers: pod.containers,
            version: 0,
            updated_at_ms: chrono::Utc::now().timestamp_millis(),
        }
    }
}

/// 容器记录
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ContainerRecord {
    pub container_id: String,
    pub pod_uid: String,
    pub cgroup_ids: Vec<String>,
    pub updated_at_ms: i64,
}

impl From<super::nri_mapping::ContainerMapping> for ContainerRecord {
    fn from(c: super::nri_mapping::ContainerMapping) -> Self {
        Self {
            container_id: c.container_id,
            pod_uid: c.pod_uid,
            cgroup_ids: c.cgroup_ids,
            updated_at_ms: chrono::Utc::now().timestamp_millis(),
        }
    }
}

/// cgroup 记录
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CgroupRecord {
    pub cgroup_id: String,
    pub pod_uid: Option<String>,
    pub container_id: Option<String>,
    pub pids: Vec<u32>,
    pub updated_at_ms: i64,
}

impl From<super::nri_mapping::CgroupMapping> for CgroupRecord {
    fn from(cg: super::nri_mapping::CgroupMapping) -> Self {
        Self {
            cgroup_id: cg.cgroup_id,
            pod_uid: cg.pod_uid,
            container_id: cg.container_id,
            pids: cg.pids,
            updated_at_ms: chrono::Utc::now().timestamp_millis(),
        }
    }
}

/// PID 记录
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PidRecord {
    pub pid: u32,
    pub cgroup_id: String,
    pub updated_at_ms: i64,
}

impl From<super::nri_mapping::PidMapping> for PidRecord {
    fn from(p: super::nri_mapping::PidMapping) -> Self {
        Self {
            pid: p.pid,
            cgroup_id: p.cgroup_id,
            updated_at_ms: chrono::Utc::now().timestamp_millis(),
        }
    }
}

/// 持久化元数据
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PersistMetadata {
    pub last_snapshot_ms: i64,
    pub pod_count: usize,
    pub container_count: usize,
    pub cgroup_count: usize,
    pub pid_count: usize,
}

/// 快照信息
#[derive(Debug, Clone)]
pub struct SnapshotInfo {
    pub pod_count: usize,
    pub container_count: usize,
    pub cgroup_count: usize,
    pub pid_count: usize,
    pub elapsed_ms: u64,
    pub timestamp_ms: i64,
}

/// 持久化错误类型
#[derive(Debug)]
pub enum PersistError {
    /// sled 错误
    Sled(sled::Error),
    /// 序列化错误
    Serialization(serde_json::Error),
    /// IO 错误
    Io(std::io::Error),
    /// 其他错误
    Other(String),
}

impl From<sled::Error> for PersistError {
    fn from(e: sled::Error) -> Self {
        PersistError::Sled(e)
    }
}

impl From<serde_json::Error> for PersistError {
    fn from(e: serde_json::Error) -> Self {
        PersistError::Serialization(e)
    }
}

impl From<std::io::Error> for PersistError {
    fn from(e: std::io::Error) -> Self {
        PersistError::Io(e)
    }
}

impl std::fmt::Display for PersistError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            PersistError::Sled(e) => write!(f, "Database error: {}", e),
            PersistError::Serialization(e) => write!(f, "Serialization error: {}", e),
            PersistError::Io(e) => write!(f, "IO error: {}", e),
            PersistError::Other(msg) => write!(f, "{}", msg),
        }
    }
}

impl std::error::Error for PersistError {
    fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
        match self {
            PersistError::Sled(e) => Some(e),
            PersistError::Serialization(e) => Some(e),
            PersistError::Io(e) => Some(e),
            PersistError::Other(_) => None,
        }
    }
}

/// 从持久化存储恢复映射表
/// 
/// 返回恢复后的表和元数据
pub fn restore_from_persist(
    config: PersistConfig,
) -> Result<(super::nri_mapping_v2::NriMappingTableV2, Option<PersistMetadata>), PersistError> {
    let store = NriPersistStore::open(config)?;
    
    // 加载元数据
    let meta = store.load_metadata()?;
    
    // 加载所有数据
    let pods = store.load_all_pods()?;
    let containers = store.load_all_containers()?;
    let cgroups = store.load_all_cgroups()?;
    let pids = store.load_all_pids()?;
    
    // 构建新的映射表
    let table = super::nri_mapping_v2::NriMappingTableV2::with_capacity(
        pods.len() * 2,
        containers.len() * 2,
        cgroups.len() * 2,
        pids.len() * 2,
    );
    
    // 恢复容器映射
    for container in containers {
        table.container_map.insert(
            container.container_id.clone(),
            super::nri_mapping::ContainerMapping {
                container_id: container.container_id,
                pod_uid: container.pod_uid,
                cgroup_ids: container.cgroup_ids,
            },
        );
    }
    
    // 恢复 cgroup 映射
    for cgroup in cgroups {
        table.cgroup_map.insert(
            cgroup.cgroup_id.clone(),
            super::nri_mapping::CgroupMapping {
                cgroup_id: cgroup.cgroup_id,
                pod_uid: cgroup.pod_uid,
                container_id: cgroup.container_id,
                pids: cgroup.pids,
            },
        );
    }
    
    // 恢复 PID 映射
    for (pid, pid_record) in pids {
        table.pid_map.insert(
            pid,
            super::nri_mapping::PidMapping {
                pid,
                cgroup_id: pid_record.cgroup_id,
            },
        );
    }
    
    // 恢复 Pod 映射（最后，因为依赖容器信息）
    for pod in pods {
        table.pod_map.insert(
            pod.pod_uid.clone(),
            super::nri_mapping_v2::PodInfo {
                pod_uid: pod.pod_uid,
                pod_name: pod.pod_name,
                namespace: pod.namespace,
                containers: pod.containers,
            },
        );
    }
    
    // 更新时间戳
    if let Some(ref m) = meta {
        table.last_update_ms.store(m.last_snapshot_ms, std::sync::atomic::Ordering::Release);
    }
    
    tracing::info!(
        "[NriPersist] Restored {} pods, {} containers, {} cgroups, {} pids from database",
        table.pod_count(), table.container_count(), table.cgroup_count(), table.pid_count()
    );
    
    // 关闭存储
    store.close()?;
    
    Ok((table, meta))
}

/// 启动后台快照任务
/// 
/// 定期将映射表快照保存到持久化存储
pub fn start_snapshot_task(
    table: Arc<super::nri_mapping_v2::NriMappingTableV2>,
    config: PersistConfig,
) -> tokio::task::JoinHandle<()> {
    let interval = std::time::Duration::from_secs(config.snapshot_interval_secs);
    
    tokio::spawn(async move {
        let store = match NriPersistStore::open(config) {
            Ok(s) => s,
            Err(e) => {
                tracing::error!("[NriPersist] Failed to open store for snapshot task: {}", e);
                return;
            }
        };
        
        let mut ticker = tokio::time::interval(interval);
        
        loop {
            ticker.tick().await;
            
            // 执行快照
            match store.snapshot_table(
                &table.pod_map,
                &table.container_map,
                &table.cgroup_map,
                &table.pid_map,
            ) {
                Ok(info) => {
                    tracing::info!(
                        "[NriPersist] Scheduled snapshot completed: {} pods in {}ms",
                        info.pod_count, info.elapsed_ms
                    );
                }
                Err(e) => {
                    tracing::error!("[NriPersist] Scheduled snapshot failed: {}", e);
                }
            }
        }
    })
}
