//! LLM 客户端抽象层
//!
//! 支持多种 LLM 后端：OpenAI, Claude, 本地模型等

use async_trait::async_trait;
use serde::{Deserialize, Serialize};
use std::time::Duration;

/// LLM 提供商类型
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum LlmProvider {
    OpenAi,
    Anthropic,
    Local,
    Custom,
}

impl LlmProvider {
    pub fn as_str(&self) -> &'static str {
        match self {
            LlmProvider::OpenAi => "openai",
            LlmProvider::Anthropic => "anthropic",
            LlmProvider::Local => "local",
            LlmProvider::Custom => "custom",
        }
    }

    pub fn default_endpoint(&self) -> String {
        match self {
            LlmProvider::OpenAi => "https://api.openai.com/v1/chat/completions".to_string(),
            LlmProvider::Anthropic => "https://api.anthropic.com/v1/messages".to_string(),
            LlmProvider::Local => "http://localhost:11434/v1/chat/completions".to_string(), // OpenAI compatible endpoint
            LlmProvider::Custom => String::new(),
        }
    }

    pub fn default_model(&self) -> String {
        match self {
            LlmProvider::OpenAi => "gpt-4".to_string(),
            LlmProvider::Anthropic => "claude-3-sonnet-20240229".to_string(),
            LlmProvider::Local => "llama2".to_string(),
            LlmProvider::Custom => "default".to_string(),
        }
    }
}

/// LLM 配置
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LlmConfig {
    pub provider: LlmProvider,
    pub model: String,
    pub endpoint: String,
    pub api_key: Option<String>,
    #[serde(default = "default_timeout")]
    pub timeout_secs: u64,
    #[serde(default = "default_max_tokens")]
    pub max_tokens: i32,
    #[serde(default = "default_temperature")]
    pub temperature: f32,
}

fn default_timeout() -> u64 {
    60
}

fn default_max_tokens() -> i32 {
    2000
}

fn default_temperature() -> f32 {
    0.3
}

impl Default for LlmConfig {
    fn default() -> Self {
        let provider = LlmProvider::OpenAi;
        Self {
            model: provider.default_model(),
            endpoint: provider.default_endpoint(),
            api_key: None,
            timeout_secs: default_timeout(),
            max_tokens: default_max_tokens(),
            temperature: default_temperature(),
            provider,
        }
    }
}

impl LlmConfig {
    pub fn for_provider(provider: LlmProvider) -> Self {
        Self {
            provider,
            model: provider.default_model(),
            endpoint: provider.default_endpoint(),
            api_key: None,
            timeout_secs: default_timeout(),
            max_tokens: default_max_tokens(),
            temperature: default_temperature(),
        }
    }

    pub fn with_api_key(mut self, key: &str) -> Self {
        self.api_key = Some(key.to_string());
        self
    }

    pub fn with_model(mut self, model: &str) -> Self {
        self.model = model.to_string();
        self
    }
}

/// LLM 客户端接口
#[async_trait]
pub trait LlmClient: Send + Sync {
    /// 发送聊天完成请求
    async fn chat_completion(&self, request: ChatCompletionRequest) -> Result<ChatCompletionResponse, LlmError>;

    /// 健康检查
    async fn health_check(&self) -> Result<(), LlmError>;

    /// 获取配置
    fn config(&self) -> &LlmConfig;
}

/// 聊天完成请求
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ChatCompletionRequest {
    pub model: String,
    pub messages: Vec<Message>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub temperature: Option<f32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub max_tokens: Option<i32>,
}

/// 消息
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Message {
    pub role: String,
    pub content: String,
}

impl Message {
    pub fn system(content: &str) -> Self {
        Self {
            role: "system".to_string(),
            content: content.to_string(),
        }
    }

    pub fn user(content: &str) -> Self {
        Self {
            role: "user".to_string(),
            content: content.to_string(),
        }
    }

    pub fn assistant(content: &str) -> Self {
        Self {
            role: "assistant".to_string(),
            content: content.to_string(),
        }
    }
}

