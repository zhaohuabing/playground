#!/usr/bin/env bash
#
# Demo verifier: shows each request flowing through BOTH composer filters attached
# by envoyextensionpolicy.yaml. For every step it pauses for Enter, prints the exact
# curl command, then shows the response:
#   1. a normal request passes the WAF and is stamped by my-filter (200 + x-hello)
#   2. a SQL-injection request is blocked by the WAF (403)
#
# Uses port-forward so it works on clusters without an external LoadBalancer IP.
set -uo pipefail

HOSTNAME_VALUE="${HOSTNAME_OVERRIDE:-www.example.com}" # matches the quickstart HTTPRoute
LOCAL_PORT="${LOCAL_PORT:-8888}"
EXPECT_HEADER="x-hello"
EXPECT_VALUE="built-on-envoy"

# colors
B="\033[1m"; C="\033[1;36m"; Y="\033[1;33m"; G="\033[0;32m"; R="\033[0;31m"; N="\033[0m"

echo -e ">> Locating the envoy service for gateway 'eg'"
SVC="$(kubectl -n envoy-gateway-system get svc \
  -l gateway.envoyproxy.io/owning-gateway-name=eg \
  -o jsonpath='{.items[0].metadata.name}')"
if [ -z "${SVC}" ]; then
  echo -e "${R}ERROR: could not find the envoy service (is the Gateway programmed?)${N}" >&2
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

# run_step "title" "explanation" EXPECTED_CODE curl-args...
run_step() {
  local title="$1" explain="$2" expect="$3"; shift 3
  echo
  echo -e "${C}════════════════════════════════════════════════════════════════${N}"
  echo -e "${C}${B} ${title}${N}"
  echo -e "   ${explain}"
  echo -e "${C}════════════════════════════════════════════════════════════════${N}"
  printf "${Y}Press Enter to send the request...${N}"; read -r _ || true

  # show the exact command (quote only args containing spaces, for readability)
  local pretty="curl -s -i" a
  for a in "$@"; do
    case "$a" in
      *" "*) pretty+=" \"$a\"" ;;
      *)     pretty+=" $a" ;;
    esac
  done
  echo -e "${G}\$ ${pretty}${N}"
  echo

  # run and show the response
  local out code; out="$(curl -s -i "$@")"
  echo "${out}"
  code="$(printf '%s' "${out}" | head -n1 | awk '{print $2}')"

  echo
  if [ "${code}" = "${expect}" ]; then
    echo -e "${G}✔ got HTTP ${code} (expected ${expect})${N}"
  else
    echo -e "${R}✗ got HTTP ${code} (expected ${expect})${N}"; fail=1
  fi
  # header check on the normal request
  if [ "${expect}" = "200" ]; then
    if printf '%s' "${out}" | grep -iq "^${EXPECT_HEADER}: ${EXPECT_VALUE}"; then
      echo -e "${G}✔ response carries '${EXPECT_HEADER}: ${EXPECT_VALUE}' (my-filter ran)${N}"
    else
      echo -e "${R}✗ missing '${EXPECT_HEADER}: ${EXPECT_VALUE}'${N}"; fail=1
    fi
  fi
}

run_step "Test 1 — normal request" \
  "Passes the WAF, reaches the backend, and my-filter stamps x-hello on the response." \
  200 \
  -H "Host: ${HOSTNAME_VALUE}" "${BASE}/get"

run_step "Test 2 — SQL-injection request" \
  "The WAF (OWASP CRS) detects the payload and blocks it with 403 before it reaches the backend." \
  403 \
  -H "Host: ${HOSTNAME_VALUE}" "${BASE}/post" -X POST --data "1%27%20ORDER%20BY%203--%2B"

echo
echo -e "${C}════════════════════════════════════════════════════════════════${N}"
if [ "${fail}" -eq 0 ]; then
  echo -e "${G}${B}PASS: request path traverses coraza-waf AND my-filter.${N}"
else
  echo -e "${R}${B}FAIL: see above.${N}" >&2
  echo "Debug: kubectl -n envoy-gateway-system logs -l gateway.envoyproxy.io/owning-gateway-name=eg -c envoy | grep -iE 'waf|my-filter'" >&2
  exit 1
fi
