// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controller

import (
	"fmt"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/util/xdsfake"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/monitoring/monitortest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

// batchTestThreshold makes a Service with more than 3 endpoints "large" in these tests.
const batchTestThreshold = 3

func enableEndpointBatching(t *testing.T, debounce time.Duration) {
	test.SetForTest(t, &features.BatchLargeEndpointUpdates, true)
	test.SetForTest(t, &features.BatchLargeEndpointUpdatesDebounceTime, debounce)
	test.SetForTest(t, &largeEndpointBatchThreshold, batchTestThreshold)
}

// batchTestSlice builds an EndpointSlice for the Service. Endpoints reference pods named after their
// position when withPods is set.
func batchTestSlice(svc, ns string, withPods bool, ips ...string) *discovery.EndpointSlice {
	portName := "tcp-port"
	var portNum int32 = 8080
	ready := true
	eps := make([]discovery.Endpoint, 0, len(ips))
	for i, ip := range ips {
		ep := discovery.Endpoint{
			Addresses:  []string{ip},
			Conditions: discovery.EndpointConditions{Ready: &ready},
		}
		if withPods {
			ep.TargetRef = &corev1.ObjectReference{Kind: "Pod", Namespace: ns, Name: fmt.Sprintf("%s-%d", svc, i)}
		}
		eps = append(eps, ep)
	}
	return &discovery.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svc,
			Namespace: ns,
			Labels:    map[string]string{discovery.LabelServiceName: svc},
		},
		AddressType: discovery.AddressTypeIPv4,
		Endpoints:   eps,
		Ports:       []discovery.EndpointPort{{Name: &portName, Port: &portNum}},
	}
}

func batchTestIPs(prefix string, n int) []string {
	ips := make([]string, 0, n)
	for i := 0; i < n; i++ {
		ips = append(ips, fmt.Sprintf("%s.%d", prefix, i+1))
	}
	return ips
}

// collectEvents returns every event seen until none arrives for the quiet period.
func collectEvents(fx *xdsfake.Updater, quiet time.Duration) []xdsfake.Event {
	var out []xdsfake.Event
	for {
		select {
		case e := <-fx.Events:
			out = append(out, e)
		case <-time.After(quiet):
			return out
		}
	}
}

// assertBatchMetrics checks that at least the given number of events were held back and that they
// were followed by at least one batched update, whose delay was recorded.
func assertBatchMetrics(mt *monitortest.MetricsTest, heldBack int) {
	mt.Assert(endpointUpdatesBatched.Name(), nil, monitortest.AtLeast(float64(heldBack)))
	mt.Assert(endpointBatchFlushes.Name(), nil, monitortest.AtLeast(1))
	mt.Assert(endpointBatchDelay.Name(), nil, func(f any) error {
		d, ok := f.(*dto.Histogram)
		if !ok {
			return fmt.Errorf("expected a histogram, got %T", f)
		}
		if d.GetSampleCount() == 0 || d.GetSampleSum() <= 0 {
			return fmt.Errorf("expected a recorded delay, got %d samples with sum %v", d.GetSampleCount(), d.GetSampleSum())
		}
		return nil
	})
}

func eventsOf(events []xdsfake.Event, eventType, id string) []xdsfake.Event {
	var out []xdsfake.Event
	for _, e := range events {
		if e.Type == eventType && e.ID == id {
			out = append(out, e)
		}
	}
	return out
}

