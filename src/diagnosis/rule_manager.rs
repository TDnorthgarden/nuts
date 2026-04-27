//! 动态规则管理模块 - 支持热更新
//!
//! 提供规则的动态加载、更新、删除功能
//! 支持通过 HTTP API 和 CLI 进行规则管理

use crate::config::ThresholdRuleDef;
use crate::diagnosis::engine::{RuleEngine, ThresholdRule, ThresholdOperator};
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::RwLock;
use serde::{Deserialize, Serialize};
use tracing::{info, warn, error};
use std::time::SystemTime;

/// 动态规则定义（用于序列化/反序列化）
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct DynamicRuleDef {
    /// 规则ID（唯一标识）
    pub rule_id: String,
    /// 规则名称
    pub name: String,
    /// 证据类型
    pub evidence_type: String,
    /// 指标名称
    pub metric_name: String,
    /// 阈值
    pub threshold: f64,
    /// 操作符 (> < >= <=)
    pub operator: String,
    /// 诊断结论标题
    pub conclusion_title: String,
    /// 严重程度 (1-10)
    pub severity: u8,
    /// 描述说明
    #[serde(default)]
    pub description: String,
    /// 是否启用
    #[serde(default = "default_true")]
    pub enabled: bool,
    /// 创建时间
    #[serde(skip)]
    pub created_at: Option<SystemTime>,
    /// 更新时间
    #[serde(skip)]
    pub updated_at: Option<SystemTime>,
}

fn default_true() -> bool {
    true
}

impl From<DynamicRuleDef> for ThresholdRule {
    fn from(def: DynamicRuleDef) -> Self {
        let operator = match def.operator.as_str() {
            ">" => ThresholdOperator::GreaterThan,
            "<" => ThresholdOperator::LessThan,
            ">=" => ThresholdOperator::GreaterThanOrEqual,
            "<=" => ThresholdOperator::LessThanOrEqual,
            _ => ThresholdOperator::GreaterThan,
        };

        ThresholdRule::new(
            &def.rule_id,
            &def.evidence_type,
            &def.metric_name,
            def.threshold,
            operator,
            &def.conclusion_title,
            def.severity,
        )
    }
}

impl From<&ThresholdRule> for DynamicRuleDef {
    fn from(rule: &ThresholdRule) -> Self {
        DynamicRuleDef {
            rule_id: rule.name.clone(),
            name: rule.name.clone(),
            evidence_type: rule.evidence_type.clone(),
            metric_name: rule.metric_name.clone(),
            threshold: rule.threshold,
            operator: match rule.operator {
                ThresholdOperator::GreaterThan => ">".to_string(),
                ThresholdOperator::LessThan => "<".to_string(),
                ThresholdOperator::GreaterThanOrEqual => ">=".to_string(),
                ThresholdOperator::LessThanOrEqual => "<=".to_string(),
            },
            conclusion_title: rule.conclusion_title.clone(),
            severity: rule.severity,
            description: String::new(),
            enabled: true,
            created_at: Some(SystemTime::now()),
            updated_at: Some(SystemTime::now()),
        }
    }
}

/// 规则管理器 - 支持热更新
pub struct RuleManager {
    /// 动态规则存储
    rules: Arc<RwLock<HashMap<String, DynamicRuleDef>>>,
    /// 规则引擎
    engine: Arc<RwLock<RuleEngine>>,
    /// 是否加载了默认规则
    default_rules_loaded: Arc<RwLock<bool>>,
    /// 持久化路径
    persist_path: Arc<RwLock<Option<String>>>,
}

impl RuleManager {
    /// 创建新的规则管理器
    pub fn new() -> Self {
        let engine = RuleEngine::new();
        Self {
            rules: Arc::new(RwLock::new(HashMap::new())),
            engine: Arc::new(RwLock::new(engine)),
            default_rules_loaded: Arc::new(RwLock::new(true)),
            persist_path: Arc::new(RwLock::new(None)),
        }
    }

