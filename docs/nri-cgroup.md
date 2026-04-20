# NRI事件与cgroup分析

## NRI事件列表

根据概要设计文档，NRI支持以下事件：

| 事件编号 | 类别 | 事件 | 可修改容器 | 说明 |
|---------|------|------|-----------|------|
| FE-001 | Pod生命周期 | RunPodSandbox | ❌ | Pod启动时通知 |
| FE-002 | | StopPodSandbox | ❌ | Pod停止时通知 |
| FE-003 | | RemovePodSandbox | ❌ | Pod删除时通知 |
| FE-004 | 容器创建 | CreateContainer | ✅ | 容器创建时可调整配置 |
| FE-005 | | PostCreateContainer | ❌ | 容器创建完成后通知 |
| FE-006 | 容器启动 | StartContainer | ❌ | 容器启动时通知 |
| FE-007 | | PostStartContainer | ❌ | 容器启动完成后通知 |
| FE-008 | 容器更新 | UpdateContainer | ✅ | 容器资源更新时可修改 |
| FE-009 | | PostUpdateContainer | ❌ | 容器更新完成后通知 |
| FE-010 | 容器停止 | StopContainer | ✅ | 容器停止时可更新其他容器 |
| FE-011 | 容器删除 | RemoveContainer | ❌ | 容器删除时通知 |

## PID与cgroup的关系

在Linux系统中，每个进程都属于一个cgroup，可以通过以下方式获取进程的cgroup信息：

```bash
# 通过PID查看进程所属的cgroup
cat /proc/<pid>/cgroup

# 输出示例：
# 12:pids:/kubepods/burstable/pod1234/containers5678
# 11:cpu,cpuacct:/kubepods/burstable/pod1234/containers5678
# 10:memory:/kubepods/burstable/pod1234/containers5678
# ...
```

## NRI事件中PID可用性分析

### 可以通过PID获取cgroup的事件

以下事件包含容器进程PID信息，可以通过PID获取cgroup：

#### 1. CreateContainer (FE-004)
- **PID可用性**：❌ 不可用
- **cgroup获取方式**：通过NRI事件中的cgroup信息
- **说明**：容器创建时，容器进程还没有启动，没有PID
- **适用场景**：容器创建时获取cgroup，用于后续监控

#### 2. PostCreateContainer (FE-005)
- **PID可用性**：⚠️ 可能可用
- **cgroup获取方式**：优先通过NRI事件中的cgroup信息，PID作为备用
- **说明**：容器创建完成后，容器进程可能启动，但不确定
- **注意**：不应该依赖PID，应该使用NRI事件中的cgroup信息
- **适用场景**：确认容器创建成功后获取cgroup

#### 3. StartContainer (FE-006)
- **PID可用性**：✅ 可用
- **cgroup获取方式**：通过容器进程PID读取/proc/<pid>/cgroup
- **说明**：容器启动时，容器进程PID可用
- **适用场景**：容器启动时获取cgroup，启动监控

#### 4. PostStartContainer (FE-007)
- **PID可用性**：✅ 可用
- **cgroup获取方式**：通过容器进程PID读取/proc/<pid>/cgroup
- **说明**：容器启动完成后，容器进程PID可用
- **适用场景**：确认容器启动成功后获取cgroup

#### 5. UpdateContainer (FE-008)
- **PID可用性**：✅ 可用
- **cgroup获取方式**：通过容器进程PID读取/proc/<pid>/cgroup
- **说明**：容器更新时，容器进程PID可用
- **适用场景**：容器资源更新时获取cgroup

#### 6. PostUpdateContainer (FE-009)
- **PID可用性**：✅ 可用
- **cgroup获取方式**：通过容器进程PID读取/proc/<pid>/cgroup
- **说明**：容器更新完成后，容器进程PID可用
- **适用场景**：确认容器更新成功后获取cgroup

#### 7. StopContainer (FE-010)
- **PID可用性**：✅ 可用
- **cgroup获取方式**：通过容器进程PID读取/proc/<pid>/cgroup
- **说明**：容器停止时，容器进程可能仍然存在，PID可用
- **适用场景**：容器停止时获取cgroup，停止监控
- **注意**：容器停止后进程可能很快退出，需要及时获取cgroup

### 不可以通过PID获取cgroup的事件

以下事件不包含容器进程PID信息，或者PID不可用：

#### 1. RunPodSandbox (FE-001)
- **PID可用性**：❌ 不可用
- **cgroup获取方式**：通过NRI事件中的cgroup信息
- **说明**：Pod启动时，还没有容器进程，只有Pod沙箱进程
- **适用场景**：Pod级别的监控

