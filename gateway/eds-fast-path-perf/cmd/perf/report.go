package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func reportCmd(args []string) error {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	dir := fs.String("dir", "results/run", "run directory holding <mode>/summary.json")
	modes := fs.String("modes", "full,fast", "modes to compare, in table order")
	_ = fs.Parse(args)

	bySc := map[string][]summary{}
	var order []string
	for _, m := range strings.Split(*modes, ",") {
		b, err := os.ReadFile(filepath.Join(*dir, m, "summary.json"))
		if err != nil {
			return err
		}
		var ss []summary
		if err := json.Unmarshal(b, &ss); err != nil {
			return err
		}
		for _, s := range ss {
			if !slices.Contains(order, s.Scenario) {
				order = append(order, s.Scenario)
			}
			bySc[s.Scenario] = append(bySc[s.Scenario], s)
		}
	}

	var b strings.Builder
	b.WriteString("## EDS update latency (EndpointSlice write → Envoy membership change)\n\n")
	b.WriteString("| Scenario | Mode | p50 (ms) | p90 (ms) | p99 (ms) | max (ms) | misses |\n|---|---|---:|---:|---:|---:|---:|\n")
	for _, sc := range order {
		for _, s := range bySc[sc] {
			fmt.Fprintf(&b, "| %s | %s | %.0f | %.0f | %.0f | %.0f | %d/%d |\n", sc, s.Mode, s.P50Ms, s.P90Ms, s.P99Ms, s.MaxMs, s.Misses, s.Samples)
		}
	}

	b.WriteString("\n## Control plane resources and pipeline\n\n")
	b.WriteString("| Scenario | Mode | CPU avg (cores) | CPU p95 (cores) | RSS avg/max (MiB) | heap max (MiB) | goroutines max | fast / full pubs | refresh skipped | fast refresh mean (ms) | snapshot updates | background ops |\n")
	b.WriteString("|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, sc := range order {
		for _, s := range bySc[sc] {
			fmt.Fprintf(&b, "| %s | %s | %.2f | %.2f | %.0f / %.0f | %.0f | %.0f | %.0f / %.0f | %.0f | %.2f | %.0f | %d |\n",
				sc, s.Mode, s.CPUAvgCores, s.CPUP95Cores, s.RSSAvgMiB, s.RSSMaxMiB, s.HeapMaxMiB, s.GoroutinesMax,
				s.FastPublications, s.FullPublications, s.RefreshSkipped, s.FastRefreshMeanMs, s.SnapshotUpdates, s.ChurnOps)
		}
	}

	b.WriteString("\n## Full translation per runner (handled updates, mean seconds)\n\n")
	var runners []string
	for _, sc := range order {
		for _, s := range bySc[sc] {
			for r := range s.Runners {
				if !slices.Contains(runners, r) {
					runners = append(runners, r)
				}
			}
		}
	}
	slices.Sort(runners)
	b.WriteString("| Scenario | Mode |")
	for _, r := range runners {
		fmt.Fprintf(&b, " %s |", r)
	}
	b.WriteString("\n|---|---|" + strings.Repeat("---:|", len(runners)) + "\n")
	for _, sc := range order {
		for _, s := range bySc[sc] {
			fmt.Fprintf(&b, "| %s | %s |", sc, s.Mode)
			for _, r := range runners {
				st := s.Runners[r]
				fmt.Fprintf(&b, " %.0f × %.2fs |", st.Count, st.MeanSec)
			}
			b.WriteString("\n")
		}
	}

	out := filepath.Join(*dir, "summary.md")
	if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
		return err
	}
	fmt.Print(b.String())
	return nil
}
