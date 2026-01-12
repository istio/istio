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
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	mcs "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/util/xdsfake"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestEndpointSliceFromMCSShouldBeIgnored(t *testing.T) {
	const (
		ns      = "nsa"
		svcName = "svc1"
		appName = "prod-app"
	)

	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})

	node := generateNode("node1", map[string]string{
		NodeZoneLabel:              "zone1",
		NodeRegionLabel:            "region1",
		label.TopologySubzone.Name: "subzone1",
	})
	addNodes(t, controller, node)

	pod := generatePod([]string{"128.0.0.1"}, "pod1", ns, "svcaccount", "node1",
		map[string]string{"app": appName}, map[string]string{})
	pods := []*corev1.Pod{pod}
	addPods(t, controller, fx, pods...)

	createServiceWait(controller, svcName, ns, []string{"10.0.0.1"}, nil, nil,
		[]int32{8080}, map[string]string{"app": appName}, t)

	// Ensure that the service is available.
	hostname := kube.ServiceHostname(svcName, ns, controller.opts.DomainSuffix)
	svc := controller.GetService(hostname)
	if svc == nil {
		t.Fatal("failed to get service")
	}

	// Create an endpoint that indicates it's an MCS endpoint for the service.
	svc1Ips := []string{"128.0.0.1"}
	portNames := []string{"tcp-port"}
	createEndpoints(t, controller, svcName, ns, portNames, svc1Ips, nil, map[string]string{
		mcs.LabelServiceName: svcName,
	})
	fx.AssertEmpty(t, time.Millisecond*50)

	// Ensure that no endpoint is create
	endpoints := GetEndpoints(svc, controller.Endpoints)
	assert.Equal(t, len(endpoints), 0)
}

func TestEndpointSliceCache(t *testing.T) {
	cache := newEndpointSliceCache()
	hostname := host.Name("foo")

	// add a endpoint
	ep1 := &model.IstioEndpoint{
		Addresses:       []string{"1.2.3.4"},
		ServicePortName: "http",
	}

	// add a endpoint with multiple addresses
	epMulAddrs := &model.IstioEndpoint{
		Addresses:       []string{"1.1.1.1", "2001:1::1"},
		ServicePortName: "http",
	}
	cache.Update(hostname, "slice1", []*model.IstioEndpoint{ep1, epMulAddrs})
	if !testEndpointsEqual(cache.Get(hostname), []*model.IstioEndpoint{ep1, epMulAddrs}) {
		t.Fatal("unexpected endpoints")
	}
	if !cache.Has(hostname) {
		t.Fatal("expect to find the host name")
	}
	// add a new endpoint
	ep2 := &model.IstioEndpoint{
		Addresses:       []string{"2.3.4.5"},
		ServicePortName: "http",
	}
	cache.Update(hostname, "slice1", []*model.IstioEndpoint{ep1, epMulAddrs, ep2})
	if !testEndpointsEqual(cache.Get(hostname), []*model.IstioEndpoint{ep1, epMulAddrs, ep2}) {
		t.Fatal("unexpected endpoints")
	}

	// change service port name
	ep1 = &model.IstioEndpoint{
		Addresses:       []string{"1.2.3.4"},
		ServicePortName: "http2",
	}
	epMulAddrs = &model.IstioEndpoint{
		Addresses:       []string{"1.1.1.1", "2001:1::1"},
		ServicePortName: "http2",
	}
	ep2 = &model.IstioEndpoint{
		Addresses:       []string{"2.3.4.5"},
		ServicePortName: "http2",
	}
	cache.Update(hostname, "slice1", []*model.IstioEndpoint{ep1, epMulAddrs, ep2})
	if !testEndpointsEqual(cache.Get(hostname), []*model.IstioEndpoint{ep1, epMulAddrs, ep2}) {
		t.Fatal("unexpected endpoints")
	}

	// add a new slice
	ep3 := &model.IstioEndpoint{
		Addresses:       []string{"3.4.5.6"},
		ServicePortName: "http2",
	}
	cache.Update(hostname, "slice2", []*model.IstioEndpoint{ep3})
	if !testEndpointsEqual(cache.Get(hostname), []*model.IstioEndpoint{ep1, epMulAddrs, ep2, ep3}) {
		t.Fatal("unexpected endpoints")
	}

	// dedup when transitioning
	cache.Update(hostname, "slice2", []*model.IstioEndpoint{ep2, ep3})
	if !testEndpointsEqual(cache.Get(hostname), []*model.IstioEndpoint{ep1, epMulAddrs, ep2, ep3}) {
		t.Fatal("unexpected endpoints")
	}

	cache.Delete(hostname, "slice1")
	if !testEndpointsEqual(cache.Get(hostname), []*model.IstioEndpoint{ep2, ep3}) {
		t.Fatal("unexpected endpoints")
	}

	cache.Delete(hostname, "slice2")
	if cache.Get(hostname) != nil {
		t.Fatal("unexpected endpoints")
	}
}

