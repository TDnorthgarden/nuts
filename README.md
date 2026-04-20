# Nuts - 故障分析插件系统

基于Go语言开发的容器性能监控和诊断系统，通过containerd的NRI机制获取容器生命周期事件，使用BPF技术采集进程、文件、网络、IO等事件数据，并通过策略引擎、聚合引擎和诊断引擎进行性能瓶颈分析。

## 项目概述

Nuts是一个用于Kubernetes环境的容器性能监控和故障诊断系统，具有以下特点：

- **基于NRI机制**：通过containerd NRI接口获取容器生命周期事件
- **BPF数据采集**：使用eBPF技术采集系统级性能数据
- **策略驱动**：支持灵活的策略配置，按需采集数据
- **DSL规则引擎**：内置DSL语言支持复杂规则匹配和条件判断
- **状态机管理**：基于状态机的任务生命周期管理
- **智能诊断**：支持基于规则和AI的故障诊断
- **多数据库支持**：支持SQLite、MySQL、PostgreSQL、InfluxDB、ClickHouse等多种数据库

## 系统架构

### 三大核心组件

1. **CLI工具** (`nuts-cli`) - 命令行工具
   - 功能：推送策略给service
   - 交互方式：HTTP/gRPC

2. **Service** (`nuts-service`) - 核心服务
   - 功能：NRI事件接收、策略引擎、聚合引擎、诊断引擎
   - 部署方式：Deployment（普通容器，可多副本）
   - 通过gRPC调用collector服务

3. **Collector** (`nuts-collector`) - 独立采集器
   - 功能：基于bpftrace的数据采集
   - 采集类型：进程、文件、网络、IO、perf
   - 部署方式：DaemonSet（特权容器，每个节点运行）
   - 通过gRPC提供服务接口

### 核心模块

- **DataSource**：NRI事件接收和cgroup信息填充
- **PolicyEngine**：策略匹配、任务管理、通知器
- **Collector**：多种采集器和脚本管理器
- **AggregationEngine**：事件聚合、多种聚合算法
- **DiagnosticEngine**：审计分析、瓶颈检测、报告生成

## 技术栈

- **开发语言**: Go（主要）、C/bpftrace（BPF部分）、Python（AI部分）
- **容器运行时**: containerd 1.6+
- **NRI版本**: containerd NRI v0.8.0
- **Web框架**: Gin（HTTP RESTful API）
- **RPC框架**: gRPC
- **BPF工具**: bpftrace、bcc
- **数据库**: SQLite、MySQL、PostgreSQL、InfluxDB、ClickHouse、LevelDB等
- **定时任务**: robfig/cron
- **AI框架**: OpenAI或本地大模型（第三阶段）

## 目录结构

```
nuts/
├── cmd/                          # 主程序入口
│   ├── cli/                      # CLI工具
│   ├── service/                  # Service主程序
│   └── collector/                # 独立Collector二进制
├── pkg/                          # 可复用库
│   ├── aggregation/              # 聚合引擎库
│   │   └── algorithm/          # 聚合算法接口
│   ├── collector/                # Collector接口定义
│   ├── datasource/               # 数据源库（NRI集成）
│   ├── diagnostic/               # 诊断引擎库
│   │   └── strategy/           # 诊断策略接口
│   ├── libdslgo/                 # DSL规则引擎
│   │   ├── docs/               # DSL文档
│   │   └── tests/              # DSL测试
│   ├── policy/                   # 策略接口定义
│   │   └── task/               # 任务接口定义
│   ├── policyengine/             # 策略引擎实现
│   ├── statemachine/             # 状态机实现
│   ├── storage/                  # 数据库抽象层
│   │   ├── audit/              # 审计存储
│   │   ├── diagnosis/          # 诊断存储
│   │   ├── event/              # 事件存储
│   │   └── policy/             # 策略存储
│   ├── task/                     # 任务实现
│   └── client/                   # Collector客户端
├── internal/                     # 内部实现
│   ├── api/                      # HTTP API处理器
│   └── service/                  # Service核心实现
├── scripts/                      # BPF脚本
│   ├── file.bt                   # 文件IO采集
│   ├── io.bt                     # IO采集
│   ├── network.bt                # 网络采集
│   ├── perf.bt                   # 性能采集
│   ├── process.bt                # 进程采集
│   └── crictl/                   # crictl测试脚本
├── configs/                      # 配置文件
│   ├── collector.yaml            # Collector配置
│   └── service.yaml              # Service配置
├── deployments/                  # 部署文件
│   ├── collector.yaml            # Collector部署
│   └── service.yaml              # Service部署
├── docs/                         # 文档
│   ├── arch.md                   # 架构设计
│   ├── event.md                  # 事件定义
│   ├── nri-cgroup.md             # NRI与cgroup分析
│   └── plan.md                   # 开发计划
├── example/                      # 示例文件
│   └── rules/                    # 策略规则示例
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## 快速开始

### 前置要求

- Go 1.19+
- containerd 1.6+
- Linux内核 4.10+（支持eBPF）
- root权限（运行BPF程序）

### 构建

```bash
# 构建所有二进制文件
make build

