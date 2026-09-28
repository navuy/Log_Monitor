#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

STREAMER_IMAGE_NAME="${STREAMER_IMAGE_NAME:-k8s-log-streamer:dev}"
STREAMER_IMAGE_TAR_PATH="${STREAMER_IMAGE_TAR_PATH:-$ROOT_DIR/k8s-log-streamer-dev.tar}"
STREAMER_DOCKERFILE_PATH="${STREAMER_DOCKERFILE_PATH:-$ROOT_DIR/go_pipeline/k8s-log-streamer/dockerfile}"
STREAMER_BUILD_CONTEXT="${STREAMER_BUILD_CONTEXT:-$ROOT_DIR/go_pipeline/k8s-log-streamer}"

CLASSIFIER_IMAGE_NAME="${CLASSIFIER_IMAGE_NAME:-log-classifier:dev}"
CLASSIFIER_IMAGE_TAR_PATH="${CLASSIFIER_IMAGE_TAR_PATH:-$ROOT_DIR/log-classifier-dev.tar}"
CLASSIFIER_DOCKERFILE_PATH="${CLASSIFIER_DOCKERFILE_PATH:-$ROOT_DIR/go_pipeline/log_classifier/dockerfile}"
CLASSIFIER_BUILD_CONTEXT="${CLASSIFIER_BUILD_CONTEXT:-$ROOT_DIR/go_pipeline/log_classifier}"

API_IMAGE_NAME="${API_IMAGE_NAME:-log-api-service:dev}"
API_IMAGE_TAR_PATH="${API_IMAGE_TAR_PATH:-$ROOT_DIR/log-api-service-dev.tar}"
API_DOCKERFILE_PATH="${API_DOCKERFILE_PATH:-$ROOT_DIR/go_pipeline/log_api_service/dockerfile}"
API_BUILD_CONTEXT="${API_BUILD_CONTEXT:-$ROOT_DIR/go_pipeline/log_api_service}"

NAMESPACE="${NAMESPACE:-log-monitor}"
TEST_LOGGER_NAMESPACE="${TEST_LOGGER_NAMESPACE:-test-logger}"
RELEASE_NAME="${RELEASE_NAME:-log-monitor}"
CHART_PATH="${CHART_PATH:-$ROOT_DIR/k8s/helm/log-monitor}"
VALUES_FILE="${VALUES_FILE:-$CHART_PATH/values.local-k3s.yaml}"

INFLUXDB_STATEFULSET="${INFLUXDB_STATEFULSET:-${RELEASE_NAME}-influxdb2}"
INFLUXDB_SERVICE="${INFLUXDB_SERVICE:-${RELEASE_NAME}-influxdb2}"
POSTGRES_DEPLOYMENT="${POSTGRES_DEPLOYMENT:-${RELEASE_NAME}-postgres}"

echo "Building image: $STREAMER_IMAGE_NAME"
docker build -t "$STREAMER_IMAGE_NAME" -f "$STREAMER_DOCKERFILE_PATH" "$STREAMER_BUILD_CONTEXT"

echo "Saving image tarball: $STREAMER_IMAGE_TAR_PATH"
docker save "$STREAMER_IMAGE_NAME" -o "$STREAMER_IMAGE_TAR_PATH"

echo "Importing streamer image into k3s containerd"
sudo k3s ctr images import "$STREAMER_IMAGE_TAR_PATH"

echo "Building image: $CLASSIFIER_IMAGE_NAME"
docker build -t "$CLASSIFIER_IMAGE_NAME" -f "$CLASSIFIER_DOCKERFILE_PATH" "$CLASSIFIER_BUILD_CONTEXT"

echo "Saving image tarball: $CLASSIFIER_IMAGE_TAR_PATH"
docker save "$CLASSIFIER_IMAGE_NAME" -o "$CLASSIFIER_IMAGE_TAR_PATH"

echo "Importing classifier image into k3s containerd"
sudo k3s ctr images import "$CLASSIFIER_IMAGE_TAR_PATH"

