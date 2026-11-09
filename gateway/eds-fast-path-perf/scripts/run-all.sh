#!/usr/bin/env bash
# Run every scenario in full mode, then fast mode, and write the comparison.
# Extra args go to `perf run`, e.g. -samples 30.
set -euo pipefail
source "$(dirname "$0")/env.sh"
out="${OUT:-$ROOT/results/$(date +%Y%m%d-%H%M)}"
for mode in ${MODES:-full fast}; do
  "$HERE/mode.sh" "$mode"
  # Let the restarted EG finish its first full translation before measuring.
  sleep 30
  "$ROOT/bin/perf" run -context "$CONTEXT" -mode "$mode" -out "$out" "$@"
done
"$ROOT/bin/perf" report -dir "$out"
