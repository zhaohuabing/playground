#!/usr/bin/env bash
# Phase 1 tests: CONNECT over HTTP/1.1 with per-stream JWT.
# Expected: 1) no token -> CONNECT 401, 2) wrong target claim -> CONNECT 403,
# 3) good token -> CONNECT 200 and the echo body comes back through the tunnel.
set -euo pipefail
cd "$(dirname "$0")/.."

GW_SVC=$(kubectl get svc -n envoy-gateway-system \
  -l gateway.envoyproxy.io/owning-gateway-namespace=connect-tunnel,gateway.envoyproxy.io/owning-gateway-name=tunnel-gw \
  -o jsonpath='{.items[0].metadata.name}')
PROXY="https://${GW_SVC}.envoy-gateway-system.svc.cluster.local:443"
echo "proxy: $PROXY"

TOKEN=$(scripts/mint-jwt.sh)
BAD=$(scripts/mint-jwt.sh other.internal.example:9999)

run() {
  kubectl exec -n connect-tunnel deploy/client -- \
    curl -s -o /dev/null -w 'CONNECT=%{http_connect} inner=%{http_code}\n' \
    --proxy-insecure -p -x "$PROXY" "$@" http://echo.internal.example:8080/
}

echo "--- 1) no token (expect CONNECT=401)"
run || true
echo "--- 2) wrong target claim (expect CONNECT=403)"
run --proxy-header "Proxy-Authorization: Bearer $BAD" || true
echo "--- 3) good token (expect CONNECT=200 inner=200)"
run --proxy-header "Proxy-Authorization: Bearer $TOKEN"
echo "--- 3b) echo body through the tunnel"
kubectl exec -n connect-tunnel deploy/client -- \
  curl -s --proxy-insecure -p -x "$PROXY" \
  --proxy-header "Proxy-Authorization: Bearer $TOKEN" \
  http://echo.internal.example:8080/ | head -8