    /// 创建空的规则管理器（不加载默认规则）
    pub fn new_empty() -> Self {
        let engine = RuleEngine::new_empty();
        Self {
            rules: Arc::new(RwLock::new(HashMap::new())),
            engine: Arc::new(RwLock::new(engine)),
            default_rules_loaded: Arc::new(RwLock::new(false)),
            persist_path: Arc::new(RwLock::new(None)),
        }
    }

    /// 设置持久化路径
    pub async fn set_persist_path(&self, path: String) {
        let mut persist = self.persist_path.write().await;
        *persist = Some(path);
    }

    /// 添加新规则
    pub async fn add_rule(&self, rule: DynamicRuleDef) -> Result<(), RuleManagerError> {
        let rule_id = rule.rule_id.clone();
        
        // 检查规则是否已存在
        {
            let rules = self.rules.read().await;
            if rules.contains_key(&rule_id) {
                return Err(RuleManagerError::RuleAlreadyExists(rule_id));
            }
        }

        // 检查是否启用（在move之前保存）
        let is_enabled = rule.enabled;
        
        // 准备带时间戳的规则
        let mut rule_with_time = rule;
        let now = SystemTime::now();
        rule_with_time.created_at = Some(now);
        rule_with_time.updated_at = Some(now);
        
        // 添加到规则存储
        {
            let mut rules = self.rules.write().await;
            rules.insert(rule_id.clone(), rule_with_time.clone());
        }

        // 如果启用，添加到引擎
        if is_enabled {
            let threshold_rule: ThresholdRule = rule_with_time.into();
            let mut engine = self.engine.write().await;
            engine.add_rule(Box::new(threshold_rule));
        }

        info!("Rule added: {}", rule_id);
        
        // 尝试持久化
        self.persist_rules().await;
        
        Ok(())
    }

    /// 更新规则
    pub async fn update_rule(
        &self,
        rule_id: &str,
        updates: RuleUpdates,
    ) -> Result<(), RuleManagerError> {
        // 获取并更新规则
        let updated_rule = {
            let mut rules = self.rules.write().await;
            let rule = rules.get_mut(rule_id)
                .ok_or_else(|| RuleManagerError::RuleNotFound(rule_id.to_string()))?;
            
            // 应用更新
            if let Some(name) = updates.name {
                rule.name = name;
            }
            if let Some(threshold) = updates.threshold {
                rule.threshold = threshold;
            }
            if let Some(operator) = updates.operator {
                rule.operator = operator;
            }
            if let Some(conclusion_title) = updates.conclusion_title {
                rule.conclusion_title = conclusion_title;
            }
            if let Some(severity) = updates.severity {
                rule.severity = severity;
            }
            if let Some(description) = updates.description {
                rule.description = description;
            }
            if let Some(enabled) = updates.enabled {
                rule.enabled = enabled;
            }
            
            rule.updated_at = Some(SystemTime::now());
            rule.clone()
        };

        info!("Rule updated: {}", rule_id);
        
        // 重新构建引擎（简化处理：重新加载所有启用的规则）
        self.rebuild_engine().await?;
        
        // 尝试持久化
        self.persist_rules().await;
        
        Ok(())
    }

    /// 删除规则
    pub async fn remove_rule(&self, rule_id: &str) -> Result<(), RuleManagerError> {
        // 从存储中删除
        {
            let mut rules = self.rules.write().await;
            if rules.remove(rule_id).is_none() {
                return Err(RuleManagerError::RuleNotFound(rule_id.to_string()));
            }
        }

        info!("Rule removed: {}", rule_id);
        
        // 重新构建引擎
        self.rebuild_engine().await?;
        
        // 尝试持久化
        self.persist_rules().await;
        
        Ok(())
    }

    /// 获取规则
    pub async fn get_rule(&self, rule_id: &str) -> Option<DynamicRuleDef> {
        let rules = self.rules.read().await;
        rules.get(rule_id).cloned()
    }

