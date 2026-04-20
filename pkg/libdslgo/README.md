# Go DSL Engine

A Go-based Domain Specific Language (DSL) engine for evaluating rules against container and event data, inspired by Falco rules syntax. Designed for container security monitoring and Kubernetes workload protection.

## Features

- **YAML-based rule definitions**: Define rules, macros, and lists in YAML files
- **Condition evaluation**: Support for boolean logic (and, or, not)
- **String operators**: contains, endswith, startswith
- **List membership**: Check if values belong to predefined lists using the `in` operator
- **Field access**: Support for dot-notation field access (e.g., `container.image.repository`)
- **Macros**: Reusable condition definitions that can be referenced in rules
- **Lists**: Predefined value lists for membership checks
- **Container support**: Native support for Containerd and Kubernetes fields

## Supported Operators

### Logical Operators
- `and` - Logical AND
- `or` - Logical OR
- `not` - Logical NOT

### String Operators
- `contains` - Checks if a string contains a substring
- `endswith` - Checks if a string ends with a suffix
- `startswith` - Checks if a string starts with a prefix

### Membership Operator
- `in` - Checks if a value is in a list (e.g., `container.name in (system_containers)`)

## Project Structure

```
.
├── go.mod              # Go module definition
├── types.go            # Core types (Rule, Macro, List, Engine, Event)
├── parser.go           # YAML parser and expression parser/evaluator
├── evaluator.go        # Evaluation functions
├── tests/              # Test files and examples
└── docs/               # Documentation
    └── rule-writing-guide.md  # Detailed rule writing guide
```

## Usage

### Container Security Example

```go
package main

import (
    "fmt"
    "os"
    dsl "github.com/libdslgo"
)

func main() {
    // Create a new DSL engine
    engine := dsl.NewEngine()

    // Read and parse a YAML rule file
    data, err := os.ReadFile("rules/container-rules.yaml")
    if err != nil {
        panic(err)
    }

    if err := engine.ParseFile(data); err != nil {
        panic(err)
    }

    // Create a container event to evaluate
    event := dsl.Event{
        "container.name":              "nginx-app",
        "container.image.repository":  "nginx",
        "container.image.tag":         "latest",
        "container.privileged":        "true",
        "pod.name":                    "nginx-deployment-abc123",
        "pod.namespace":               "production",
        "process.name":                "nginx",
        "process.user.uid":            "0",
    }

    // Evaluate a condition
    condition := "container.privileged = true and process.user.uid = 0"
    expr, _ := engine.ParseCompileCondition(condition)
    result, err := engine.EvaluateCondition(expr, event)
    if err != nil {
        panic(err)
    }

    fmt.Printf("Condition: %s\n", condition)
    fmt.Printf("Result: %v\n", result)
}
```

### Rule Definition Format

Rules are defined in YAML with the following structure:

```yaml
- rule: Container Security Rule
  desc: Detect privileged container running as root
  condition: >
    (privileged_container or container.host_network = true) and
    not container.name in (system_containers)
  output: >
    Container security alert:
    - Container: %container.name
    - Pod: %pod.name
    - Namespace: %pod.namespace
    - Image: %container.image.repository:%container.image.tag
    - User: %process.user.username (UID: %process.user.uid)
    - Privileged: %container.privileged
    - Host Network: %container.host_network
  priority: WARNING
  tags: [container, security, kubernetes]
```

### Macro Definition

Macros define reusable conditions:

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
```

### List Definition

Lists define collections of values for membership checks:

```yaml
- list: system_containers
  items: [pause, etcd, kube-apiserver, kube-controller-manager, kube-scheduler]

- list: allowed_images
  items: [nginx, alpine, ubuntu, registry.k8s.io/pause]

- list: sensitive_namespaces
  items: [kube-system, kube-public, kube-node-lease]
```

## Container Event Data Format

Events are represented as `map[string]interface{}` with dot-notation keys:

```go
event := dsl.Event{
    // Container fields
    "container.name":              "my-app",
    "container.id":                "abc123def456",
    "container.image.repository":  "nginx",
    "container.image.tag":         "1.21-alpine",
    "container.privileged":        "true",
    "container.host_network":      "false",
    "container.restart_count":     "3",
    
    // Pod fields
    "pod.name":                    "nginx-deployment-7c4b8f5d9-x2v4p",
    "pod.namespace":               "production",
    "pod.labels.app":              "nginx",
    "pod.labels.environment":      "production",
    
    // Process fields
    "process.name":                "nginx",
    "process.user.uid":            "0",
    "process.user.username":       "root",
    "process.pid":                 "1234",
    
    // Linux / Security fields
    "linux.resources.memory.limit": "1073741824",
    "linux.seccomp_profile":       "runtime/default",
    "linux.mounts.destination":    "/var/lib/docker",
}
```

## Container Security Examples

### Example 1: Detect Privileged Containers

```yaml
- rule: Detect Privileged Container
  desc: Alert when a privileged container is detected outside system namespaces
  condition: >
    container.privileged = true and
    not pod.namespace in (kube-system, kube-public)
  output: >
    Privileged container %container.name detected in namespace %pod.namespace
    (image: %container.image.repository)
  priority: WARNING
  tags: [container, security, privileged]
```

### Example 2: Detect Root User in Containers

```yaml
- rule: Container Running as Root
  desc: Detect container processes running as root user (UID 0)
  condition: >
    container.name exists and
    process.user.uid = 0
  output: >
    Container %container.name running as root (UID 0)
    in pod %pod.name, namespace %pod.namespace
  priority: WARNING
  tags: [container, security, root]
```

### Example 3: Detect Non-Standard Images

```yaml
- list: approved_images
  items: [nginx, alpine, ubuntu, debian, registry.k8s.io/pause]

- rule: Non-Standard Container Image
  desc: Detect containers using non-approved images
  condition: >
    not container.image.repository in (approved_images) and
    not pod.namespace in (kube-system)
  output: >
    Container %container.name using non-standard image %container.image.repository
    in namespace %pod.namespace
  priority: NOTICE
  tags: [container, security, compliance]
```

### Example 4: Detect Missing Resource Limits

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
  tags: [container, resources, compliance]
```

### Example 5: Detect Sensitive Mounts

```yaml
- macro: sensitive_mount
  condition: >
    linux.mounts.destination startswith "/etc" or
    linux.mounts.destination startswith "/root" or
    linux.mounts.destination contains "docker.sock"

- rule: Sensitive Mount Detected
  desc: Container has mounted sensitive host directories
  condition: sensitive_mount
  output: >
    Container %container.name has sensitive mount %linux.mounts.destination
  priority: WARNING
  tags: [container, security, mount]
```

## Running the Examples

```bash
go test ./tests/...
```

The test suite includes container security scenarios demonstrating:
1. Container field evaluation (name, image, privileged mode)
2. Pod namespace and label filtering
3. Kubernetes security policy enforcement
4. Resource limit compliance checking
5. Complex multi-field conditions with macros and lists

## Limitations

- Complex nested macros with multiple levels of recursion may not parse correctly
- The parser expects single-line conditions or conditions with newlines that are cleaned before parsing
- Field access uses exact key matching for dot-notation keys (e.g., `evt.arg.name` is a single key, not nested traversal)

## License

This is a demonstration project for educational purposes.
