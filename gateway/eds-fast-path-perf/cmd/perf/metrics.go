package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var httpClient = &http.Client{
	Timeout:   10 * time.Second,
	Transport: &http.Transport{MaxIdleConnsPerHost: 4},
}

func get(u string) (string, error) {
	resp, err := httpClient.Get(u)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	return string(b), nil
}

// promSamples maps a full series key (name plus label set) to its value.
type promSamples map[string]float64

func parseProm(text string) promSamples {
	out := promSamples{}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		var key, rest string
		if i := strings.LastIndex(line, "}"); i >= 0 {
			key, rest = line[:i+1], line[i+1:]
		} else if i := strings.IndexByte(line, ' '); i >= 0 {
			key, rest = line[:i], line[i:]
		} else {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			continue
		}
		out[key] = v
	}
	return out
}

func seriesName(key string) string {
	if i := strings.IndexByte(key, '{'); i >= 0 {
		return key[:i]
	}
	return key
}

// sum adds every series called name whose labels contain all of the given
// `label="value"` fragments.
func (p promSamples) sum(name string, labels ...string) float64 {
	var total float64
next:
	for k, v := range p {
		if seriesName(k) != name {
			continue
		}
		for _, l := range labels {
			if !strings.Contains(k, l) {
				continue next
			}
		}
		total += v
	}
	return total
}

var labelRe = regexp.MustCompile(`(\w+)="([^"]*)"`)

// labelValues returns the distinct values of label across series called name.
func (p promSamples) labelValues(name, label string) []string {
	seen := map[string]bool{}
	var out []string
	for k := range p {
		if seriesName(k) != name {
			continue
		}
		for _, m := range labelRe.FindAllStringSubmatch(k, -1) {
			if m[1] == label && !seen[m[2]] {
				seen[m[2]] = true
				out = append(out, m[2])
			}
		}
	}
	return out
}

type egMetrics struct{ base string }

func (e egMetrics) scrape() (promSamples, error) {
	text, err := get(e.base + "/metrics")
	if err != nil {
		return nil, err
	}
	return parseProm(text), nil
}

// resourceSample is one 1s reading of control-plane resource usage.
type resourceSample struct {
	At         time.Time
	CPUSeconds float64
	RSS        float64
	HeapInuse  float64
	Goroutines float64
}

type sampler struct {
	mu      sync.Mutex
	samples []resourceSample
}

func (s *sampler) run(ctx context.Context, eg egMetrics, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if m, err := eg.scrape(); err == nil {
			s.mu.Lock()
			s.samples = append(s.samples, resourceSample{
				At:         time.Now(),
				CPUSeconds: m.sum("process_cpu_seconds_total"),
				RSS:        m.sum("process_resident_memory_bytes"),
				HeapInuse:  m.sum("go_memstats_heap_inuse_bytes"),
				Goroutines: m.sum("go_goroutines"),
			})
			s.mu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *sampler) take() []resourceSample {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.samples
	s.samples = nil
	return out
}

type envoyAdmin struct{ base string }

// endpointCount returns how many endpoints Envoy has accepted for a cluster,
// read from the EDS section of the config dump. It filters to one cluster, so
// it stays cheap with thousands of clusters, unlike a /stats regex that scans
// every stat on Envoy's main thread. A cluster with no assignment reads as 0.
func (e envoyAdmin) endpointCount(cluster string) (int64, error) {
	q := url.QueryEscape("^" + regexp.QuoteMeta(cluster) + "$")
	text, err := get(e.base + "/config_dump?resource=dynamic_endpoint_configs&include_eds&name_regex=" + q)
	if err != nil {
		if strings.Contains(err.Error(), "404") {
			return 0, nil
		}
		return 0, err
	}
	var dump struct {
		Configs []struct {
			EndpointConfig struct {
				Endpoints []struct {
					LbEndpoints []json.RawMessage `json:"lb_endpoints"`
				} `json:"endpoints"`
			} `json:"endpoint_config"`
		} `json:"configs"`
	}
	if err := json.Unmarshal([]byte(text), &dump); err != nil {
		// Envoy answers with plain text when nothing matches.
		return 0, nil
	}
	var n int64
	for _, c := range dump.Configs {
		for _, l := range c.EndpointConfig.Endpoints {
			n += int64(len(l.LbEndpoints))
		}
	}
	return n, nil
}

// allocatedMiB reads Envoy's heap allocation from /memory.
func (e envoyAdmin) allocatedMiB() (float64, error) {
	text, err := get(e.base + "/memory")
	if err != nil {
		return 0, err
	}
	var m struct {
		Allocated string `json:"allocated"`
	}
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		return 0, err
	}
	v, err := strconv.ParseFloat(m.Allocated, 64)
	return v / (1 << 20), err
}
