# Kubernetes Deployment Layout

This folder contains the Kubernetes-facing deployment assets for the log monitor playground.

## Structure

- `helm/log-monitor`
  - Helm chart for Redis, `k8s-log-streamer`, and the optional test logger
- `scripts/deploy-local-k3s.sh`
  - Build the collector image, save it as a tarball, import it into k3s, and install the chart
- `scripts/undeploy-local-k3s.sh`
  - Remove the local k3s deployment

## Local k3s workflow

Use this when the cluster runs on the same machine and uses `containerd`.

```bash
chmod +x k8s/scripts/deploy-local-k3s.sh
./k8s/scripts/deploy-local-k3s.sh
```

This does five things:

1. builds `k8s-log-streamer:dev`
2. saves it as `k8s-log-streamer-dev.tar`
3. imports it into k3s with `sudo k3s ctr images import`
4. installs the Helm chart into the `log-monitor` namespace
5. restarts the collector pod so the DaemonSet uses the imported image

## Notes

- The image import step uses `sudo k3s ctr images import` because k3s owns the runtime.
- `values.local-k3s.yaml` sets `imagePullPolicy: Never` so the DaemonSet uses the locally imported image.
- The chart currently deploys:
  - Redis
  - `k8s-log-streamer` as a DaemonSet
  - a simple busybox test logger