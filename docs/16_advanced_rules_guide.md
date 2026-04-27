# Nuts Observer 高级诊断规则使用指南

## 概述

Nuts Observer 提供四层诊断规则架构，支持从简单阈值到复杂预测分析的多种诊断能力：

```
┌─────────────────────────────────────────────────────────────┐
│                    诊断规则架构                              │
├─────────────────────────────────────────────────────────────┤
│  1. 阈值型规则（Threshold Rules）✅ 完整实现              │
│     - 简单阈值比较 (> < >= <=)                              │
│     - 20+ 内置默认规则                                      │
│                                                             │
│  2. 关联型规则（Correlation Rules）✅ 完整实现          │
│     - 多指标同时异常检测（AND/OR）                          │
│     - 跨证据类型关联分析                                    │
│     - 时间窗口关联                                          │
│                                                             │
│  3. 统计异常型规则（Statistical Rules）✅ 完整实现      │
│     - 突发异常检测（SuddenSpike/Drop）                      │
│     - 离群值检测（3-sigma）                                 │
│     - 方差异常检测                                          │
│                                                             │
│  4. 趋势分析规则（Trend Rules）✅ 完整实现                │
│     - 线性回归趋势检测                                      │
│     - 预测性告警（5分钟预测窗口）                           │
│     - R² 拟合优度评估                                       │
└─────────────────────────────────────────────────────────────┘
```

---

## 一、阈值型规则（Threshold Rules）

### 1.1 基本结构

```rust
ThresholdRule {
    name: "规则名称",
    evidence_type: "证据类型",
    metric_name: "指标名称",
    threshold: 100.0,          // 阈值
    operator: GreaterThan,      // 操作符: > < >= <=
    conclusion_title: "诊断结论",
    severity: 8,               // 严重程度 1-10
}
```

### 1.2 操作符说明

| 操作符 | 说明 | 示例 |
|:-------|:-----|:-----|
| `>` | 大于 | CPU使用率 > 80% |
| `<` | 小于 | 连通成功率 < 95% |
| `>=` | 大于等于 | 内存使用率 >= 90% |
| `<=` | 小于等于 | 响应时间 <= 50ms |

### 1.3 创建阈值规则 API

```bash
curl -X POST http://localhost:8080/v1/rules \
  -H "Content-Type: application/json" \
  -d '{
    "rule": {
      "rule_id": "custom_cpu_rule",
      "name": "自定义CPU规则",
      "evidence_type": "cgroup_contention",
      "metric_name": "cpu_usage_percent",
      "threshold": 80.0,
      "operator": ">=",
      "conclusion_title": "CPU使用率超过80%，需要关注",
      "severity": 6,
      "enabled": true
    }
  }'
```

---

## 二、关联型规则（Correlation Rules）

### 2.1 设计目标

检测多指标/多证据之间的关联关系，减少误报，提高诊断准确性。

### 2.2 关联模式

| 模式 | 说明 | 使用场景 |
|:-----|:-----|:---------|
| **AND** | 多个指标同时异常 | 网络延迟高 + 丢包率高 |
| **OR** | 任一指标异常 | 多维度异常检测 |
| **比率** | 指标间比率异常 | 错误率/请求数 |

### 2.3 基本结构

```rust
CorrelationRule {
    name: "规则名称",
    primary_evidence_type: "主要证据类型",
    conditions: CorrelationCondition::All(vec![
        MetricThreshold { metric_name: "latency", threshold: 100.0, operator: GreaterThan },
        MetricThreshold { metric_name: "loss", threshold: 0.01, operator: GreaterThan },
    ]),
    conclusion_title: "关联诊断结论",
    severity: 8,
    related_evidence_types: vec!["network", "block_io"],
    time_window_ms: 60000,  // 60秒关联窗口
}
```

### 2.4 创建关联规则 API

```bash
curl -X POST http://localhost:8080/v1/rules/correlation \
  -H "Content-Type: application/json" \
  -d '{
    "rule_id": "network_latency_loss_correlation",
    "name": "网络延迟丢包关联规则",
    "primary_evidence_type": "network",
    "related_types": ["network"],
    "conditions": [
      {"metric_name": "latency_p99_ms", "threshold": 100.0, "operator": ">"},
      {"metric_name": "packet_loss_rate", "threshold": 0.01, "operator": ">"}
    ],
    "conclusion_title": "网络延迟高且伴随丢包，可能存在网络拥塞",
    "severity": 8
  }'
```

