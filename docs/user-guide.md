# NUTS 使用手册

## 目录

1. [架构说明](#架构说明)
2. [快速开始](#快速开始)
3. [安装指南](#安装指南)
4. [配置说明](#配置说明)
5. [服务端使用](#服务端使用)
6. [CLI 客户端使用](#cli-客户端使用)
7. [API 使用示例](#api-使用示例)

---

## 架构说明

NUTS 采用 **服务端-客户端** 架构：

| 组件 | 可执行文件 | 角色 | 说明 |
|------|-----------|------|------|
| **服务端** | `nuts` | Daemon | 后台运行的核心服务，处理事件采集、策略匹配、任务调度、工作流编排 |
| **客户端** | `nuts-cli` | CLI | 命令行管理工具，通过 HTTP API 与服务端通信 |

```
┌─────────────┐      HTTP API      ┌─────────────┐
│  nuts-cli   │  ◄──────────────►  │    nuts     │
│  (CLI客户端) │                    │   (服务端)   │
└─────────────┘                    └─────────────┘
                                          │
                   ┌──────────────────────┼──────────────────────┐
                   ▼                      ▼                      ▼
              ┌─────────┐           ┌─────────┐           ┌─────────┐
              │EventBus │           │  Task   │           │Workflow │
              │ 通信层   │           │Scheduler│           │  Engine │
              └─────────┘           └─────────┘           └─────────┘
```

---

## 快速开始

### 1. 启动服务端

```bash
# 使用默认配置启动
./nuts

# 或使用自定义配置
./nuts --config ./configs/nuts.toml
```

### 2. 使用客户端管理

在另一个终端窗口：

```bash
# 查看服务状态
./nuts-cli status

# 列出数据源
./nuts-cli datasource list

# 列出策略
./nuts-cli policy list
```

---

## 安装指南

### 二进制安装

```bash
# 下载最新版本
wget https://github.com/sig-cloudnative/nuts/releases/latest/download/nuts-linux-amd64.tar.gz

# 解压
tar -xzf nuts-linux-amd64.tar.gz

# 移动到系统路径
sudo mv nuts /usr/local/bin/
sudo mv nuts-cli /usr/local/bin/
```

### 源码编译

```bash
# 克隆仓库
git clone https://github.com/sig-cloudnative/nuts.git
cd nuts

# 编译服务端和客户端
make build

# 编译结果在 build/ 目录
# - build/nuts      (服务端)
# - build/nuts-cli  (客户端)
```

---

## 配置说明

### 配置文件位置

- 默认路径：`configs/nuts.toml`
- 自定义路径：通过 `--config` 参数指定

### 基本配置示例

```toml
[global]
log_level = "info"

[datasource.mock]
event_interval_ms = 5000
event_types = ["ContainerStart", "ContainerStop", "ContainerUpdate"]

[policy]
type = "cel"
rule_path = "configs/rules.yaml"

[scheduler]
type = "memory"
max_concurrent_tasks = 100
task_timeout_sec = 300

[eventbus]
type = "grpc"
```

### 完整配置参考

详见 [framework.md](./framework.md) 中的"完整配置示例"章节。

---

## 服务端使用

### 启动服务

```bash
# 使用默认配置启动
./nuts

# 使用自定义配置文件
./nuts --config ./configs/nuts.toml
```

### 服务端命令

服务端支持以下参数：

| 参数 | 说明 | 示例 |
|------|------|------|
| `--config` | 指定配置文件路径 | `--config ./configs/nuts.toml` |

### API 认证

所有 `/api/v1/*` 接口需要 Bearer Token 认证。

**方式一：自动生成（默认）**

启动时自动生成随机 token，输出到日志：

```bash
# 直接启动，token 会在日志中打印
./nuts
```

示例日志输出：

```
INFO ... API auth token (set NUTS_AUTH_TOKEN to customize) token=a1b2c3d4e5f6g7h8
```

**方式二：环境变量指定**

```bash
# 设置固定 token
NUTS_AUTH_TOKEN=my-secret-token ./nuts

# 或 export 到环境
export NUTS_AUTH_TOKEN=my-secret-token
./nuts
```

客户端调用：

```bash
# 使用 Authorization header
curl -H "Authorization: Bearer a1b2c3d4e5f6g7h8" http://localhost:8080/api/v1/status

# CLI 通过 --token 参数
nuts-cli --token a1b2c3d4e5f6g7h8 status

# TUI 通过 --token 参数
nuts-tui --token a1b2c3d4e5f6g7h8

# 或通过环境变量设置（CLI 和 TUI 均支持）
NUTS_AUTH_TOKEN=a1b2c3d4e5f6g7h8 nuts-cli status
```

---

## CLI 客户端使用

### 全局选项

```bash
# 指定服务端地址（默认 localhost:8080）
nuts-cli --server http://192.168.1.100:8080 status

# 指定 API 认证 token
nuts-cli --token a1b2c3d4e5f6g7h8 status

# 也可以通过 NUTS_AUTH_TOKEN 环境变量
NUTS_AUTH_TOKEN=a1b2c3d4e5f6g7h8 nuts-cli status
```

### 状态检查

```bash
# 查看服务端状态
nuts-cli status
```

### 数据源管理

```bash
# 列出所有数据源
nuts-cli datasource list

# 获取数据源详情
nuts-cli datasource status mock

# 切换当前数据源
nuts-cli datasource switch mock

# 禁用数据源
nuts-cli datasource disable mock
```

### 策略管理

```bash
# 列出策略
nuts-cli policy list

# 获取策略详情
nuts-cli policy get policy-001

# 添加策略（从JSON文件）
nuts-cli policy add ./my-policy.json

# 移除策略
nuts-cli policy remove policy-001

# 启用/禁用策略
nuts-cli policy enable policy-001
nuts-cli policy disable policy-001

# 评估策略（校验 JSON 文件的 DSL 语法，不保存到服务端）
nuts-cli policy evaluate ./my-policy.json
```

### 任务管理

```bash
# 列出所有任务
nuts-cli task list

# 获取任务详情
nuts-cli task get task-001

# 查看任务历史（包括已完成任务）
nuts-cli task history
```

---

## API 使用示例

### 基础信息

- Base URL: `http://localhost:8080/api/v1`
- Content-Type: `application/json`
- 认证：所有请求需在 Header 中携带 Bearer Token（参见 [API 认证](#api-认证)）
- 快捷环境变量：
  ```bash
  export TOKEN=my-secret-token
  # 后续示例使用 ${TOKEN} 代替
  ```

### 状态检查

```bash
curl -H "Authorization: Bearer ${TOKEN}" http://localhost:8080/api/v1/status
```

### 数据源 API

#### 列出数据源

```bash
curl -H "Authorization: Bearer ${TOKEN}" http://localhost:8080/api/v1/datasources
```

#### 获取数据源详情

```bash
curl -H "Authorization: Bearer ${TOKEN}" http://localhost:8080/api/v1/datasources/mock
```

#### 切换数据源

```bash
curl -X POST -H "Authorization: Bearer ${TOKEN}" http://localhost:8080/api/v1/datasources/mock/switch
```

#### 禁用数据源

```bash
curl -X POST -H "Authorization: Bearer ${TOKEN}" http://localhost:8080/api/v1/datasources/mock/disable
```

### 策略 API

#### 列出策略

```bash
curl -H "Authorization: Bearer ${TOKEN}" http://localhost:8080/api/v1/policies
```

#### 获取策略详情

```bash
curl -H "Authorization: Bearer ${TOKEN}" http://localhost:8080/api/v1/policies/policy-001
```

#### 创建策略

```bash
curl -X POST http://localhost:8080/api/v1/policies \
  -H "Authorization: Bearer ${TOKEN}" \
  -d '{
    "id": "policy-001",
    "description": "当容器启动时记录日志",
    "dsl": "event.type == '\''ContainerStart'\''",
    "dsl_engine": "cel",
    "enabled": true,
    "version": 1
  }'
```

#### 评估策略

```bash
curl -X POST http://localhost:8080/api/v1/policies/validate \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "policy-001",
    "description": "当容器启动时记录日志",
    "dsl": "event.type == '\''ContainerStart'\''",
    "dsl_engine": "cel",
    "enabled": true,
    "version": 1
  }'
```

#### 启用/禁用策略

```bash
curl -X POST -H "Authorization: Bearer ${TOKEN}" http://localhost:8080/api/v1/policies/policy-001/enable
curl -X POST -H "Authorization: Bearer ${TOKEN}" http://localhost:8080/api/v1/policies/policy-001/disable
```

#### 删除策略

```bash
curl -X DELETE -H "Authorization: Bearer ${TOKEN}" http://localhost:8080/api/v1/policies/policy-001
```

### 任务 API

#### 列出任务

```bash
curl -H "Authorization: Bearer ${TOKEN}" http://localhost:8080/api/v1/tasks
```

#### 获取任务详情

```bash
curl -H "Authorization: Bearer ${TOKEN}" http://localhost:8080/api/v1/tasks/task-001
```

#### 查看任务历史

```bash
curl -H "Authorization: Bearer ${TOKEN}" "http://localhost:8080/api/v1/tasks?include_completed=true"
```

**说明**：历史任务默认不会自动删除，所有任务（包括已完成、失败、取消的任务）都会保留在存储中，除非显式调用删除 API。

---

## 常见问题

### Q: 如何调试服务端？

查看服务端日志：

```bash
# 如果日志输出到文件
tail -f /var/log/nuts/nuts.log

# 如果日志输出到 stdout
# 直接查看终端输出
```

### Q: 如何连接远程服务端？

```bash
# 使用 --server 参数指定服务端地址
nuts-cli --server http://192.168.1.100:8080 status
```

### Q: 策略 DSL 语法错误怎么办？

使用 `evaluate` 命令校验策略语法：

```bash
nuts-cli policy evaluate ./my-policy.json
```

### Q: 如何查看所有可用命令？

```bash
nuts-cli --help
nuts-cli datasource --help
nuts-cli policy --help
nuts-cli task --help
```

---

## 开发指南

### 添加自定义数据源

实现 `DataSource` 接口并注册到工厂：

```go
type MyDataSource struct{}

func (d *MyDataSource) Start(ctx context.Context, eventCh chan<- *common.Event) error { ... }
func (d *MyDataSource) Stop() error { ... }
func (d *MyDataSource) Health() error { ... }
func (d *MyDataSource) GetStats() *DataSourceStats { ... }
func (d *MyDataSource) Ready() <-chan struct{} { ... }

// 注册
datasource.Factory.Register("mytype", parser, validator, creator)
```

### 添加自定义 DSL 引擎

实现 `DSLEngine` 接口并注册到工厂：

```go
type MyDSLEngine struct{}

func (e *MyDSLEngine) Match(event *common.Event) ([]*PolicyMatch, error) { ... }

// 注册
policy.Factory.Register("mydsl", parser, validator, creator)
```

---

## 更多文档

- [框架设计文档](./framework.md) - 完整的框架设计说明
- [开发计划](./development-plan.md) - 开发计划和进度
- [进度报告](./progress-report.md) - 当前开发进度
