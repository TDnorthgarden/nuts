# 故障分析插件系统架构图

## 1. 系统整体架构

```mermaid
graph TB
    subgraph "用户层"
        CLI[CLI命令行工具]
        Sidecar[Sidecar进程<br/>第二阶段]
        AIAgent[AI Agent<br/>第三阶段]
    end

    subgraph "Service层 - Deployment"
        API[Gin HTTP API]
        PolicyEngine[策略引擎]
        AggregationEngine[聚合引擎]
        DiagnosticEngine[诊断引擎]
    end

    subgraph "Collector层 - DaemonSet"
        Collector[Collector采集器]
        ProcessCollector[进程采集器]
        FileCollector[文件采集器]
        NetworkCollector[网络采集器]
        IOCollector[IO采集器]
        PerfCollector[Perf采集器]
    end

    subgraph "数据存储层"
        PolicyDB[(策略数据库<br/>SQLite/MySQL/PostgreSQL)]
        EventDB[(事件数据库<br/>InfluxDB/ClickHouse)]
        AuditDB[(审计数据库<br/>SQLite/MySQL/PostgreSQL)]
        DiagnosisDB[(诊断数据库<br/>SQLite/MySQL/PostgreSQL)]
    end

    subgraph "基础设施层"
        Containerd[containerd]
        NRI[NRI v0.8.0]
        Kernel[Linux Kernel<br/>eBPF]
    end

    %% 用户层到Service层
    CLI -->|HTTP/gRPC<br/>推送策略| API
    Sidecar -->|HTTP/gRPC<br/>推送策略| API
    AIAgent -->|MCP协议<br/>推送策略| API

    %% Service层内部
    API --> PolicyEngine
    PolicyEngine --> AggregationEngine
    AggregationEngine --> DiagnosticEngine

    %% Service层到Collector层
    PolicyEngine -->|gRPC<br/>启动/停止采集| Collector
    Collector --> ProcessCollector
    Collector --> FileCollector
    Collector --> NetworkCollector
    Collector --> IOCollector
    Collector --> PerfCollector

    %% Service层到数据存储层
    PolicyEngine -->|CRUD| PolicyDB
    Collector -->|写入事件| EventDB
    AggregationEngine -->|读取事件| EventDB
    AggregationEngine -->|写入审计| AuditDB
    DiagnosticEngine -->|读取审计| AuditDB
    DiagnosticEngine -->|写入诊断| DiagnosisDB

    %% 基础设施层到Service层
    Containerd --> NRI
    NRI -->|NRI事件| PolicyEngine

    %% Collector层到基础设施层
    ProcessCollector --> Kernel
    FileCollector --> Kernel
    NetworkCollector --> Kernel
    IOCollector --> Kernel
    PerfCollector --> Kernel
```

## 2. 数据流向图

```mermaid
sequenceDiagram
    participant Containerd as containerd
    participant NRI as NRI Plugin
    participant PolicyEngine as 策略引擎
    participant Collector as Collector
    participant Aggregation as 聚合引擎
    participant Diagnostic as 诊断引擎
    participant DB as 数据库

    Note over Containerd,DB: 容器启动流程
    Containerd->>NRI: CreateContainer事件
    NRI->>PolicyEngine: 推送事件
    PolicyEngine->>PolicyEngine: 匹配策略
    alt 策略匹配成功
        PolicyEngine->>Collector: 启动采集(gRPC)
        PolicyEngine->>Aggregation: 创建聚合任务
        Collector->>DB: 写入事件数据
        Aggregation->>DB: 读取事件数据
        Aggregation->>Aggregation: 聚合处理
        Aggregation->>DB: 写入审计数据
        Aggregation->>Diagnostic: 通知诊断
        Diagnostic->>DB: 读取审计数据
        Diagnostic->>Diagnostic: 分析诊断
        Diagnostic->>DB: 写入诊断结果
    end

    Note over Containerd,DB: 容器停止流程
    Containerd->>NRI: StopContainer事件
    NRI->>PolicyEngine: 推送事件
    PolicyEngine->>Collector: 停止采集(gRPC)
    PolicyEngine->>Aggregation: 完成聚合任务
    Aggregation->>Aggregation: 生成最终审计
    Aggregation->>Diagnostic: 通知诊断
```

