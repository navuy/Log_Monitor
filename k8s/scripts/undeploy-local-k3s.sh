#!/usr/bin/env bash

set -euo pipefail

NAMESPACE="${NAMESPACE:-log-monitor}"
RELEASE_NAME="${RELEASE_NAME:-log-monitor}"

helm uninstall "$RELEASE_NAME" -n "$NAMESPACE" || true
kubectl delete namespace "$NAMESPACE" --ignore-not-found=true
kubectl delete namespace test-logger