func testEndpointsEqual(a, b []*model.IstioEndpoint) bool {
	if len(a) != len(b) {
		return false
	}
	m1 := make(map[endpointKey]int)
	m2 := make(map[endpointKey]int)
	for _, i := range a {
		m1[endpointKey{i.FirstAddressOrNil(), i.ServicePortName}]++
	}
	for _, i := range b {
		m2[endpointKey{i.FirstAddressOrNil(), i.ServicePortName}]++
	}
	return reflect.DeepEqual(m1, m2)
}

func TestUpdateEndpointCacheForSlice(t *testing.T) {
	const (
		ns      = "nsa"
		svcName = "svc1"
		podName = "pod1"
		appName = "prod-app"
	)

	portName := "tcp-port"
	portNum := int32(8080)

	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})

	node := generateNode("node1", map[string]string{
		NodeZoneLabel:              "zone1",
		NodeRegionLabel:            "region1",
		label.TopologySubzone.Name: "subzone1",
	})
	addNodes(t, controller, node)

	pod := generatePod([]string{"128.0.0.1"}, podName, ns, "svcaccount", "node1",
		map[string]string{"app": appName}, map[string]string{})

	addPods(t, controller, fx, pod)

	createServiceWait(controller, svcName, ns, []string{"10.0.0.1"}, nil, nil,
		[]int32{portNum}, map[string]string{"app": appName}, t)

	// Ensure that the service is available.
	hostname := kube.ServiceHostname(svcName, ns, controller.opts.DomainSuffix)
	svc := controller.GetService(hostname)
	if svc == nil {
		t.Fatal("failed to get service")
	}

	ref := &corev1.ObjectReference{
		Kind:      "Pod",
		Namespace: ns,
		Name:      podName,
	}
	// Add the reference to the service. Used by EndpointSlice logic only.
	labels := make(map[string]string)
	labels[discovery.LabelServiceName] = svcName
	eas := make([]corev1.EndpointAddress, 0)
	eas = append(eas, corev1.EndpointAddress{IP: "128.0.0.1", TargetRef: ref})

	eps := make([]corev1.EndpointPort, 0)
	eps = append(eps, corev1.EndpointPort{Name: portName, Port: portNum})

	// Endpoints is deprecated in k8s >=1.33, but we should still support it.
	// nolint: staticcheck
	endpoint := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
			Labels:    labels,
		},
		Subsets: []corev1.EndpointSubset{{
			Addresses: eas,
			Ports:     eps,
		}},
	}

	// Endpoints is deprecated in k8s >=1.33, but we should still support it.
	// nolint: staticcheck
	clienttest.NewWriter[*corev1.Endpoints](t, controller.client).CreateOrUpdate(endpoint)

	esps := make([]discovery.EndpointPort, 0)
	esps = append(esps, discovery.EndpointPort{Name: &portName, Port: &portNum})

	sliceEndpoint := make([]discovery.Endpoint, 0, 2)
	// Add IPv4 slice endpoint for the istioEndpoint
	sliceEndpoint = append(sliceEndpoint, discovery.Endpoint{
		Addresses: []string{"128.0.0.1"},
		TargetRef: ref,
	})

	// Add slice endpoint for a istioEndpoint
	endpointSlice := &discovery.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
			Labels:    labels,
		},
		Endpoints: sliceEndpoint,
		Ports:     esps,
	}

	expectedIstioEP := &model.IstioEndpoint{
		Addresses:       []string{"128.0.0.1"},
		ServicePortName: "tcp-port",
	}
	controller.endpoints.updateEndpointCacheForSlice(hostname, endpointSlice)
	istioEPs := controller.endpoints.endpointCache.Get(hostname)

	if len(istioEPs) == 0 {
		t.Errorf("Failed: no istioEndpoint instance can be found based on host name [%v]", hostname)
	}
	if len(istioEPs) != 1 {
		t.Errorf("Failed: the number of istioEndpoint instance is incorrect, expected %v, but got %v", 1, len(istioEPs))
	}
	if len(istioEPs[0].Addresses) != len(expectedIstioEP.Addresses) {
		t.Errorf("Failed: the istioEndpoint has different Addresses, expected %v, but got %v", len(expectedIstioEP.Addresses), len(istioEPs[0].Addresses))
	}

	// Check the IP address of the istioEndpoint
	var containIPaddr bool
	for _, addr := range istioEPs[0].Addresses {
		containIPaddr = false
		for _, expectedAddr := range expectedIstioEP.Addresses {
			if addr == expectedAddr {
				containIPaddr = true
			}
		}
		if !containIPaddr {
			t.Errorf("The istioEndpoint IP address [%v] is unexpected", addr)
		}
	}
}

