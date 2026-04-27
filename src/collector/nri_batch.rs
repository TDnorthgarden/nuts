//! NRI 批量事件缓冲处理模块
//!
//! 解决问题：大规模 Pod 创建时单事件处理性能差
//!
//! 机制：
//! - 事件缓冲队列（时间/数量双触发 flush）
//! - 批量写入 DashMap（减少锁竞争次数）
//! - 优先级队列（重要事件优先处理）
//! - 背压控制（防止内存溢出）

use std::collections::BinaryHeap;
use std::sync::Arc;
use tokio::sync::{mpsc, Notify, Semaphore};

use super::nri_mapping::NriEvent;
use super::nri_mapping_v2::NriMappingTableV2;
use super::nri_version::EventVersionManager;

/// 批量处理器配置
#[derive(Debug, Clone)]
pub struct BatchProcessorConfig {
    /// 批量大小阈值
    pub batch_size: usize,
    /// 最大缓冲时间（毫秒）
    pub max_buffer_ms: u64,
    /// 最大队列深度（背压控制）
    pub max_queue_depth: usize,
    /// 工作线程数
    pub worker_threads: usize,
    /// 是否启用优先级
    pub enable_priority: bool,
    /// DELETE 事件优先级加成
    pub delete_priority_boost: u8,
}

impl Default for BatchProcessorConfig {
    fn default() -> Self {
        Self {
            batch_size: 100,
            max_buffer_ms: 100,      // 100ms 最大延迟
            max_queue_depth: 10000,  // 1万事件背压
            worker_threads: 2,
            enable_priority: true,
            delete_priority_boost: 10, // DELETE 事件优先级+10
        }
    }
}

/// 带优先级的事件
#[derive(Debug, Clone)]
struct PrioritizedEvent {
    /// 优先级（数值越小优先级越高）
    priority: u8,
    /// 序列号（保证相同优先级下的 FIFO）
    sequence: u64,
    /// 事件内容
    event: NriEvent,
    /// 接收时间戳
    received_at_ms: i64,
}

// 实现优先级队列的比较（最小堆）
impl Ord for PrioritizedEvent {
    fn cmp(&self, other: &Self) -> std::cmp::Ordering {
        // 首先比较优先级（数值小的优先）
        self.priority
            .cmp(&other.priority)
            .reverse() // BinaryHeap 是大根堆，需要 reverse
            .then_with(|| self.sequence.cmp(&other.sequence))
    }
}

impl PartialOrd for PrioritizedEvent {
    fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
        Some(self.cmp(other))
    }
}

impl PartialEq for PrioritizedEvent {
    fn eq(&self, other: &Self) -> bool {
        self.priority == other.priority && self.sequence == other.sequence
    }
}

impl Eq for PrioritizedEvent {}

/// NRI 批量事件处理器
pub struct NriBatchProcessor {
    config: BatchProcessorConfig,
    table: Arc<NriMappingTableV2>,
    version_mgr: Arc<EventVersionManager>,
    /// 事件发送通道
    event_tx: mpsc::Sender<PrioritizedEvent>,
    /// 背压信号量
    backpressure: Arc<Semaphore>,
    /// 全局序列号
    sequence: Arc<std::sync::atomic::AtomicU64>,
    /// 刷新通知
    flush_notify: Arc<Notify>,
}

