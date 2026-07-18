#!/usr/bin/env bash
#
# Verify a request flows through BOTH composer filters attached by
# envoyextensionpolicy.yaml:
#   1. a normal request passes the WAF and is stamped by my-filter (200 + x-hello)
#   2. a SQL-injection request is blocked by the WAF (403)
#
# Uses port-forward so it works on clusters without an external LoadBalancer IP.
set -euo pipefail

HOSTNAME_VALUE="${HOSTNAME_OVERRIDE:-www.example.com}" # matches the quickstart HTTPRoute
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

BASE="http://127.0.0.1:${LOCAL_PORT}"
fail=0

echo "---------------------------------------------------------------"
echo ">> 1) Normal request -> expect 200 and '${EXPECT_HEADER}: ${EXPECT_VALUE}'"
NORMAL="$(curl -s -i -H "Host: ${HOSTNAME_VALUE}" "${BASE}/get")"
NCODE="$(printf '%s' "${NORMAL}" | head -n1 | awk '{print $2}')"
if [ "${NCODE}" = "200" ] && printf '%s' "${NORMAL}" | grep -iq "^${EXPECT_HEADER}: ${EXPECT_VALUE}"; then
  echo "   PASS: allowed by WAF (200) and stamped by my-filter."
else
  echo "   FAIL: expected 200 + header, got ${NCODE}." >&2; fail=1
fi

echo "---------------------------------------------------------------"
echo ">> 2) SQL-injection request -> expect 403 (blocked by the WAF)"
ATTACK="$(curl -s -i -H "Host: ${HOSTNAME_VALUE}" "${BASE}/post" -X POST --data "1%27%20ORDER%20BY%203--%2B")"
ACODE="$(printf '%s' "${ATTACK}" | head -n1 | awk '{print $2}')"
if [ "${ACODE}" = "403" ]; then
  echo "   PASS: blocked by the WAF (403)."
else
  echo "   FAIL: expected 403, got ${ACODE} (is coraza-waf enforcing? SecRuleEngine On?)." >&2; fail=1
fi

echo "---------------------------------------------------------------"
if [ "${fail}" -eq 0 ]; then
  echo "PASS: request path traverses coraza-waf AND my-filter."
else
  echo "FAIL: see above." >&2
  echo "Debug tips:" >&2
  echo "  kubectl get envoyextensionpolicy my-filter-extension -o yaml" >&2
  echo "  kubectl -n envoy-gateway-system logs -l gateway.envoyproxy.io/owning-gateway-name=eg -c envoy | grep -iE 'waf|my-filter'" >&2
  exit 1
fi