# 构建单个组件
make build-cli
make build-service
make build-collector
```

### 运行

```bash
# 运行Service
export NRI_PLUGIN_NAME="nuts-datasource"
export NRI_PLUGIN_IDX="01"
export NUTS_API_URL=http://localhost:8080
make run-service

# 运行Collector
make run-collector

# 运行CLI
make run-cli ARGS="policy list"
```

### 使用CLI

```bash
# 创建策略（从JSON和YAML文件）
./build/nuts-cli policy create --policy example/rules/test-policy.json --rule example/rules/test-rule-valid.yaml

# 更新策略
./build/nuts-cli policy update --id <policy-id> --policy example/rules/test-policy.json --rule example/rules/test-rule-valid.yaml

# 查询策略
./build/nuts-cli policy get <policy-id>

# 列出所有策略
./build/nuts-cli policy list

# 删除策略
./build/nuts-cli policy delete <policy-id>

# 查看版本
./build/nuts-cli version
```

### 使用HTTP API

Service提供RESTful API接口，默认监听端口8080：

```bash
# 健康检查
curl http://localhost:8080/health

# 创建策略
curl -X POST http://localhost:8080/api/v1/policies \
  -H "Content-Type: application/json" \
  -d '{
    "id": "my-policy",
    "name": "My Policy",
    "metrics": {
      "process": ["process.bt"],
      "network": ["network.bt"]
    },
    "duration": 300,
    "rule": "event.type == \"RunPodSandbox\""
  }'

# 获取策略
curl http://localhost:8080/api/v1/policies/<policy-id>

# 列出所有策略
curl http://localhost:8080/api/v1/policies

# 更新策略
curl -X PUT http://localhost:8080/api/v1/policies/<policy-id> \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Updated Policy",
    "duration": 600
  }'

# 删除策略
curl -X DELETE http://localhost:8080/api/v1/policies/<policy-id>

# 列出所有任务
curl http://localhost:8080/api/v1/tasks

# 获取任务详情
curl http://localhost:8080/api/v1/tasks/<task-id>

# 按状态列出任务
curl http://localhost:8080/api/v1/tasks/state?state=running

# 按策略列出任务
curl http://localhost:8080/api/v1/tasks/policy/<policy-id>

# 按cgroup列出任务
curl http://localhost:8080/api/v1/tasks/cgroup/<cgroup-id>
```

## 开发阶段

### 第一阶段：CLI策略推送（已完成）
- ✅ 项目初始化
- ✅ Service基础框架搭建
- ✅ 策略引擎实现
- ✅ CLI工具开发
- ✅ HTTP API实现
- ✅ DSL规则引擎集成
- ✅ 状态机任务管理
- ✅ NRI数据源集成

### 第二阶段：Sidecar进程（进行中）
- ✅ Collector基础框架
- ✅ gRPC服务框架
- 🔄 采集器实现
- 🔄 聚合引擎实现
- ⏳ 集成测试

### 第三阶段：AI Agent集成（计划中）
- ⏳ 诊断引擎基础
- ⏳ AI集成准备
- ⏳ AI模型集成
- ⏳ 测试和优化

## 配置

### Service配置

Service组件提供HTTP RESTful API服务，默认监听端口8080。当前版本支持：

- **策略管理**：创建、更新、删除、查询策略
- **任务管理**：查看任务状态、按策略/cgroup/状态筛选任务
- **NRI集成**：接收容器生命周期事件
- **策略引擎**：基于DSL规则匹配和任务调度
- **状态机管理**：管理任务生命周期状态

### Collector配置

Collector组件提供gRPC服务，默认监听端口50051。当前版本支持：

- **gRPC服务框架**：基础服务已搭建
- **BPF脚本管理**：支持多种采集脚本（进程、文件、网络、IO、perf）
- **采集器接口**：定义了采集器标准接口

### BPF脚本

项目包含以下BPF采集脚本：

- [`process.bt`](scripts/process.bt) - 进程事件采集
- [`file.bt`](scripts/file.bt) - 文件IO事件采集
- [`network.bt`](scripts/network.bt) - 网络事件采集
- [`io.bt`](scripts/io.bt) - IO事件采集
- [`perf.bt`](scripts/perf.bt) - 性能事件采集

## 部署

### Kubernetes部署

```bash
# 部署Service
kubectl apply -f deployments/service.yaml

