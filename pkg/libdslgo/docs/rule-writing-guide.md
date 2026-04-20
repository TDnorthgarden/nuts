# DSL 规则编写指南 - 容器安全监控

本文档详细介绍如何使用 DSL 引擎编写容器安全监控规则，特别针对 Kubernetes 和 Containerd 环境。

## 目录

1. [概述](#概述)
2. [规则文件格式](#规则文件格式)
3. [Rule（规则）](#rule规则)
4. [Macro（宏）](#macro宏)
5. [List（列表）](#list列表)
6. [Condition 语法](#condition-语法)
7. [支持的操作符](#支持的操作符)
8. [容器字段详解](#容器字段详解)
9. [输出格式](#输出格式)
10. [规则优先级](#规则优先级)
11. [标签系统](#标签系统)
12. [安全检测场景示例](#安全检测场景示例)
13. [最佳实践](#最佳实践)

## 概述

DSL 引擎是一个专为容器化环境设计的规则引擎，支持基于 Containerd Pod Spec 的事件数据检测。规则文件使用 YAML 格式，包含三种核心元素：
- **Rule（规则）**：定义容器安全检测逻辑和告警输出
- **Macro（宏）**：可重用的容器条件表达式，简化规则编写
- **List（列表）**：预定义的容器值集合（如镜像白名单、系统容器列表）

### 容器安全监控场景

- 特权容器检测
- 非授权镜像使用
- Root 用户运行检测
- 资源限制合规检查
- 敏感目录挂载监控
- Kubernetes 命名空间策略

## 规则文件格式

规则文件使用 YAML 格式，每个元素以 `-` 开头。一个文件可以包含多个规则、宏和列表。

```yaml
# 规则文件示例
- rule: 规则名称
  desc: 规则描述
  condition: 条件表达式
  output: 输出格式
  priority: 优先级
  tags: [标签1, 标签2]
  enabled: true

- macro: 宏名称
  condition: 条件表达式

- list: 列表名称
  items: [值1, 值2, 值3]
```

## Rule（规则）

规则是检测的核心，定义了何时触发告警以及如何输出。

### 必需字段

- `rule`: 规则名称（字符串，1-256字符，仅ASCII字符）
- `condition`: 条件表达式（字符串）

### 可选字段

- `desc`: 规则描述（字符串）
- `output`: 输出格式字符串（字符串）
- `priority`: 优先级（字符串：EMERGENCY, ALERT, CRITICAL, ERROR, WARNING, NOTICE, INFO, DEBUG）
- `tags`: 标签数组（字符串数组）
- `enabled`: 是否启用（布尔值或字符串 "true"/"false"）

### 容器安全规则示例

```yaml
- rule: Detect Privileged Container
  desc: Detect container running with privileged access
  condition: >
    (container.privileged = true or
     container.host_network = true or
     container.host_pid = true) and
    not pod.namespace in (kube-system, kube-public)
  output: >
    Privileged container detected: %container.name
    (image=%container.image.repository, namespace=%pod.namespace,
     privileged=%container.privileged, host_network=%container.host_network)
  priority: WARNING
  tags: [container, security, privileged, kubernetes]
  enabled: true
```

### 规则名称验证

规则名称必须满足以下条件：
- 长度：1-256 字符
- 仅包含可打印的 ASCII 字符
- 不能为空

## Macro（宏）

宏是可重用的条件表达式，用于简化复杂的规则编写。宏可以在规则的条件中被引用。

### 字段

- `macro`: 宏名称（字符串）
- `condition`: 条件表达式（字符串）

### 容器宏示例

```yaml
- macro: privileged_container
  condition: >
    container.privileged = true or
    container.host_network = true or
    container.host_pid = true or
    container.host_ipc = true

- macro: production_pod
  condition: >
    pod.labels.environment = "production" or
    pod.namespace = "production"

- macro: container_without_resource_limits
  condition: >
    not linux.resources.memory.limit exists or
    not linux.resources.cpu.limit exists
```

### 宏引用

在规则中引用宏时，直接使用宏名称：

```yaml
- rule: Detect spawned process
  condition: spawned_process and proc.name = bash
```

## List（列表）

列表是预定义的值集合，用于条件匹配。列表可以在规则或宏的 `in` 操作符中使用。

### 字段

- `list`: 列表名称（字符串）
- `items`: 值数组（字符串数组，支持数字自动转换）

### 容器列表示例

```yaml
- list: system_containers
  items: [pause, etcd, kube-apiserver, kube-controller-manager, kube-scheduler]

- list: approved_images
  items: [
    nginx, alpine, ubuntu, debian,
    registry.k8s.io/pause,
    registry.k8s.io/kube-apiserver
  ]

- list: sensitive_namespaces
  items: [kube-system, kube-public, kube-node-lease]

- list: privileged_capabilities
  items: [CAP_SYS_ADMIN, CAP_SYS_PTRACE, CAP_SYS_MODULE]
```

### 列表引用

在条件中使用列表：

```yaml
- rule: System Container Whitelist
  condition: container.name in (system_containers)

- rule: Approved Image Check
  condition: not container.image.repository in (approved_images)
```

## Condition 语法

条件表达式使用类 SQL 的语法，支持多种操作符和函数。

### 基本语法

```
字段 操作符 值
```

### 复合条件

使用 `and`、`or`、`not` 组合多个条件：

```yaml
condition: >
  container.name exists and
  process.user.uid = 0 and
  container.image.repository contains "alpine"
```

### 条件优先级

使用括号控制优先级：

```yaml
condition: >
  (container.image.repository = nginx or container.image.repository = apache) and
  pod.namespace = production
```

### 多行条件

使用 `>` 符号支持多行条件：

```yaml
condition: >
  (container.privileged = true or
   container.host_network = true) and
  not pod.namespace in (kube-system, kube-public) and
  process.user.uid = 0
```

## 支持的操作符

### 比较操作符

| 操作符 | 说明 | 容器字段示例 |
|--------|------|--------------|
| `=` | 等于 | `container.privileged = true` |
| `!=` | 不等于 | `pod.namespace != kube-system` |
| `>` | 大于 | `container.restart_count > 3` |
| `>=` | 大于等于 | `linux.resources.memory.limit >= 1073741824` |
| `<` | 小于 | `process.user.uid < 1000` |
| `<=` | 小于等于 | `task.exit_status <= 0` |

### 字符串操作符

| 操作符 | 说明 | 容器字段示例 |
|--------|------|--------------|
| `contains` | 包含子串 | `container.image.repository contains nginx` |
| `startswith` | 以...开头 | `pod.name startswith nginx-deployment` |
| `endswith` | 以...结尾 | `container.image.tag endswith alpine` |
| `=~` | 正则匹配 | `pod.name =~ "^.*-deployment-[0-9]+$"` |
| `pmatch` | 路径匹配 | `linux.mounts.destination pmatch (/etc, /root)` |
| `glob` | 通配符匹配 | `container.image.repository glob "registry.k8s.io/*"` |

### 集合操作符

| 操作符 | 说明 | 容器字段示例 |
|--------|------|--------------|
| `in` | 在列表中 | `container.name in (system_containers)` |
| `exists` | 字段存在 | `linux.seccomp_profile exists` |

### 逻辑操作符

| 操作符 | 说明 | 容器字段示例 |
|--------|------|--------------|
| `and` | 逻辑与 | `container.privileged = true and pod.namespace = production` |
| `or` | 逻辑或 | `container.host_network = true or container.host_pid = true` |
| `not` | 逻辑非 | `not container.image.repository in (approved_images)` |

### 正则表达式

使用 `=~` 操作符进行正则匹配：

```yaml
- rule: Deployment pod detection
  condition: pod.name =~ "^.*-deployment-[0-9a-z]+-[0-9a-z]+$"

- rule: Web server container (case insensitive)
  condition: container.image.repository =~ "(?i)^(nginx|apache|httpd)"

- rule: Production namespace
  condition: pod.name =~ "^prod-"
```

支持正则修饰符：
- `(?i)` - 不区分大小写
- `(?m)` - 多行模式
- `(?s)` - 单行模式

## 容器字段详解

### 点号表示法

使用点号访问嵌套容器字段：

```yaml
container.name                    # 容器名称
container.image.repository        # 镜像仓库
container.image.tag               # 镜像标签
pod.labels.app                    # Pod 标签
linux.resources.memory.limit      # 资源限制
```

### 数组索引

使用方括号访问数组元素：

```yaml
process.args[0]                   # 进程第一个参数
linux.mounts[0].destination     # 第一个挂载点目标
process.capabilities.add[0]      # 第一个添加的能力
```

### 字段数据类型

容器字段支持以下数据类型：
- 字符串：`container.name = "nginx-app"`
- 数字：`container.restart_count = 3`
- 布尔值：`container.privileged = true`
- 数组：`process.args`

### Container 核心字段

| 字段 | 类型 | 说明 | 示例值 |
|------|------|------|--------|
| `container.name` | string | 容器名称 | `nginx-app` |
| `container.id` | string | 容器 ID | `abc123def456` |
| `container.image` | string | 完整镜像名称 | `nginx:1.21-alpine` |
| `container.image.repository` | string | 镜像仓库 | `nginx` |
| `container.image.tag` | string | 镜像标签 | `1.21-alpine` |
| `container.state` | string | 容器状态 | `running`, `stopped` |
| `container.privileged` | bool | 是否特权容器 | `true`, `false` |
| `container.host_network` | bool | 使用主机网络 | `true`, `false` |
| `container.host_pid` | bool | 使用主机 PID | `true`, `false` |
| `container.host_ipc` | bool | 使用主机 IPC | `true`, `false` |
| `container.restart_count` | int | 重启次数 | `0`, `3` |
| `container.pid` | int | 容器进程 ID | `1234` |
| `container.ip` | string | 容器 IP 地址 | `10.244.1.5` |
| `container.runtime` | string | 容器运行时 | `io.containerd.runc.v2` |
| `container.health_status` | string | 健康状态 | `healthy`, `unhealthy` |

### Pod 核心字段

| 字段 | 类型 | 说明 | 示例值 |
|------|------|------|--------|
| `pod.name` | string | Pod 名称 | `nginx-deployment-7c4b8f5d9-x2v4p` |
| `pod.namespace` | string | 命名空间 | `production` |
| `pod.uid` | string | Pod UID | `abc123-...` |
| `pod.labels.<key>` | string | Pod 标签值 | `production`, `nginx` |
| `pod.annotations.<key>` | string | Pod 注解值 | `value` |

### Process 核心字段

| 字段 | 类型 | 说明 | 示例值 |
|------|------|------|--------|
| `process.name` | string | 进程名称 | `nginx`, `bash` |
| `process.pid` | int | 进程 ID | `1234` |
| `process.args` | array | 进程参数 | `["nginx", "-g", "daemon off"]` |
| `process.user.uid` | int | 用户 ID | `0`, `1000` |
| `process.user.gid` | int | 组 ID | `0`, `1000` |
| `process.user.username` | string | 用户名 | `root`, `app` |
| `process.cwd` | string | 工作目录 | `/app` |
| `process.env` | array | 环境变量 | `["PATH=/usr/bin"]` |
| `process.capabilities.add` | array | 添加的能力 | `["CAP_NET_BIND_SERVICE"]` |
| `process.capabilities.drop` | array | 移除的能力 | `["CAP_SYS_ADMIN"]` |
| `process.no_new_privileges` | bool | 禁止新权限 | `true` |
| `process.apparmor_profile` | string | AppArmor 配置 | `docker-default` |
| `process.selinux_label` | string | SELinux 标签 | `system_u:...` |
| `process.terminal` | bool | 使用终端 | `true`, `false` |

### Linux / Security 核心字段

| 字段 | 类型 | 说明 | 示例值 |
|------|------|------|--------|
| `linux.resources.memory.limit` | int | 内存限制（字节） | `1073741824` |
| `linux.resources.memory.reservation` | int | 内存预留 | `536870912` |
| `linux.resources.cpu.limit` | int | CPU 限制 | `100000` |
| `linux.resources.cpu.shares` | int | CPU 份额 | `1024` |
| `linux.mounts.destination` | string | 挂载目标 | `/var/lib/docker` |
| `linux.mounts.type` | string | 挂载类型 | `bind`, `tmpfs` |
| `linux.mounts.source` | string | 挂载源 | `/host/data` |
| `linux.seccomp_profile` | string | Seccomp 配置 | `runtime/default` |
| `linux.masked_paths` | array | 屏蔽路径 | `["/proc/kcore"]` |
| `linux.readonly_paths` | array | 只读路径 | `["/proc/sys"]` |
| `linux.cgroup_path` | string | Cgroup 路径 | `/kubepods/...` |

### Task 核心字段

| 字段 | 类型 | 说明 | 示例值 |
|------|------|------|--------|
| `task.id` | string | 任务 ID | `task-abc123` |
| `task.pid` | int | 任务 PID | `1234` |
| `task.state` | string | 任务状态 | `running`, `stopped` |
| `task.exit_status` | int | 退出状态码 | `0`, `1` |
| `task.start_time` | string | 开始时间 | RFC3339 格式 |
| `task.end_time` | string | 结束时间 | RFC3339 格式 |

## 输出格式

输出格式字符串使用 `%字段名` 语法引用容器事件字段。

### 基本容器输出

```yaml
output: Privileged container detected (name=%container.name)
```

### 完整容器信息输出

```yaml
output: >
  Privileged container detected
  (container=%container.name
   image=%container.image.repository:%container.image.tag
   namespace=%pod.namespace
   pod=%pod.name
   privileged=%container.privileged
   host_network=%container.host_network
   user=%process.user.username uid=%process.user.uid)
```

### Kubernetes 上下文输出

```yaml
output: >
  Container %container.name in pod %pod.name
  (namespace=%pod.namespace, node=%pod.spec.node_name)
  running image %container.image.repository with UID %process.user.uid
```

### 多行输出

使用 `>` 符号支持多行输出：

```yaml
output: >
  Container security alert:
  - Container: %container.name
  - Pod: %pod.name
  - Namespace: %pod.namespace
  - Image: %container.image.repository:%container.image.tag
  - User: %process.user.username (UID: %process.user.uid)
  - Privileged: %container.privileged
  - Host Network: %container.host_network
```

## 规则优先级

优先级用于标识告警的严重程度，从高到低：

| 优先级 | 说明 |
|--------|------|
| EMERGENCY | 紧急情况，系统不可用 |
| ALERT | 需要立即采取行动 |
| CRITICAL | 严重问题 |
| ERROR | 错误情况 |
| WARNING | 警告情况 |
| NOTICE | 正常但重要的事件 |
| INFO | 信息性消息 |
| DEBUG | 调试信息 |

### 容器安全优先级示例

```yaml
# 紧急 - 特权容器 + Root + 生产环境
- rule: Critical Privileged Root Container
  condition: >
    container.privileged = true and
    process.user.uid = 0 and
    pod.namespace = "production"
  priority: EMERGENCY

# 严重 - 特权容器
- rule: Privileged Container
  condition: container.privileged = true
  priority: CRITICAL

# 警告 - 缺少资源限制
- rule: Container Without Memory Limits
  condition: not linux.resources.memory.limit exists
  priority: WARNING

# 通知 - 容器重启
- rule: Container Restart Detected
  condition: container.restart_count > 0
  priority: NOTICE

# 信息 - 新容器启动
- rule: New Container Started
  condition: container.state = "running"
  priority: INFO
```

## 标签系统

标签用于对容器安全规则进行分类和过滤。

### 容器安全标签分类

常见的标签分类：
- **工作负载类型**: `container`, `pod`, `kubernetes`, `host`
- **安全领域**: `security`, `compliance`, `privileged`, `root`
- **资源管理**: `resources`, `limits`, `quotas`
- **网络安全**: `network`, `host_network`, `ingress`, `egress`
- **存储安全**: `mount`, `volume`, `persistent_volume`
- **MITRE ATT&CK**: `mitre_privilege_escalation`, `mitre_defense_evasion`, `mitre_execution`
- **CIS 基准**: `cis_5.2`, `cis_5.3`, `cis_5.6`

### 容器安全标签示例

```yaml
- rule: Privileged container detected
  tags: [container, security, privileged, kubernetes, mitre_privilege_escalation]

- rule: Container without resource limits
  tags: [container, resources, compliance, cis_5.3]

- rule: Container with sensitive mount
  tags: [container, security, mount, cis_5.6]

- rule: Root user in container
  tags: [container, security, root, compliance]
```

### MITRE ATT&CK 容器安全标签

容器环境下的 MITRE ATT&CK 映射：

- **特权升级**: `mitre_privilege_escalation`, T1610
- **防御绕过**: `mitre_defense_evasion`, T1611
- **执行**: `mitre_execution`, T1609
- **持久化**: `mitre_persistence`, T1611
- **发现**: `mitre_discovery`, T1613

参考：https://attack.mitre.org/techniques/enterprise/

## 安全检测场景示例

### 场景 1: 特权容器检测

检测具有特权模式或共享主机命名空间的容器：

```yaml
- macro: privileged_container
  condition: >
    container.privileged = true or
    container.host_network = true or
    container.host_pid = true or
    container.host_ipc = true

- rule: Privileged Container in Production
  desc: Detect privileged containers in production namespaces
  condition: >
    privileged_container and
    pod.namespace = "production"
  output: >
    Privileged container %container.name detected in production
    (namespace=%pod.namespace, image=%container.image.repository)
  priority: WARNING
  tags: [container, security, privileged, production]
```

### 场景 2: Root 用户检测

检测以 root (UID 0) 运行的容器进程：

```yaml
- rule: Container Running as Root
  desc: Detect container processes running as root user
  condition: >
    container.name exists and
    process.user.uid = 0
  output: >
    Container %container.name running as root (UID 0)
    in pod %pod.name, namespace %pod.namespace
    (image=%container.image.repository)
  priority: WARNING
  tags: [container, security, root]
```

### 场景 3: 非授权镜像检测

检测使用非白名单镜像的容器：

```yaml
- list: approved_images
  items: [
    nginx, alpine, ubuntu, debian,
    registry.k8s.io/pause,
    registry.k8s.io/nginx
  ]

- rule: Non-Approved Container Image
  desc: Detect containers using non-approved images
  condition: >
    not container.image.repository in (approved_images) and
    not pod.namespace in (kube-system, kube-public)
  output: >
    Container %container.name using non-approved image %container.image.repository
    in namespace %pod.namespace
  priority: WARNING
  tags: [container, security, compliance]
```

### 场景 4: 资源限制合规检测

检测缺少资源限制的容器：

```yaml
- rule: Container Without Memory Limit
  desc: Detect running containers without memory resource limits
  condition: >
    container.name exists and
    not linux.resources.memory.limit exists
  output: >
    Container %container.name has no memory limit set
    in pod %pod.name, namespace %pod.namespace
  priority: WARNING
  tags: [container, resources, compliance, cis_5.3]

- rule: Container Without CPU Limit
  desc: Detect running containers without CPU resource limits
  condition: >
    container.name exists and
    not linux.resources.cpu.limit exists
  output: >
    Container %container.name has no CPU limit set
    in pod %pod.name, namespace %pod.namespace
  priority: WARNING
  tags: [container, resources, compliance, cis_5.3]
```

### 场景 5: 敏感挂载点检测

检测挂载了敏感主机目录的容器：

```yaml
- macro: sensitive_mount
  condition: >
    linux.mounts.destination startswith "/etc" or
    linux.mounts.destination startswith "/root" or
    linux.mounts.destination startswith "/var/lib/docker" or
    linux.mounts.destination contains "docker.sock" or
    linux.mounts.destination contains "/proc/sys"

- rule: Sensitive Mount Detected
  desc: Container has mounted sensitive host directories
  condition: sensitive_mount
  output: >
    Container %container.name has sensitive mount %linux.mounts.destination
    (source=%linux.mounts.source, type=%linux.mounts.type)
  priority: WARNING
  tags: [container, security, mount, cis_5.6]
```

### 场景 6: Seccomp/AppArmor 检测

检测缺少安全配置文件的容器：

```yaml
- rule: Container Without Seccomp
  desc: Detect container without seccomp profile
  condition: >
    container.name exists and
    (not linux.seccomp_profile exists or linux.seccomp_profile = "")
  output: >
    Container %container.name has no seccomp profile
    in namespace %pod.namespace
  priority: WARNING
  tags: [container, security, seccomp]

- rule: Container Without AppArmor
  desc: Detect container without AppArmor profile
  condition: >
    container.name exists and
    (not process.apparmor_profile exists or process.apparmor_profile = "")
  output: >
    Container %container.name has no AppArmor profile
    in namespace %pod.namespace
  priority: WARNING
  tags: [container, security, apparmor]
```

### 场景 7: 危险能力检测

检测添加了危险 Linux capabilities 的容器：

```yaml
- list: dangerous_capabilities
  items: [
    CAP_SYS_ADMIN, CAP_SYS_PTRACE, CAP_SYS_MODULE,
    CAP_DAC_READ_SEARCH, CAP_SYS_PACCT, CAP_SYS_BOOT
  ]

- rule: Dangerous Capability Added
  desc: Detect container with dangerous capabilities
  condition: >
    container.name exists and
    process.capabilities.add exists and
    process.capabilities.add intersects (dangerous_capabilities)
  output: >
    Container %container.name has dangerous capabilities: %process.capabilities.add
    in namespace %pod.namespace
  priority: WARNING
  tags: [container, security, capabilities]
```

### 场景 8: 命名空间策略检测

检测跨命名空间的异常行为：

```yaml
- list: sensitive_namespaces
  items: [kube-system, kube-public, kube-node-lease]

- list: production_namespaces
  items: [production, prod, production-apps]

- rule: Production Pod in System Namespace
  desc: Detect production pods incorrectly placed in system namespaces
  condition: >
    pod.labels.environment = "production" and
    pod.namespace in (sensitive_namespaces)
  output: >
    Production pod %pod.name found in system namespace %pod.namespace
  priority: NOTICE
  tags: [kubernetes, policy, namespace]

- rule: Non-Production Image in Production Namespace
  desc: Detect non-production images in production namespaces
  condition: >
    pod.namespace in (production_namespaces) and
    container.image.tag =~ "dev|test|staging|latest"
  output: >
    Non-production image %container.image used in production namespace %pod.namespace
  priority: WARNING
  tags: [container, policy, production]
```

### 场景 9: 容器重启风暴检测

检测频繁重启的容器：

```yaml
- rule: Container Restart Storm
  desc: Detect containers with excessive restart count
  condition: container.restart_count >= 5
  output: >
    Container %container.name has been restarted %container.restart_count times
    in pod %pod.name, namespace %pod.namespace
  priority: WARNING
  tags: [container, reliability, restart]
```

### 场景 10: 复合安全检测

组合多个条件进行复杂安全检测：

```yaml
- macro: high_risk_container
  condition: >
    (container.privileged = true or process.user.uid = 0) and
    (not container.image.repository in (approved_images))

- rule: High Risk Container in Production
  desc: Detect high-risk configurations in production
  condition: >
    high_risk_container and
    pod.namespace = "production" and
    not linux.resources.memory.limit exists
  output: >
    HIGH RISK: Container %container.name in production with
    privileged=%container.privileged, uid=%process.user.uid,
    no memory limits, image=%container.image.repository
  priority: CRITICAL
  tags: [container, security, critical, production]
```


## 最佳实践

### 1. 使用宏简化复杂条件

将重复使用的容器条件定义为宏：

```yaml
# 好的做法
- macro: privileged_container
  condition: >
    container.privileged = true or
    container.host_network = true or
    container.host_pid = true

- rule: Detect privileged container
  condition: privileged_container and not pod.namespace in (kube-system)

# 不好的做法
- rule: Detect privileged container
  condition: >
    (container.privileged = true or
     container.host_network = true or
     container.host_pid = true) and
    not pod.namespace in (kube-system)
```

### 2. 使用列表管理镜像和命名空间

将常用的镜像白名单和系统命名空间定义为列表：

```yaml
# 好的做法
- list: approved_images
  items: [nginx, alpine, ubuntu, debian, registry.k8s.io/pause]

- rule: Non-approved image detected
  condition: not container.image.repository in (approved_images)

# 不好的做法
- rule: Non-approved image detected
  condition: >
    not container.image.repository in (nginx, alpine, ubuntu, debian)
```

### 3. 使用有意义的规则名称

规则名称应该清晰描述容器安全问题：

```yaml
# 好的做法
- rule: Privileged Container in Production
- rule: Container Running as Root
- rule: Container Without Resource Limits

# 不好的做法
- rule: rule1
- rule: container_alert
- rule: test
```

### 4. 添加描述和安全标签

为容器安全规则添加清晰的描述和相关标签：

```yaml
- rule: Privileged Container in Production
  desc: Detect containers with privileged access in production namespaces
  condition: >
    (container.privileged = true or container.host_network = true) and
    pod.namespace = "production"
  output: >
    Privileged container %container.name detected in production
    (image=%container.image.repository, namespace=%pod.namespace)
  priority: WARNING
  tags: [container, security, privileged, kubernetes, cis_5.2]
```

### 5. 合理设置优先级

根据容器安全事件的严重程度设置优先级：

```yaml
# 关键安全事件 - 特权容器 + Root 用户
- rule: Critical Container Configuration
  condition: container.privileged = true and process.user.uid = 0
  priority: CRITICAL

# 高危事件 - 缺少资源限制
- rule: Container Without Memory Limit
  condition: not linux.resources.memory.limit exists
  priority: WARNING

# 信息性事件 - 新容器启动
- rule: New Container Started
  condition: container.state = "running"
  priority: INFO
```

### 6. 使用排除条件减少误报

添加排除条件以减少系统容器的误报：

```yaml
- rule: Privileged Container Detected
  condition: >
    container.privileged = true and
    not pod.namespace in (kube-system, kube-public) and
    not container.name in (pause, etcd, kube-apiserver)
  output: >
    Non-system privileged container detected
    (container=%container.name, namespace=%pod.namespace)
```

### 7. 使用多行格式提高可读性

对于复杂的容器条件，使用多行格式：

```yaml
condition: >
  (container.privileged = true or
   container.host_network = true or
   container.host_pid = true) and
  not pod.namespace in (kube-system, kube-public) and
  not container.image.repository in (approved_images) and
  process.user.uid = 0
```

### 8. 验证规则名称

确保规则名称符合验证要求：
- 长度：1-256 字符
- 仅包含可打印的 ASCII 字符
- 不能为空

### 9. 测试容器规则

编写规则后，使用容器测试事件验证规则：

```go
event := Event{
    "container.name":              "test-app",
    "container.image.repository":  "nginx",
    "container.privileged":        "true",
    "pod.name":                    "nginx-deployment-abc123",
    "pod.namespace":               "production",
    "process.user.uid":            "0",
}
matching, err := engine.EvaluateAll(event)
```

### 10. 文档化容器宏和列表

为自定义的容器宏和列表添加注释：

```yaml
# System containers that are expected to run with elevated privileges
- list: system_containers
  items: [pause, etcd, kube-apiserver, kube-controller-manager]

# Detect container with privileged security settings
- macro: privileged_container
  condition: >
    container.privileged = true or
    container.host_network = true or
    container.host_pid = true or
    container.host_ipc = true
```

### 11. 按层次组织规则

将容器安全规则按层次组织：

```yaml
# Level 1: 基础容器检测
- list: approved_images
  items: [nginx, alpine, ubuntu]

- macro: container_exists
  condition: container.name exists

# Level 2: 安全策略宏
- macro: privileged_container
  condition: >
    container.privileged = true or
    container.host_network = true

# Level 3: 具体检测规则
- rule: Non-approved Image
  condition: container_exists and not container.image.repository in (approved_images)

- rule: Privileged Container
  condition: privileged_container and not pod.namespace in (kube-system)
```

### 12. 关注 CIS 基准

参考 CIS Kubernetes Benchmark 编写合规规则：

```yaml
# CIS 5.2.1 - 确保使用批准的镜像
- rule: CIS_5.2.1_Approved_Images
  desc: Ensure only approved container images are used
  condition: not container.image.repository in (approved_images)
  priority: WARNING
  tags: [cis_5.2, compliance]

# CIS 5.2.3 - 限制容器运行权限
- rule: CIS_5.2.3_No_Privileged
  desc: Containers should not run as privileged
  condition: container.privileged = true
  priority: WARNING
  tags: [cis_5.2, compliance, privileged]

# CIS 5.3.2 - 确保内存限制已设置
- rule: CIS_5.3.2_Memory_Limits
  desc: Containers should have memory limits set
  condition: not linux.resources.memory.limit exists
  priority: WARNING
  tags: [cis_5.3, compliance, resources]
```

## 性能优化

### 1. 使用列表代替多个 or 条件

```yaml
# 性能较好
- list: approved_images
  items: [nginx, alpine, ubuntu, debian]
condition: container.image.repository in (approved_images)

# 性能较差
condition: >
  container.image.repository = nginx or
  container.image.repository = alpine or
  container.image.repository = ubuntu or
  container.image.repository = debian
```

### 2. 将高选择性条件放在前面

```yaml
# 性能较好
condition: >
  pod.namespace = "production" and    # 高选择性条件
  container.privileged = true and     # 中等选择性条件
  process.user.uid = 0                # 低选择性条件

# 性能较差
condition: >
  process.user.uid = 0 and            # 低选择性条件
  container.privileged = true and     # 中等选择性条件
  pod.namespace = "production"        # 高选择性条件
```

### 3. 避免过度嵌套

```yaml
# 性能较好
condition: >
  (container.image.repository = nginx or
   container.image.repository = apache) and
  pod.namespace = production

# 性能较差
condition: >
  container.image.repository = nginx and pod.namespace = production or
  container.image.repository = apache and pod.namespace = production
```

## 常见问题

### Q: 如何调试容器规则？

A: 使用测试事件验证规则，检查条件是否正确匹配：

```go
event := Event{
    "container.name":             "test",
    "container.privileged":     "true",
    "pod.namespace":              "production",
}
matching, err := engine.EvaluateAll(event)
for _, rule := range matching {
    fmt.Printf("Matched: %s\n", rule.Rule)
}
```

### Q: 如何处理容器字段不存在的情况？

A: 使用 `exists` 操作符检查字段是否存在：

```yaml
condition: container.name exists and container.privileged = true
```

### Q: 如何访问 Pod 标签？

A: 使用点号表示法访问 Pod 标签：

```yaml
condition: pod.labels.environment = "production"
condition: pod.labels.app = "nginx"
```

### Q: 正则表达式如何匹配容器镜像仓库？

A: 使用 `(?i)` 修饰符进行不区分大小写的匹配：

```yaml
condition: container.image.repository =~ "(?i)^registry.k8s.io"
```

### Q: 如何检测缺失的资源限制？

A: 使用 `exists` 操作符：

```yaml
condition: not linux.resources.memory.limit exists
```

### Q: 如何禁用规则？

A: 设置 `enabled: false`：

```yaml
- rule: Test Container Rule
  enabled: false
```

### Q: 规则名称可以包含中文吗？

A: 不可以，规则名称仅支持 ASCII 字符。

### Q: 如何排除系统命名空间的告警？

A: 使用 `not` 和列表：

```yaml
- list: system_namespaces
  items: [kube-system, kube-public]
  
condition: >
  container.privileged = true and
  not pod.namespace in (system_namespaces)
```

## 参考资料

- [Falco 规则文档](https://falco.org/docs/rules/)
- [MITRE ATT&CK 容器威胁矩阵](https://attack.mitre.org/matrices/enterprise/containers/)
- [CIS Kubernetes Benchmark](https://www.cisecurity.org/benchmark/kubernetes)
- [YAML 语法](https://yaml.org/spec/1.2/spec.html)
- [Containerd API 文档](https://github.com/containerd/containerd/blob/main/api/next.pb.txt)

## 版本历史

- v3.0.0: 重新组织文档，专注于容器安全场景
- v2.0.0: 添加 Containerd Pod Spec 字段支持和示例
- v1.0.0: 初始版本