## 3. 策略引擎内部架构

```mermaid
graph TB
    subgraph "策略引擎 PolicyEngine"
        PolicyReceiver[PolicyReceiver<br/>策略接收器]
        PolicyManager[PolicyManager<br/>策略管理器]
        PolicyMatcher[PolicyMatcher<br/>策略匹配器]
        PolicyTaskManager[PolicyTaskManager<br/>任务管理器]
        PolicyNotifier[PolicyNotifier<br/>策略通知器]
    end

    subgraph "外部输入"
        CLI[CLI]
        Sidecar[Sidecar]
        AIAgent[AI Agent]
        DataSource[DataSource<br/>NRI事件]
    end

    subgraph "外部输出"
        Collector[Collector]
        AggregationEngine[AggregationEngine]
        PolicyDB[(PolicyDB)]
    end

    %% 外部输入到策略引擎
    CLI -->|HTTP/gRPC| PolicyReceiver
    Sidecar -->|HTTP/gRPC| PolicyReceiver
    AIAgent -->|MCP| PolicyReceiver
    DataSource -->|NRI事件| PolicyMatcher

    %% 策略引擎内部
    PolicyReceiver --> PolicyManager
    PolicyManager --> PolicyDB
    PolicyMatcher --> PolicyTaskManager
    PolicyTaskManager --> PolicyNotifier

    %% 策略引擎到外部输出
    PolicyNotifier -->|gRPC| Collector
    PolicyNotifier -->|内部RPC| AggregationEngine

```

## 4. 任务状态机

```mermaid
stateDiagram-v2
    [*] --> Idle: 策略创建

    Idle --> Pending: NRI事件匹配成功<br/>(Sync/Pod启动/容器启动)

    Pending --> Running: 启动成功

    Running --> Completed: 时长到期
    Running --> Stopped: 容器停止
    Running --> Failed: 启动失败/异常

    Pending --> Failed: 采集器启动失败
    Pending --> Failed: 聚合引擎启动失败

    Stopped --> Pending: 容器重启<br/>(可选)

    Completed --> [*]
    Stopped --> [*]
    Failed --> [*]

    note right of Idle
        空闲状态
        策略已创建，等待匹配
    end note

    note right of Pending
        等待状态
        匹配成功，等待启动
    end note

    note right of Running
        运行中
        采集中
    end note

    note right of Completed
        已完成
        采集时长到期
    end note

    note right of Stopped
        已停止
        pod/容器停止
    end note

    note right of Failed
        失败
        采集器或聚合引擎失败
    end note
```

## 5. 采集器架构

```mermaid
graph TB
    subgraph "Collector采集器"
        ScriptManager[ScriptManager<br/>脚本管理器]
        ProcessCollector[ProcessCollector<br/>进程采集器]
        FileCollector[FileCollector<br/>文件采集器]
        NetworkCollector[NetworkCollector<br/>网络采集器]
        IOCollector[IOCollector<br/>IO采集器]
        PerfCollector[PerfCollector<br/>Perf采集器]
        GRPCServer[gRPC Server]
    end

    subgraph "BPF脚本"
        ProcessScript[process.bt]
        FileScript[file.bt]
        NetworkScript[network.bt]
        IOScript[io.bt]
        PerfScript[perf.bt]
    end

    subgraph "Linux Kernel"
        eBPF[eBPF子系统]
    end

    subgraph "外部"
        PolicyEngine[PolicyEngine]
        EventDB[(EventDB)]
    end

    %% 外部到Collector
    PolicyEngine -->|gRPC| GRPCServer

    %% Collector内部
    GRPCServer --> ScriptManager
    ScriptManager --> ProcessCollector
    ScriptManager --> FileCollector
    ScriptManager --> NetworkCollector
    ScriptManager --> IOCollector
    ScriptManager --> PerfCollector

    %% 采集器到BPF脚本
    ProcessCollector --> ProcessScript
    FileCollector --> FileScript
    NetworkCollector --> NetworkScript
    IOCollector --> IOScript
    PerfCollector --> PerfScript

    %% BPF脚本到Kernel
    ProcessScript --> eBPF
    FileScript --> eBPF
    NetworkScript --> eBPF
    IOScript --> eBPF
    PerfScript --> eBPF

    %% 采集器到数据库
    ProcessCollector --> EventDB
    FileCollector --> EventDB
    NetworkCollector --> EventDB
    IOCollector --> EventDB
    PerfCollector --> EventDB

```

