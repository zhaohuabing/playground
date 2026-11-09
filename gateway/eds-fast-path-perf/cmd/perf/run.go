package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type runOptions struct {
	mode          string
	scenarios     string
	out           string
	kubeContext   string
	ns            string
	gateway       string
	systemNS      string
	samples       int
	interval      time.Duration
	timeout       time.Duration
	pollEvery     time.Duration
	churnRate     float64
	configEvery   time.Duration
	quietFor      time.Duration
	warmup        time.Duration
	envoyLocal    int
	egLocal       int
	metricsSample time.Duration
}

// scenario describes one measurement: which probe slice to toggle, between
// which endpoint counts, and what background load runs meanwhile.
type scenario struct {
	name          string
	backend       string
	ipSet         int
	counts        [2]int
	endpointChurn bool
	configChurn   bool
}

var allScenarios = []scenario{
	{name: "idle", backend: "probe", ipSet: 0, counts: [2]int{3, 4}},
	{name: "endpoint-churn", backend: "probe", ipSet: 0, counts: [2]int{3, 4}, endpointChurn: true},
	{name: "config-churn", backend: "probe", ipSet: 0, counts: [2]int{3, 4}, configChurn: true},
	// 0 <-> 1 endpoints is left to the full path by design, so fast should match full here.
	{name: "ineligible", backend: "probe-inel", ipSet: 1, counts: [2]int{1, 0}},
}

type latencySample struct {
	Scenario  string
	Seq       int
	From, To  int
	T0        time.Time
	LatencyMs float64
	Missed    bool
}

type runnerStat struct {
	Count   float64 `json:"count"`
	MeanSec float64 `json:"meanSec"`
}

type summary struct {
	Mode     string `json:"mode"`
	Scenario string `json:"scenario"`
	Samples  int    `json:"samples"`
	Misses   int    `json:"misses"`

	P50Ms  float64 `json:"p50Ms"`
	P90Ms  float64 `json:"p90Ms"`
	P99Ms  float64 `json:"p99Ms"`
	MaxMs  float64 `json:"maxMs"`
	MeanMs float64 `json:"meanMs"`

	CPUAvgCores   float64 `json:"cpuAvgCores"`
	CPUP95Cores   float64 `json:"cpuP95Cores"`
	RSSAvgMiB     float64 `json:"rssAvgMiB"`
	RSSMaxMiB     float64 `json:"rssMaxMiB"`
	HeapMaxMiB    float64 `json:"heapMaxMiB"`
	GoroutinesMax float64 `json:"goroutinesMax"`

	Runners           map[string]runnerStat `json:"runners"`
	FastPublications  float64               `json:"fastPublications"`
	FullPublications  float64               `json:"fullPublications"`
	RefreshSkipped    float64               `json:"refreshSkipped"`
	FastRefreshMeanMs float64               `json:"fastRefreshMeanMs"`
	SnapshotUpdates   float64               `json:"snapshotUpdates"`
	EnvoyMemMiB       float64               `json:"envoyMemMiB"`
	ChurnOps          int64                 `json:"churnOps"`
	DurationSec       float64               `json:"durationSec"`
}