    /// 列出所有规则
    pub async fn list_rules(&self) -> Vec<DynamicRuleDef> {
        let rules = self.rules.read().await;
        rules.values().cloned().collect()
    }

    /// 按证据类型过滤规则
    pub async fn list_rules_by_type(&self, evidence_type: &str) -> Vec<DynamicRuleDef> {
        let rules = self.rules.read().await;
        rules.values()
            .filter(|r| r.evidence_type == evidence_type)
            .cloned()
            .collect()
    }

    /// 重新加载默认规则
    pub async fn reload_defaults(&self) -> Result<(), RuleManagerError> {
        // 清空当前规则
        {
            let mut rules = self.rules.write().await;
            rules.clear();
        }
        
        // 创建新的引擎并加载默认规则
        {
            let mut engine = self.engine.write().await;
            *engine = RuleEngine::new();
        }

        {
            let mut loaded = self.default_rules_loaded.write().await;
            *loaded = true;
        }

        info!("Default rules reloaded");
        
        // 尝试持久化
        self.persist_rules().await;
        
        Ok(())
    }

    /// 清空所有规则
    pub async fn clear_all(&self) -> Result<(), RuleManagerError> {
        {
            let mut rules = self.rules.write().await;
            rules.clear();
        }
        
        {
            let mut engine = self.engine.write().await;
            *engine = RuleEngine::new_empty();
        }

        {
            let mut loaded = self.default_rules_loaded.write().await;
            *loaded = false;
        }

        info!("All rules cleared");
        
        // 尝试持久化
        self.persist_rules().await;
        
        Ok(())
    }

    /// 导出规则到 YAML
    pub async fn export_yaml(&self) -> Result<String, RuleManagerError> {
        let rules = self.list_rules().await;
        let rules_def = RulesFileDef { rules };
        
        serde_yaml::to_string(&rules_def)
            .map_err(|e| RuleManagerError::SerializeError(e.to_string()))
    }

    /// 从 YAML 导入规则
    pub async fn import_yaml(&self, yaml: &str) -> Result<ImportResult, RuleManagerError> {
        let def: RulesFileDef = serde_yaml::from_str(yaml)
            .map_err(|e| RuleManagerError::DeserializeError(e.to_string()))?;
        
        let mut added = 0;
        let mut updated = 0;
        let mut errors = Vec::new();

        for rule in def.rules {
            let rule_id = rule.rule_id.clone();
            // 在move之前创建updates
            let updates = RuleUpdates::from_rule_def(&rule);
            
            match self.add_rule(rule).await {
                Ok(_) => added += 1,
                Err(RuleManagerError::RuleAlreadyExists(_)) => {
                    // 尝试更新
                    if let Err(e) = self.update_rule(&rule_id, updates).await {
                        errors.push(format!("{}: {:?}", rule_id, e));
                    } else {
                        updated += 1;
                    }
                }
                Err(e) => {
                    errors.push(format!("{}: {:?}", rule_id, e));
                }
            }
        }

        info!("Import complete: {} added, {} updated, {} errors", added, updated, errors.len());
        
        Ok(ImportResult {
            added,
            updated,
            errors,
        })
    }

    /// 获取规则引擎（用于诊断）
    pub fn get_engine(&self) -> Arc<RwLock<RuleEngine>> {
        self.engine.clone()
    }

    /// 获取规则数量
    pub async fn rule_count(&self) -> usize {
        let rules = self.rules.read().await;
        rules.len()
    }

    /// 获取状态报告
    pub async fn status_report(&self) -> RuleManagerStatus {
        let rules = self.list_rules().await;
        let enabled_count = rules.iter().filter(|r| r.enabled).count();
        let default_loaded = *self.default_rules_loaded.read().await;
        let persist_path = self.persist_path.read().await.clone();
        
        RuleManagerStatus {
            total_rules: rules.len(),
            enabled_rules: enabled_count,
            default_rules_loaded: default_loaded,
            persist_path,
        }
    }