## 6. 聚合引擎架构

```mermaid
graph TB
    subgraph "聚合引擎 AggregationEngine"
        TaskScheduler[TaskScheduler<br/>任务调度器]
        EventAggregator[EventAggregator<br/>事件聚合器]
        AuditGenerator[AuditGenerator<br/>审计生成器]
        DiagnosticNotifier[DiagnosticNotifier<br/>诊断通知器]
    end

    subgraph "聚合算法"
        Simple[SimpleAggregation<br/>简单去重]
        TimeWindow[TimeWindowAggregation<br/>时间窗口]
        Statistical[StatisticalAggregation<br/>统计聚合]
        Frequency[FrequencyAggregation<br/>频率聚合]
        Custom[CustomAggregation<br/>自定义]
    end

    subgraph "外部"
        PolicyEngine[PolicyEngine]
        DiagnosticEngine[DiagnosticEngine]
        EventDB[(EventDB)]
        AuditDB[(AuditDB)]
    end

    %% 外部到聚合引擎
    PolicyEngine -->|创建任务| TaskScheduler

    %% 聚合引擎内部
    TaskScheduler --> EventAggregator
    EventAggregator --> Simple
    EventAggregator --> TimeWindow
    EventAggregator --> Statistical
    EventAggregator --> Frequency
    EventAggregator --> Custom
    EventAggregator --> AuditGenerator
    AuditGenerator --> DiagnosticNotifier

    %% 聚合引擎到外部
    EventAggregator -->|读取事件| EventDB
    AuditGenerator -->|写入审计| AuditDB
    DiagnosticNotifier -->|通知诊断| DiagnosticEngine

```

## 7. 诊断引擎架构

```mermaid
graph TB
    subgraph "诊断引擎 DiagnosticEngine"
        AuditAnalyzer[AuditAnalyzer<br/>审计分析器]
        BottleneckDetector[BottleneckDetector<br/>瓶颈检测器]
        ReportGenerator[ReportGenerator<br/>报告生成器]
    end

    subgraph "诊断策略"
        BuiltIn[BuiltInDiagnosticStrategy<br/>内置规则引擎]
        AI[AIDiagnosticStrategy<br/>AI诊断策略]
    end

    subgraph "外部"
        AggregationEngine[AggregationEngine]
        AuditDB[(AuditDB)]
        DiagnosisDB[(DiagnosisDB)]
        AIClient[AI Client<br/>OpenAI/本地模型]
    end

    %% 外部到诊断引擎
    AggregationEngine -->|通知诊断| AuditAnalyzer

    %% 诊断引擎内部
    AuditAnalyzer --> BottleneckDetector
    BottleneckDetector --> ReportGenerator

    %% 诊断策略
    AuditAnalyzer --> BuiltIn
    AuditAnalyzer --> AI
    AI --> AIClient

    %% 诊断引擎到外部
    AuditAnalyzer -->|读取审计| AuditDB
    ReportGenerator -->|写入诊断| DiagnosisDB

```

## 8. 部署架构