/// 聊天完成响应
#[derive(Debug, Clone, Deserialize)]
pub struct ChatCompletionResponse {
    pub id: String,
    pub model: String,
    pub content: String,
    pub usage: Option<TokenUsage>,
    pub finish_reason: Option<String>,
}

/// Token 使用情况
#[derive(Debug, Clone, Deserialize)]
pub struct TokenUsage {
    pub prompt_tokens: i32,
    pub completion_tokens: i32,
    pub total_tokens: i32,
}

/// LLM 错误
#[derive(Debug, Clone)]
pub struct LlmError {
    pub code: String,
    pub message: String,
    pub retryable: bool,
}

impl LlmError {
    pub fn new(code: &str, message: &str, retryable: bool) -> Self {
        Self {
            code: code.to_string(),
            message: message.to_string(),
            retryable,
        }
    }

    pub fn non_retryable(code: &str, message: &str) -> Self {
        Self::new(code, message, false)
    }

    pub fn retryable(code: &str, message: &str) -> Self {
        Self::new(code, message, true)
    }
}

impl std::fmt::Display for LlmError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "LlmError[{}]: {} (retryable: {})", self.code, self.message, self.retryable)
    }
}

impl std::error::Error for LlmError {}

/// OpenAI 客户端
pub struct OpenAiClient {
    config: LlmConfig,
    client: reqwest::Client,
}

impl OpenAiClient {
    pub fn new(config: LlmConfig) -> Result<Self, LlmError> {
        let client = reqwest::Client::builder()
            .timeout(Duration::from_secs(config.timeout_secs))
            .build()
            .map_err(|e| LlmError::non_retryable("CLIENT_BUILD_FAILED", &e.to_string()))?;

        Ok(Self { config, client })
    }

    /// 从 API key 快速创建
    pub fn with_api_key(api_key: &str) -> Self {
        let config = LlmConfig::for_provider(LlmProvider::OpenAi)
            .with_api_key(api_key);
        Self::new(config).expect("Failed to create OpenAI client")
    }
}

#[async_trait]
impl LlmClient for OpenAiClient {
    async fn chat_completion(&self, request: ChatCompletionRequest) -> Result<ChatCompletionResponse, LlmError> {
        if self.config.api_key.is_none() {
            return Err(LlmError::non_retryable("API_KEY_MISSING", "API key not configured"));
        }

        let api_key = self.config.api_key.as_ref().unwrap();

        let request_body = serde_json::json!({
            "model": request.model,
            "messages": request.messages,
            "temperature": request.temperature.unwrap_or(self.config.temperature),
            "max_tokens": request.max_tokens.unwrap_or(self.config.max_tokens),
        });

        let response = self.client
            .post(&self.config.endpoint)
            .header("Content-Type", "application/json")
            .header("Authorization", format!("Bearer {}", api_key))
            .json(&request_body)
            .send()
            .await
            .map_err(|e| {
                if e.is_timeout() {
                    LlmError::retryable("TIMEOUT", &e.to_string())
                } else {
                    LlmError::retryable("REQUEST_FAILED", &e.to_string())
                }
            })?;

        let status = response.status();
        if !status.is_success() {
            let error_text = response.text().await.unwrap_or_default();
            return Err(LlmError::non_retryable(
                &format!("HTTP_{}", status.as_u16()),
                &format!("API error: {}", error_text),
            ));
        }

        // 解析 OpenAI 格式响应
        let openai_response: OpenAiResponse = response.json().await.map_err(|e| {
            LlmError::non_retryable("PARSE_ERROR", &e.to_string())
        })?;

        let content = openai_response
            .choices
            .first()
            .and_then(|c| c.message.content.clone())
            .ok_or_else(|| LlmError::non_retryable("EMPTY_RESPONSE", "No content in response"))?;

        Ok(ChatCompletionResponse {
            id: openai_response.id,
            model: openai_response.model,
            content,
            usage: openai_response.usage.map(|u| TokenUsage {
                prompt_tokens: u.prompt_tokens,
                completion_tokens: u.completion_tokens,
                total_tokens: u.total_tokens,
            }),
            finish_reason: openai_response.choices.first().and_then(|c| c.finish_reason.clone()),
        })
    }