func runCmd(args []string) error {
	var o runOptions
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	fs.StringVar(&o.mode, "mode", "", "label for this EG setup: full or fast (required)")
	fs.StringVar(&o.scenarios, "scenarios", "idle,endpoint-churn,config-churn,ineligible", "comma-separated scenarios")
	fs.StringVar(&o.out, "out", "results/run", "output directory; files go to <out>/<mode>/")
	fs.StringVar(&o.kubeContext, "context", "", "kubeconfig context (default current)")
	fs.StringVar(&o.ns, "namespace", "perf", "namespace of the generated config")
	fs.StringVar(&o.gateway, "gateway", "eg", "Gateway name")
	fs.StringVar(&o.systemNS, "system-namespace", "envoy-gateway-system", "namespace of EG and the Envoy pods")
	fs.IntVar(&o.samples, "samples", 60, "endpoint changes measured per scenario")
	fs.DurationVar(&o.interval, "interval", 3*time.Second, "pause between probes (plus up to 1s jitter)")
	fs.DurationVar(&o.timeout, "timeout", 60*time.Second, "give up on one probe after this long")
	fs.DurationVar(&o.pollEvery, "poll", 5*time.Millisecond, "Envoy admin poll interval")
	fs.Float64Var(&o.churnRate, "churn-rate", 5, "background EndpointSlice updates per second (endpoint-churn)")
	fs.DurationVar(&o.configEvery, "config-every", 2*time.Second, "background HTTPRoute edit interval (config-churn)")
	fs.DurationVar(&o.quietFor, "quiet", 10*time.Second, "wait for EG to be idle this long before each scenario")
	fs.DurationVar(&o.warmup, "warmup", 15*time.Second, "background load runs this long before probes start")
	fs.IntVar(&o.envoyLocal, "envoy-port", 19100, "local port for the Envoy admin port-forward")
	fs.IntVar(&o.egLocal, "eg-port", 19101, "local port for the EG metrics port-forward")
	fs.DurationVar(&o.metricsSample, "metrics-every", time.Second, "EG resource sampling interval")
	_ = fs.Parse(args)
	if o.mode == "" {
		return fmt.Errorf("-mode is required")
	}

	sigCtx, stopSig := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stopSig()
	ctx, cancel := context.WithCancelCause(sigCtx)
	defer cancel(nil)
	pfDied := func(err error) { cancel(err) }

	k, err := newKube(o.kubeContext)
	if err != nil {
		return err
	}
	envoyPod, err := k.runningPod(ctx, o.systemNS, fmt.Sprintf(
		"gateway.envoyproxy.io/owning-gateway-name=%s,gateway.envoyproxy.io/owning-gateway-namespace=%s", o.gateway, o.ns))
	if err != nil {
		return err
	}
	egPod, err := k.runningPod(ctx, o.systemNS, "control-plane=envoy-gateway")
	if err != nil {
		return err
	}
	log.Printf("envoy pod %s, eg pod %s", envoyPod, egPod)
	if err := k.portForward(ctx, o.systemNS, envoyPod, o.envoyLocal, 19000, pfDied); err != nil {
		return err
	}
	if err := k.portForward(ctx, o.systemNS, egPod, o.egLocal, 19001, pfDied); err != nil {
		return err
	}
	envoy := envoyAdmin{base: fmt.Sprintf("http://127.0.0.1:%d", o.envoyLocal)}
	eg := egMetrics{base: fmt.Sprintf("http://127.0.0.1:%d", o.egLocal)}

	dir := filepath.Join(o.out, o.mode)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	var summaries []summary
	var latencies []latencySample
	for _, name := range strings.Split(o.scenarios, ",") {
		idx := slices.IndexFunc(allScenarios, func(s scenario) bool { return s.name == name })
		if idx < 0 {
			return fmt.Errorf("unknown scenario %q", name)
		}
		sum, lat, err := runScenario(ctx, o, k, envoy, eg, allScenarios[idx], dir)
		if cause := context.Cause(ctx); cause != nil {
			return fmt.Errorf("scenario %s aborted: %w", name, cause)
		}
		if err != nil {
			return fmt.Errorf("scenario %s: %w", name, err)
		}
		summaries = append(summaries, sum)
		latencies = append(latencies, lat...)
		log.Printf("[%s/%s] p50=%.0fms p90=%.0fms p99=%.0fms max=%.0fms misses=%d cpu=%.2f cores rss=%.0fMiB",
			o.mode, name, sum.P50Ms, sum.P90Ms, sum.P99Ms, sum.MaxMs, sum.Misses, sum.CPUAvgCores, sum.RSSMaxMiB)
	}

	if err := writeLatencies(filepath.Join(dir, "latencies.csv"), o.mode, latencies); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(summaries, "", "  ")
	return os.WriteFile(filepath.Join(dir, "summary.json"), b, 0o644)
}

func probeIPs(set, n int) []string {
	ips := make([]string, n)
	for k := range n {
		ips[k] = fmt.Sprintf("10.199.%d.%d", set, k+1)
	}
	return ips
}

func clusterName(ns, backend string) string {
	// Probe routes have one rule; EG names its cluster httproute/<ns>/<name>/rule/0.
	return fmt.Sprintf("httproute/%s/%s/rule/0", ns, backend)
}

// waitFor polls Envoy until the cluster has want endpoints or the timeout passes.
func waitFor(ctx context.Context, envoy envoyAdmin, cluster string, want int64, poll, timeout time.Duration) (time.Time, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if v, err := envoy.endpointCount(cluster); err == nil && v == want {
			return time.Now(), true
		}
		select {
		case <-ctx.Done():
			return time.Time{}, false
		case <-time.After(poll):
		}
	}
	return time.Time{}, false
}