```mermaid
graph TB
    subgraph "Kubernetes集群"
        subgraph "Node 1"
            subgraph "Pod: nuts-service"
                Service1[Service<br/>Deployment]
            end
            subgraph "Pod: nuts-collector"
                Collector1[Collector<br/>DaemonSet<br/>特权容器]
            end
            Containerd1[containerd]
            NRI1[NRI Plugin]
        end

        subgraph "Node 2"
            subgraph "Pod: nuts-service"
                Service2[Service<br/>Deployment]
            end
            subgraph "Pod: nuts-collector"
                Collector2[Collector<br/>DaemonSet<br/>特权容器]
            end
            Containerd2[containerd]
            NRI2[NRI Plugin]
        end

        subgraph "Node N"
            subgraph "Pod: nuts-service"
                ServiceN[Service<br/>Deployment]
            end
            subgraph "Pod: nuts-collector"
                CollectorN[Collector<br/>DaemonSet<br/>特权容器]
            end
            ContainerdN[containerd]
            NRIN[NRI Plugin]
        end
    end

    subgraph "外部"
        CLI[CLI工具]
        Sidecar[Sidecar进程]
        AIAgent[AI Agent]
        DB[(数据库集群)]
    end

    %% 外部到集群
    CLI --> Service1
    CLI --> Service2
    CLI --> ServiceN
    Sidecar --> Service1
    Sidecar --> Service2
    Sidecar --> ServiceN
    AIAgent --> Service1
    AIAgent --> Service2
    AIAgent --> ServiceN

    %% Service到Collector
    Service1 -->|gRPC| Collector1
    Service2 -->|gRPC| Collector2
    ServiceN -->|gRPC| CollectorN

    %% Service到DB
    Service1 --> DB
    Service2 --> DB
    ServiceN --> DB

    %% Collector到DB
    Collector1 --> DB
    Collector2 --> DB
    CollectorN --> DB

    %% NRI到Service
    NRI1 --> Service1
    NRI2 --> Service2
    NRIN --> ServiceN

```

## 9. 接口关系图

```mermaid
graph LR
    subgraph "外部接口"
        HTTP[HTTP RESTful API]
        GRPC[gRPC API]
        MCP[MCP Protocol]
    end

    subgraph "内部接口"
        EventMatch[EventMatch<br/>事件匹配]
        StartCollection[StartCollection<br/>启动采集]
        StopCollection[StopCollection<br/>停止采集]
        CreateTask[CreateTask<br/>创建任务]
        NotifyDiagnostic[NotifyDiagnostic<br/>诊断通知]
    end

    subgraph "模块"
        PolicyEngine[PolicyEngine]
        Collector[Collector]
        AggregationEngine[AggregationEngine]
        DiagnosticEngine[DiagnosticEngine]
    end

    %% 外部接口到模块
    HTTP --> PolicyEngine
    GRPC --> PolicyEngine
    MCP --> PolicyEngine

    %% 内部接口
    EventMatch --> PolicyEngine
    PolicyEngine --> StartCollection
    PolicyEngine --> StopCollection
    PolicyEngine --> CreateTask
    AggregationEngine --> NotifyDiagnostic

    %% 内部接口到模块
    StartCollection --> Collector
    StopCollection --> Collector
    CreateTask --> AggregationEngine
    NotifyDiagnostic --> DiagnosticEngine

```

## 10. 数据库架构

```mermaid
graph TB
    subgraph "策略数据库 PolicyDB"
        SQLite1[SQLite]
        MySQL1[MySQL]
        PostgreSQL1[PostgreSQL]
        LevelDB1[LevelDB]
    end

    subgraph "事件数据库 EventDB"
        InfluxDB[InfluxDB]
        ClickHouse[ClickHouse]
        TimescaleDB[TimescaleDB]
        LevelDB2[LevelDB]
    end

    subgraph "审计数据库 AuditDB"
        SQLite2[SQLite]
        MySQL2[MySQL]
        PostgreSQL2[PostgreSQL]
    end

    subgraph "诊断数据库 DiagnosisDB"
        SQLite3[SQLite]
        MySQL3[MySQL]
        PostgreSQL3[PostgreSQL]
    end

    subgraph "模块"
        PolicyEngine[PolicyEngine]
        Collector[Collector]
        AggregationEngine[AggregationEngine]
        DiagnosticEngine[DiagnosticEngine]
    end

    %% 模块到数据库
    PolicyEngine --> PolicyDB
    Collector --> EventDB
    AggregationEngine --> EventDB
    AggregationEngine --> AuditDB
    DiagnosticEngine --> AuditDB
    DiagnosticEngine --> DiagnosisDB

```

