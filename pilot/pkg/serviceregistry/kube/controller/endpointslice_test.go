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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	mcs "sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/host"
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

	pod := generatePod("128.0.0.1", "pod1", ns, "svcaccount", "node1",
		map[string]string{"app": appName}, map[string]string{})
	pods := []*corev1.Pod{pod}
	addPods(t, controller, fx, pods...)

	createServiceWait(controller, svcName, ns, nil, nil,
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
		Address:         "1.2.3.4",
		ServicePortName: "http",
	}
	cache.Update(hostname, "slice1", []*model.IstioEndpoint{ep1})
	if !testEndpointsEqual(cache.Get(hostname), []*model.IstioEndpoint{ep1}) {
		t.Fatalf("unexpected endpoints")
	}
	if !cache.Has(hostname) {
		t.Fatalf("expect to find the host name")
	}
	// add a new endpoint
	ep2 := &model.IstioEndpoint{
		Address:         "2.3.4.5",
		ServicePortName: "http",
	}
	cache.Update(hostname, "slice1", []*model.IstioEndpoint{ep1, ep2})
	if !testEndpointsEqual(cache.Get(hostname), []*model.IstioEndpoint{ep1, ep2}) {
		t.Fatalf("unexpected endpoints")
	}

	// change service port name
	ep1 = &model.IstioEndpoint{
		Address:         "1.2.3.4",
		ServicePortName: "http2",
	}
	ep2 = &model.IstioEndpoint{
		Address:         "2.3.4.5",
		ServicePortName: "http2",
	}
	cache.Update(hostname, "slice1", []*model.IstioEndpoint{ep1, ep2})
	if !testEndpointsEqual(cache.Get(hostname), []*model.IstioEndpoint{ep1, ep2}) {
		t.Fatalf("unexpected endpoints")
	}

	// add a new slice
	ep3 := &model.IstioEndpoint{
		Address:         "3.4.5.6",
		ServicePortName: "http2",
	}
	cache.Update(hostname, "slice2", []*model.IstioEndpoint{ep3})
	if !testEndpointsEqual(cache.Get(hostname), []*model.IstioEndpoint{ep1, ep2, ep3}) {
		t.Fatalf("unexpected endpoints")
	}

	// dedup when transitioning
	cache.Update(hostname, "slice2", []*model.IstioEndpoint{ep2, ep3})
	if !testEndpointsEqual(cache.Get(hostname), []*model.IstioEndpoint{ep1, ep2, ep3}) {
		t.Fatalf("unexpected endpoints")
	}

	cache.Delete(hostname, "slice1")
	if !testEndpointsEqual(cache.Get(hostname), []*model.IstioEndpoint{ep2, ep3}) {
		t.Fatalf("unexpected endpoints")
	}

	cache.Delete(hostname, "slice2")
	if cache.Get(hostname) != nil {
		t.Fatalf("unexpected endpoints")
	}
}

func testEndpointsEqual(a, b []*model.IstioEndpoint) bool {
	if len(a) != len(b) {
		return false
	}
	m1 := make(map[endpointKey]int)
	m2 := make(map[endpointKey]int)
	for _, i := range a {
		m1[endpointKey{i.Address, i.ServicePortName}]++
	}
	for _, i := range b {
		m2[endpointKey{i.Address, i.ServicePortName}]++
	}
	return reflect.DeepEqual(m1, m2)
}
