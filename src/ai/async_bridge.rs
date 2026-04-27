//! AI 异步桥接层 - 实现诊断结果的异步AI增强
//!
//! 核心设计：
//! 1. 主链路提交AI任务后立即返回，不阻塞诊断输出
//! 2. 后台Worker消费队列，调用AI服务
//! 3. AI结果回填存储，触发增量推送

use crate::ai::{AiAdapter, AiAdapterConfig, AiEnhancedDiagnosis, AiStatus};
use crate::types::diagnosis::{DiagnosisResult, AiInfo};
use crate::types::evidence::Evidence;
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::{mpsc, RwLock};
use tokio::time::Duration;
use tracing;

/// AI任务优先级
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub enum AiTaskPriority {
    Low = 0,
    Normal = 1,
    High = 2,
    Critical = 3,  // OOM等紧急事件
}

/// AI任务
#[derive(Debug, Clone)]
pub struct AiTask {
    pub task_id: String,
    pub diagnosis_snapshot: DiagnosisResult,
    pub evidences: Vec<Evidence>,
    pub submitted_at_ms: i64,
    pub priority: AiTaskPriority,
    pub retry_count: u32,
}

impl AiTask {
    pub fn new(
        task_id: String,
        diagnosis: DiagnosisResult,
        evidences: Vec<Evidence>,
        priority: AiTaskPriority,
    ) -> Self {
        Self {
            task_id,
            diagnosis_snapshot: diagnosis,
            evidences,
            submitted_at_ms: chrono::Utc::now().timestamp_millis(),
            priority,
            retry_count: 0,
        }
    }
}

/// AI任务队列（内存实现，可扩展为Redis）
pub struct AiTaskQueue {
    tx: mpsc::Sender<AiTask>,
    // 用于查询任务状态
    pending_tasks: Arc<RwLock<HashMap<String, AiTaskState>>>,
}

#[derive(Debug, Clone)]
pub enum AiTaskState {
    Pending { submitted_at_ms: i64 },
    Processing { started_at_ms: i64 },
    Completed { result: AiEnhancedDiagnosis },
    Failed { error: String, retry_count: u32 },
}

impl AiTaskQueue {
    pub fn new(buffer_size: usize) -> (Self, mpsc::Receiver<AiTask>) {
        let (tx, rx) = mpsc::channel(buffer_size);
        let pending_tasks = Arc::new(RwLock::new(HashMap::new()));
        
        (Self { tx, pending_tasks }, rx)
    }
    
    /// 提交AI任务（同步返回，不等待）
    pub async fn submit(&self, task: AiTask) -> Result<(), AiQueueError> {
        let task_id = task.task_id.clone();
        let submitted_at_ms = task.submitted_at_ms;
        
        // 先更新状态
        {
            let mut tasks = self.pending_tasks.write().await;
            tasks.insert(
                task_id.clone(),
                AiTaskState::Pending { submitted_at_ms },
            );
        }
        
        // 提交到队列（非阻塞）
        match self.tx.try_send(task) {
            Ok(_) => {
                tracing::info!("[AI Queue] Task {} submitted (queue len: ?)", task_id);
                Ok(())
            }
            Err(e) => {
                tracing::warn!("[AI Queue] Failed to submit task {}: {}", task_id, e);
                // 回滚状态
                let mut tasks = self.pending_tasks.write().await;
                tasks.insert(
                    task_id,
                    AiTaskState::Failed {
                        error: format!("Queue full: {}", e),
                        retry_count: 0,
                    },
                );
                Err(AiQueueError::QueueFull)
            }
        }
    }
    
    /// 查询任务状态
    pub async fn get_state(&self, task_id: &str) -> Option<AiTaskState> {
        let tasks = self.pending_tasks.read().await;
        tasks.get(task_id).cloned()
    }
    
    /// 更新任务状态（Worker内部使用）
    pub async fn update_state(&self, task_id: &str, state: AiTaskState) {
        let mut tasks = self.pending_tasks.write().await;
        tasks.insert(task_id.to_string(), state);
    }
    
