## EDS update latency (EndpointSlice write → Envoy membership change)

| Scenario | Mode | p50 (ms) | p90 (ms) | p99 (ms) | max (ms) | misses |
|---|---|---:|---:|---:|---:|---:|
| idle | full | 7080 | 7220 | 7288 | 7288 | 0/40 |
| idle | fast | 46 | 51 | 71 | 71 | 0/40 |
| endpoint-churn | full | 11749 | 12156 | 15388 | 15388 | 0/40 |
| endpoint-churn | fast | 49 | 62 | 86 | 86 | 0/40 |
| config-churn | full | 10174 | 10527 | 14031 | 14031 | 0/40 |
| config-churn | fast | 47 | 56 | 88 | 88 | 0/40 |
| ineligible | full | 7255 | 7390 | 7525 | 7525 | 0/40 |
| ineligible | fast | 7479 | 7661 | 7794 | 7794 | 0/40 |

## Control plane resources and pipeline

| Scenario | Mode | CPU avg (cores) | CPU p95 (cores) | RSS avg/max (MiB) | heap max (MiB) | goroutines max | fast / full pubs | refresh skipped | fast refresh mean (ms) | snapshot updates | background ops |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| idle | full | 0.97 | 1.60 | 738 / 863 | 667 | 447 | 0 / 0 | 0 | 0.00 | 40 | 0 |
| idle | fast | 1.69 | 2.21 | 923 / 1021 | 881 | 455 | 40 / 20 | 160040 | 19.73 | 60 | 0 |
| endpoint-churn | full | 3.91 | 4.24 | 1494 / 1859 | 1653 | 448 | 0 / 0 | 0 | 0.00 | 80 | 3148 |
| endpoint-churn | fast | 4.27 | 4.72 | 1724 / 2103 | 1762 | 455 | 752 / 18 | 141076 | 22.43 | 770 | 791 |
| config-churn | full | 1.77 | 2.19 | 895 / 982 | 813 | 447 | 0 / 0 | 0 | 0.00 | 81 | 281 |
| config-churn | fast | 1.96 | 2.73 | 1019 / 1120 | 919 | 455 | 40 / 21 | 168042 | 21.51 | 61 | 78 |
| ineligible | full | 0.99 | 1.64 | 832 / 924 | 746 | 447 | 0 / 0 | 0 | 0.00 | 40 | 0 |
| ineligible | fast | 1.03 | 1.80 | 923 / 1045 | 846 | 455 | 0 / 40 | 320100 | 0.03 | 40 | 0 |

## Full translation per runner (handled updates, mean seconds)

| Scenario | Mode | gateway-api | infrastructure | provider | xds |
|---|---|---:|---:|---:|---:|
| idle | full | 40 × 0.14s | 40 × 0.02s | 0 × 0.00s | 40 × 6.70s |
| idle | fast | 40 × 0.16s | 40 × 0.02s | 0 × 0.00s | 20 × 6.88s |
| endpoint-churn | full | 3070 × 0.17s | 3070 × 0.02s | 0 × 0.00s | 80 × 7.63s |
| endpoint-churn | fast | 711 × 0.17s | 712 × 0.02s | 0 × 0.00s | 18 × 7.96s |
| config-churn | full | 313 × 0.16s | 313 × 0.02s | 274 × 0.00s | 81 × 6.80s |
| config-churn | fast | 111 × 0.16s | 111 × 0.02s | 71 × 0.00s | 21 × 7.01s |
| ineligible | full | 40 × 0.15s | 40 × 0.02s | 40 × 0.00s | 40 × 6.70s |
| ineligible | fast | 40 × 0.15s | 40 × 0.02s | 40 × 0.00s | 40 × 6.97s |