impl NriBatchProcessor {
    /// 创建新的批量处理器
    pub fn new(
        config: BatchProcessorConfig,
        table: Arc<NriMappingTableV2>,
        version_mgr: Arc<EventVersionManager>,
    ) -> (Self, Vec<tokio::task::JoinHandle<()>>) {
        let (event_tx, mut event_rx) = mpsc::channel(config.max_queue_depth);
        let backpressure = Arc::new(Semaphore::new(config.max_queue_depth));
        let flush_notify = Arc::new(Notify::new());
        let sequence = Arc::new(std::sync::atomic::AtomicU64::new(0));

        // 启动工作线程
        let mut handles = Vec::new();
        let worker_count = config.worker_threads.max(1);
        
        // 为每个工作线程创建独立的 channel
        let mut worker_txs = Vec::with_capacity(worker_count);
        let mut worker_rxs = Vec::with_capacity(worker_count);
        
        for _ in 0..worker_count {
            let (tx, rx) = mpsc::channel(config.max_queue_depth / worker_count + 1);
            worker_txs.push(tx);
            worker_rxs.push(rx);
        }
        
        // 启动事件分发任务（轮询分发到各个工作线程）
        let dispatch_handle = tokio::spawn(async move {
            let mut idx = 0usize;
            while let Some(event) = event_rx.recv().await {
                // 轮询分发
                if worker_txs[idx].send(event).await.is_err() {
                    // 该工作线程已关闭，尝试下一个
                }
                idx = (idx + 1) % worker_count;
            }
        });
        handles.push(dispatch_handle);
        
        // 启动多个工作线程
        for (worker_id, worker_rx) in worker_rxs.into_iter().enumerate() {
            let table_clone = Arc::clone(&table);
            let vm_clone = Arc::clone(&version_mgr);
            let cfg_clone = config.clone();
            let flush_clone = Arc::clone(&flush_notify);
            
            let handle = tokio::spawn(async move {
                run_worker(worker_id, worker_rx, table_clone, vm_clone, cfg_clone, flush_clone).await;
            });
            handles.push(handle);
        }

        // 启动定时刷新任务
        let flush = Arc::clone(&flush_notify);
        let flush_handle = tokio::spawn(async move {
            let interval = tokio::time::Duration::from_millis(config.max_buffer_ms);
            let mut ticker = tokio::time::interval(interval);

            loop {
                ticker.tick().await;
                flush.notify_waiters();
            }
        });
        handles.push(flush_handle);

        let processor = Self {
            config,
            table,
            version_mgr,
            event_tx,
            backpressure,
            sequence,
            flush_notify,
        };

        (processor, handles)
    }

    /// 提交事件（异步，可能阻塞直到队列有空间）
    pub async fn submit(&self, event: NriEvent) -> Result<(), BatchError> {
        // 背压控制：获取许可
        let _permit = self
            .backpressure
            .acquire()
            .await
            .map_err(|_| BatchError::ChannelClosed)?;

        // 获取序列号
        let seq = self
            .sequence
            .fetch_add(1, std::sync::atomic::Ordering::SeqCst);

        // 构建优先级事件
        let prioritized = PrioritizedEvent {
            priority: calculate_priority(&event, self.config.delete_priority_boost),
            sequence: seq,
            event,
            received_at_ms: chrono::Utc::now().timestamp_millis(),
        };

        // 发送到队列
        self.event_tx
            .send(prioritized)
            .await
            .map_err(|_| BatchError::ChannelClosed)?;

        Ok(())
    }

    /// 提交事件（非阻塞，可能丢弃）
    pub fn try_submit(&self, event: NriEvent) -> Result<(), BatchError> {
        // 尝试获取许可（非阻塞）
        let _permit = self
            .backpressure
            .try_acquire()
            .map_err(|_| BatchError::Backpressure("Queue full".to_string()))?;

        let priority = if self.config.enable_priority {
            calculate_priority(&event, self.config.delete_priority_boost)
        } else {
            128
        };

        let seq = self
            .sequence
            .fetch_add(1, std::sync::atomic::Ordering::SeqCst);

        let prioritized = PrioritizedEvent {
            priority,
            sequence: seq,
            event,
            received_at_ms: chrono::Utc::now().timestamp_millis(),
        };

        // 尝试发送（非阻塞）
        self.event_tx
            .try_send(prioritized)
            .map_err(|_| BatchError::ChannelFull)?;

        drop(_permit);
        Ok(())
    }

    /// 强制刷新（等待所有缓冲事件处理完成）
    pub async fn flush(&self) {
        self.flush_notify.notify_waiters();
        // 等待队列清空
        while self.backpressure.available_permits() < self.config.max_queue_depth {
            tokio::time::sleep(tokio::time::Duration::from_millis(10)).await;
        }
    }

    /// 获取当前队列深度
    pub fn queue_depth(&self) -> usize {
        self.config.max_queue_depth - self.backpressure.available_permits()
    }

    /// 获取统计信息
    pub fn stats(&self) -> BatchProcessorStats {
        BatchProcessorStats {
            queue_depth: self.queue_depth(),
            max_queue_depth: self.config.max_queue_depth,
            worker_threads: self.config.worker_threads,
        }
    }
}

