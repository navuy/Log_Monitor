# Log Monitor

Log Monitor is a Kubernetes log collection and rule-based alerting system. It tails container logs from every node, buffers them in Redis Streams, dynamically loads keyword and regex rules from PostgreSQL, and stores matched alerts in InfluxDB.

```mermaid
flowchart LR
    Workloads[Kubernetes workloads] --> Logs[/var/log/containers]
    Logs --> Streamer[k8s-log-streamer]
    Streamer --> Redis[(Redis Streams)]
    Redis --> Classifier[log-classifier]
    API[log-api-service] --> Postgres[(PostgreSQL)]
    Postgres --> Classifier
    Classifier --> Influx[(InfluxDB)]
    Operator[Operator or UI] --> API
```

## Runtime flow

1. Kubernetes writes container stdout/stderr to node log files.
2. One `k8s-log-streamer` pod per node tails those files and uses `XADD` to publish enriched entries to Redis.
3. Two classifier replicas share a Redis consumer group and consume assigned entries with `XREADGROUP`.
4. Before matching, it discards entries from globally ignored namespaces such as `log-monitor`.
5. It applies enabled PostgreSQL rules in ID order and stops at the first match.
6. Matching entries are written to the InfluxDB measurement `k8s_log_alerts`; unmatched entries are not persisted there.
7. Successfully evaluated entries are acknowledged with `XACK`; failed InfluxDB writes remain pending for recovery.
8. Rule changes made through the API are picked up by the classifier on its next refresh without a redeployment.

## Active components

- [Kubernetes log streamer](go_pipeline/k8s-log-streamer/README.md)
- [Log classifier](go_pipeline/log_classifier/README.md)
- [Rule API service](go_pipeline/log_api_service/README.md)
- [Kubernetes and Helm deployment](k8s/helm/log-monitor/README.md)

## Infrastructure

- Redis provides bounded, temporary per-container streams.
- PostgreSQL stores rules and namespace/pod/container scope arrays.
- InfluxDB stores matched alerts for querying and visualization.
- The Helm test logger emits predictable messages for validating keyword and regex rules.

## Prototype and legacy components

- [Docker log streamer](go_pipeline/log_streamer/README.md) is the older Docker API collector.
- [Log generator](go_pipeline/log_generator/README.md) replays a text file into container stdout.
- [ML pipeline](ml_pipeline/README.md) contains disconnected TF-IDF and Isolation Forest experiments.

The ML prototype is not part of the current Helm-deployed runtime.

## Local deployment

The local script builds the three custom images, imports them into k3s containerd, updates Helm dependencies, installs the chart, and waits for the workloads:

```bash
./k8s/scripts/deploy-local-k3s.sh
```

See [k8s/README.md](k8s/README.md) for deployment details.

