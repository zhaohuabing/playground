#!/usr/bin/env bash
# Mint a short-lived RS256 JWT, standing in for the Tunnel-Controller issuing
# a per-target grant. Usage: mint-jwt.sh [target] [ttl-seconds]
set -euo pipefail
cd "$(dirname "$0")/.."

TARGET="${1:-echo.internal.example:8080}"
TTL="${2:-600}"

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }

HEADER=$(printf '{"alg":"RS256","typ":"JWT","kid":"tunnel-key"}' | b64url)
NOW=$(date +%s)
PAYLOAD=$(printf '{"iss":"tunnel-controller","sub":"robin","target":"%s","iat":%s,"exp":%s}' \
  "$TARGET" "$NOW" "$((NOW + TTL))" | b64url)
SIG=$(printf '%s.%s' "$HEADER" "$PAYLOAD" \
  | openssl dgst -sha256 -sign secrets/jwt.key -binary | b64url)

echo "$HEADER.$PAYLOAD.$SIG"
