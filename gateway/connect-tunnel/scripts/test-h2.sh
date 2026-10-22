#!/usr/bin/env bash
# Phase 2: the same tunnel over HTTP/2 (curl --proxy-http2). Plain H2 CONNECT
# needs NO allow_connect — verified by running this with no patch applied.
# allow_connect only gates RFC 8441 extended CONNECT (":protocol"), see
# manifests/60-patchpolicy-h2.yaml.
set -euo pipefail
cd "$(dirname "$0")/.."

GW_SVC=$(kubectl get svc -n envoy-gateway-system \
  -l gateway.envoyproxy.io/owning-gateway-namespace=connect-tunnel,gateway.envoyproxy.io/owning-gateway-name=tunnel-gw \
  -o jsonpath='{.items[0].metadata.name}')
PROXY="https://${GW_SVC}.envoy-gateway-system.svc.cluster.local:443"
TOKEN=$(scripts/mint-jwt.sh)

# Use the device cert automatically when phase 3 (mTLS) is applied.
CERT=""
kubectl get clienttrafficpolicy -n connect-tunnel tunnel-mtls >/dev/null 2>&1 \
  && CERT="--proxy-cert /certs/tls.crt --proxy-key /certs/tls.key"

echo "--- CONNECT over HTTP/2 (expect 'CONNECT tunnel: HTTP/2' and response 200)"
kubectl exec -n connect-tunnel deploy/client -- \
  curl -sv -o /dev/null -w 'inner=%{http_code}\n' \
  --proxy-insecure --proxy-http2 -p -x "$PROXY" $CERT \
  --proxy-header "Proxy-Authorization: Bearer $TOKEN" \
  http://echo.internal.example:8080/ 2>&1 | grep -E 'CONNECT tunnel|established|inner='
