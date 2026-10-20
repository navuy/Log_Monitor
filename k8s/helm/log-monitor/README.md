# Log Monitor Helm Chart

This chart deploys the complete active Log Monitor stack.

```mermaid
flowchart LR
    Pods[Kubernetes workloads] --> Files[/var/log/containers]
    Files --> Streamer[k8s-log-streamer DaemonSet]
    Streamer --> Redis[(Redis Streams)]
    Redis --> Classifier[log-classifier]
    API[log-api-service] --> Postgres[(PostgreSQL rules)]
    Postgres --> Classifier
    Classifier --> Influx[(InfluxDB alerts)]
    Test[test-logger] --> Files
```

## Components

- Redis temporarily buffers one stream per Kubernetes container.
- `k8s-log-streamer` tails node container logs and publishes enriched entries.
- PostgreSQL persists keyword/regex rules and their scopes.
- `log-api-service` provides CRUD operations for rules.
- Two `log-classifier` replicas dynamically reload rules, share Redis work through `XREADGROUP`, recover stale pending entries, and write matches to InfluxDB.
- InfluxDB stores the `k8s_log_alerts` measurement.
- `test-logger` generates INFO, WARNING, ERROR, regex-probe, and authentication-failure samples.

The `log-monitor` namespace is ignored by the classifier, although its logs are still collected into Redis.

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