// waitQuiet waits until EG has handled no new watchable messages for o.quietFor.
func waitQuiet(ctx context.Context, eg egMetrics, quietFor time.Duration) {
	deadline := time.Now().Add(10 * time.Minute)
	last, lastChange := -1.0, time.Now()
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if m, err := eg.scrape(); err == nil {
			if v := m.sum("watchable_subscribe_total"); v != last {
				last, lastChange = v, time.Now()
			}
		}
		if time.Since(lastChange) >= quietFor {
			return
		}
		time.Sleep(time.Second)
	}
	log.Printf("warning: EG never went quiet for %s", quietFor)
}

func runScenario(ctx context.Context, o runOptions, k *kube, envoy envoyAdmin, eg egMetrics, sc scenario, dir string) (summary, []latencySample, error) {
	cluster := clusterName(o.ns, sc.backend)
	log.Printf("[%s/%s] resetting %s to %d endpoints", o.mode, sc.name, sc.backend, sc.counts[0])
	if _, err := k.setSliceEndpoints(ctx, o.ns, sc.backend+"-1", probeIPs(sc.ipSet, sc.counts[0])); err != nil {
		return summary{}, nil, err
	}
	if _, ok := waitFor(ctx, envoy, cluster, int64(sc.counts[0]), 50*time.Millisecond, 5*time.Minute); !ok {
		return summary{}, nil, fmt.Errorf("envoy never showed %d endpoints for %s", sc.counts[0], cluster)
	}
	waitQuiet(ctx, eg, o.quietFor)

	sctx, stop := context.WithCancel(ctx)
	defer stop()
	var churnOps atomic.Int64
	if sc.endpointChurn {
		names, err := k.churnSlices(ctx, o.ns)
		if err != nil {
			return summary{}, nil, err
		}
		go loop(sctx, time.Duration(float64(time.Second)/o.churnRate), func() {
			if err := k.toggleSlice(sctx, o.ns, names[rand.IntN(len(names))]); err == nil {
				churnOps.Add(1)
			}
		})
	}
	if sc.configChurn {
		names, err := k.churnRoutes(ctx, o.ns)
		if err != nil {
			return summary{}, nil, err
		}
		go loop(sctx, o.configEvery, func() {
			if err := k.touchRoute(sctx, o.ns, names[rand.IntN(len(names))]); err == nil {
				churnOps.Add(1)
			} else {
				log.Printf("route churn: %v", err)
			}
		})
	}
	if sc.endpointChurn || sc.configChurn {
		log.Printf("[%s/%s] warming up background load for %s", o.mode, sc.name, o.warmup)
		time.Sleep(o.warmup)
	}

	before, err := eg.scrape()
	if err != nil {
		return summary{}, nil, err
	}
	var smp sampler
	mctx, mstop := context.WithCancel(sctx)
	go smp.run(mctx, eg, o.metricsSample)
	start := time.Now()

	var lats []latencySample
	cur := sc.counts[0]
	for i := 0; i < o.samples && ctx.Err() == nil; i++ {
		next := sc.counts[(i+1)%2]
		t0, err := k.setSliceEndpoints(ctx, o.ns, sc.backend+"-1", probeIPs(sc.ipSet, next))
		if err != nil {
			mstop()
			return summary{}, nil, err
		}
		t1, ok := waitFor(ctx, envoy, cluster, int64(next), o.pollEvery, o.timeout)
		s := latencySample{Scenario: sc.name, Seq: i, From: cur, To: next, T0: t0, Missed: !ok}
		if ok {
			s.LatencyMs = float64(t1.Sub(t0).Microseconds()) / 1000
		} else {
			// Get back in sync before the next probe so one miss doesn't cascade.
			waitFor(ctx, envoy, cluster, int64(next), 50*time.Millisecond, 5*time.Minute)
		}
		lats = append(lats, s)
		cur = next
		time.Sleep(o.interval + time.Duration(rand.Int64N(int64(time.Second))))
	}
	elapsed := time.Since(start)
	mstop()
	stop()

	after, err := eg.scrape()
	if err != nil {
		return summary{}, nil, err
	}
	res := smp.take()
	if err := writeResources(filepath.Join(dir, "resources-"+sc.name+".csv"), start, res); err != nil {
		return summary{}, nil, err
	}
	_ = os.WriteFile(filepath.Join(dir, "metrics-"+sc.name+"-after.prom"), []byte(dumpProm(after)), 0o644)

	sum := summarize(o.mode, sc.name, lats, res, before, after)
	sum.ChurnOps = churnOps.Load()
	sum.DurationSec = elapsed.Seconds()
	if mem, err := envoy.allocatedMiB(); err == nil {
		sum.EnvoyMemMiB = mem
	}
	return sum, lats, nil
}