    /// 持久化规则（内部方法）
    async fn persist_rules(&self) {
        let path_opt = self.persist_path.read().await.clone();
        
        if let Some(path) = path_opt {
            match self.export_yaml().await {
                Ok(yaml) => {
                    if let Err(e) = tokio::fs::write(&path, yaml).await {
                        warn!("Failed to persist rules to {}: {}", path, e);
                    } else {
                        info!("Rules persisted to {}", path);
                    }
                }
                Err(e) => {
                    warn!("Failed to export rules for persistence: {:?}", e);
                }
            }
        }
    }

    /// 重新构建引擎（内部方法）
    async fn rebuild_engine(&self) -> Result<(), RuleManagerError> {
        let new_engine = RuleEngine::new_empty();
        
        // 收集所有启用的规则
        let enabled_rules: Vec<DynamicRuleDef> = {
            let rules = self.rules.read().await;
            rules.values()
                .filter(|r| r.enabled)
                .cloned()
                .collect()
        };

        // 创建新引擎并添加规则
        let mut engine = self.engine.write().await;
        *engine = new_engine;
        
        for rule in enabled_rules {
            let threshold_rule: ThresholdRule = rule.into();
            engine.add_rule(Box::new(threshold_rule));
        }

        Ok(())
    }
}

impl Default for RuleManager {
    fn default() -> Self {
        Self::new()
    }
}

impl Clone for RuleManager {
    fn clone(&self) -> Self {
        Self {
            rules: Arc::clone(&self.rules),
            engine: Arc::clone(&self.engine),
            default_rules_loaded: Arc::clone(&self.default_rules_loaded),
            persist_path: Arc::clone(&self.persist_path),
        }
    }
}

/// 规则更新请求
#[derive(Debug, Clone, Default)]
pub struct RuleUpdates {
    pub name: Option<String>,
    pub threshold: Option<f64>,
    pub operator: Option<String>,
    pub conclusion_title: Option<String>,
    pub severity: Option<u8>,
    pub description: Option<String>,
    pub enabled: Option<bool>,
}

impl RuleUpdates {
    fn from_rule_def(rule: &DynamicRuleDef) -> Self {
        Self {
            name: Some(rule.name.clone()),
            threshold: Some(rule.threshold),
            operator: Some(rule.operator.clone()),
            conclusion_title: Some(rule.conclusion_title.clone()),
            severity: Some(rule.severity),
            description: Some(rule.description.clone()),
            enabled: Some(rule.enabled),
        }
    }
}

/// 规则文件定义（用于 YAML 序列化）
#[derive(Debug, Clone, Deserialize, Serialize)]
struct RulesFileDef {
    pub rules: Vec<DynamicRuleDef>,
}

/// 导入结果
#[derive(Debug, Clone)]
pub struct ImportResult {
    pub added: usize,
    pub updated: usize,
    pub errors: Vec<String>,
}

/// 规则管理器状态
#[derive(Debug, Clone, Serialize)]
pub struct RuleManagerStatus {
    pub total_rules: usize,
    pub enabled_rules: usize,
    pub default_rules_loaded: bool,
    pub persist_path: Option<String>,
}

/// 规则管理器错误
#[derive(Debug, Clone)]
pub enum RuleManagerError {
    RuleAlreadyExists(String),
    RuleNotFound(String),
    SerializeError(String),
    DeserializeError(String),
    PersistenceError(String),
}

impl std::fmt::Display for RuleManagerError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::RuleAlreadyExists(id) => write!(f, "Rule already exists: {}", id),
            Self::RuleNotFound(id) => write!(f, "Rule not found: {}", id),
            Self::SerializeError(msg) => write!(f, "Serialization error: {}", msg),
            Self::DeserializeError(msg) => write!(f, "Deserialization error: {}", msg),
            Self::PersistenceError(msg) => write!(f, "Persistence error: {}", msg),
        }
    }
}

impl std::error::Error for RuleManagerError {}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_add_and_get_rule() {
        let manager = RuleManager::new_empty();
        