    async fn health_check(&self) -> Result<(), LlmError> {
        // 发送一个简单的请求检查服务可用性
        let request = ChatCompletionRequest {
            model: self.config.model.clone(),
            messages: vec![Message::system("Hi")],
            temperature: Some(0.0),
            max_tokens: Some(1),
        };

        match self.chat_completion(request).await {
            Ok(_) => Ok(()),
            Err(e) if e.code.starts_with("HTTP_4") => {
                // 4xx 错误通常是认证或参数问题，服务是可用的
                Ok(())
            }
            Err(e) => Err(e),
        }
    }

    fn config(&self) -> &LlmConfig {
        &self.config
    }
}

/// Ollama 客户端（使用 OpenAI 兼容格式）
pub struct OllamaClient {
    config: LlmConfig,
    client: reqwest::Client,
}

impl OllamaClient {
    pub fn new(config: LlmConfig) -> Result<Self, LlmError> {
        let client = reqwest::Client::builder()
            .timeout(Duration::from_secs(config.timeout_secs))
            .build()
            .map_err(|e| LlmError::non_retryable("CLIENT_BUILD_FAILED", &e.to_string()))?;

        Ok(Self { config, client })
    }

    /// 快速创建本地 Ollama 客户端
    pub fn local_default() -> Self {
        let config = LlmConfig::for_provider(LlmProvider::Local);
        Self::new(config).expect("Failed to create Ollama client")
    }
}

#[async_trait]
impl LlmClient for OllamaClient {
    async fn chat_completion(&self, request: ChatCompletionRequest) -> Result<ChatCompletionResponse, LlmError> {
        let request_body = serde_json::json!({
            "model": request.model,
            "messages": request.messages,
            "temperature": request.temperature.unwrap_or(self.config.temperature),
            "max_tokens": request.max_tokens.unwrap_or(self.config.max_tokens),
        });

        let mut request_builder = self.client
            .post(&self.config.endpoint)
            .header("Content-Type", "application/json")
            .json(&request_body);

        // Ollama 可选支持 API key（用于代理场景）
        if let Some(ref api_key) = self.config.api_key {
            request_builder = request_builder.header("Authorization", format!("Bearer {}", api_key));
        }

        let response = request_builder
            .send()
            .await
            .map_err(|e| {
                if e.is_timeout() {
                    LlmError::retryable("TIMEOUT", &e.to_string())
                } else {
                    LlmError::retryable("REQUEST_FAILED", &e.to_string())
                }
            })?;

        let status = response.status();
        if !status.is_success() {
            let error_text = response.text().await.unwrap_or_default();
            return Err(LlmError::non_retryable(
                &format!("HTTP_{}", status.as_u16()),
                &format!("Ollama API error: {}", error_text),
            ));
        }

        // Ollama OpenAI 兼容格式响应与 OpenAI 相同
        let openai_response: OpenAiResponse = response.json().await.map_err(|e| {
            LlmError::non_retryable("PARSE_ERROR", &e.to_string())
        })?;

        let content = openai_response
            .choices
            .first()
            .and_then(|c| c.message.content.clone())
            .ok_or_else(|| LlmError::non_retryable("EMPTY_RESPONSE", "No content in response"))?;

        Ok(ChatCompletionResponse {
            id: openai_response.id,
            model: openai_response.model,
            content,
            usage: openai_response.usage.map(|u| TokenUsage {
                prompt_tokens: u.prompt_tokens,
                completion_tokens: u.completion_tokens,
                total_tokens: u.total_tokens,
            }),
            finish_reason: openai_response.choices.first().and_then(|c| c.finish_reason.clone()),
        })
    }