### 2.5 内置关联规则

| 规则ID | 关联条件 | 结论 |
|:-------|:---------|:-----|
| `network_latency_with_packet_loss` | 延迟 > 100ms AND 丢包 > 1% | 网络拥塞 |
| `cpu_throttle_with_memory_pressure` | CPU节流 > 10% AND 内存压力 > 50 | 综合资源争抢 |
| `io_wait_with_cpu_stall` | IO延迟 > 100ms AND CPU等待 > 50ms | 存储瓶颈 |

---

## 三、统计异常型规则（Statistical Rules）

### 3.1 异常类型

| 类型 | 说明 | 适用场景 |
|:-----|:-----|:---------|
| `SuddenSpike` | 突发跃迁 | 延迟突然增加 |
| `SuddenDrop` | 骤降 | 流量突然下降 |
| `VarianceIncrease` | 方差异常 | 不稳定性增加 |
| `OutlierDetection` | 离群值 | 3-sigma 异常点 |
| `DistributionShift` | 分布偏移 | 基线变化 |

### 3.2 基本结构

```rust
StatisticalRule {
    name: "规则名称",
    evidence_type: "证据类型",
    metric_name: "指标名称",
    anomaly_type: AnomalyType::SuddenSpike,
    window_secs: 60,      // 统计窗口（秒）
    threshold: 3.0,       // 阈值倍数（如3-sigma）
    conclusion_title: "统计异常结论",
    severity: 8,
}
```

### 3.3 创建统计规则 API

```bash
# 突发异常检测
curl -X POST http://localhost:8080/v1/rules/statistical \
  -H "Content-Type: application/json" \
  -d '{
    "rule_id": "network_latency_spike",
    "name": "网络延迟突发检测",
    "evidence_type": "network",
    "metric_name": "latency_p99_ms",
    "anomaly_type": "SuddenSpike",
    "window_secs": 60,
    "threshold": 3.0,
    "conclusion_title": "网络延迟突发性跃迁，可能存在网络拥塞",
    "severity": 8
  }'

# 离群值检测
curl -X POST http://localhost:8080/v1/rules/statistical \
  -H "Content-Type: application/json" \
  -d '{
    "rule_id": "io_latency_outlier",
    "name": "IO延迟离群值检测",
    "evidence_type": "block_io",
    "metric_name": "io_latency_ms",
    "anomaly_type": "OutlierDetection",
    "window_secs": 300,
    "threshold": 3.0,
    "conclusion_title": "I/O延迟出现离群值，可能存在存储设备异常",
    "severity": 7
  }'
```

### 3.4 统计检测算法

**突发异常检测**：
```
判定条件: |current - previous| / previous > threshold
```

**离群值检测（3-sigma）**：
```
判定条件: |current - mean| / std_dev > threshold (默认3.0)
置信度: 0.5 + (z_score / threshold) * 0.5
```

---

## 四、趋势分析规则（Trend Rules）

### 4.1 设计目标

基于时间序列的趋势检测和预测，实现预测性告警。

### 4.2 趋势类型

| 类型 | 说明 | 示例 |
|:-----|:-----|:-----|
| `SustainedGrowth` | 持续增长 | 内存使用持续上升 |
| `SustainedDecline` | 持续下降 | 成功率持续下降 |
| `AcceleratingGrowth` | 加速增长 | CPU使用率加速上升 |
| `TrendReversal` | 趋势反转 | 稳定→异常 |

### 4.3 趋势方向

| 方向 | 说明 |
|:-----|:-----|
| `Increasing` | 上升趋势 |
| `Decreasing` | 下降趋势 |
| `Stable` | 平稳趋势 |
| `Fluctuating` | 波动趋势 |

### 4.4 基本结构

