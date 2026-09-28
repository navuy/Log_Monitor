# Log Monitor Helm Chart

This chart installs:

- Redis for stream storage
- `k8s-log-streamer` as a DaemonSet
- an optional noisy test logger deployment

## Local smoke test

Build the collector image:

```bash
docker build -t k8s-log-streamer:dev go_pipeline/k8s-log-streamer
```

Import it into k3s/containerd:

```bash
docker save k8s-log-streamer:dev | sudo ctr -n k8s.io images import -
```

Install the chart:

```bash
helm upgrade --install log-monitor ./k8s/helm/log-monitor -n log-monitor --create-namespace
```

Check the pods:

```bash
kubectl get pods -n log-monitor -o wide
```

Check collector logs:

```bash
kubectl logs -n log-monitor daemonset/log-monitor-log-monitor-k8s-log-streamer
```

Inspect Redis streams:

```bash
kubectl exec -n log-monitor deploy/log-monitor-log-monitor-redis -- redis-cli KEYS 'logs:k8s*'
kubectl exec -n log-monitor deploy/log-monitor-log-monitor-redis -- redis-cli XRANGE <stream-key> - +
```
