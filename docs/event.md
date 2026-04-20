```json
{
    "cgroup.id": "/k8s.io/658a0a668c7cd1d85c1780b05c0042b18fcb46c1c15ab50f8e4ed9c63128e025",
    "container.name": "",
    "event.id": "658a0a668c7cd1d85c1780b05c0042b18fcb46c1c15ab50f8e4ed9c63128e025",
    "event.type": "RunPodSandbox",
    "pid": 7973,
    "pod.annotation.io.kubernetes.cri.container-type": "sandbox",
    "pod.annotation.io.kubernetes.cri.podsandbox.image-name": "registry.aliyuncs.com/google_containers/pause:3.9",
    "pod.annotation.io.kubernetes.cri.sandbox-id": "658a0a668c7cd1d85c1780b05c0042b18fcb46c1c15ab50f8e4ed9c63128e025",
    "pod.annotation.io.kubernetes.cri.sandbox-log-directory": "/tmp/crius-manual-logs",
    "pod.annotation.io.kubernetes.cri.sandbox-name": "manual-verify-pod",
    "pod.annotation.io.kubernetes.cri.sandbox-namespace": "default",
    "pod.annotation.io.kubernetes.cri.sandbox-uid": "manual-verify-pod-001",
    "pod.id": "658a0a668c7cd1d85c1780b05c0042b18fcb46c1c15ab50f8e4ed9c63128e025",
    "pod.label.io.cri-containerd.kind": "sandbox",
    "pod.name": "manual-verify-pod",
    "pod.namespace": "default",
    "pod.uid": "manual-verify-pod-001",
    "timestamp": "2026-04-20T09:21:39.620269885+08:00"
}
```

```json
{
    "cgroup.id": "/k8s.io/3a429cb8b3d9ef4ac7c971d12c4e04b27ba7acf9f721086d09d7e4b2a7a4b7d0",
    "container.annotation.io.kubernetes.cri.container-name": "manual-verify-container",
    "container.annotation.io.kubernetes.cri.container-type": "container",
    "container.annotation.io.kubernetes.cri.image-name": "swr.cn-north-4.myhuaweicloud.com/ddn-k8s/docker.io/busybox:1.36.0",
    "container.annotation.io.kubernetes.cri.sandbox-id": "5a571e489166e734b35de84c279a5889f750d07e01046c9852b0dcf0c7064578",
    "container.annotation.io.kubernetes.cri.sandbox-name": "manual-verify-pod",
    "container.annotation.io.kubernetes.cri.sandbox-namespace": "default",
    "container.annotation.io.kubernetes.cri.sandbox-uid": "manual-verify-pod-001",
    "container.arg": " /bin/sh -c trap : TERM INT; while true; do sleep 3600; done",
    "container.env": " PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin HOSTNAME=mangosteen",
    "container.id": "3a429cb8b3d9ef4ac7c971d12c4e04b27ba7acf9f721086d09d7e4b2a7a4b7d0",
    "container.label.io.cri-containerd.kind": "container",
    "container.name": "manual-verify-container",
    "container.state": "CONTAINER_CREATED",
    "event.id": "3a429cb8b3d9ef4ac7c971d12c4e04b27ba7acf9f721086d09d7e4b2a7a4b7d0",
    "event.type": "StartContainer",
    "pid": 9388,
    "pod.annotation.io.kubernetes.cri.container-type": "sandbox",
    "pod.annotation.io.kubernetes.cri.podsandbox.image-name": "registry.aliyuncs.com/google_containers/pause:3.9",
    "pod.annotation.io.kubernetes.cri.sandbox-id": "5a571e489166e734b35de84c279a5889f750d07e01046c9852b0dcf0c7064578",
    "pod.annotation.io.kubernetes.cri.sandbox-log-directory": "/tmp/crius-manual-logs",
    "pod.annotation.io.kubernetes.cri.sandbox-name": "manual-verify-pod",
    "pod.annotation.io.kubernetes.cri.sandbox-namespace": "default",
    "pod.annotation.io.kubernetes.cri.sandbox-uid": "manual-verify-pod-001",
    "pod.id": "5a571e489166e734b35de84c279a5889f750d07e01046c9852b0dcf0c7064578",
    "pod.label.io.cri-containerd.kind": "sandbox",
    "pod.name": "manual-verify-pod",
    "pod.namespace": "default",
    "pod.uid": "manual-verify-pod-001",
    "pod_sandbox.id": "5a571e489166e734b35de84c279a5889f750d07e01046c9852b0dcf0c7064578",
    "timestamp": "2026-04-20T09:30:19.041390528+08:00"
}
```