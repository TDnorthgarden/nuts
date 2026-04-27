//! NRI V3 模块单元测试
//!
//! 测试覆盖:
//! - NriMappingTableV2 并发操作
//! - EventVersionManager CAS 版本控制
//! - NriBatchProcessor 批量处理
//! - NriV3 集成流程

#[cfg(test)]
mod tests {
    use crate::collector::nri_mapping_v2::{NriMappingTableV2, PodInfo, ContainerInfo};
    use crate::collector::nri_mapping::{NriMappingTable, PodInfo as NriPodInfo};
    use std::sync::Arc;
    use tokio::time::{sleep, Duration};

    /// 测试 NriMappingTableV2 基本 CRUD
    #[tokio::test]
    async fn test_mapping_table_v2_basic() {
        let table = Arc::new(NriMappingTableV2::new());

        // 测试插入 Pod
        let pod_uid = "test-pod-123".to_string();
        let pod_info = PodInfo {
            pod_uid: pod_uid.clone(),
            pod_name: "test-pod".to_string(),
            namespace: "default".to_string(),
            containers: vec![],
        };
        table.insert_pod(pod_info.clone());

        // 验证查询 - 使用 get_pod_details
        let found = table.get_pod_details(&pod_uid);
        assert!(found.is_some());
        let (pod, _) = found.unwrap();
        assert_eq!(pod.pod_name, "test-pod");

        // 测试更新
        let updated_pod = PodInfo {
            pod_uid: pod_uid.clone(),
            pod_name: "updated-pod".to_string(),
            namespace: "default".to_string(),
            containers: vec![ContainerInfo {
                container_id: "container-1".to_string(),
                name: "main".to_string(),
                image: "nginx".to_string(),
            }],
        };
        table.insert_pod(updated_pod);

        let found = table.get_pod_details(&pod_uid);
        assert_eq!(found.unwrap().0.containers.len(), 1);

        // 测试删除
        table.remove_pod(&pod_uid);
        assert!(table.get_pod(&pod_uid).is_none());
    }

    /// 测试并发写入性能
    #[tokio::test]
    async fn test_mapping_table_v2_concurrent() {
        let table = Arc::new(NriMappingTableV2::new());
        let mut handles = vec![];

        // 10 个并发任务，每个写入 100 个 Pod
        for i in 0..10 {
            let table_clone = Arc::clone(&table);
            let handle = tokio::spawn(async move {
                for j in 0..100 {
                    let pod_uid = format!("pod-{}-{}", i, j);
                    let pod_info = PodInfo {
                        pod_uid: pod_uid.clone(),
                        pod_name: format!("pod-{}", j),
                        namespace: "default".to_string(),
                        containers: vec![],
                    };
                    table_clone.insert_pod(pod_info);
                }
            });
            handles.push(handle);
        }

        // 等待所有任务完成
        for handle in handles {
            handle.await.unwrap();
        }

        // 验证总数
        let stats = table.stats();
        assert_eq!(stats.pod_count, 1000);
    }

    /// 测试 EventVersionManager CAS 操作
    #[tokio::test]
    async fn test_version_manager_cas() {
        let vm = Arc::new(EventVersionManager::new());

        // 获取初始版本
        let v1 = vm.generate_version();
        assert_eq!(v1, 1);

        // CAS 更新成功
        let result = vm.try_update(1, 2);
        assert!(result.is_ok());
        assert_eq!(vm.current_version(), 2);

        // CAS 更新失败（版本已变）
        let result = vm.try_update(1, 3);
        assert!(result.is_err());

        // 生成新版本
        let v3 = vm.generate_version();
        assert_eq!(v3, 3);
    }

    /// 测试并发版本生成
    #[tokio::test]
    async fn test_version_manager_concurrent() {
        let vm = Arc::new(EventVersionManager::new());
        let mut handles = vec![];

        // 100 个并发任务生成版本
        for _ in 0..100 {
            let vm_clone = Arc::clone(&vm);
            let handle = tokio::spawn(async move {
                vm_clone.generate_version()
            });
            handles.push(handle);
        }

        let mut versions = vec![];
        for handle in handles {
            versions.push(handle.await.unwrap());
        }

        // 验证版本唯一且递增
        versions.sort();
        for i in 0..versions.len() {
            assert_eq!(versions[i], (i + 1) as i64);
        }

        assert_eq!(vm.current_version(), 100);
    }

