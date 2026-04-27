//! 配置管理模块
//!
//! 支持从 YAML 配置文件读取服务配置

use crate::ai::AiAdapterConfig;
use crate::api::condition::{ConditionTriggerConfig, ThresholdRule, ComparisonOperator};
use crate::publisher::AlertPlatformConfig;
use serde::{Deserialize, Serialize};
use std::fs;
use std::path::Path;

/// 主配置结构
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct Config {
    /// 服务配置
    #[serde(default)]
    pub server: ServerConfig,
    /// AI 适配器配置
    #[serde(default)]
    pub ai: AiConfig,
    /// 告警平台配置
    #[serde(default)]
    pub alert: AlertConfig,
    /// 条件触发器配置列表
    #[serde(default)]
    pub condition_triggers: Vec<ConditionTriggerConfigDef>,
    /// 输出目录
    #[serde(default = "default_output_dir")]
    pub output_dir: String,
    /// 日志级别
    #[serde(default = "default_log_level")]
    pub log_level: String,
    /// 权限控制配置
    #[serde(default)]
    pub permission: PermissionConfig,
}

fn default_output_dir() -> String {
    "/tmp/nuts".to_string()
}

fn default_log_level() -> String {
    "info".to_string()
}

impl Default for Config {
    fn default() -> Self {
        Self {
            server: ServerConfig::default(),
            ai: AiConfig::default(),
            alert: AlertConfig::default(),
            condition_triggers: vec![],
            output_dir: default_output_dir(),
            log_level: default_log_level(),
            permission: PermissionConfig::default(),
        }
    }
}

/// 服务器配置
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct ServerConfig {
    /// 监听地址
    #[serde(default = "default_bind_address")]
    pub bind_address: String,
    /// 监听端口
    #[serde(default = "default_port")]
    pub port: u16,
}

fn default_bind_address() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    3000
}

impl Default for ServerConfig {
    fn default() -> Self {
        Self {
            bind_address: default_bind_address(),
            port: default_port(),
        }
    }
}

/// AI 配置
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct AiConfig {
    /// 是否启用 AI
    #[serde(default)]
    pub enabled: bool,
    /// AI 服务端点
    #[serde(default = "default_ai_endpoint")]
    pub endpoint: String,
    /// API 密钥
    pub api_key: Option<String>,
    /// 模型名称
    #[serde(default = "default_ai_model")]
    pub model: String,
    /// 请求超时（秒）
    #[serde(default = "default_ai_timeout")]
    pub timeout_secs: u64,
    /// 降级模式
    #[serde(default)]
    pub fallback_mode: String,
}

fn default_ai_endpoint() -> String {
    "http://localhost:8000/v1/chat/completions".to_string()
}

fn default_ai_model() -> String {
    "nuts-ai-diagnosis".to_string()
}

fn default_ai_timeout() -> u64 {
    60
}

impl Default for AiConfig {
    fn default() -> Self {
        Self {
            enabled: false,
            endpoint: default_ai_endpoint(),
            api_key: None,
            model: default_ai_model(),
            timeout_secs: default_ai_timeout(),
            fallback_mode: "keep_original".to_string(),
        }
    }
}

impl From<AiConfig> for AiAdapterConfig {
    fn from(config: AiConfig) -> Self {
        AiAdapterConfig {
            endpoint: config.endpoint,
            api_key: config.api_key,
            timeout_secs: config.timeout_secs,
            max_retries: 2,
            fallback_mode: match config.fallback_mode.as_str() {
                "reduce_confidence" => crate::ai::AiFallbackMode::ReduceConfidence,
                "mark_for_review" => crate::ai::AiFallbackMode::MarkForReview,
                _ => crate::ai::AiFallbackMode::KeepOriginal,
            },
            model: config.model,
        }
    }
}

/// 告警配置
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct AlertConfig {
    /// 是否启用告警推送
    #[serde(default)]
    pub enabled: bool,
    /// 告警平台端点
    #[serde(default = "default_alert_endpoint")]
    pub endpoint: String,
    /// API 密钥
    pub api_key: Option<String>,
    /// 请求超时（秒）
    #[serde(default = "default_alert_timeout")]
    pub timeout_secs: u64,
    /// 最大重试次数
    #[serde(default = "default_alert_retries")]
    pub max_retries: u32,
    /// 重试间隔（毫秒）
    #[serde(default = "default_alert_retry_interval")]
    pub retry_interval_ms: u64,
}

fn default_alert_endpoint() -> String {
    "http://localhost:8080/api/v1/alerts".to_string()
}

fn default_alert_timeout() -> u64 {
    30
}

fn default_alert_retries() -> u32 {
    3
}

fn default_alert_retry_interval() -> u64 {
    1000
}

/// 权限控制配置
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct PermissionConfig {
    /// 权限运行模式
    #[serde(default = "default_permission_mode")]
    pub mode: String,
    /// 特权代理路径
    pub privileged_proxy: Option<String>,
    /// 是否检查 capabilities
    #[serde(default = "default_check_capabilities")]
    pub check_capabilities: bool,
    /// 是否允许开发模式（sudo）
    #[serde(default = "default_allow_dev_mode")]
    pub allow_dev_mode: bool,
}