```rust
TrendRule {
    name: "规则名称",
    evidence_type: "证据类型",
    metric_name: "指标名称",
    config: TrendRuleConfig {
        direction: TrendDirection::Increasing,
        trend_type: TrendType::SustainedGrowth,
        min_slope: 0.5,              // 最小斜率（每秒变化）
        forecast_window_secs: 300,    // 预测窗口（5分钟）
        forecast_threshold: 90.0,   // 预测阈值
        window_size: 20,             // 分析窗口大小
    },
    conclusion_title: "趋势分析结论",
    severity: 8,
}
```

### 4.5 创建趋势规则 API

```bash
# 内存使用增长趋势（OOM预测）
curl -X POST http://localhost:8080/v1/rules/trend \
  -H "Content-Type: application/json" \
  -d '{
    "rule_id": "memory_growth_forecast",
    "name": "内存增长趋势预测",
    "evidence_type": "cgroup_contention",
    "metric_name": "memory_usage_percent",
    "direction": "increasing",
    "min_slope": 0.5,
    "forecast_window_secs": 300,
    "forecast_threshold": 90.0,
    "conclusion_title": "内存使用率呈上升趋势，预测5分钟后将超过90%，存在OOM风险",
    "severity": 9
  }'

# 网络延迟上升趋势
curl -X POST http://localhost:8080/v1/rules/trend \
  -H "Content-Type: application/json" \
  -d '{
    "rule_id": "network_latency_trend",
    "name": "网络延迟趋势",
    "evidence_type": "network",
    "metric_name": "latency_p99_ms",
    "direction": "increasing",
    "min_slope": 2.0,
    "forecast_window_secs": 180,
    "forecast_threshold": 200.0,
    "conclusion_title": "网络延迟呈上升趋势，预测3分钟后将超过200ms",
    "severity": 7
  }'
```

### 4.6 趋势分析算法

**线性回归**：
```
y = slope * x + intercept
R² = 1 - (SS_res / SS_tot)  // 拟合优度
```

**预测告警**：
```
forecast_value = intercept + slope * forecast_window_secs
触发条件: direction匹配 AND forecast_value > threshold
```

---

## 五、规则管理 API 汇总

### 5.1 通用规则管理

| 方法 | 端点 | 功能 |
|:-----|:-----|:-----|
| GET | `/v1/rules` | 列出所有规则 |
| GET | `/v1/rules/:rule_id` | 获取单个规则 |
| POST | `/v1/rules` | 创建阈值型规则 |
| PUT | `/v1/rules/:rule_id` | 更新规则 |
| DELETE | `/v1/rules/:rule_id` | 删除规则 |
| GET | `/v1/rules/status` | 获取规则管理器状态 |

### 5.2 高级规则创建

| 方法 | 端点 | 功能 |
|:-----|:-----|:-----|
| POST | `/v1/rules/correlation` | 创建关联型规则 |
| POST | `/v1/rules/statistical` | 创建统计型规则 |
| POST | `/v1/rules/trend` | 创建趋势型规则 |

### 5.3 导入导出

| 方法 | 端点 | 功能 |
|:-----|:-----|:-----|
| GET | `/v1/rules/export` | 导出规则（YAML） |
| POST | `/v1/rules/import` | 导入规则（YAML） |
| POST | `/v1/rules/reload` | 重载默认规则 |
| DELETE | `/v1/rules/clear` | 清空所有规则 |

---

## 六、诊断触发 API

### 6.1 触发诊断

```bash
curl -X POST http://localhost:8080/v1/diagnose \
  -H "Content-Type: application/json" \
  -d '{
    "scope": {
      "scope_type": "pod",
      "scope_key": "default/nginx-pod"
    },
    "evidence_list": [
      {
        "evidence_id": "evidence-001",
        "task_id": "task-001",
        "evidence_type": "cgroup_contention",
        "metric_summary": {
          "cpu_usage_percent": 85.5,
          "memory_usage_percent": 78.0
        }
      },
      {
        "evidence_id": "evidence-002",
        "task_id": "task-001",
        "evidence_type": "network",
        "metric_summary": {
          "latency_p99_ms": 150.0,
          "packet_loss_rate": 0.02
        }
      }
    ]
  }'
```

### 6.2 响应示例