func TestBatchLargeEndpointUpdates(t *testing.T) {
	const ns = "nsa"

	t.Run("updates of a large Service are coalesced", func(t *testing.T) {
		const debounce = 300 * time.Millisecond
		const updates = 20
		enableEndpointBatching(t, debounce)
		mt := monitortest.New(t)
		c, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})
		initTestEnv(t, c.client.Kube(), fx)
		createServiceWait(c, "large", ns, []string{"10.0.0.1"}, nil, nil, []int32{8080}, map[string]string{"app": "large"}, t)
		largeHost := string(kube.ServiceHostname("large", ns, c.opts.DomainSuffix))
		slices := clienttest.NewWriter[*discovery.EndpointSlice](t, c.client)

		slices.CreateOrUpdate(batchTestSlice("large", ns, false, batchTestIPs("10.1.0", 5)...))
		fx.MatchOrFail(t, xdsfake.Event{Type: "eds", ID: largeHost, EndpointCount: 5})
		collectEvents(fx, debounce*2)

		// Many quick updates are sent far fewer times, and the last one sent carries the final state.
		final := 5 + updates
		for n := 6; n <= final; n++ {
			slices.CreateOrUpdate(batchTestSlice("large", ns, false, batchTestIPs("10.1.0", n)...))
		}
		sent := eventsOf(collectEvents(fx, debounce*4), "eds", largeHost)
		if len(sent) == 0 || len(sent) >= updates {
			t.Fatalf("expected the %d updates to be coalesced, got %d EDS updates", updates, len(sent))
		}
		assert.Equal(t, len(sent[len(sent)-1].Endpoints), final)
		assertBatchMetrics(mt, updates)
	})

	t.Run("only large Services are held back", func(t *testing.T) {
		// Long enough that a held-back update is never sent during the test.
		enableEndpointBatching(t, time.Hour)
		c, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})
		initTestEnv(t, c.client.Kube(), fx)
		createServiceWait(c, "large", ns, []string{"10.0.0.1"}, nil, nil, []int32{8080}, map[string]string{"app": "large"}, t)
		createServiceWait(c, "small", ns, []string{"10.0.0.2"}, nil, nil, []int32{8080}, map[string]string{"app": "small"}, t)
		largeHost := string(kube.ServiceHostname("large", ns, c.opts.DomainSuffix))
		smallHost := string(kube.ServiceHostname("small", ns, c.opts.DomainSuffix))
		slices := clienttest.NewWriter[*discovery.EndpointSlice](t, c.client)

		// A Service at the threshold is not large: its updates are sent inline.
		slices.CreateOrUpdate(batchTestSlice("small", ns, false, batchTestIPs("10.2.0", batchTestThreshold-1)...))
		fx.MatchOrFail(t, xdsfake.Event{Type: "eds", ID: smallHost, EndpointCount: batchTestThreshold - 1})
		slices.CreateOrUpdate(batchTestSlice("small", ns, false, batchTestIPs("10.2.0", batchTestThreshold)...))
		fx.MatchOrFail(t, xdsfake.Event{Type: "eds", ID: smallHost, EndpointCount: batchTestThreshold})

		// A Service above it is held back, while its per-slice cache is still updated right away.
		slices.CreateOrUpdate(batchTestSlice("large", ns, false, batchTestIPs("10.1.0", batchTestThreshold+1)...))
		fx.AssertNoMatch(t, 300*time.Millisecond, xdsfake.EventMatcher{Type: "eds", IDPrefix: largeHost})
		retry.UntilOrFail(t, func() bool {
			return len(c.endpoints.endpointCache.Get(kube.ServiceHostname("large", ns, c.opts.DomainSuffix))) == batchTestThreshold+1
		}, retry.Timeout(time.Second*2))
	})
}

func TestBatchLargeEndpointPodLabelUpdates(t *testing.T) {
	const ns = "nsa"
	const pods = 10
	const debounce = 300 * time.Millisecond
	enableEndpointBatching(t, debounce)
	mt := monitortest.New(t)
	c, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})
	initTestEnv(t, c.client.Kube(), fx)
	createServiceWait(c, "large", ns, []string{"10.0.0.1"}, nil, nil, []int32{8080}, map[string]string{"app": "large"}, t)
	hostname := kube.ServiceHostname("large", ns, c.opts.DomainSuffix)
	ips := batchTestIPs("10.1.0", pods)
	podObjs := make([]*corev1.Pod, 0, pods)
	for i, ip := range ips {
		pod := generatePod([]string{ip}, fmt.Sprintf("large-%d", i), ns, "", "",
			map[string]string{"app": "large", "role": "canary"}, map[string]string{})
		addPods(t, c, fx, pod)
		podObjs = append(podObjs, pod)
	}
	clienttest.NewWriter[*discovery.EndpointSlice](t, c.client).CreateOrUpdate(batchTestSlice("large", ns, true, ips...))
	fx.MatchOrFail(t, xdsfake.Event{Type: "eds", ID: string(hostname), EndpointCount: pods})
	collectEvents(fx, debounce*2)

	// Relabel every pod. Each proxy is refreshed, but the Service is rebuilt far fewer times than there are pods.
	pc := clienttest.Wrap(t, c.podsClient)
	for _, pod := range podObjs {
		pod.Labels["role"] = "stable"
		pc.CreateOrUpdate(pod)
	}
	events := collectEvents(fx, debounce*4)
	proxies := 0
	for _, e := range events {
		if e.Type == "proxy" {
			proxies++
		}
	}
	assert.Equal(t, proxies, pods)
	rebuilds := eventsOf(events, "eds cache", string(hostname))
	if len(rebuilds) == 0 || len(rebuilds) >= pods {
		t.Fatalf("expected the %d label changes to be coalesced, got %d rebuilds", pods, len(rebuilds))
	}
	assert.Equal(t, len(eventsOf(events, "xds", string(hostname))), len(rebuilds))
	assertBatchMetrics(mt, pods)

	retry.UntilOrFail(t, func() bool {
		eps := c.endpoints.endpointCache.Get(hostname)
		if len(eps) != pods {
			return false
		}
		for _, ep := range eps {
			if ep.Labels["role"] != "stable" {
				return false
			}
		}
		return true
	}, retry.Timeout(time.Second*2))
}