    async fn health_check(&self) -> Result<(), LlmError> {
        // 发送一个简单的请求检查服务可用性
        let request = ChatCompletionRequest {
            model: self.config.model.clone(),
            messages: vec![Message::system("Hi")],
            temperature: Some(0.0),
            max_tokens: Some(1),
        };

        match self.chat_completion(request).await {
            Ok(_) => Ok(()),
            Err(e) if e.code.starts_with("HTTP_4") => {
                // 4xx 错误通常是模型不存在，服务是可用的
                Ok(())
            }
            Err(e) => Err(e),
        }
    }

    fn config(&self) -> &LlmConfig {
        &self.config
    }
}

/// Anthropic Claude 客户端
pub struct AnthropicClient {
    config: LlmConfig,
    client: reqwest::Client,
}

impl AnthropicClient {
    pub fn new(config: LlmConfig) -> Result<Self, LlmError> {
        let client = reqwest::Client::builder()
            .timeout(Duration::from_secs(config.timeout_secs))
            .build()
            .map_err(|e| LlmError::non_retryable("CLIENT_BUILD_FAILED", &e.to_string()))?;

        Ok(Self { config, client })
    }

    /// 快速创建 Claude 客户端
    pub fn claude(api_key: &str) -> Self {
        let config = LlmConfig::for_provider(LlmProvider::Anthropic)
            .with_api_key(api_key);
        Self::new(config).expect("Failed to create Claude client")
    }
}

#[async_trait]
impl LlmClient for AnthropicClient {
    async fn chat_completion(&self, request: ChatCompletionRequest) -> Result<ChatCompletionResponse, LlmError> {
        // Anthropic 使用 x-api-key 头而非 Bearer token
        let api_key = self.config.api_key.as_ref()
            .ok_or_else(|| LlmError::non_retryable("MISSING_API_KEY", "Anthropic requires API key"))?;

        // Anthropic 的消息格式与 OpenAI 类似，但 system 消息需要特殊处理
        let mut messages = Vec::new();
        let mut system_content: Option<String> = None;

        for msg in &request.messages {
            if msg.role == "system" {
                system_content = Some(msg.content.clone());
            } else {
                messages.push(serde_json::json!({
                    "role": msg.role,
                    "content": msg.content
                }));
            }
        }

        // Anthropic 请求体
        let mut request_body = serde_json::json!({
            "model": request.model,
            "messages": messages,
            "max_tokens": request.max_tokens.unwrap_or(self.config.max_tokens),
        });

        // 添加 system 字段（如果有）
        if let Some(sys) = system_content {
            request_body["system"] = serde_json::json!(sys);
        }

        // 可选参数
        if let Some(temp) = request.temperature {
            request_body["temperature"] = serde_json::json!(temp);
        }

        let response = self.client
            .post(&self.config.endpoint)
            .header("x-api-key", api_key)
            .header("anthropic-version", "2023-06-01")
            .header("Content-Type", "application/json")
            .json(&request_body)
            .send()
            .await
            .map_err(|e| {
                if e.is_timeout() {
                    LlmError::retryable("TIMEOUT", &e.to_string())
                } else {
                    LlmError::retryable("REQUEST_FAILED", &e.to_string())
                }
            })?;

        let status = response.status();
        if !status.is_success() {
            let error_text = response.text().await.unwrap_or_default();
            return Err(LlmError::non_retryable(
                &format!("HTTP_{}", status.as_u16()),
                &format!("Anthropic API error: {}", error_text),
            ));
        }

        // Anthropic 响应格式与 OpenAI 不同
        let anthropic_response: AnthropicResponse = response.json().await.map_err(|e| {
            LlmError::non_retryable("PARSE_ERROR", &e.to_string())
        })?;

        let content = anthropic_response
            .content
            .first()
            .map(|c| c.text.clone())
            .ok_or_else(|| LlmError::non_retryable("EMPTY_RESPONSE", "No content in response"))?;

        Ok(ChatCompletionResponse {
            id: anthropic_response.id,
            model: anthropic_response.model,
            content,
            usage: anthropic_response.usage.map(|u| TokenUsage {
                prompt_tokens: u.input_tokens,
                completion_tokens: u.output_tokens,
                total_tokens: u.input_tokens + u.output_tokens,
            }),
            finish_reason: anthropic_response.stop_reason,
        })
    }

