// Command gen writes the heavy Envoy Gateway config used by the EDS latency test.
//
// The output is plain multi-document YAML meant for `kubectl apply --server-side`.
// Every backend is a selector-less Service with a hand-written EndpointSlice that
// points at fake IPs, so endpoint changes are fully controlled by the test and no
// pods are needed. Envoy never sends traffic to them; we only read its cluster
// membership through the admin API.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

type config struct {
	ns           string
	gateway      string
	routes       int
	hosts        int
	rules        int
	securityPols int
	backendPols  int
	patchPols    int
	patchOps     int
	patchTarget  string
}

func main() {
	var c config
	flag.StringVar(&c.ns, "namespace", "perf", "namespace for all generated resources")
	flag.StringVar(&c.gateway, "gateway", "eg", "Gateway name")
	flag.IntVar(&c.routes, "routes", 2000, "number of HTTPRoutes, each with its own Service")
	flag.IntVar(&c.hosts, "hosts", 50, "number of distinct hostnames the routes are spread over")
	flag.IntVar(&c.rules, "rules", 4, "rules per HTTPRoute (each becomes its own Envoy cluster)")
	flag.IntVar(&c.securityPols, "security-policies", 200, "SecurityPolicies (CORS), one per route from route-0 up")
	flag.IntVar(&c.backendPols, "backend-policies", 500, "BackendTrafficPolicies, one per route from route-0 up")
	flag.IntVar(&c.patchPols, "patch-policies", 100, "EnvoyPatchPolicies targeting the Gateway")
	flag.IntVar(&c.patchOps, "patch-ops", 2, "JSON patch operations per EnvoyPatchPolicy (1 or 2)")
	flag.StringVar(&c.patchTarget, "patch-target", "", "xDS listener / route config name to patch (default <ns>/<gateway>/http)")
	flag.Parse()
	if c.patchTarget == "" {
		c.patchTarget = fmt.Sprintf("%s/%s/http", c.ns, c.gateway)
	}
	if c.rules < 1 || c.hosts < 1 {
		fmt.Fprintln(os.Stderr, "rules and hosts must be >= 1")
		os.Exit(2)
	}

	w := bufio.NewWriterSize(os.Stdout, 1<<20)
	defer w.Flush()
	generate(w, c)
}

func doc(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, "---\n"+strings.TrimLeft(format, "\n"), args...)
}

func generate(w io.Writer, c config) {
	doc(w, `
apiVersion: v1
kind: Namespace
metadata:
  name: %s
`, c.ns)

	doc(w, `
apiVersion: gateway.networking.k8s.io/v1
kind: GatewayClass
metadata:
  name: eg-perf
spec:
  controllerName: gateway.envoyproxy.io/gatewayclass-controller
`)

	// ClusterIP keeps kind from needing a load balancer; we never send traffic.
	doc(w, `
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: EnvoyProxy
metadata:
  name: perf-proxy
  namespace: %s
spec:
  provider:
    type: Kubernetes
    kubernetes:
      envoyService:
        type: ClusterIP
      envoyDeployment:
        replicas: 1
        container:
          resources:
            requests:
              cpu: "2"
              memory: 2Gi
`, c.ns)

	doc(w, `
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: %s
  namespace: %s
spec:
  gatewayClassName: eg-perf
  infrastructure:
    parametersRef:
      group: gateway.envoyproxy.io
      kind: EnvoyProxy
      name: perf-proxy
  listeners:
  - name: http
    protocol: HTTP
    port: 80
`, c.gateway, c.ns)

	doc(w, `
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: ClientTrafficPolicy
metadata:
  name: perf-ctp
  namespace: %s
spec:
  targetRefs:
  - group: gateway.networking.k8s.io
    kind: Gateway
    name: %s
  headers:
    enableEnvoyHeaders: true
  connection:
    bufferLimit: 64Ki
  timeout:
    http:
      idleTimeout: 60s
`, c.ns, c.gateway)

	for i := 0; i < c.routes; i++ {
		writeBackend(w, c, fmt.Sprintf("svc-%d", i), routeIPs(i, 2), true)
		writeRoute(w, c, i)
	}
	for i := 0; i < c.securityPols && i < c.routes; i++ {
		writeSecurityPolicy(w, c, i)
	}
	for i := 0; i < c.backendPols && i < c.routes; i++ {
		writeBackendPolicy(w, c, i)
	}
	for i := 0; i < c.patchPols; i++ {
		writePatchPolicy(w, c, i)
	}

	// Probe backends are not part of the background churn.
	writeBackend(w, c, "probe", probeIPs(0, 3), false)
	writeProbeRoute(w, c, "probe")
	writeBackend(w, c, "probe-inel", probeIPs(1, 1), false)
	writeProbeRoute(w, c, "probe-inel")
}

// routeIPs returns n fake addresses for route i from 10.200.0.0/14.
func routeIPs(i, n int) []string {
	ips := make([]string, n)
	for k := range n {
		v := i*4 + k
		ips[k] = fmt.Sprintf("10.%d.%d.%d", 200+v/65536, (v/256)%256, v%256)
	}
	return ips
}

func probeIPs(set, n int) []string {
	ips := make([]string, n)
	for k := range n {
		ips[k] = fmt.Sprintf("10.199.%d.%d", set, k+1)
	}
	return ips
}

