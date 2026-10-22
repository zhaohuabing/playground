#!/usr/bin/env bash
# Deploy the whole scenario: gateway, backend, JWKS server, CONNECT route,
# JWT SecurityPolicy, and mTLS.
set -euo pipefail
cd "$(dirname "$0")/.."

kubectl apply -f manifests/00-namespace.yaml
scripts/gen-keys.sh
kubectl apply -f manifests/

kubectl -n connect-tunnel rollout status deploy/echo deploy/client deploy/jwks --timeout=180s
kubectl -n connect-tunnel wait --for=condition=Programmed gateway/tunnel-gw --timeout=120s
kubectl -n connect-tunnel get gateway,httproute,backendtrafficpolicy,securitypolicy,clienttrafficpolicy
