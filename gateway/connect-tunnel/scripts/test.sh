#!/usr/bin/env bash
# Full matrix, HTTP/2 only (the target transport — one multiplexed connection
# per client, one authenticated CONNECT stream per app connection).
# A) no device cert     -> TLS handshake rejected
# B) cert, no token     -> CONNECT rejected (401 from the jwt filter)
# C) cert, wrong target -> CONNECT rejected (403 from the authorization rules)
# D) cert + valid token -> CONNECT tunnel established, response 200 + body
# Note: on h2, curl reports a rejected CONNECT only as exit 56 and never
# surfaces the status code, so B and C assert the rejection itself.
set -euo pipefail
cd "$(dirname "$0")/.."

GW_SVC=$(kubectl get svc -n envoy-gateway-system \
  -l gateway.envoyproxy.io/owning-gateway-namespace=connect-tunnel,gateway.envoyproxy.io/owning-gateway-name=tunnel-gw \
  -o jsonpath='{.items[0].metadata.name}')
PROXY="https://${GW_SVC}.envoy-gateway-system.svc.cluster.local:443"
TOKEN=$(scripts/mint-jwt.sh)
BAD=$(scripts/mint-jwt.sh other.internal.example:9999)
CERT="--proxy-cert /certs/tls.crt --proxy-key /certs/tls.key"

h2() {
  kubectl exec -n connect-tunnel deploy/client -- \
    curl -s -o /dev/null --proxy-insecure --proxy-http2 -p -x "$PROXY" "$@" \
    http://echo.internal.example:8080/
}

expect_reject() {
  if h2 "$@" >/dev/null 2>&1; then
    echo "UNEXPECTED: tunnel succeeded"
  else
    echo "rejected as expected (curl exit $?)"
  fi
}

echo "--- A) no device cert (expect TLS handshake failure)"
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
