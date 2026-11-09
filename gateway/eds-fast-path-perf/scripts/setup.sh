#!/usr/bin/env bash
# Build EG from the PR checkout, create the kind cluster and install EG (full mode).
set -euo pipefail
source "$(dirname "$0")/env.sh"

cd "$EG_SRC"
if [[ "${SKIP_BUILD:-}" != 1 ]]; then
  make image.build.envoy-gateway IMAGE="$EG_IMAGE" TAG="$EG_TAG"
fi
kind get clusters | grep -qx "$CLUSTER" || kind create cluster --name "$CLUSTER" --wait 120s
kind load docker-image "$EG_IMAGE:$EG_TAG" --name "$CLUSTER"

make helm-generate.gateway-helm IMAGE="$EG_IMAGE" TAG="$EG_TAG" IMAGE_PULL_POLICY=IfNotPresent
helm --kube-context "$CONTEXT" upgrade --install eg charts/gateway-helm \
  -n "$EG_NS" --create-namespace -f "$HERE/values-common.yaml" --wait --timeout 5m
k -n "$EG_NS" rollout status deploy/envoy-gateway --timeout 5m