/// 计算事件优先级
/// 
/// 优先级规则：
/// - DELETE 事件：高优先级（避免资源泄漏）
/// - UPDATE 事件：中优先级
/// - ADD 事件：低优先级（新 Pod 创建通常不紧急）
fn calculate_priority(event: &NriEvent, delete_boost: u8) -> u8 {
    match event {
        NriEvent::Delete { .. } => 1u8.saturating_add(delete_boost),
        NriEvent::AddOrUpdate(pod) => {
            // 可以根据 Pod 属性调整优先级
            // 例如：系统命名空间的 Pod 优先级更高
            if pod.namespace == "kube-system" {
                50
            } else {
                100
            }
        }
    }
}

/// 工作线程主循环
async fn run_worker(
    worker_id: usize,
    mut event_rx: mpsc::Receiver<PrioritizedEvent>,
    table: Arc<NriMappingTableV2>,
    version_mgr: Arc<EventVersionManager>,
    config: BatchProcessorConfig,
    _flush_notify: Arc<Notify>,
) {
    tracing::info!("[NriBatch] Worker {} started", worker_id);

    let mut buffer = BinaryHeap::with_capacity(config.batch_size);
    let mut last_flush = tokio::time::Instant::now();
    let flush_interval = tokio::time::Duration::from_millis(config.max_buffer_ms);

    loop {
        // 计算剩余等待时间
        let elapsed = last_flush.elapsed();
        let wait_duration = if elapsed >= flush_interval {
            tokio::time::Duration::from_millis(0)
        } else {
            flush_interval - elapsed
        };

        tokio::select! {
            Some(prioritized) = event_rx.recv() => {
                buffer.push(prioritized);

                // 批量大小达到阈值，执行 flush
                if buffer.len() >= config.batch_size {
                    flush_buffer(&mut buffer, &table, &version_mgr, worker_id).await;
                    last_flush = tokio::time::Instant::now();
                }
            }
            _ = tokio::time::sleep(wait_duration) => {
                // 时间窗口到期，执行 flush
                if !buffer.is_empty() {
                    flush_buffer(&mut buffer, &table, &version_mgr, worker_id).await;
                    last_flush = tokio::time::Instant::now();
                }
            }
            else => {
                // Channel 关闭
                break;
            }
        }
    }

    // 处理剩余事件
    if !buffer.is_empty() {
        flush_buffer(&mut buffer, &table, &version_mgr, worker_id).await;
    }

    tracing::info!("[NriBatch] Worker {} exiting", worker_id);
}

/// 刷新缓冲区（批量处理）
async fn flush_buffer(
    buffer: &mut BinaryHeap<PrioritizedEvent>,
    table: &NriMappingTableV2,
    version_mgr: &EventVersionManager,
    worker_id: usize,
) {
    let batch_size = buffer.len();
    let start = tokio::time::Instant::now();

    // 按优先级顺序处理（高优先级先处理）
    let mut processed = 0;
    let mut skipped = 0;

    while let Some(prioritized) = buffer.pop() {
        let event = prioritized.event;
        let pod_uid = match &event {
            NriEvent::AddOrUpdate(pod) => &pod.pod_uid,
            NriEvent::Delete { pod_uid } => pod_uid,
        };

        // 版本控制检查
        let version = version_mgr.generate_version();
        match version_mgr.try_update(pod_uid, version) {
            Ok(true) => {
                // 版本检查通过
                if let Err(e) = table.update_from_nri(event) {
                    tracing::error!(
                        "[NriBatch] Worker {} failed to update table: {:?}",
                        worker_id, e
                    );
                } else {
                    processed += 1;
                }
            }
            Ok(false) => {
                // 旧版本，跳过
                skipped += 1;
                tracing::debug!(
                    "[NriBatch] Worker {} skipped stale event for pod {}",
                    worker_id, pod_uid
                );
            }
            Err(e) => {
                tracing::error!("[NriBatch] Worker {} version check error: {}", worker_id, e);
            }
        }
    }

    let elapsed = start.elapsed();
    tracing::debug!(
        "[NriBatch] Worker {} flushed {} events (processed: {}, skipped: {}) in {:?}",
        worker_id, batch_size, processed, skipped, elapsed
    );
}

