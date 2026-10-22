#!/usr/bin/env bash
# Full matrix, HTTP/2 only (the target transport — one multiplexed connection
# per client, one authenticated CONNECT stream per app connection).
# A) no device cert     -> TLS handshake rejected (alert: certificate required)
# B) cert, no token     -> CONNECT rejected (401 from the jwt filter)
# C) cert, wrong target -> CONNECT rejected (403 from the authorization rules)
# D) cert + valid token -> CONNECT tunnel established, response 200 + body
# Note: on h2, curl never surfaces a rejected CONNECT's status code (the
# stream just closes), so B and C assert the rejection and show curl's error;
# the 401/403 split is visible in gateway access logs with a standard format.
set -euo pipefail
cd "$(dirname "$0")/.."

GW_SVC=$(kubectl get svc -n envoy-gateway-system \
  -l gateway.envoyproxy.io/owning-gateway-namespace=connect-tunnel,gateway.envoyproxy.io/owning-gateway-name=tunnel-gw \
  -o jsonpath='{.items[0].metadata.name}')
PROXY="https://${GW_SVC}.envoy-gateway-system.svc.cluster.local:443"
TOKEN=$(scripts/mint-jwt.sh)
BAD=$(scripts/mint-jwt.sh other.internal.example:9999)
CERT="--proxy-cert /certs/tls.crt --proxy-key /certs/tls.key"

expect_reject() {
  local err
  if err=$(kubectl exec -n connect-tunnel deploy/client -- \
      curl -sS -o /dev/null --proxy-insecure --proxy-http2 -p -x "$PROXY" "$@" \
      http://echo.internal.example:8080/ 2>&1); then
    echo "UNEXPECTED: tunnel succeeded"
  else
    echo "rejected as expected"
    echo "$err" | grep -m1 '^curl:' || echo "$err" | tail -1
  fi
}

echo "--- A) no device cert (expect TLS alert: certificate required)"
expect_reject --proxy-header "Proxy-Authorization: Bearer $TOKEN"

echo "--- B) cert, no token (expect rejection by the jwt filter)"
expect_reject $CERT

echo "--- C) cert, token for another target (expect rejection by authorization)"
expect_reject $CERT --proxy-header "Proxy-Authorization: Bearer $BAD"

echo "--- D) cert + valid token (expect established, response 200 + echo body)"
kubectl exec -n connect-tunnel deploy/client -- \
  curl -sv --proxy-insecure --proxy-http2 -p -x "$PROXY" $CERT \
  --proxy-header "Proxy-Authorization: Bearer $TOKEN" \
  http://echo.internal.example:8080/ 2>&1 | grep -E 'CONNECT tunnel|"host"|"proto"'