echo "Building image: $API_IMAGE_NAME"
docker build -t "$API_IMAGE_NAME" -f "$API_DOCKERFILE_PATH" "$API_BUILD_CONTEXT"

echo "Saving image tarball: $API_IMAGE_TAR_PATH"
docker save "$API_IMAGE_NAME" -o "$API_IMAGE_TAR_PATH"

echo "Importing API image into k3s containerd"
sudo k3s ctr images import "$API_IMAGE_TAR_PATH"

echo "Updating Helm dependencies"
helm dependency update "$CHART_PATH"

echo "Ensuring namespaces exist"
kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace "$TEST_LOGGER_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

echo "Deploying Helm release: $RELEASE_NAME"
helm upgrade --install "$RELEASE_NAME" "$CHART_PATH" \
  --namespace "$NAMESPACE" \
  --create-namespace \
  -f "$VALUES_FILE"

echo "Waiting for deployments, daemonset, and InfluxDB to become ready"
kubectl rollout status -n "$NAMESPACE" deployment/log-monitor-redis --timeout=120s
kubectl rollout status -n "$NAMESPACE" deployment/"$POSTGRES_DEPLOYMENT" --timeout=120s
kubectl rollout status -n "$TEST_LOGGER_NAMESPACE" deployment/log-monitor-test-logger --timeout=120s
kubectl rollout status -n "$NAMESPACE" daemonset/log-monitor-k8s-log-streamer --timeout=120s
kubectl rollout status -n "$NAMESPACE" deployment/log-monitor-log-classifier --timeout=120s
kubectl rollout status -n "$NAMESPACE" deployment/log-monitor-log-api-service --timeout=120s
kubectl rollout status -n "$NAMESPACE" statefulset/"$INFLUXDB_STATEFULSET" --timeout=180s

echo
echo "Current pods in namespace $NAMESPACE:"
kubectl get pods -n "$NAMESPACE" -o wide

echo
echo "Current pods in namespace $TEST_LOGGER_NAMESPACE:"
kubectl get pods -n "$TEST_LOGGER_NAMESPACE" -o wide

echo
echo "Services in namespace $NAMESPACE:"
kubectl get svc -n "$NAMESPACE"

echo
echo "PVCs in namespace $NAMESPACE:"
kubectl get pvc -n "$NAMESPACE"

echo
echo "InfluxDB health:"
kubectl exec -n "$NAMESPACE" statefulset/"$INFLUXDB_STATEFULSET" -- influx ping || true

echo
echo "Postgres logs:"
kubectl logs -n "$NAMESPACE" deployment/"$POSTGRES_DEPLOYMENT" --tail=10 || true

echo
echo "Collector pod logs:"
kubectl logs -n "$NAMESPACE" daemonset/log-monitor-k8s-log-streamer --tail=10 || true

echo
echo "Log classifier logs:"
kubectl logs -n "$NAMESPACE" deployment/log-monitor-log-classifier --tail=10 || true

echo
echo "Log API service logs:"
kubectl logs -n "$NAMESPACE" deployment/log-monitor-log-api-service --tail=10 || true

echo
echo "Test logger logs:"
kubectl logs -n "$TEST_LOGGER_NAMESPACE" deployment/log-monitor-test-logger --tail=10 || true

echo
echo "InfluxDB logs:"
kubectl logs -n "$NAMESPACE" statefulset/"$INFLUXDB_STATEFULSET" --tail=10 || true

echo
echo "Redis stream keys:"
kubectl exec -n "$NAMESPACE" deploy/log-monitor-redis -- redis-cli KEYS 'logs:k8s*' || true

echo
echo "InfluxDB service URL inside cluster:"
echo "http://$INFLUXDB_SERVICE.$NAMESPACE.svc.cluster.local"

echo
echo "Log API service URL inside cluster:"
echo "http://log-monitor-log-api-service.$NAMESPACE.svc.cluster.local:8080"