## 11. 开发阶段演进图

```mermaid
graph TB
    subgraph "第一阶段: CLI策略推送 (2个月)"
        CLI1[CLI工具]
        Service1[Service基础框架]
        PolicyEngine1[策略引擎]
        PolicyDB1[(策略数据库)]
    end

    subgraph "第二阶段: Sidecar进程 (1.5个月)"
        NRI[NRI集成]
        DataSource[DataSource模块]
        Collector[Collector采集器]
        AggregationEngine[聚合引擎]
        EventDB[(事件数据库)]
        AuditDB[(审计数据库)]
    end

    subgraph "第三阶段: AI Agent集成 (2个月)"
        DiagnosticEngine[诊断引擎]
        AIAgent[AI Agent]
        DiagnosisDB[(诊断数据库)]
        MCP[MCP协议]
    end

    %% 阶段演进
    第一阶段 --> 第二阶段
    第二阶段 --> 第三阶段

```

## 12. NRI事件处理流程

```mermaid
flowchart TD
    Start([开始]) --> NRIEvent[接收NRI事件]
    NRIEvent --> FillCgroup[填充cgroup信息]
    FillCgroup --> MatchPolicy[匹配策略]

    MatchPolicy -->|匹配成功| CheckTask{检查现有任务}
    MatchPolicy -->|匹配失败| End([结束])

    CheckTask -->|无任务| CreateTask[创建新任务]
    CheckTask -->|有任务| UpdateTask[更新现有任务]

    CreateTask --> NotifyCollector[通知Collector启动]
    UpdateTask --> NotifyCollector

    NotifyCollector --> NotifyAggregation[通知Aggregation启动]
    NotifyAggregation --> UpdateState[更新状态为Running]
    UpdateState --> End

```

## 13. cgroup获取策略流程

```mermaid
flowchart TD
    Start([开始]) --> GetEvent[获取NRI事件]
    GetEvent --> CheckCgroup{检查cgroup字段}

    CheckCgroup -->|cgroup不为空| ReturnCgroup[返回cgroup]
    CheckCgroup -->|cgroup为空| CheckPID{检查PID}

    CheckPID -->|PID>0| ReadProc[读取/proc/PID/cgroup]
    CheckPID -->|PID<=0| Error[返回错误]

    ReadProc --> ParseCgroup[解析cgroup路径]
    ParseCgroup --> ReturnCgroup

    ReturnCgroup --> End([结束])
    Error --> End

```

## 14. 错误处理流程

```mermaid
flowchart TD
    Start([开始]) --> Error[发生错误]
    Error --> CheckType{错误类型}

    CheckType -->|NRI连接失败| RetryNRI[重试NRI连接<br/>最大3次]
    CheckType -->|策略存储失败| RetryStorage[重试存储<br/>最大3次]
    CheckType -->|BPF脚本加载失败| StopCollection[停止采集<br/>记录日志]
    CheckType -->|数据写入失败| BufferRetry[缓存并重试<br/>最大5次]
    CheckType -->|AI调用失败| FallbackRule[降级到规则引擎]

    RetryNRI --> CheckSuccess{重试成功?}
    RetryStorage --> CheckSuccess
    BufferRetry --> CheckSuccess

    CheckSuccess -->|是| Continue([继续执行])
    CheckSuccess -->|否| LogAlert[记录日志<br/>发送告警]

    StopCollection --> LogAlert
    FallbackRule --> Continue

    LogAlert --> End([结束])
    Continue --> End

```

## 图例说明

- **白色背景**：所有模块使用默认白色背景
- **黑色文字**：所有文字使用默认黑色
- **简洁风格**：移除所有颜色填充，采用简洁的黑白风格