        let rule = DynamicRuleDef {
            rule_id: "test-rule".to_string(),
            name: "Test Rule".to_string(),
            evidence_type: "network".to_string(),
            metric_name: "latency_ms".to_string(),
            threshold: 100.0,
            operator: ">".to_string(),
            conclusion_title: "High latency detected".to_string(),
            severity: 7,
            description: "Test description".to_string(),
            enabled: true,
            created_at: None,
            updated_at: None,
        };

        manager.add_rule(rule.clone()).await.unwrap();
        
        let retrieved = manager.get_rule("test-rule").await;
        assert!(retrieved.is_some());
        assert_eq!(retrieved.unwrap().rule_id, "test-rule");
    }

    #[tokio::test]
    async fn test_duplicate_rule_error() {
        let manager = RuleManager::new_empty();
        
        let rule = DynamicRuleDef {
            rule_id: "test-rule".to_string(),
            name: "Test Rule".to_string(),
            evidence_type: "network".to_string(),
            metric_name: "latency_ms".to_string(),
            threshold: 100.0,
            operator: ">".to_string(),
            conclusion_title: "High latency detected".to_string(),
            severity: 7,
            description: String::new(),
            enabled: true,
            created_at: None,
            updated_at: None,
        };

        manager.add_rule(rule.clone()).await.unwrap();
        let result = manager.add_rule(rule).await;
        
        assert!(matches!(result, Err(RuleManagerError::RuleAlreadyExists(_))));
    }

    #[tokio::test]
    async fn test_update_rule() {
        let manager = RuleManager::new_empty();
        
        let rule = DynamicRuleDef {
            rule_id: "test-rule".to_string(),
            name: "Test Rule".to_string(),
            evidence_type: "network".to_string(),
            metric_name: "latency_ms".to_string(),
            threshold: 100.0,
            operator: ">".to_string(),
            conclusion_title: "High latency detected".to_string(),
            severity: 7,
            description: String::new(),
            enabled: true,
            created_at: None,
            updated_at: None,
        };

        manager.add_rule(rule).await.unwrap();
        
        let updates = RuleUpdates {
            threshold: Some(200.0),
            ..Default::default()
        };

        manager.update_rule("test-rule", updates).await.unwrap();
        
        let updated = manager.get_rule("test-rule").await.unwrap();
        assert_eq!(updated.threshold, 200.0);
    }

    #[tokio::test]
    async fn test_remove_rule() {
        let manager = RuleManager::new_empty();
        
        let rule = DynamicRuleDef {
            rule_id: "test-rule".to_string(),
            name: "Test Rule".to_string(),
            evidence_type: "network".to_string(),
            metric_name: "latency_ms".to_string(),
            threshold: 100.0,
            operator: ">".to_string(),
            conclusion_title: "High latency detected".to_string(),
            severity: 7,
            description: String::new(),
            enabled: true,
            created_at: None,
            updated_at: None,
        };

        manager.add_rule(rule).await.unwrap();
        manager.remove_rule("test-rule").await.unwrap();
        
        assert!(manager.get_rule("test-rule").await.is_none());
    }

    #[tokio::test]
    async fn test_yaml_export_import() {
        let manager = RuleManager::new_empty();
        
        let rule = DynamicRuleDef {
            rule_id: "test-rule".to_string(),
            name: "Test Rule".to_string(),
            evidence_type: "network".to_string(),
            metric_name: "latency_ms".to_string(),
            threshold: 100.0,
            operator: ">".to_string(),
            conclusion_title: "High latency detected".to_string(),
            severity: 7,
            description: "Test".to_string(),
            enabled: true,
            created_at: None,
            updated_at: None,
        };

        manager.add_rule(rule).await.unwrap();
        
        let yaml = manager.export_yaml().await.unwrap();
        assert!(yaml.contains("test-rule"));
        
        // 测试导入
        let result = manager.import_yaml(&yaml).await.unwrap();
        assert_eq!(result.updated, 1); // 已存在则更新
    }
}