    /// 清理已完成任务（定期执行）
    pub async fn cleanup_completed(&self, max_age_ms: i64) {
        let cutoff = chrono::Utc::now().timestamp_millis() - max_age_ms;
        let mut tasks = self.pending_tasks.write().await;
        tasks.retain(|_task_id, state| match state {
            AiTaskState::Completed { result } => {
                result.processing_ms > cutoff  // 保留较新的结果
            }
            _ => true,  // 保留未完成/失败的任务
        });
    }
    
    pub fn get_pending_tasks(&self) -> Arc<RwLock<HashMap<String, AiTaskState>>> {
        Arc::clone(&self.pending_tasks)
    }
}

#[derive(Debug)]
pub enum AiQueueError {
    QueueFull,
    TaskNotFound,
}

impl std::fmt::Display for AiQueueError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            AiQueueError::QueueFull => write!(f, "AI task queue is full"),
            AiQueueError::TaskNotFound => write!(f, "Task not found"),
        }
    }
}

impl std::error::Error for AiQueueError {}

/// AI结果存储（支持回填查询）
pub struct AiResultStore {
    results: Arc<RwLock<HashMap<String, AiEnhancedDiagnosis>>>,
}

impl AiResultStore {
    pub fn new() -> Self {
        Self {
            results: Arc::new(RwLock::new(HashMap::new())),
        }
    }
    
    /// 存储AI增强结果
    pub async fn store(&self, task_id: &str, result: AiEnhancedDiagnosis) {
        let mut results = self.results.write().await;
        results.insert(task_id.to_string(), result);
        tracing::info!("[AI Store] Stored enhanced result for task {}", task_id);
    }
    
    /// 获取增强后的诊断结果
    pub async fn get(&self, task_id: &str) -> Option<AiEnhancedDiagnosis> {
        let results = self.results.read().await;
        results.get(task_id).cloned()
    }
    
    /// 列出所有 AI 增强结果（用于 API 查询）
    pub async fn list_all(&self) -> Vec<(String, AiEnhancedDiagnosis)> {
        let results = self.results.read().await;
        results.iter()
            .map(|(k, v)| (k.clone(), v.clone()))
            .collect()
    }
    
    /// 获取内部结果存储的引用（用于诊断 API）
    pub fn get_results_ref(&self) -> Arc<RwLock<HashMap<String, AiEnhancedDiagnosis>>> {
        Arc::clone(&self.results)
    }
    
    /// 获取带AI信息的最新诊断（用于API查询）
    pub async fn get_enhanced_diagnosis(&self, original: &DiagnosisResult) -> DiagnosisResult {
        match self.get(&original.task_id).await {
            Some(enhanced) if enhanced.ai_status == crate::ai::AiStatus::Ok => {
                enhanced.enhanced
            }
            Some(enhanced) => {
                // AI失败但已处理，返回带失败标记的原始结果
                let mut diag = original.clone();
                diag.ai = Some(AiInfo {
                    enabled: true,
                    status: crate::types::diagnosis::AiStatus::Failed,
                    summary: Some("AI enhancement failed".to_string()),
                    version: Some("v1".to_string()),
                    submitted_at_ms: Some(enhanced.original.task_id.parse().unwrap_or(0)),
                    completed_at_ms: Some(chrono::Utc::now().timestamp_millis()),
                    processing_duration_ms: Some(enhanced.processing_ms),
                });
                diag
            }
            None => {
                // AI尚未完成或不可用
                original.clone()
            }
        }
    }
    
    /// 清理过期结果
    pub async fn cleanup(&self, max_age_ms: i64) {
        let _cutoff = chrono::Utc::now().timestamp_millis() - max_age_ms;
        let mut results = self.results.write().await;
        results.retain(|_task_id, result| {
            let age = chrono::Utc::now().timestamp_millis() - result.processing_ms;
            age < max_age_ms
        });
    }
}