    /// 测试 NriBatchProcessor 基本功能
    #[tokio::test]
    async fn test_batch_processor_basic() {
        let table = Arc::new(NriMappingTableV2::new());
        let vm = Arc::new(EventVersionManager::new());
        
        let config = BatchProcessorConfig {
            worker_threads: 1,
            max_queue_depth: 100,
            batch_size: 10,
            max_buffer_ms: 1000,
            enable_priority: false,
            delete_priority_boost: 10,
        };

        let (processor, handles) = NriBatchProcessor::new(config, table, vm);
        let processor = Arc::new(processor);

        // 提交事件
        let event = nri_mapping::NriEvent::Add(nri_mapping::NriPodEvent {
            pod_uid: "test-pod".to_string(),
            pod_name: "test".to_string(),
            namespace: "default".to_string(),
            containers: vec![],
        });

        let result = processor.try_submit(event);
        assert!(result.is_ok());

        // 等待处理
        sleep(Duration::from_millis(100)).await;

        // 刷新并关闭
        processor.flush().await;
        drop(processor);

        for handle in handles {
            handle.abort();
        }
    }

    /// 测试批量事件处理
    #[tokio::test]
    async fn test_batch_processor_batch() {
        let table = Arc::new(NriMappingTableV2::new());
        let vm = Arc::new(EventVersionManager::new());
        
        let config = BatchProcessorConfig {
            worker_threads: 2,
            max_queue_depth: 1000,
            batch_size: 50,
            max_buffer_ms: 500,
            enable_priority: true,
            delete_priority_boost: 10,
        };

        let (processor, handles) = NriBatchProcessor::new(config, table.clone(), vm);
        let processor = Arc::new(processor);

        // 批量提交 200 个事件
        for i in 0..200 {
            let event = nri_mapping::NriEvent::Add(nri_mapping::NriPodEvent {
                pod_uid: format!("pod-{}", i),
                pod_name: format!("pod-{}", i),
                namespace: "default".to_string(),
                containers: vec![],
            });
            processor.try_submit(event).unwrap();
        }

        // 等待批处理完成
        sleep(Duration::from_millis(1000)).await;

        // 验证处理结果
        let stats = table.stats();
        assert!(stats.pod_count > 0);

        // 清理
        processor.flush().await;
        for handle in handles {
            handle.abort();
        }
    }

    /// 测试 NriV3 完整集成
    #[tokio::test]
    async fn test_nri_v3_integration() {
        let config = NriV3Config {
            enable_persist: false,
            persist_config: PersistConfig::default(),
            batch_config: BatchProcessorConfig {
                worker_threads: 2,
                max_queue_depth: 100,
                batch_size: 10,
                max_buffer_ms: 100,
                enable_priority: true,
                delete_priority_boost: 10,
            },
        };

        // 创建 NRI V3 实例
        let nri_v3 = create_nri_v3(config).await;
        assert!(nri_v3.is_ok());

        let (v3, _) = nri_v3.unwrap();

        // 提交 Pod 创建事件
        let event = nri_mapping::NriEvent::Add(nri_mapping::NriPodEvent {
            pod_uid: "integration-pod".to_string(),
            pod_name: "integration-test".to_string(),
            namespace: "default".to_string(),
            containers: vec![ContainerInfo {
                container_id: "container-1".to_string(),
                name: "main".to_string(),
                image: "nginx".to_string(),
            }],
        });

        let result = v3.submit_event(event).await;
        assert!(result.is_ok());

        // 验证事件被处理
        sleep(Duration::from_millis(200)).await;

        let metrics = v3.metrics();
        assert!(metrics.event_count > 0);

        // 关闭
        v3.shutdown().await;
    }

    /// 测试优先级事件处理
    #[tokio::test]
    async fn test_priority_events() {
        let table = Arc::new(NriMappingTableV2::new());
        let vm = Arc::new(EventVersionManager::new());
        
        let config = BatchProcessorConfig {
            worker_threads: 1,
            max_queue_depth: 100,
            batch_size: 5,
            max_buffer_ms: 1000,
            enable_priority: true,
            delete_priority_boost: 100, // 删除事件优先级更高
        };

        let (processor, handles) = NriBatchProcessor::new(config, table, vm);
        let processor = Arc::new(processor);

        // 先提交普通创建事件
        for i in 0..10 {
            let event = nri_mapping::NriEvent::Add(nri_mapping::NriPodEvent {
                pod_uid: format!("pod-{}", i),
                pod_name: format!("pod-{}", i),
                namespace: "default".to_string(),
                containers: vec![],
            });
            processor.try_submit(event).unwrap();
        }

        // 提交高优先级的删除事件
        let delete_event = nri_mapping::NriEvent::Delete(nri_mapping::NriPodEvent {
            pod_uid: "delete-pod".to_string(),
            pod_name: "delete".to_string(),
            namespace: "default".to_string(),
            containers: vec![],
        });
        processor.try_submit(delete_event).unwrap();

        // 优先级高的应该先被处理
        sleep(Duration::from_millis(100)).await;

        processor.flush().await;
        for handle in handles {
            handle.abort();
        }
    }
}
