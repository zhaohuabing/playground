#!/usr/bin/env bash
# Switch EG between full (flag off) and fast (EndpointFastPath on) and restart it.
set -euo pipefail
source "$(dirname "$0")/env.sh"
mode="${1:?usage: mode.sh full|fast}"
extra=()
[[ "$mode" == fast ]] && extra=(-f "$HERE/values-fast.yaml")
helm --kube-context "$CONTEXT" upgrade eg "$EG_SRC/charts/gateway-helm" -n "$EG_NS" \
  -f "$HERE/values-common.yaml" "${extra[@]}" --wait --timeout 5m
k -n "$EG_NS" rollout restart deploy/envoy-gateway
k -n "$EG_NS" rollout status deploy/envoy-gateway --timeout 10m
# A new EG build can re-render the Envoy Deployment; wait so we don't measure a pod that is going away.
sleep 10
for d in $(k -n "$EG_NS" get deploy -l gateway.envoyproxy.io/owning-gateway-name=eg -o name); do
  k -n "$EG_NS" rollout status "$d" --timeout 10m
done
k -n "$EG_NS" get cm envoy-gateway-config -o jsonpath='{.data.envoy-gateway\.yaml}' | grep -A3 runtimeFlags || echo "runtimeFlags: none (full path)"