# 部署Collector（DaemonSet）
kubectl apply -f deployments/collector.yaml
```

## 测试

```bash
# 运行所有测试
make test

# 运行测试并生成覆盖率报告
make test-coverage
```

## 代码质量

```bash
# 格式化代码
make fmt

# 运行go vet
make vet

# 运行linter
make lint
```

## 文档

详细文档请参考：

- [架构设计](docs/arch.md)
- [开发计划](docs/plan.md)
- [NRI与cgroup分析](docs/nri-cgroup.md)
- [事件定义](docs/event.md)
- [DSL规则编写指南](pkg/libdslgo/docs/rule-writing-guide.md)

## DSL规则引擎

Nuts内置了强大的DSL规则引擎，支持复杂的规则匹配和条件判断：

### 规则语法

规则使用YAML格式编写，支持以下特性：

- **事件匹配**：基于容器生命周期事件进行匹配
- **条件判断**：支持复杂的逻辑表达式
- **宏定义**：支持自定义宏简化规则编写
- **列表操作**：支持对列表进行过滤、映射等操作

### 示例规则

```yaml
rule: event.type == "RunPodSandbox" && pod.labels.app == "nginx"
desc: "匹配nginx应用的Pod启动事件"
condition: "pod.labels.app == 'nginx'"
output: "start_monitoring"
priority: "high"
```

更多DSL规则示例和语法说明，请参考[DSL规则编写指南](pkg/libdslgo/docs/rule-writing-guide.md)。

## 任务状态机

Nuts使用状态机管理任务生命周期，支持以下状态：

- **Pending**：任务已创建，等待执行
- **Running**：任务正在执行中
- **Completed**：任务成功完成
- **Failed**：任务执行失败

状态转换由策略引擎和通知器控制，确保任务按预期流程执行。

## 当前版本功能

### v0.2.0

**已实现功能：**

- ✅ CLI工具：策略的创建、更新、删除、查询
- ✅ HTTP API：RESTful API接口，支持策略和任务管理
- ✅ 策略引擎：基于DSL规则的策略匹配和任务调度
- ✅ 状态机：任务生命周期状态管理
- ✅ NRI数据源：接收容器生命周期事件
- ✅ Collector框架：gRPC服务框架和采集器接口定义
- ✅ BPF脚本：进程、文件、网络、IO、perf采集脚本

**开发中功能：**

- 🔄 Collector采集器实现
- 🔄 聚合引擎实现
- 🔄 Service与Collector的gRPC集成

**计划中功能：**

- ⏳ 诊断引擎
- ⏳ AI Agent集成
- ⏳ 数据库持久化
- ⏳ 审计日志
- ⏳ 诊断报告生成

## 贡献

欢迎贡献代码！请遵循以下步骤：

1. Fork本仓库
2. 创建特性分支 (`git checkout -b feature/AmazingFeature`)
3. 提交更改 (`git commit -m 'Add some AmazingFeature'`)
4. 推送到分支 (`git push origin feature/AmazingFeature`)
5. 开启Pull Request

## 许可证

本项目采用MIT许可证 - 详见LICENSE文件

## 联系方式

如有问题或建议，请提交Issue或Pull Request。