func loop(ctx context.Context, every time.Duration, fn func()) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			fn()
		}
	}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1)*p + 0.5)
	return sorted[idx]
}

func summarize(mode, name string, lats []latencySample, res []resourceSample, before, after promSamples) summary {
	s := summary{Mode: mode, Scenario: name, Samples: len(lats), Runners: map[string]runnerStat{}}
	var vals []float64
	var total float64
	for _, l := range lats {
		if l.Missed {
			s.Misses++
			continue
		}
		vals = append(vals, l.LatencyMs)
		total += l.LatencyMs
	}
	slices.Sort(vals)
	if len(vals) > 0 {
		s.P50Ms, s.P90Ms, s.P99Ms = percentile(vals, .5), percentile(vals, .9), percentile(vals, .99)
		s.MaxMs, s.MeanMs = vals[len(vals)-1], total/float64(len(vals))
	}

	if len(res) > 1 {
		first, last := res[0], res[len(res)-1]
		s.CPUAvgCores = (last.CPUSeconds - first.CPUSeconds) / last.At.Sub(first.At).Seconds()
		var rates []float64
		var rssTotal float64
		for i, r := range res {
			rssTotal += r.RSS
			s.RSSMaxMiB = max(s.RSSMaxMiB, r.RSS/(1<<20))
			s.HeapMaxMiB = max(s.HeapMaxMiB, r.HeapInuse/(1<<20))
			s.GoroutinesMax = max(s.GoroutinesMax, r.Goroutines)
			if i > 0 {
				if dt := r.At.Sub(res[i-1].At).Seconds(); dt > 0 {
					rates = append(rates, (r.CPUSeconds-res[i-1].CPUSeconds)/dt)
				}
			}
		}
		s.RSSAvgMiB = rssTotal / float64(len(res)) / (1 << 20)
		slices.Sort(rates)
		s.CPUP95Cores = percentile(rates, .95)
	}

	delta := func(name string, labels ...string) float64 {
		return after.sum(name, labels...) - before.sum(name, labels...)
	}
	for _, r := range after.labelValues("watchable_subscribe_duration_seconds_count", "runner") {
		l := fmt.Sprintf(`runner="%s"`, r)
		n := delta("watchable_subscribe_duration_seconds_count", l)
		st := runnerStat{Count: n}
		if n > 0 {
			st.MeanSec = delta("watchable_subscribe_duration_seconds_sum", l) / n
		}
		s.Runners[r] = st
	}
	s.FastPublications = delta("xds_endpoint_publications_total", `path="fast"`)
	s.FullPublications = delta("xds_endpoint_publications_total", `path="full"`)
	s.RefreshSkipped = delta("xds_endpoint_refresh_skipped_total")
	if n := delta("xds_endpoint_refresh_duration_seconds_count"); n > 0 {
		s.FastRefreshMeanMs = delta("xds_endpoint_refresh_duration_seconds_sum") / n * 1000
	}
	s.SnapshotUpdates = delta("xds_snapshot_update_total")
	return s
}

func dumpProm(p promSamples) string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s %g\n", k, p[k])
	}
	return b.String()
}

func writeLatencies(path, mode string, lats []latencySample) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"mode", "scenario", "seq", "from", "to", "t0", "latency_ms", "missed"})
	for _, l := range lats {
		_ = w.Write([]string{mode, l.Scenario, strconv.Itoa(l.Seq), strconv.Itoa(l.From), strconv.Itoa(l.To),
			l.T0.Format(time.RFC3339Nano), strconv.FormatFloat(l.LatencyMs, 'f', 3, 64), strconv.FormatBool(l.Missed)})
	}
	w.Flush()
	return w.Error()
}

func writeResources(path string, start time.Time, res []resourceSample) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"elapsed_s", "cpu_seconds_total", "rss_bytes", "heap_inuse_bytes", "goroutines"})
	for _, r := range res {
		_ = w.Write([]string{
			strconv.FormatFloat(r.At.Sub(start).Seconds(), 'f', 2, 64),
			strconv.FormatFloat(r.CPUSeconds, 'f', 3, 64),
			strconv.FormatFloat(r.RSS, 'f', 0, 64),
			strconv.FormatFloat(r.HeapInuse, 'f', 0, 64),
			strconv.FormatFloat(r.Goroutines, 'f', 0, 64),
		})
	}
	w.Flush()
	return w.Error()
}