func TestUpdateEndpointCacheForSliceWithMultiAddrs(t *testing.T) {
	const (
		ns      = "nsa"
		svcName = "svc1"
		podName = "pod1"
		appName = "prod-app"
	)

	portName := "tcp-port"
	portNum := int32(8080)

	// Enable the Dual Stack features for testing UpdateEndpointCacheForSlice
	test.SetForTest(t, &features.EnableDualStack, true)

	dualStackPod := generatePod([]string{"128.0.0.1", "2001:1::1"}, podName, ns, "svcaccount", "node1",
		map[string]string{"app": appName}, map[string]string{})

	basicService := generateService(svcName, ns, nil, nil, []int32{portNum}, map[string]string{"app": appName}, []string{"10.0.0.1", "2001:1::255"})
	serviceWithoutSelector := basicService.DeepCopy()
	serviceWithoutSelector.Spec.Selector = nil
	hostname := kube.ServiceHostname(svcName, ns, defaultFakeDomainSuffix)

	podReference := &corev1.ObjectReference{
		Kind:      "Pod",
		Namespace: ns,
		Name:      podName,
	}
	// Add the reference to the service. Used by EndpointSlice logic only.
	sliceLabels := map[string]string{
		discovery.LabelServiceName: svcName,
	}
	managedsliceLabels := map[string]string{
		discovery.LabelServiceName: svcName,
		discovery.LabelManagedBy:   "endpointslice-controller.k8s.io",
	}

	cases := []struct {
		name      string
		pod       *corev1.Pod
		service   *corev1.Service
		slices    []*discovery.EndpointSlice
		assertion [][]string
	}{
		{
			name:    "simple dual stack",
			pod:     dualStackPod,
			service: basicService,
			slices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v4",
						Namespace: ns,
						Labels:    managedsliceLabels,
					},
					AddressType: discovery.AddressTypeIPv4,
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"128.0.0.1"},
							TargetRef: podReference,
						},
					},
					Ports: []discovery.EndpointPort{{
						Name: &portName, Port: &portNum,
					}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v6",
						Namespace: ns,
						Labels:    managedsliceLabels,
					},
					AddressType: discovery.AddressTypeIPv6,
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"2001:1::1"},
							TargetRef: podReference,
						},
					},
					Ports: []discovery.EndpointPort{{
						Name: &portName, Port: &portNum,
					}},
				},
			},
			assertion: [][]string{{"128.0.0.1", "2001:1::1"}},
		},
		{
			name:    "dual stack but only one endpointslice",
			pod:     dualStackPod,
			service: basicService,
			slices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v4",
						Namespace: ns,
						Labels:    managedsliceLabels,
					},
					AddressType: discovery.AddressTypeIPv4,
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"128.0.0.1"},
							TargetRef: podReference,
						},
					},
					Ports: []discovery.EndpointPort{{
						Name: &portName, Port: &portNum,
					}},
				},
			},
			// TODO: should be only 128.0.0.1 probably
			assertion: [][]string{{"128.0.0.1", "2001:1::1"}},
		},
		{
			name:    "manual endpoints without targetRef",
			pod:     dualStackPod,
			service: serviceWithoutSelector,
			slices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "manual-v4",
						Namespace: ns,
						Labels:    sliceLabels,
					},
					AddressType: discovery.AddressTypeIPv4,
					Endpoints: []discovery.Endpoint{
						// No targetRef! But happens to match a pod IP
						{Addresses: []string{"128.0.0.1"}},
						// Completely random IP
						{Addresses: []string{"129.0.0.1"}},
					},
					Ports: []discovery.EndpointPort{{
						Name: &portName, Port: &portNum,
					}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "manual-v6",
						Namespace: ns,
						Labels:    sliceLabels,
					},
					AddressType: discovery.AddressTypeIPv6,
					Endpoints: []discovery.Endpoint{
						// No targetRef! But happens to match a pod IP
						{Addresses: []string{"2001:1::1"}},
						// Completely random IP
						{Addresses: []string{"2001:1::42"}},
					},
					Ports: []discovery.EndpointPort{{
						Name: &portName, Port: &portNum,
					}},
				},
			},
			assertion: [][]string{{"128.0.0.1"}, {"129.0.0.1"}, {"2001:1::1"}, {"2001:1::42"}},
		},
		{
			name:    "manual endpoints with targetRef",
			pod:     dualStackPod,
			service: serviceWithoutSelector,
			slices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "manual-v4",
						Namespace: ns,
						Labels:    sliceLabels,
					},
					AddressType: discovery.AddressTypeIPv4,
					Endpoints: []discovery.Endpoint{
						{Addresses: []string{"128.0.0.1"}, TargetRef: podReference},
						// Completely random IP
						{Addresses: []string{"129.0.0.1"}},
					},
					Ports: []discovery.EndpointPort{{
						Name: &portName, Port: &portNum,
					}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "manual-v6",
						Namespace: ns,
						Labels:    sliceLabels,
					},
					AddressType: discovery.AddressTypeIPv6,
					Endpoints: []discovery.Endpoint{
						{Addresses: []string{"2001:1::1"}, TargetRef: podReference},
						// Completely random IP
						{Addresses: []string{"2001:1::42"}},
					},
					Ports: []discovery.EndpointPort{{
						Name: &portName, Port: &portNum,
					}},
				},
			},
			assertion: [][]string{{"128.0.0.1", "2001:1::1"}, {"129.0.0.1"}, {"2001:1::42"}},
		},
		{
			name:    "mixed manual and selector",
			pod:     dualStackPod,
			service: basicService,
			slices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v4",
						Namespace: ns,
						Labels:    managedsliceLabels,
					},
					AddressType: discovery.AddressTypeIPv4,
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"128.0.0.1"},
							TargetRef: podReference,
						},
					},
					Ports: []discovery.EndpointPort{{
						Name: &portName, Port: &portNum,
					}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v6",
						Namespace: ns,
						Labels:    managedsliceLabels,
					},
					AddressType: discovery.AddressTypeIPv6,
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"2001:1::1"},
							TargetRef: podReference,
						},
					},
					Ports: []discovery.EndpointPort{{
						Name: &portName, Port: &portNum,
					}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "manual-v4",
						Namespace: ns,
						Labels:    sliceLabels,
					},
					AddressType: discovery.AddressTypeIPv4,
					Endpoints: []discovery.Endpoint{
						// Completely random IP
						{Addresses: []string{"129.0.0.1"}},
					},
					Ports: []discovery.EndpointPort{{
						Name: &portName, Port: &portNum,
					}},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "manual-v6",
						Namespace: ns,
						Labels:    sliceLabels,
					},
					AddressType: discovery.AddressTypeIPv6,
					Endpoints: []discovery.Endpoint{
						// Completely random IP
						{Addresses: []string{"2001:1::42"}},
					},
					Ports: []discovery.EndpointPort{{
						Name: &portName, Port: &portNum,
					}},
				},
			},
			assertion: [][]string{{"128.0.0.1", "2001:1::1"}, {"129.0.0.1"}, {"2001:1::42"}},
		},
		{
			name:    "unknown pod targetRef",
			pod:     dualStackPod,
			service: basicService,
			slices: []*discovery.EndpointSlice{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "v4",
						Namespace: ns,
						Labels:    managedsliceLabels,
					},
					AddressType: discovery.AddressTypeIPv4,
					Endpoints: []discovery.Endpoint{
						{
							Addresses: []string{"128.0.0.1"},
							TargetRef: podReference,
						},
						{
							Addresses: []string{"129.0.0.1"},
							TargetRef: &corev1.ObjectReference{
								Kind:      "Pod",
								Namespace: ns,
								Name:      "unknown-pod-name",
							},
						},
					},
					Ports: []discovery.EndpointPort{{
						Name: &portName, Port: &portNum,
					}},
				},
			},
			// 129.0.0.1 references a pod, but we don't know about it (yet?). Do not include it. Otherwise we would send traffic
			// to it but without valid configuration.
			assertion: [][]string{{"128.0.0.1", "2001:1::1"}},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})
			eps := clienttest.Wrap(t, controller.endpoints.slices)

			node := generateNode("node1", map[string]string{
				NodeZoneLabel:              "zone1",
				NodeRegionLabel:            "region1",
				label.TopologySubzone.Name: "subzone1",
			})
			addNodes(t, controller, node)

			addPods(t, controller, fx, tt.pod)
			clienttest.Wrap(t, controller.services).CreateOrUpdate(tt.service)
			controller.opts.XDSUpdater.(*xdsfake.Updater).WaitOrFail(t, "service")

			for _, ep := range tt.slices {
				eps.Create(ep)
			}
			assert.EventuallyEqual(t, func() [][]string {
				iep := controller.endpoints.endpointCache.Get(hostname)
				return slices.SortBy(slices.Map(iep, func(e *model.IstioEndpoint) []string {
					return e.Addresses
				}), func(a []string) string {
					return strings.Join(a, ",")
				})
			}, tt.assertion)
		})
	}
}
