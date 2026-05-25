# Prometheus 指标说明

`GET /metrics` 端点输出的指标分为三类：Nuts 业务指标、Go 运行时指标、进程指标。

---

## 1. Nuts 业务指标

前缀：`nuts_`

### 1.1 任务计数器（Counter）

| 指标名 | 类型 | 标签 | 说明 |
|--------|------|------|------|
| `nuts_task_created_total` | Counter | - | 任务创建总数 |
| `nuts_task_state_transition_total` | Counter | `from`, `to` | 状态转换总数（按 from/to 维度） |
| `nuts_task_timeout_total` | Counter | - | 任务超时总数 |
| `nuts_task_retry_total` | Counter | - | 任务重试总数 |
| `nuts_task_archived_total` | Counter | - | 任务归档总数 |
| `nuts_task_deleted_total` | Counter | - | 归档清理删除总数 |
| `nuts_task_error_total` | Counter | - | 任务错误总数（状态转换失败等） |

#### 示例

```promql
# 任务创建速率（每秒）
rate(nuts_task_created_total[5m])

# 状态转换速率（按 from/to 维度）
rate(nuts_task_state_transition_total[5m])

# 超时率
rate(nuts_task_timeout_total[5m])

# 错误率
rate(nuts_task_error_total[5m])

# 重试率
rate(nuts_task_retry_total[5m])
```

### 1.2 标签说明

`nuts_task_state_transition_total` 的标签：

| 标签 | 说明 | 示例值 |
|------|------|--------|
| `from` | 源状态 | `pending`, `validating`, `processing`, `failover` |
| `to` | 目标状态 | `validating`, `processing`, `completed`, `failed`, `abandoned` |

---

## 2. Go 运行时指标

前缀：`go_`

### 2.1 Goroutine

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `go_goroutines` | Gauge | 当前 goroutine 数量 |
| `go_threads` | Gauge | OS 线程数 |
| `go_sched_gomaxprocs_threads` | Gauge | GOMAXPROCS 设置 |

#### 告警建议

```promql
# goroutine 泄漏告警
go_goroutines > 10000
```

### 2.2 内存

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `go_memstats_alloc_bytes` | Gauge | 当前堆内存分配量（正在使用） |
| `go_memstats_alloc_bytes_total` | Counter | 累计堆内存分配量（含已释放） |
| `go_memstats_sys_bytes` | Gauge | 从 OS 获取的总内存 |
| `go_memstats_heap_alloc_bytes` | Gauge | 堆分配量（同 alloc_bytes） |
| `go_memstats_heap_inuse_bytes` | Gauge | 堆正在使用的内存 |
| `go_memstats_heap_idle_bytes` | Gauge | 堆空闲内存 |
| `go_memstats_heap_objects` | Gauge | 堆对象数量 |
| `go_memstats_stack_inuse_bytes` | Gauge | 栈内存使用量 |
| `go_memstats_next_gc_bytes` | Gauge | 下次 GC 触发的堆大小 |

### 2.3 GC

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `go_gc_duration_seconds` | Summary | GC 暂停时间分布（quantile: 0, 0.25, 0.5, 0.75, 1） |
| `go_gc_gogc_percent` | Gauge | GOGC 配置值（默认 100） |
| `go_gc_gomemlimit_bytes` | Gauge | GOMEMLIMIT 配置值 |

#### 示例

```promql
# GC 暂停 P99 超过 10ms 告警
histogram_quantile(0.99, go_gc_duration_seconds) > 0.01
```

---

## 3. 进程指标

前缀：`process_`

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `process_cpu_seconds_total` | Counter | 进程累计 CPU 时间（秒） |
| `process_resident_memory_bytes` | Gauge | 进程常驻内存（RSS） |
| `process_virtual_memory_bytes` | Gauge | 进程虚拟内存 |
| `process_open_fds` | Gauge | 当前打开的文件描述符数 |
| `process_max_fds` | Gauge | 文件描述符上限 |
| `process_start_time_seconds` | Gauge | 进程启动时间（Unix 时间戳） |
| `process_network_receive_bytes_total` | Counter | 网络接收字节数 |
| `process_network_transmit_bytes_total` | Counter | 网络发送字节数 |

#### 告警建议

```promql
# 文件描述符泄漏
process_open_fds / process_max_fds > 0.8

# 内存使用过高
process_resident_memory_bytes > 1e9  # > 1GB
```

---

## 4. 常用 Grafana PromQL

### 4.1 任务面板

```promql
# 任务创建速率
rate(nuts_task_created_total[5m])

# 各状态转换速率（热力图）
rate(nuts_task_state_transition_total[5m])

# 超时率
rate(nuts_task_timeout_total[5m])

# 错误率
rate(nuts_task_error_total[5m])

# 重试率
rate(nuts_task_retry_total[5m])
```

### 4.2 系统面板

```promql
# goroutine 数量
go_goroutines

# 堆内存使用
go_memstats_heap_inuse_bytes

# RSS 内存
process_resident_memory_bytes

# CPU 使用率（秒/秒）
rate(process_cpu_seconds_total[5m])

# GC 暂停 P99
histogram_quantile(0.99, rate(go_gc_duration_seconds_sum[5m]) / rate(go_gc_duration_seconds_count[5m]))
```

### 4.3 告警规则

```yaml
groups:
  - name: nuts
    rules:
      - alert: TaskTimeoutRateHigh
        expr: rate(nuts_task_timeout_total[5m]) > 10
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Task timeout rate exceeded 10/s"

      - alert: TaskErrorRateHigh
        expr: rate(nuts_task_error_total[5m]) > 20
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Task error rate exceeded 20/s"

      - alert: GoroutineLeak
        expr: go_goroutines > 10000
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Possible goroutine leak"

      - alert: HighMemoryUsage
        expr: process_resident_memory_bytes > 1e9
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Process RSS exceeds 1GB"
```