fn default_permission_mode() -> String {
    "auto".to_string()
}

fn default_check_capabilities() -> bool {
    true
}

fn default_allow_dev_mode() -> bool {
    cfg!(debug_assertions) // 仅在debug模式默认允许
}

impl Default for PermissionConfig {
    fn default() -> Self {
        Self {
            mode: default_permission_mode(),
            privileged_proxy: None,
            check_capabilities: default_check_capabilities(),
            allow_dev_mode: default_allow_dev_mode(),
        }
    }
}

impl Default for AlertConfig {
    fn default() -> Self {
        Self {
            enabled: false,
            endpoint: default_alert_endpoint(),
            api_key: None,
            timeout_secs: default_alert_timeout(),
            max_retries: default_alert_retries(),
            retry_interval_ms: default_alert_retry_interval(),
        }
    }
}

impl From<AlertConfig> for AlertPlatformConfig {
    fn from(config: AlertConfig) -> Self {
        AlertPlatformConfig {
            endpoint: config.endpoint,
            api_key: config.api_key,
            timeout_secs: config.timeout_secs,
            max_retries: config.max_retries,
            retry_interval_ms: config.retry_interval_ms,
        }
    }
}

/// 阈值规则定义（配置文件格式）
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct ThresholdRuleDef {
    /// 指标名称
    pub metric_name: String,
    /// 证据类型
    pub evidence_type: String,
    /// 操作符字符串
    pub operator: String,
    /// 阈值
    pub threshold: f64,
    /// 描述
    #[serde(default)]
    pub description: String,
}

impl From<ThresholdRuleDef> for ThresholdRule {
    fn from(def: ThresholdRuleDef) -> Self {
        ThresholdRule {
            metric_name: def.metric_name,
            evidence_type: def.evidence_type,
            operator: ComparisonOperator::from_str(&def.operator)
                .unwrap_or(ComparisonOperator::GreaterThan),
            threshold: def.threshold,
            description: def.description,
        }
    }
}

/// 条件触发器配置定义（配置文件格式）
#[derive(Debug, Clone, Deserialize, Serialize)]
pub struct ConditionTriggerConfigDef {
    /// 触发器 ID
    pub trigger_id: String,
    /// 条件名称
    pub name: String,
    /// 目标 Pod UID
    pub pod_uid: String,
    /// 目标 cgroup ID（可选）
    pub cgroup_id: Option<String>,
    /// 目标命名空间
    pub namespace: String,
    /// Pod 名称
    pub pod_name: String,
    /// 需要监控的证据类型
    pub evidence_types: Vec<String>,
    /// 阈值规则列表
    pub thresholds: Vec<ThresholdRuleDef>,
    /// 检查间隔（秒）
    #[serde(default = "default_check_interval")]
    pub check_interval_sec: u64,
    /// 采集时间窗长度（毫秒）
    #[serde(default = "default_collection_window")]
    pub collection_window_ms: i64,
    /// 幂等键前缀
    #[serde(default = "default_idempotency_prefix")]
    pub idempotency_prefix: String,
    /// 冷却期（毫秒）
    #[serde(default = "default_cooldown")]
    pub cooldown_ms: i64,
}

fn default_check_interval() -> u64 {
    30
}

fn default_collection_window() -> i64 {
    5000
}

fn default_idempotency_prefix() -> String {
    "condition".to_string()
}

fn default_cooldown() -> i64 {
    60000
}

impl From<ConditionTriggerConfigDef> for ConditionTriggerConfig {
    fn from(def: ConditionTriggerConfigDef) -> Self {
        ConditionTriggerConfig {
            trigger_id: def.trigger_id,
            name: def.name,
            pod_uid: def.pod_uid,
            cgroup_id: def.cgroup_id,
            namespace: def.namespace,
            pod_name: def.pod_name,
            evidence_types: def.evidence_types,
            thresholds: def.thresholds.into_iter().map(Into::into).collect(),
            check_interval_sec: def.check_interval_sec,
            collection_window_ms: def.collection_window_ms,
            idempotency_prefix: def.idempotency_prefix,
        }
    }
}

impl Config {
    /// 从 YAML 文件加载配置
    pub fn from_file<P: AsRef<Path>>(path: P) -> Result<Self, ConfigError> {
        let content = fs::read_to_string(path)?;
        let config: Config = serde_yaml::from_str(&content)?;
        Ok(config)
    }

    /// 从 YAML 字符串加载配置
    pub fn from_str(content: &str) -> Result<Self, ConfigError> {
        let config: Config = serde_yaml::from_str(content)?;
        Ok(config)
    }

    /// 保存配置到 YAML 文件
    pub fn save_to_file<P: AsRef<Path>>(&self, path: P) -> Result<(), ConfigError> {
        let yaml = serde_yaml::to_string(self)?;
        fs::write(path, yaml)?;
        Ok(())
    }

