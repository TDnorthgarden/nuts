# NRI 事件版本控制方案

## 问题
无版本控制时，网络延迟可能导致旧事件覆盖新事件

## 解决方案

```rust
use std::sync::atomic::{AtomicU64, Ordering};

/// 带版本的事件包装
#[derive(Debug, Clone)]
pub struct VersionedNriEvent {
    /// 事件内容
    pub event: NriEvent,
    /// 版本号（时间戳 + 序列号）
    pub version: u64,
    /// 来源 NRI 实例 ID
    pub source_id: String,
}

/// 版本管理器
pub struct EventVersionManager {
    /// 每个 Pod 的最新版本号
    pod_versions: DashMap<String, AtomicU64>,
    /// 全局序列号生成器
    global_seq: AtomicU64,
}

impl EventVersionManager {
    /// 生成新版本号（毫秒时间戳 + 序列号）
    pub fn generate_version(&self) -> u64 {
        let now = chrono::Utc::now().timestamp_millis() as u64;
        let seq = self.global_seq.fetch_add(1, Ordering::SeqCst);
        (now << 20) | (seq & 0xFFFFF)  // 时间戳 + 序列号
    }
    
    /// 检查并更新（CAS 操作）
    pub fn try_update(&self, pod_uid: &str, new_version: u64) -> bool {
        let entry = self.pod_versions
            .entry(pod_uid.to_string())
            .or_insert_with(|| AtomicU64::new(0));
        
        let current = entry.load(Ordering::Acquire);
        if new_version <= current {
            tracing::warn!(
                "Stale event rejected: pod={}, new_version={}, current={}",
                pod_uid, new_version, current
            );
            return false;
        }
        
        entry.store(new_version, Ordering::Release);
        true
    }
}

/// NRI 事件处理器（带版本控制）
pub async fn handle_versioned_event(
    versioned: VersionedNriEvent,
    version_mgr: &EventVersionManager,
    table: &NriMappingTable,
) -> Result<(), Error> {
    let pod_uid = match &versioned.event {
        NriEvent::AddOrUpdate(pod) => &pod.pod_uid,
        NriEvent::Delete { pod_uid } => pod_uid,
    };
    
    // 版本检查
    if !version_mgr.try_update(pod_uid, versioned.version) {
        return Err(Error::StaleEvent);
    }
    
    // 处理事件
    table.update_from_nri(versioned.event).await
}
```

## 收益
- 防止乱序事件覆盖
- 支持事件幂等处理
- 可检测并丢弃过期事件