impl Default for AiResultStore {
    fn default() -> Self {
        Self::new()
    }
}

/// AI Worker配置
#[derive(Debug, Clone)]
pub struct AiWorkerConfig {
    pub adapter_config: AiAdapterConfig,
    pub max_concurrent: usize,        // 最大并发AI调用数
    pub queue_timeout_ms: i64,        // 任务在队列中的最大等待时间
    pub retry_limit: u32,             // 单个任务最大重试次数
    pub poll_interval_ms: u64,        // 队列轮询间隔
    pub cleanup_interval_secs: u64,   // 清理任务间隔
}

impl Default for AiWorkerConfig {
    fn default() -> Self {
        Self {
            adapter_config: AiAdapterConfig::default(),
            max_concurrent: 3,              // 默认3个并发
            queue_timeout_ms: 300_000,      // 5分钟超时
            retry_limit: 3,
            poll_interval_ms: 100,        // 100ms轮询
            cleanup_interval_secs: 300,   // 5分钟清理一次
        }
    }
}

/// AI Worker - 后台任务处理器
/// AI 任务完成通知消息
#[derive(Debug, Clone)]
pub struct AiCompletionNotification {
    pub task_id: String,
    pub diagnosis_id: String,
    pub status: String,
    pub completed_at_ms: i64,
}

pub struct AiWorker {
    config: AiWorkerConfig,
    adapter: AiAdapter,
    queue_rx: mpsc::Receiver<AiTask>,
    result_store: Arc<AiResultStore>,
    queue_state: Arc<RwLock<HashMap<String, AiTaskState>>>,
    /// 增量 Publisher 通知发送器
    notification_tx: Option<mpsc::Sender<AiCompletionNotification>>,
}

impl AiWorker {
    pub fn new(
        config: AiWorkerConfig,
        queue_rx: mpsc::Receiver<AiTask>,
        result_store: Arc<AiResultStore>,
        queue_state: Arc<RwLock<HashMap<String, AiTaskState>>>,
        notification_tx: Option<mpsc::Sender<AiCompletionNotification>>,
    ) -> Self {
        let adapter = AiAdapter::new(config.adapter_config.clone());
        
        Self {
            config,
            adapter,
            queue_rx,
            result_store,
            queue_state,
            notification_tx,
        }
    }
    
    /// 发送增量通知给 Publisher
    async fn notify_completion(&self, task_id: &str, diagnosis_id: &str, status: &str) {
        if let Some(ref tx) = self.notification_tx {
            let notification = AiCompletionNotification {
                task_id: task_id.to_string(),
                diagnosis_id: diagnosis_id.to_string(),
                status: status.to_string(),
                completed_at_ms: chrono::Utc::now().timestamp_millis(),
            };
            
            if let Err(e) = tx.send(notification).await {
                tracing::warn!("[AI Worker] Failed to send completion notification: {}", e);
            } else {
                tracing::info!("[AI Worker] Notified publisher of completion for task {}", task_id);
            }
        }
    }
    
    /// 启动Worker（阻塞运行）
    pub async fn run(mut self) {
        tracing::info!("[AI Worker] Starting with max_concurrent={}", self.config.max_concurrent);
        
        let semaphore = Arc::new(tokio::sync::Semaphore::new(self.config.max_concurrent));
        let mut cleanup_interval = tokio::time::interval(Duration::from_secs(self.config.cleanup_interval_secs));
        
        loop {
            tokio::select! {
                // 接收新任务
                Some(task) = self.queue_rx.recv() => {
                    let permit = semaphore.clone().acquire_owned().await.unwrap();
                    let adapter = self.adapter.clone();
                    let store = Arc::clone(&self.result_store);
                    let queue_state = Arc::clone(&self.queue_state);
                    let retry_limit = self.config.retry_limit;
                    
                    let notification_tx = self.notification_tx.clone();
                    
                    tokio::spawn(async move {
                        let _permit = permit; // 持有到任务完成
                        Self::process_task(task, adapter, store, queue_state, retry_limit, notification_tx).await;
                    });
                }
                
                // 定期清理
                _ = cleanup_interval.tick() => {
                    self.result_store.cleanup(3600_000).await; // 保留1小时
                    tracing::debug!("[AI Worker] Cleanup completed");
                }
                
                else => {
                    tracing::info!("[AI Worker] Queue closed, shutting down");
                    break;
                }
            }
        }
    }
    
