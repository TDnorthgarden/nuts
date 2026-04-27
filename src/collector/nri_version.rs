//! NRI 事件版本控制模块
//!
//! 解决问题：网络延迟/重排序导致旧事件覆盖新事件
//!
//! 机制：
//! - 每个 Pod 维护单调递增的版本号
//! - 使用 CAS 操作确保版本一致性
//! - 旧版本事件自动丢弃

use dashmap::DashMap;
use std::sync::atomic::{AtomicU64, Ordering};

/// 事件版本管理器
#[derive(Debug)]
pub struct EventVersionManager {
    /// 每个 Pod 的最新版本号 (pod_uid -> version)
    pod_versions: DashMap<String, AtomicU64>,
    /// 全局序列号生成器（用于时间戳相同的情况）
    global_seq: AtomicU64,
}

impl Default for EventVersionManager {
    fn default() -> Self {
        Self::new()
    }
}

impl EventVersionManager {
    /// 创建新的版本管理器
    pub fn new() -> Self {
        Self {
            pod_versions: DashMap::new(),
            global_seq: AtomicU64::new(0),
        }
    }

    /// 生成新版本号（毫秒时间戳 + 序列号）
    /// 
    /// 格式：高 44 位 = 毫秒时间戳，低 20 位 = 序列号
    /// 支持 174 年时间范围，每毫秒 100 万个序列号
    pub fn generate_version(&self) -> u64 {
        let now = chrono::Utc::now().timestamp_millis() as u64;
        let seq = self.global_seq.fetch_add(1, Ordering::SeqCst) & 0xFFFFF; // 20位序列号
        (now << 20) | seq
    }

    /// 尝试更新 Pod 版本（CAS 操作）
    /// 
    /// 返回：
    /// - Ok(true): 版本更新成功，事件应被处理
    /// - Ok(false): 版本已过时，事件应被丢弃
    /// - Err(e): 内部错误
    pub fn try_update(&self, pod_uid: &str, new_version: u64) -> Result<bool, VersionError> {
        // 获取或创建该 Pod 的版本条目
        let entry = self.pod_versions
            .entry(pod_uid.to_string())
            .or_insert_with(|| AtomicU64::new(0));

        let current = entry.load(Ordering::Acquire);

        // 版本比较
        if new_version <= current {
            tracing::debug!(
                "[VersionManager] Stale event rejected: pod={}, new_version={}, current={}",
                pod_uid, new_version, current
            );
            return Ok(false);
        }

        // CAS 更新
        match entry.compare_exchange(current, new_version, Ordering::Release, Ordering::Acquire) {
            Ok(_) => {
                tracing::debug!(
                    "[VersionManager] Version updated: pod={}, version={}",
                    pod_uid, new_version
                );
                Ok(true)
            }
            Err(actual_current) => {
                // CAS 失败，说明并发更新发生
                if new_version <= actual_current {
                    tracing::debug!(
                        "[VersionManager] Concurrent update detected, stale rejected: pod={}, new={}, actual={}",
                        pod_uid, new_version, actual_current
                    );
                    Ok(false)
                } else {
                    // 虽然 CAS 失败，但新版本仍比当前高，重试
                    tracing::debug!(
                        "[VersionManager] CAS failed but new version higher, retry: pod={}",
                        pod_uid
                    );
                    self.try_update(pod_uid, new_version)
                }
            }
        }
    }

    /// 强制设置版本（用于从持久化恢复）
    pub fn force_set_version(&self, pod_uid: &str, version: u64) {
        let entry = self.pod_versions
            .entry(pod_uid.to_string())
            .or_insert_with(|| AtomicU64::new(0));
        entry.store(version, Ordering::Release);
    }

    /// 获取 Pod 的当前版本
    pub fn get_version(&self, pod_uid: &str) -> u64 {
        self.pod_versions
            .get(pod_uid)
            .map(|v| v.load(Ordering::Acquire))
            .unwrap_or(0)
    }

    /// 检查版本差值（用于检测时钟回拨或异常）
    /// 
    /// 如果新版本比当前版本小超过阈值，可能是时钟回拨
    pub fn is_clock_rollback(&self, pod_uid: &str, new_version: u64, threshold_ms: u64) -> bool {
        let current = self.get_version(pod_uid);
        if current == 0 || new_version >= current {
            return false;
        }

        // 提取时间戳部分（高44位）
        let current_ts = current >> 20;
        let new_ts = new_version >> 20;

        // 如果新时间戳比当前时间戳小超过阈值
        current_ts.saturating_sub(new_ts) > threshold_ms
    }

