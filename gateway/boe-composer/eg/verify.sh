#!/usr/bin/env bash
#
# Verify the my-filter composer filter is active: send a request through the eg
# Gateway and assert the response carries the header stamped by the plugin.
#
# Uses port-forward so it works on clusters without an external LoadBalancer IP.
set -euo pipefail

NS="${NS:-default}"
HOSTNAME="${HOSTNAME_OVERRIDE:-www.example.com}" # matches the quickstart HTTPRoute
LOCAL_PORT="${LOCAL_PORT:-8888}"
EXPECT_HEADER="x-hello"
EXPECT_VALUE="built-on-envoy"

echo ">> Locating the envoy service for gateway 'eg'"
SVC="$(kubectl -n envoy-gateway-system get svc \
  -l gateway.envoyproxy.io/owning-gateway-name=eg \
  -o jsonpath='{.items[0].metadata.name}')"
if [ -z "${SVC}" ]; then
  echo "ERROR: could not find the envoy service (is the Gateway programmed?)" >&2
  exit 1
fi
echo "   service: ${SVC}"

echo ">> Port-forwarding svc/${SVC} ${LOCAL_PORT} -> 80"
kubectl -n envoy-gateway-system port-forward "svc/${SVC}" "${LOCAL_PORT}:80" >/dev/null 2>&1 &
PF_PID=$!
trap 'kill "${PF_PID}" 2>/dev/null || true' EXIT
sleep 3

echo ">> curl -i http://127.0.0.1:${LOCAL_PORT}/ (Host: ${HOSTNAME})"
RESP="$(curl -s -i -H "Host: ${HOSTNAME}" "http://127.0.0.1:${LOCAL_PORT}/")"
echo "----------------------------------------"
echo "${RESP}" | head -n 20
echo "----------------------------------------"

if echo "${RESP}" | grep -iq "^${EXPECT_HEADER}: ${EXPECT_VALUE}"; then
  echo "PASS: response carries '${EXPECT_HEADER}: ${EXPECT_VALUE}' -- the composer plugin is active."
else
  echo "FAIL: header '${EXPECT_HEADER}: ${EXPECT_VALUE}' not found." >&2
  echo "Debug tips:" >&2
  echo "  kubectl get envoyextensionpolicy my-filter-extension -n ${NS} -o yaml" >&2
  echo "  kubectl -n envoy-gateway-system logs -l gateway.envoyproxy.io/owning-gateway-name=eg -c envoy | grep my-filter" >&2
  exit 1
fi