    /// 处理单个任务
    async fn process_task(
        task: AiTask,
        adapter: AiAdapter,
        store: Arc<AiResultStore>,
        queue_state: Arc<RwLock<HashMap<String, AiTaskState>>>,
        retry_limit: u32,
        notification_tx: Option<mpsc::Sender<AiCompletionNotification>>,
    ) {
        let task_id = task.task_id.clone();
        let started_at_ms = chrono::Utc::now().timestamp_millis();
        
        tracing::info!("[AI Worker] Processing task {} (priority={:?}, retry={})", 
            task_id, task.priority, task.retry_count);
        
        // 更新状态为处理中
        {
            let mut state = queue_state.write().await;
            state.insert(
                task_id.clone(),
                AiTaskState::Processing { started_at_ms },
            );
        }
        
        // 构建输入并调用AI
        let input = adapter.build_input(&task.diagnosis_snapshot, &task.evidences);
        
        match adapter.call_ai(&input).await {
            Ok(ai_output) => {
                // 成功：合并结果
                let enhanced = adapter.enhance_diagnosis(&task.diagnosis_snapshot, &ai_output);
                let completed_at_ms = chrono::Utc::now().timestamp_millis();
                let processing_ms = completed_at_ms - task.submitted_at_ms;
                
                let result = AiEnhancedDiagnosis {
                    original: task.diagnosis_snapshot.clone(),
                    ai_output: Some(ai_output),
                    enhanced,
                    ai_status: AiStatus::Ok,
                    processing_ms,
                    created_at: std::time::Instant::now(),
                };
                
                // 存储结果
                store.store(&task_id, result.clone()).await;
                
                // 更新状态
                {
                    let mut state = queue_state.write().await;
                    state.insert(task_id.clone(), AiTaskState::Completed { result });
                }
                
                tracing::info!("[AI Worker] Task {} completed in {}ms", task_id, processing_ms);
                
                // 触发增量 Publisher 通知
                if let Some(ref tx) = notification_tx {
                    let notification = AiCompletionNotification {
                        task_id: task_id.clone(),
                        diagnosis_id: task_id.clone(), // 使用 task_id 作为 diagnosis_id
                        status: "completed".to_string(),
                        completed_at_ms: chrono::Utc::now().timestamp_millis(),
                    };
                    if let Err(e) = tx.send(notification).await {
                        tracing::warn!("[AI Worker] Failed to send notification: {}", e);
                    }
                }
            }
            
            Err(e) => {
                tracing::warn!("[AI Worker] Task {} failed: {}", task_id, e);
                
                if task.retry_count < retry_limit {
                    // 重试（实际应重新入队，这里简化处理）
                    let mut state = queue_state.write().await;
                    state.insert(
                        task_id.clone(),
                        AiTaskState::Failed {
                            error: format!("{} (will retry)", e),
                            retry_count: task.retry_count + 1,
                        },
                    );
                } else {
                    // 最终失败
                    let processing_ms = chrono::Utc::now().timestamp_millis() - task.submitted_at_ms;
                    let fallback = adapter.apply_fallback(&task.diagnosis_snapshot);
                    
                    let result = AiEnhancedDiagnosis {
                        original: task.diagnosis_snapshot.clone(),
                        ai_output: None,
                        enhanced: fallback,
                        ai_status: AiStatus::Unavailable,
                        processing_ms,
                        created_at: std::time::Instant::now(),
                    };
                    
                    store.store(&task_id, result.clone()).await;
                    
                    let mut state = queue_state.write().await;
                    state.insert(
                        task_id.clone(),
                        AiTaskState::Failed {
                            error: format!("{} (final after {} retries)", e, task.retry_count),
                            retry_count: task.retry_count,
                        },
                    );
                    
                    tracing::error!("[AI Worker] Task {} failed after {} retries", task_id, retry_limit);
                }
            }
        }
    }
}