#### 2. StopPodSandbox (FE-002)
- **PID可用性**：❌ 不可用
- **cgroup获取方式**：通过NRI事件中的cgroup信息
- **说明**：Pod停止时，容器进程可能已经退出
- **适用场景**：Pod级别的监控

#### 3. RemovePodSandbox (FE-003)
- **PID可用性**：❌ 不可用
- **cgroup获取方式**：通过NRI事件中的cgroup信息
- **说明**：Pod删除时，所有进程都已退出
- **适用场景**：Pod级别的监控

#### 4. RemoveContainer (FE-011)
- **PID可用性**：❌ 不可用
- **cgroup获取方式**：通过NRI事件中的cgroup信息
- **说明**：容器删除时，容器进程已经退出
- **适用场景**：容器删除时的清理操作

## NRI事件中的cgroup信息

根据containerd NRI的设计，NRI事件对象中通常包含以下cgroup相关信息：

```go
type Container struct {
    ID        string
    Name      string
    Namespace string
    Pid       int32      // 容器进程PID（部分事件）
    Cgroup    string     // cgroup路径
    // ... 其他字段
}
```

## 推荐的cgroup获取策略

### 策略1：优先使用NRI事件中的cgroup信息
- **优点**：直接从NRI事件获取，无需额外系统调用
- **适用事件**：所有NRI事件
- **实现**：直接读取NRI事件对象中的Cgroup字段

### 策略2：通过PID获取cgroup（备用方案）
- **优点**：当NRI事件中cgroup信息不可用时，可以通过PID获取
- **适用事件**：CreateContainer、PostCreateContainer、StartContainer、PostStartContainer、UpdateContainer、PostUpdateContainer、StopContainer
- **实现**：读取/proc/<pid>/cgroup文件
- **注意**：需要处理PID不存在的情况

### 推荐实现逻辑

```go
func GetCgroupFromNRIEvent(event *NRIEvent) (string, error) {
    // 1. 优先使用NRI事件中的cgroup信息
    if event.Cgroup != "" {
        return event.Cgroup, nil
    }

    // 2. 如果cgroup为空，尝试通过PID获取
    if event.Pid > 0 {
        cgroupPath := fmt.Sprintf("/proc/%d/cgroup", event.Pid)
        data, err := os.ReadFile(cgroupPath)
        if err != nil {
            return "", fmt.Errorf("failed to read cgroup for pid %d: %v", event.Pid, err)
        }

        // 解析cgroup路径
        lines := strings.Split(string(data), "\n")
        for _, line := range lines {
            parts := strings.Split(line, ":")
            if len(parts) >= 3 {
                // 格式：hierarchy-ID:controller-list:cgroup-path
                cgroupPath := parts[2]
                return cgroupPath, nil
            }
        }
    }

    return "", fmt.Errorf("no cgroup information available")
}
```

## 总结

| 事件 | PID可用 | 可通过PID获取cgroup | 推荐获取方式 |
|-----|--------|-------------------|-------------|
| RunPodSandbox | ❌ | ❌ | NRI事件cgroup字段 |
| StopPodSandbox | ❌ | ❌ | NRI事件cgroup字段 |
| RemovePodSandbox | ❌ | ❌ | NRI事件cgroup字段 |
| CreateContainer | ❌ | ❌ | NRI事件cgroup字段 |
| PostCreateContainer | ⚠️ 可能 | ⚠️ 不推荐 | NRI事件cgroup字段（优先） |
| StartContainer | ✅ | ✅ | NRI事件cgroup字段（优先），PID备用 |
| PostStartContainer | ✅ | ✅ | NRI事件cgroup字段（优先），PID备用 |
| UpdateContainer | ✅ | ✅ | NRI事件cgroup字段（优先），PID备用 |
| PostUpdateContainer | ✅ | ✅ | NRI事件cgroup字段（优先），PID备用 |
| StopContainer | ✅ | ✅ | NRI事件cgroup字段（优先），PID备用 |
| RemoveContainer | ❌ | ❌ | NRI事件cgroup字段 |

**建议**：
1. **优先使用NRI事件中自带的cgroup信息**：这是最可靠的方式，适用于所有NRI事件
2. **PID作为备用方案**：仅适用于容器已启动的事件（StartContainer到StopContainer），当NRI事件中cgroup信息为空时使用
3. **不要依赖PID**：对于CreateContainer、PostCreateContainer等容器未启动事件，不应该依赖PID获取cgroup
4. **实现时需要处理异常情况**：PID不存在、进程已退出、/proc文件不可读等
5. **Pod级别事件**：只能使用NRI事件中的cgroup信息，无法通过PID获取