    /// 获取所有 Pod 的版本统计
    pub fn stats(&self) -> VersionStats {
        let total_pods = self.pod_versions.len();
        let total_versions: u64 = self.pod_versions
            .iter()
            .map(|e| e.value().load(Ordering::Acquire))
            .sum();

        VersionStats {
            total_pods,
            total_versions,
            average_version: if total_pods > 0 {
                total_versions / total_pods as u64
            } else {
                0
            },
        }
    }

    /// 清理已删除 Pod 的版本记录（可选的 GC）
    pub fn cleanup_deleted_pods(&self, active_pods: &[String]) {
        let active_set: std::collections::HashSet<_> = active_pods.iter().cloned().collect();
        
        let to_remove: Vec<String> = self.pod_versions
            .iter()
            .filter(|e| !active_set.contains(e.key()))
            .map(|e| e.key().clone())
            .collect();

        for pod_uid in to_remove {
            self.pod_versions.remove(&pod_uid);
            tracing::debug!("[VersionManager] Cleaned up version record for deleted pod: {}", pod_uid);
        }
    }
}

/// 版本错误类型
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum VersionError {
    /// 内部存储错误
    StorageError(String),
    /// 无效的版本号
    InvalidVersion(u64),
}

impl std::fmt::Display for VersionError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            VersionError::StorageError(msg) => write!(f, "Storage error: {}", msg),
            VersionError::InvalidVersion(v) => write!(f, "Invalid version: {}", v),
        }
    }
}

impl std::error::Error for VersionError {}

/// 版本统计信息
#[derive(Debug, Clone)]
pub struct VersionStats {
    pub total_pods: usize,
    pub total_versions: u64,
    pub average_version: u64,
}

/// 带版本的事件包装
#[derive(Debug, Clone)]
pub struct VersionedEvent<T> {
    /// 事件内容
    pub event: T,
    /// 版本号
    pub version: u64,
    /// 来源标识
    pub source: String,
    /// 接收时间戳
    pub received_at_ms: i64,
}

impl<T> VersionedEvent<T> {
    /// 创建带版本的事件
    pub fn new(event: T, version: u64, source: impl Into<String>) -> Self {
        Self {
            event,
            version,
            source: source.into(),
            received_at_ms: chrono::Utc::now().timestamp_millis(),
        }
    }

    /// 计算处理延迟
    pub fn processing_delay_ms(&self) -> i64 {
        let now = chrono::Utc::now().timestamp_millis();
        now - self.received_at_ms
    }
}

/// 事件版本控制的 trait 接口
/// 
/// 可以被 NriMappingTableV2 实现以集成版本控制
pub trait VersionedEventHandler {
    type Event;

    /// 处理带版本的事件
    /// 
    /// 自动进行版本检查，旧版本事件会被丢弃
    fn handle_versioned_event(
        &self,
        versioned: VersionedEvent<Self::Event>,
    ) -> Result<bool, VersionError>;
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_version_generation() {
        let manager = EventVersionManager::new();
        let v1 = manager.generate_version();
        let v2 = manager.generate_version();
        
        assert!(v2 >= v1, "Versions should be monotonically increasing");
    }

    #[test]
    fn test_version_update_accept() {
        let manager = EventVersionManager::new();
        let version = manager.generate_version();
        
        let result = manager.try_update("pod-001", version);
        assert!(result.unwrap(), "First update should be accepted");
        
        // 相同版本应该被拒绝
        let result = manager.try_update("pod-001", version);
        assert!(!result.unwrap(), "Same version should be rejected");
    }

    #[test]
    fn test_stale_version_reject() {
        let manager = EventVersionManager::new();
        let v1 = manager.generate_version();
        let v2 = manager.generate_version();
        
        // 先更新到 v2
        manager.try_update("pod-002", v2).unwrap();
        
        // 尝试用 v1 更新（应该被拒绝）
        let result = manager.try_update("pod-002", v1);
        assert!(!result.unwrap(), "Stale version should be rejected");
    }

    #[test]
    fn test_clock_rollback_detection() {
        let manager = EventVersionManager::new();
        let now = chrono::Utc::now().timestamp_millis() as u64;
        let current_version = (now << 20) | 1;
        
        manager.force_set_version("pod-003", current_version);
        
        // 模拟 10 秒前的版本
        let old_ts = now - 10000;
        let old_version = (old_ts << 20) | 1;
        
        assert!(
            manager.is_clock_rollback("pod-003", old_version, 5000),
            "Should detect clock rollback"
        );
    }
}
