#!/usr/bin/env bash
# Phase 3 tests: mTLS (cert per device) + JWT (token per stream) together.
# Expected: A) no client cert -> TLS handshake rejected (curl exits nonzero),
# B) cert but no token -> CONNECT=401, C) cert + token -> CONNECT=200,
# D) same over HTTP/2.
set -euo pipefail
cd "$(dirname "$0")/.."

scripts/gen-keys.sh
kubectl apply -f manifests/15-client.yaml
kubectl apply -f manifests/70-ctp-mtls.yaml
kubectl -n connect-tunnel rollout status deploy/client --timeout=120s
sleep 5  # let xDS deliver the new listener TLS config

GW_SVC=$(kubectl get svc -n envoy-gateway-system \
  -l gateway.envoyproxy.io/owning-gateway-namespace=connect-tunnel,gateway.envoyproxy.io/owning-gateway-name=tunnel-gw \
  -o jsonpath='{.items[0].metadata.name}')
PROXY="https://${GW_SVC}.envoy-gateway-system.svc.cluster.local:443"
TOKEN=$(scripts/mint-jwt.sh)

run() {
  kubectl exec -n connect-tunnel deploy/client -- \
    curl -s -o /dev/null -w 'CONNECT=%{http_connect} inner=%{http_code}\n' \
    --proxy-insecure -p -x "$PROXY" "$@" http://echo.internal.example:8080/
}

echo "--- A) no client cert (expect TLS handshake failure)"
run --proxy-header "Proxy-Authorization: Bearer $TOKEN" \
  && echo "UNEXPECTED: handshake succeeded" || echo "rejected as expected"
echo "--- B) client cert, no token (expect CONNECT=401)"
run --proxy-cert /certs/tls.crt --proxy-key /certs/tls.key || true
echo "--- C) client cert + token (expect CONNECT=200 inner=200)"
run --proxy-cert /certs/tls.crt --proxy-key /certs/tls.key \
  --proxy-header "Proxy-Authorization: Bearer $TOKEN"
echo "--- D) same over HTTP/2"
kubectl exec -n connect-tunnel deploy/client -- \
  curl -sv -o /dev/null -w 'inner=%{http_code}\n' \
  --proxy-insecure --proxy-http2 -p -x "$PROXY" \
  --proxy-cert /certs/tls.crt --proxy-key /certs/tls.key \
  --proxy-header "Proxy-Authorization: Bearer $TOKEN" \
  http://echo.internal.example:8080/ 2>&1 | grep -E 'CONNECT tunnel|inner='