```json
{
  "success": true,
  "data": {
    "diagnosis_id": "diag-xxx",
    "status": "Done",
    "conclusions": [
      {
        "conclusion_id": "con-xxx",
        "title": "CPU使用率超过80%，需要关注",
        "confidence": 0.75,
        "evidence_strength": "Medium",
        "severity": 6,
        "details": {
          "rule_type": "threshold",
          "metric": "cpu_usage_percent",
          "value": 85.5,
          "threshold": 80.0
        }
      },
      {
        "conclusion_id": "corr-xxx",
        "title": "网络延迟高且伴随丢包，可能存在网络拥塞",
        "confidence": 0.85,
        "evidence_strength": "High",
        "severity": 8,
        "details": {
          "rule_type": "correlation",
          "correlation_score": 0.85,
          "matched_evidence_count": 2
        }
      }
    ],
    "recommendations": [
      {
        "recommendation_id": "rec-xxx",
        "type": "investigation",
        "action": "检查Pod资源限制和节点负载",
        "priority": 7
      }
    ]
  }
}
```

---

## 七、规则配置示例

### 7.1 完整规则集 YAML

```yaml
# rules.yaml
rules:
  # 阈值型规则
  - rule_id: cpu_usage_high
    name: CPU使用率过高
    evidence_type: cgroup_contention
    metric_name: cpu_usage_percent
    threshold: 80.0
    operator: ">="
    conclusion_title: CPU使用率超过80%
    severity: 7
    enabled: true

  # 关联型规则
  - rule_id: network_correlation
    name: 网络关联异常
    evidence_type: network
    metric_name: correlation_multi_metric
    threshold: 0.0
    operator: "CORRELATION"
    conclusion_title: 网络延迟高且伴随丢包
    severity: 8
    description: "关联型规则: 延迟>100ms AND 丢包>1%"

  # 统计型规则
  - rule_id: latency_spike
    name: 延迟突发检测
    evidence_type: network
    metric_name: latency_p99_ms
    threshold: 3.0
    operator: "STATISTICAL:SuddenSpike"
    conclusion_title: 网络延迟突发性跃迁
    severity: 8
    description: "统计型规则: 窗口60秒, 异常类型SuddenSpike"

  # 趋势型规则
  - rule_id: memory_growth
    name: 内存增长趋势
    evidence_type: cgroup_contention
    metric_name: memory_usage_percent
    threshold: 90.0
    operator: "TREND:increasing"
    conclusion_title: 内存使用率呈上升趋势
    severity: 9
    description: "趋势型规则: 方向increasing, 预测窗口300秒, 最小斜率0.5"
```

---

## 八、测试脚本

### 8.1 运行测试

```bash
# 运行诊断模块单元测试
cargo test diagnosis

# 运行高级规则测试脚本
./scripts/test_advanced_rules.sh

# 完整集成测试
cargo test --bin nuts-observer
```

---

## 九、最佳实践

### 9.1 规则设计原则

1. **阈值型规则**：适用于明确的性能基线
2. **关联型规则**：适用于减少误报，提高准确性
3. **统计型规则**：适用于检测突发异常
4. **趋势型规则**：适用于预测性告警

### 9.2 严重度建议

| 严重度 | 场景 | 响应时间 |
|:------:|:-----|:--------:|
| 9-10 | OOM预测、系统崩溃风险 | 立即 |
| 7-8 | 性能降级、资源争抢 | 1小时内 |
| 5-6 | 潜在问题、趋势异常 | 4小时内 |
| 1-4 | 信息提示 | 24小时内 |

### 9.3 窗口大小选择

| 规则类型 | 建议窗口 | 说明 |
|:---------|:--------:|:-----|
| 阈值型 | 即时 | 无窗口，实时检测 |
| 关联型 | 60秒 | 关联近期证据 |
| 统计型 | 60-300秒 | 足够样本计算统计量 |
| 趋势型 | 10-20个数据点 | 平衡响应速度和准确性 |

---

## 十、API 合同更新

参见 `docs/05_api_cli_contract.md` 第 4 节获取完整 API 规范。

---

## 版本信息

- **文档版本**: 1.0
- **适用版本**: nuts-observer >= 0.3.0
- **最后更新**: 2026-04-24