/// 便捷函数：启动AI异步系统
/// 
/// 返回：任务队列、结果存储、任务接收器、通知接收器
pub fn start_ai_system(
    _config: AiWorkerConfig,
) -> (AiTaskQueue, Arc<AiResultStore>, mpsc::Receiver<AiTask>, mpsc::Receiver<AiCompletionNotification>) {
    let (queue, rx) = AiTaskQueue::new(1000); // 队列容量1000
    let store = Arc::new(AiResultStore::new());
    let _queue_state = queue.get_pending_tasks();
    
    // 创建通知通道（容量100，避免通知丢失）
    let (_notif_tx, notif_rx) = mpsc::channel(100);
    
    // 将通知发送器附加到队列（供 Worker 使用）
    // 注意：实际 Worker 创建时需要通过 queue_state 获取
    // 这里仅创建通道，Worker 创建时传入发送端
    
    (queue, store, rx, notif_rx)
}

/// 启动AI Worker（带通知功能）
/// 
/// 此函数创建并启动一个AI Worker，支持向主线程发送完成通知
pub async fn run_ai_worker_with_notifications(
    config: AiWorkerConfig,
    queue_rx: mpsc::Receiver<AiTask>,
    result_store: Arc<AiResultStore>,
    notification_tx: Option<mpsc::Sender<AiCompletionNotification>>,
) {
    let queue_state = Arc::new(RwLock::new(HashMap::new()));
    let worker = AiWorker::new(
        config,
        queue_rx,
        result_store,
        queue_state,
        notification_tx,
    );
    worker.run().await;
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::types::diagnosis::{DiagnosisResult, DiagnosisStatus, TriggerInfo, Traceability};
    
    fn create_test_diagnosis() -> DiagnosisResult {
        DiagnosisResult {
            schema_version: "diagnosis.v0.2".to_string(),
            task_id: "test-task-001".to_string(),
            status: DiagnosisStatus::Done,
            runtime: None,
            trigger: TriggerInfo {
                trigger_type: "manual".to_string(),
                trigger_reason: "test".to_string(),
                trigger_time_ms: 1000,
                matched_condition: None,
                event_type: None,
            },
            evidence_refs: vec![],
            conclusions: vec![],
            recommendations: vec![],
            traceability: Traceability {
                references: vec![],
                engine_version: None,
            },
            ai: None,
        }
    }
    
    #[tokio::test]
    async fn test_task_queue_submit() {
        let (queue, _rx) = AiTaskQueue::new(10);
        let diagnosis = create_test_diagnosis();
        let task = AiTask::new(
            "test-001".to_string(),
            diagnosis,
            vec![],
            AiTaskPriority::Normal,
        );
        
        let result = queue.submit(task).await;
        assert!(result.is_ok());
        
        let state = queue.get_state("test-001").await;
        assert!(matches!(state, Some(AiTaskState::Pending { .. })));
    }
    
    #[tokio::test]
    async fn test_result_store() {
        let store = AiResultStore::new();
        let diagnosis = create_test_diagnosis();
        
        let enhanced = AiEnhancedDiagnosis {
            original: diagnosis.clone(),
            ai_output: None,
            enhanced: diagnosis.clone(),
            ai_status: AiStatus::Ok,
            processing_ms: 1000,
            created_at: std::time::Instant::now(),
        };
        
        store.store("test-001", enhanced.clone()).await;
        
        let retrieved = store.get("test-001").await;
        assert!(retrieved.is_some());
        assert_eq!(retrieved.unwrap().original.task_id, "test-task-001");
    }
}
