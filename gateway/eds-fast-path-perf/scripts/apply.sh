#!/usr/bin/env bash
# Generate and apply the heavy config. Extra args go to cmd/gen, e.g. -routes 4000 -patch-policies 200.
set -euo pipefail
source "$(dirname "$0")/env.sh"
"$ROOT/bin/gen" "$@" > "$ROOT/config.yaml"
echo "generated $(grep -c '^kind:' "$ROOT/config.yaml") objects"
k apply --server-side --force-conflicts -f "$ROOT/config.yaml" >/dev/null
k -n perf wait --for=condition=Programmed gateway/eg --timeout 10m
