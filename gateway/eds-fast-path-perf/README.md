# EDS fast path latency test

Measures how long an EndpointSlice change takes to reach Envoy with Envoy Gateway's
`EndpointFastPath` runtime flag off (full translation) and on (fast path), under a
config where one full translation takes several seconds. It also records what the
control plane spends meanwhile (CPU, memory, translation counts).

Context: [envoyproxy/gateway#9777](https://github.com/envoyproxy/gateway/issues/9777),
[envoyproxy/gateway#10398](https://github.com/envoyproxy/gateway/pull/10398).

## How it works

- `cmd/gen` writes the heavy config: one Gateway, N HTTPRoutes with 4 rules each
  (4 Envoy clusters per route), SecurityPolicies, BackendTrafficPolicies, a
  ClientTrafficPolicy and EnvoyPatchPolicies. Every patch policy re-marshals the
  whole route configuration, which is what makes full translation slow, as in #9777.
  Backends are selector-less Services with hand-written EndpointSlices pointing at
  fake IPs, so the test controls every endpoint change and needs no pods.
- `cmd/perf run` changes the probe EndpointSlice and polls Envoy's admin API until
  Envoy has the new endpoint set. Latency is the time from just before the API write
  to the first poll that shows it. Envoy is read with an EDS-only `config_dump`
  filtered to the probe cluster (~10ms). A `/stats` regex poll scans every stat on
  Envoy's main thread (~160ms with 8000 clusters) and skews the result.
- During each scenario it scrapes EG `/metrics` every second for CPU, RSS, heap and
  goroutines, and diffs the pipeline counters (handled updates and mean time per
  runner, fast/full publications) from start to end.
- `cmd/perf report` writes `summary.md` comparing the modes.

### Scenarios

| Scenario | Probe change | Background load |
|---|---|---|
| `idle` | 3 ↔ 4 endpoints | none |
| `endpoint-churn` | 3 ↔ 4 endpoints | random route EndpointSlices flip 2 ↔ 3 endpoints at 5/s |
| `config-churn` | 3 ↔ 4 endpoints | a random HTTPRoute header value changes every 2s |
| `ineligible` | 1 ↔ 0 endpoints | none; the PR leaves this case to the full path, so both modes should match |

## Running

On a Linux box with docker, kind, helm, kubectl and Go:

```sh
# PR checkout to build EG from (see env.sh for the defaults)
git -C ~/workspace/gateway worktree add --detach ~/workspace/eg-fastpath <commit>
GOOS=linux GOARCH=amd64 go build -o bin/ ./cmd/...   # or build on the box
./scripts/setup.sh                                   # build image, kind cluster, install EG
./scripts/apply.sh -routes 2000 -patch-policies 15   # heavy config
./scripts/run-all.sh -samples 40                     # full mode, fast mode, report
```

`./scripts/mode.sh full|fast` switches the flag and waits for EG and Envoy to roll.
Calibrate with a short `perf run -mode fast -scenarios idle -samples 4`; the `xds`
column in the runner table is the time for one full xDS translation.

## Notes from running against #10398

- With the flag on, the new endpoint controller uses controller-runtime's default
  workqueue metrics, whose help text conflicts with EG's own, so `/metrics` returns
  500. The runs here patch the controller to use EG's `WorkqueueMetricsProvider`
  (`NewQueue` option), as the main controller does.
- `xds_endpoint_refresh_skipped_total{reason="already_applied"}` counts one skip per
  unchanged cluster on every full replay, so it grows by the cluster count on each
  full build.
