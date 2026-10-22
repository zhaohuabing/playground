#!/usr/bin/env bash
# Deploy phase 1 (H1 CONNECT + JWT). The H2 patch (60-*) is applied separately
# by test-h2.sh.
set -euo pipefail
cd "$(dirname "$0")/.."

kubectl apply -f manifests/00-namespace.yaml
scripts/gen-keys.sh
for f in manifests/[0-5]*.yaml; do
  kubectl apply -f "$f"
done

kubectl -n connect-tunnel rollout status deploy/echo deploy/client deploy/jwks --timeout=180s
kubectl -n connect-tunnel wait --for=condition=Programmed gateway/tunnel-gw --timeout=120s
kubectl -n connect-tunnel get gateway,httproute,backendtrafficpolicy,securitypolicy