    async fn health_check(&self) -> Result<(), LlmError> {
        let request = ChatCompletionRequest {
            model: self.config.model.clone(),
            messages: vec![Message::user("Hi")],
            temperature: Some(0.0),
            max_tokens: Some(1),
        };

        match self.chat_completion(request).await {
            Ok(_) => Ok(()),
            Err(e) if e.code.starts_with("HTTP_4") => {
                // 4xx 错误通常是认证或参数问题，服务是可用的
                Ok(())
            }
            Err(e) => Err(e),
        }
    }

    fn config(&self) -> &LlmConfig {
        &self.config
    }
}

/// Anthropic API 响应结构
#[derive(Debug, Deserialize)]
struct AnthropicResponse {
    id: String,
    model: String,
    content: Vec<AnthropicContent>,
    stop_reason: Option<String>,
    usage: Option<AnthropicUsage>,
}

#[derive(Debug, Deserialize)]
struct AnthropicContent {
    #[serde(rename = "type")]
    content_type: String,
    text: String,
}

#[derive(Debug, Deserialize)]
struct AnthropicUsage {
    input_tokens: i32,
    output_tokens: i32,
}

/// OpenAI API 响应结构（Ollama 兼容格式复用）
#[derive(Debug, Deserialize)]
struct OpenAiResponse {
    id: String,
    model: String,
    choices: Vec<OpenAiChoice>,
    usage: Option<OpenAiUsage>,
}

#[derive(Debug, Deserialize)]
struct OpenAiChoice {
    message: OpenAiMessage,
    finish_reason: Option<String>,
}

#[derive(Debug, Deserialize)]
struct OpenAiMessage {
    role: String,
    content: Option<String>,
}

#[derive(Debug, Deserialize)]
struct OpenAiUsage {
    prompt_tokens: i32,
    completion_tokens: i32,
    total_tokens: i32,
}

/// LLM 客户端工厂
pub struct LlmClientFactory;

impl LlmClientFactory {
    /// 创建 LLM 客户端
    pub fn create(config: LlmConfig) -> Result<Box<dyn LlmClient>, LlmError> {
        match config.provider {
            LlmProvider::OpenAi => {
                let client = OpenAiClient::new(config)?;
                Ok(Box::new(client))
            }
            LlmProvider::Anthropic => {
                let client = AnthropicClient::new(config)?;
                Ok(Box::new(client))
            }
            LlmProvider::Local => {
                let client = OllamaClient::new(config)?;
                Ok(Box::new(client))
            }
            LlmProvider::Custom => {
                // 对于自定义端点，使用 OpenAI 兼容格式
                let client = OpenAiClient::new(config)?;
                Ok(Box::new(client))
            }
        }
    }

    /// 快速创建 OpenAI 客户端
    pub fn openai(api_key: &str) -> Box<dyn LlmClient> {
        let config = LlmConfig::for_provider(LlmProvider::OpenAi)
            .with_api_key(api_key);
        Box::new(OpenAiClient::new(config).expect("Failed to create OpenAI client"))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_llm_config_defaults() {
        let config = LlmConfig::for_provider(LlmProvider::OpenAi);
        assert_eq!(config.provider, LlmProvider::OpenAi);
        assert!(!config.endpoint.is_empty());
    }

    #[test]
    fn test_message_builder() {
        let system_msg = Message::system("You are a helpful assistant");
        assert_eq!(system_msg.role, "system");

        let user_msg = Message::user("Hello");
        assert_eq!(user_msg.role, "user");
    }

    #[test]
    fn test_llm_error() {
        let error = LlmError::retryable("TIMEOUT", "Request timed out");
        assert!(error.retryable);
        assert_eq!(error.code, "TIMEOUT");
    }
}
