# Kubernetes Log Streamer

The Kubernetes log streamer is the collection layer of Log Monitor. It runs as a DaemonSet, tails container log files on each Kubernetes node, enriches every line with Kubernetes metadata, and publishes it to Redis Streams.

```mermaid
flowchart LR
    A[Container stdout/stderr] --> B[/var/log/containers/*.log]
    B --> C[k8s-log-streamer DaemonSet]
    C --> D[Parse CRI line and filename]
    D --> E[Redis Stream per container]
```

## How it works

1. The collector scans `LOG_DIR` for `*.log` files every `DISCOVERY_INTERVAL`.
2. Each newly discovered file receives its own tailing goroutine.
3. `START_FROM_END=true` makes the collector process only lines appended after startup.
4. The CRI line supplies timestamp, stdout/stderr stream, tag, and message.
5. The filename supplies namespace, pod, container, and container ID.
6. The entry is added to Redis with `XADD`.
7. A mutex protects the shared map that prevents the same file from being tailed twice.

The Redis key format is:

```text
logs:k8s:ns:<namespace>:pod:<pod>:container:<container>
```

Each entry contains `namespace`, `pod_name`, `container_name`, `container_id`, `log`, `raw`, `stream`, `tag`, `timestamp`, and `ingested_at`.

The stream is approximately trimmed to `STREAM_MAXLEN`, and its expiry is refreshed using `STREAM_IDLE_TTL`. The streamer currently collects all namespaces; the classifier later ignores configured namespaces such as `log-monitor`.

## Configuration

| Variable | Default | Purpose |
|---|---:|---|
| `REDIS_ADDR` | `redis:6379` | Redis endpoint |
| `LOG_DIR` | `/var/log/containers` | Kubernetes container log directory |
| `DISCOVERY_INTERVAL` | `10s` | File discovery interval |
| `READ_INTERVAL` | `1s` | Wait after reaching end of a file |
| `STREAM_MAXLEN` | `10000` | Approximate maximum entries per stream |
| `STREAM_IDLE_TTL` | `24h` | Expiry for inactive streams |
| `START_FROM_END` | `true` | Ignore existing file contents at startup |
| `STREAM_KEY_PREFIX` | `logs:k8s` | Redis key prefix |

## Failure behavior

- Redis must be reachable when the process starts.
- A failed Redis write is logged; that individual log line is not retried.
- File truncation is detected and the file is reopened from the beginning.
- The tracked-file entry is removed when a tailing goroutine exits, allowing later rediscovery.

