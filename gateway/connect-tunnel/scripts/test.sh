#!/usr/bin/env bash
# Full matrix, HTTP/2 only (the target transport — one multiplexed connection
# per client, one authenticated CONNECT stream per app connection).
# A) no device cert     -> TLS handshake rejected (alert: certificate required)
# B) cert, no token     -> 401, denied by the jwt filter
# C) cert, wrong target -> 403, denied by the authorization policy
# D) cert + valid token -> CONNECT tunnel established, response 200 + body
# On h2, curl never surfaces a rejected CONNECT's status or body (the stream
# just closes), so the auth error is read from the gateway itself: the denial
# counters on the proxy's prometheus endpoint, diffed around each request.
set -euo pipefail
cd "$(dirname "$0")/.."

SELECTOR="gateway.envoyproxy.io/owning-gateway-namespace=connect-tunnel,gateway.envoyproxy.io/owning-gateway-name=tunnel-gw"
GW_SVC=$(kubectl get svc -n envoy-gateway-system -l "$SELECTOR" \
  -o jsonpath='{.items[0].metadata.name}')
GW_POD_IP=$(kubectl get pod -n envoy-gateway-system -l "$SELECTOR" \
  -o jsonpath='{.items[0].status.podIP}')
PROXY="https://${GW_SVC}.envoy-gateway-system.svc.cluster.local:443"
TOKEN=$(scripts/mint-jwt.sh)
BAD=$(scripts/mint-jwt.sh other.internal.example:9999)
CERT="--proxy-cert /certs/tls.crt --proxy-key /certs/tls.key"

# "<jwt_authn_denied> <rbac_denied>" from the proxy's prometheus endpoint.
denials() {
  kubectl exec -n connect-tunnel deploy/client -- \
    curl -s --max-time 5 "http://${GW_POD_IP}:19001/stats/prometheus" 2>/dev/null \
    | awk '/^envoy_http_jwt_authn_denied/ {j=$2} /^envoy_http_rbac_denied/ {r=$2} END {print j+0, r+0}' \
    || echo "0 0"
}

expect_reject() {
  local before after err jb rb ja ra
  before=$(denials)
  if err=$(kubectl exec -n connect-tunnel deploy/client -- \
      curl -sS -o /dev/null --proxy-insecure --proxy-http2 -p -x "$PROXY" "$@" \
      http://echo.internal.example:8080/ 2>&1); then
    echo "UNEXPECTED: tunnel succeeded"
    return
  fi
  echo "$err" | grep -m1 '^curl:' || true
  after=$(denials)
  read -r jb rb <<<"$before"
  read -r ja ra <<<"$after"
  if [ "$ja" -gt "$jb" ]; then
    echo "gateway: denied by the jwt filter (401)"
  elif [ "$ra" -gt "$rb" ]; then
    echo "gateway: denied by the authorization policy (403)"
  else
    echo "gateway: no auth-filter denial — rejected at the TLS layer"
  fi
}

echo "--- A) no device cert (expect TLS alert: certificate required)"
expect_reject --proxy-header "Proxy-Authorization: Bearer $TOKEN"

echo "--- B) cert, no token (expect 401 from the jwt filter)"
expect_reject $CERT

echo "--- C) cert, token for another target (expect 403 from authorization)"
expect_reject $CERT --proxy-header "Proxy-Authorization: Bearer $BAD"

echo "--- D) cert + valid token (expect established, response 200 + echo body)"
kubectl exec -n connect-tunnel deploy/client -- \
  curl -sv --proxy-insecure --proxy-http2 -p -x "$PROXY" $CERT \
  --proxy-header "Proxy-Authorization: Bearer $TOKEN" \
  http://echo.internal.example:8080/ 2>&1 | grep -E 'CONNECT tunnel|"host"|"proto"'
