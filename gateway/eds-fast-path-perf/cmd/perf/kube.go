package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"os/exec"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
)

var httpRouteGVR = schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes"}

type kube struct {
	cs      kubernetes.Interface
	dyn     dynamic.Interface
	context string
}

func newKube(kubeContext string) (*kube, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: kubeContext}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return nil, err
	}
	// The churn loops issue a steady stream of writes; keep client-side throttling out of the numbers.
	cfg.QPS, cfg.Burst = 500, 1000
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &kube{cs: cs, dyn: dyn, context: kubeContext}, nil
}

func (k *kube) runningPod(ctx context.Context, ns, selector string) (string, error) {
	pods, err := k.cs.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return "", err
	}
	for _, p := range pods.Items {
		if p.Status.Phase == corev1.PodRunning && p.DeletionTimestamp == nil {
			return p.Name, nil
		}
	}
	return "", fmt.Errorf("no running pod in %s matching %q", ns, selector)
}

// portForward runs `kubectl port-forward` until ctx is done and waits for the
// local port to accept connections. If the forward dies early (for example the
// pod was replaced), onExit is called so the run fails instead of timing out.
func (k *kube) portForward(ctx context.Context, ns, pod string, local, remote int, onExit func(error)) error {
	args := []string{"port-forward", "-n", ns, "pod/" + pod, fmt.Sprintf("%d:%d", local, remote)}
	if k.context != "" {
		args = append([]string{"--context", k.context}, args...)
	}
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		err := cmd.Wait()
		if ctx.Err() == nil {
			onExit(fmt.Errorf("port-forward to %s/%s exited: %v", ns, pod, err))
		}
	}()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", local), time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("port-forward to %s/%s:%d did not come up", ns, pod, remote)
}

func endpointsFor(ips []string) []discoveryv1.Endpoint {
	eps := make([]discoveryv1.Endpoint, len(ips))
	for i, ip := range ips {
		eps[i] = discoveryv1.Endpoint{
			Addresses:  []string{ip},
			Conditions: discoveryv1.EndpointConditions{Ready: new(true)},
		}
	}
	return eps
}

// setSliceEndpoints rewrites an EndpointSlice to the given addresses and returns
// the time just before the successful write.
func (k *kube) setSliceEndpoints(ctx context.Context, ns, name string, ips []string) (time.Time, error) {
	var t0 time.Time
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		slice, err := k.cs.DiscoveryV1().EndpointSlices(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		slice.Endpoints = endpointsFor(ips)
		t0 = time.Now()
		_, err = k.cs.DiscoveryV1().EndpointSlices(ns).Update(ctx, slice, metav1.UpdateOptions{})
		return err
	})
	return t0, err
}

func (k *kube) churnSlices(ctx context.Context, ns string) ([]string, error) {
	list, err := k.cs.DiscoveryV1().EndpointSlices(ns).List(ctx, metav1.ListOptions{LabelSelector: "perf.eg/churn=true"})
	if err != nil {
		return nil, err
	}
	names := make([]string, len(list.Items))
	for i, s := range list.Items {
		names[i] = s.Name
	}
	return names, nil
}

// toggleSlice flips a churn slice between 2 and 3 endpoints. Both sizes are
// non-empty so the change stays eligible for the fast path.
func (k *kube) toggleSlice(ctx context.Context, ns, name string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		slice, err := k.cs.DiscoveryV1().EndpointSlices(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if len(slice.Endpoints) == 0 {
			return nil
		}
		base := slice.Endpoints[0].Addresses[0]
		ip := net.ParseIP(base).To4()
		n := 3
		if len(slice.Endpoints) >= 3 {
			n = 2
		}
		ips := make([]string, n)
		for i := range n {
			ips[i] = net.IPv4(ip[0], ip[1], ip[2], ip[3]+byte(i)).String()
		}
		slice.Endpoints = endpointsFor(ips)
		_, err = k.cs.DiscoveryV1().EndpointSlices(ns).Update(ctx, slice, metav1.UpdateOptions{})
		return err
	})
}

func (k *kube) churnRoutes(ctx context.Context, ns string) ([]string, error) {
	list, err := k.dyn.Resource(httpRouteGVR).Namespace(ns).List(ctx, metav1.ListOptions{LabelSelector: "perf.eg/churn=true"})
	if err != nil {
		return nil, err
	}
	names := make([]string, len(list.Items))
	for i, r := range list.Items {
		names[i] = r.GetName()
	}
	return names, nil
}

// touchRoute changes a header value in rule 0, which bumps the route's
// generation and forces a full translation.
func (k *kube) touchRoute(ctx context.Context, ns, name string) error {
	patch := fmt.Sprintf(`[{"op":"replace","path":"/spec/rules/0/filters/0/requestHeaderModifier/set/0/value","value":"v%d"}]`, rand.Int64())
	_, err := k.dyn.Resource(httpRouteGVR).Namespace(ns).Patch(ctx, name, types.JSONPatchType, []byte(patch), metav1.PatchOptions{})
	return err
}