func writeBackend(w io.Writer, c config, name string, ips []string, churn bool) {
	doc(w, `
apiVersion: v1
kind: Service
metadata:
  name: %s
  namespace: %s
spec:
  ports:
  - name: http
    port: 8080
    targetPort: 8080
    protocol: TCP
`, name, c.ns)

	var eps strings.Builder
	for _, ip := range ips {
		fmt.Fprintf(&eps, "- addresses: [\"%s\"]\n  conditions:\n    ready: true\n", ip)
	}
	doc(w, `
apiVersion: discovery.k8s.io/v1
kind: EndpointSlice
metadata:
  name: %s-1
  namespace: %s
  labels:
    kubernetes.io/service-name: %s
    endpointslice.kubernetes.io/managed-by: eds-fast-path-perf
    perf.eg/churn: "%t"
addressType: IPv4
ports:
- name: http
  port: 8080
  protocol: TCP
endpoints:
%s`, name, c.ns, name, churn, eps.String())
}

func writeRoute(w io.Writer, c config, i int) {
	var rules strings.Builder
	for j := 0; j < c.rules; j++ {
		fmt.Fprintf(&rules, "  - matches:\n    - path:\n        type: PathPrefix\n        value: /r%d/p%d\n", i, j)
		if j > 0 {
			fmt.Fprintf(&rules, "      headers:\n      - name: x-tenant\n        value: t%d\n", j)
		}
		if j%4 == 3 {
			fmt.Fprintf(&rules, "      queryParams:\n      - name: v\n        value: \"%d\"\n", j)
		}
		switch j % 4 {
		case 0:
			// Config churn rewrites this header value to force full translations.
			rules.WriteString("    filters:\n    - type: RequestHeaderModifier\n      requestHeaderModifier:\n        set:\n        - name: x-perf\n          value: v0\n")
		case 1:
			rules.WriteString("    filters:\n    - type: ResponseHeaderModifier\n      responseHeaderModifier:\n        add:\n        - name: x-perf-resp\n          value: \"1\"\n")
		case 2:
			fmt.Fprintf(&rules, "    filters:\n    - type: URLRewrite\n      urlRewrite:\n        path:\n          type: ReplacePrefixMatch\n          replacePrefixMatch: /rewritten/%d\n", j)
		}
		fmt.Fprintf(&rules, "    backendRefs:\n    - name: svc-%d\n      port: 8080\n", i)
	}
	doc(w, `
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: route-%d
  namespace: %s
  labels:
    perf.eg/churn: "true"
spec:
  parentRefs:
  - name: %s
  hostnames:
  - h%d.perf.example.com
  rules:
%s`, i, c.ns, c.gateway, i%c.hosts, rules.String())
}

func writeProbeRoute(w io.Writer, c config, name string) {
	doc(w, `
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: %s
  namespace: %s
spec:
  parentRefs:
  - name: %s
  hostnames:
  - %s.perf.example.com
  rules:
  - backendRefs:
    - name: %s
      port: 8080
`, name, c.ns, c.gateway, name, name)
}

func writeSecurityPolicy(w io.Writer, c config, i int) {
	doc(w, `
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: SecurityPolicy
metadata:
  name: sp-%d
  namespace: %s
spec:
  targetRefs:
  - group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: route-%d
  cors:
    allowOrigins:
    - "https://*.example.com"
    allowMethods: [GET, POST]
    allowHeaders: [x-tenant]
    maxAge: 600s
`, i, c.ns, i)
}

func writeBackendPolicy(w io.Writer, c config, i int) {
	doc(w, `
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: BackendTrafficPolicy
metadata:
  name: btp-%d
  namespace: %s
spec:
  targetRefs:
  - group: gateway.networking.k8s.io
    kind: HTTPRoute
    name: route-%d
  loadBalancer:
    type: LeastRequest
  circuitBreaker:
    maxConnections: 2048
    maxPendingRequests: 1024
  retry:
    numRetries: 2
    retryOn:
      triggers: [connect-failure, retriable-status-codes]
      httpStatusCodes: [503]
  timeout:
    http:
      requestTimeout: 15s
`, i, c.ns, i)
}

// writePatchPolicy adds an EnvoyPatchPolicy whose patches each marshal and
// unmarshal the whole route configuration or listener, the expensive case in
// envoyproxy/gateway#9777. Every policy overwrites the same fields, so the final
// config stays valid no matter how many are applied.
func writePatchPolicy(w io.Writer, c config, i int) {
	ops := fmt.Sprintf(`
  - type: type.googleapis.com/envoy.config.route.v3.RouteConfiguration
    name: %s
    operation:
      op: add
      jsonPath: "virtual_hosts[0]"
      path: /request_headers_to_add
      value:
      - header:
          key: x-epp
          value: "%d"
        append_action: OVERWRITE_IF_EXISTS_OR_ADD
`, c.patchTarget, i)
	if c.patchOps > 1 {
		ops += fmt.Sprintf(`  - type: type.googleapis.com/envoy.config.listener.v3.Listener
    name: %s
    operation:
      op: add
      path: /per_connection_buffer_limit_bytes
      value: %d
`, c.patchTarget, 32768+i)
	}
	doc(w, `
apiVersion: gateway.envoyproxy.io/v1alpha1
kind: EnvoyPatchPolicy
metadata:
  name: epp-%d
  namespace: %s
spec:
  targetRef:
    group: gateway.networking.k8s.io
    kind: Gateway
    name: %s
  type: JSONPatch
  priority: %d
  jsonPatches:%s`, i, c.ns, c.gateway, i, ops)
}