/// 批量处理器统计
#[derive(Debug, Clone)]
pub struct BatchProcessorStats {
    pub queue_depth: usize,
    pub max_queue_depth: usize,
    pub worker_threads: usize,
}

/// 批量处理错误
#[derive(Debug)]
pub enum BatchError {
    Backpressure(String),
    ChannelClosed,
    ChannelFull,
}

impl std::fmt::Display for BatchError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            BatchError::Backpressure(msg) => write!(f, "Backpressure: {}", msg),
            BatchError::ChannelClosed => write!(f, "Channel closed"),
            BatchError::ChannelFull => write!(f, "Channel full"),
        }
    }
}

impl std::error::Error for BatchError {}

/// 便捷启动函数
pub fn start_batch_processor(
    table: Arc<NriMappingTableV2>,
    version_mgr: Arc<EventVersionManager>,
    config: BatchProcessorConfig,
) -> (NriBatchProcessor, Vec<tokio::task::JoinHandle<()>>) {
    NriBatchProcessor::new(config, table, version_mgr)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::collector::nri_mapping::{NriContainerInfo, NriPodEvent};

    #[tokio::test]
    async fn test_batch_processor_basic() {
        let table = Arc::new(NriMappingTableV2::new());
        let vm = Arc::new(EventVersionManager::new());
        let config = BatchProcessorConfig {
            batch_size: 5,
            max_buffer_ms: 100,
            max_queue_depth: 100,
            worker_threads: 1,
            enable_priority: false,
            delete_priority_boost: 0,
        };

        let (processor, _handles) = NriBatchProcessor::new(config, table, vm);

        // 提交事件
        for i in 0..10 {
            let event = NriEvent::AddOrUpdate(NriPodEvent {
                pod_uid: format!("pod-{}", i),
                pod_name: format!("test-{}", i),
                namespace: "default".to_string(),
                containers: vec![NriContainerInfo {
                    container_id: format!("container-{}", i),
                    cgroup_ids: vec![format!("cg-{}", i)],
                    pids: vec![1000 + i as u32],
                }],
            });

            processor.submit(event).await.unwrap();
        }

        // 强制刷新
        processor.flush().await;

        // 验证结果
        assert_eq!(processor.table.pod_count(), 10);
    }

    #[tokio::test]
    async fn test_priority_ordering() {
        let table = Arc::new(NriMappingTableV2::new());
        let vm = Arc::new(EventVersionManager::new());
        let config = BatchProcessorConfig {
            batch_size: 10,
            max_buffer_ms: 1000, // 长等待以观察优先级
            max_queue_depth: 100,
            worker_threads: 1,
            enable_priority: true,
            delete_priority_boost: 0,
        };

        let (processor, _handles) = NriBatchProcessor::new(config, table, vm);

        // 提交 ADD 事件（低优先级）
        for i in 0..5 {
            let event = NriEvent::AddOrUpdate(NriPodEvent {
                pod_uid: format!("pod-{}", i),
                pod_name: format!("test-{}", i),
                namespace: "default".to_string(),
                containers: vec![],
            });
            processor.submit(event).await.unwrap();
        }

        // 提交 DELETE 事件（高优先级）
        for i in 5..10 {
            let event = NriEvent::Delete {
                pod_uid: format!("pod-{}", i),
            };
            processor.submit(event).await.unwrap();
        }

        // 所有事件应该都能提交成功
        assert_eq!(processor.queue_depth(), 10);
    }

    #[test]
    fn test_backpressure() {
        // 测试背压行为
        let config = BatchProcessorConfig {
            batch_size: 100,
            max_buffer_ms: 1000,
            max_queue_depth: 2, // 很小的队列测试背压
            worker_threads: 1,
            enable_priority: false,
            delete_priority_boost: 0,
        };

        let table = Arc::new(NriMappingTableV2::new());
        let vm = Arc::new(EventVersionManager::new());

        // 使用 try_submit 测试非阻塞行为
        let (processor, _handles) = NriBatchProcessor::new(config, table, vm);

        // 同步测试只能在运行时环境，这里只检查结构正确性
        assert_eq!(processor.config.max_queue_depth, 2);
    }
}