    /// 创建默认配置文件示例
    pub fn create_example_config() -> Self {
        Config {
            server: ServerConfig::default(),
            ai: AiConfig {
                enabled: true,
                endpoint: "http://localhost:8000/v1/chat/completions".to_string(),
                api_key: Some("your-api-key-here".to_string()),
                model: "nuts-ai-diagnosis".to_string(),
                timeout_secs: 60,
                fallback_mode: "keep_original".to_string(),
            },
            alert: AlertConfig {
                enabled: true,
                endpoint: "http://localhost:8080/api/v1/alerts".to_string(),
                api_key: Some("your-alert-api-key".to_string()),
                timeout_secs: 30,
                max_retries: 3,
                retry_interval_ms: 1000,
            },
            condition_triggers: vec![
                ConditionTriggerConfigDef {
                    trigger_id: "io-latency-trigger".to_string(),
                    name: "I/O 延迟异常检测".to_string(),
                    pod_uid: "example-pod-001".to_string(),
                    cgroup_id: Some("example-cgroup-001".to_string()),
                    namespace: "default".to_string(),
                    pod_name: "example-pod".to_string(),
                    evidence_types: vec!["block_io".to_string(), "syscall_latency".to_string()],
                    thresholds: vec![
                        ThresholdRuleDef {
                            metric_name: "io_latency_p99_ms".to_string(),
                            evidence_type: "block_io".to_string(),
                            operator: ">".to_string(),
                            threshold: 100.0,
                            description: "I/O 延迟 P99 超过 100ms".to_string(),
                        },
                    ],
                    check_interval_sec: 30,
                    collection_window_ms: 5000,
                    idempotency_prefix: "condition".to_string(),
                    cooldown_ms: 60000,
                },
            ],
            output_dir: "/tmp/nuts".to_string(),
            log_level: "info".to_string(),
            permission: PermissionConfig::default(),
        }
    }
}

/// 配置错误
#[derive(Debug)]
pub enum ConfigError {
    Io(std::io::Error),
    Yaml(serde_yaml::Error),
}

impl std::fmt::Display for ConfigError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            ConfigError::Io(e) => write!(f, "IO error: {}", e),
            ConfigError::Yaml(e) => write!(f, "YAML error: {}", e),
        }
    }
}

impl std::error::Error for ConfigError {}

impl From<std::io::Error> for ConfigError {
    fn from(e: std::io::Error) -> Self {
        ConfigError::Io(e)
    }
}

impl From<serde_yaml::Error> for ConfigError {
    fn from(e: serde_yaml::Error) -> Self {
        ConfigError::Yaml(e)
    }
}

impl Config {
    /// 从文件重新加载配置（用于 SIGHUP 热重载）
    pub fn reload(&mut self) -> Result<(), ConfigError> {
        // 尝试从多个路径加载配置文件
        let config_paths = vec![
            "nuts.yaml",
            "/etc/nuts/config.yaml",
            "config/nuts.yaml",
        ];

        for path in &config_paths {
            if std::path::Path::new(path).exists() {
                let content = std::fs::read_to_string(path)?;
                let new_config: Config = serde_yaml::from_str(&content)?;
                
                // 更新当前配置（保留一些运行时状态）
                *self = new_config;
                
                tracing::info!("Configuration reloaded from: {}", path);
                return Ok(());
            }
        }

        // 如果没有找到配置文件，检查环境变量
        if let Ok(config_path) = std::env::var("NUTS_CONFIG") {
            if std::path::Path::new(&config_path).exists() {
                let content = std::fs::read_to_string(&config_path)?;
                let new_config: Config = serde_yaml::from_str(&content)?;
                
                *self = new_config;
                
                tracing::info!("Configuration reloaded from NUTS_CONFIG: {}", config_path);
                return Ok(());
            }
        }

        Err(ConfigError::Io(std::io::Error::new(
            std::io::ErrorKind::NotFound,
            "No config file found for reload"
        )))
    }

    /// 获取可热重载的配置摘要
    pub fn reload_summary(&self) -> String {
        format!(
            "Server port: {}, AI enabled: {}, Alert enabled: {}, {} condition triggers",
            self.server.port,
            self.ai.enabled,
            self.alert.enabled,
            self.condition_triggers.len()
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_default_config() {
        let config = Config::default();
        assert_eq!(config.server.port, 3000);
        assert!(!config.ai.enabled);
        assert!(!config.alert.enabled);
    }

    #[test]
    fn test_yaml_roundtrip() {
        let config = Config::create_example_config();
        let yaml = serde_yaml::to_string(&config).unwrap();
        let parsed = Config::from_str(&yaml).unwrap();
        assert_eq!(parsed.server.port, config.server.port);
        assert_eq!(parsed.ai.enabled, config.ai.enabled);
    }

    #[test]
    fn test_threshold_rule_conversion() {
        let def = ThresholdRuleDef {
            metric_name: "io_latency_p99_ms".to_string(),
            evidence_type: "block_io".to_string(),
            operator: ">=".to_string(),
            threshold: 100.0,
            description: "Test".to_string(),
        };
        let rule: ThresholdRule = def.into();
        assert_eq!(rule.metric_name, "io_latency_p99_ms");
        assert!(matches!(rule.operator, ComparisonOperator::GreaterThanOrEqual));
    }
}
