// Copyright 2017 Istio Authors
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
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	coreV1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"

	"istio.io/api/annotation"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/pkg/log"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/env"
)

func makeClient(t *testing.T) kubernetes.Interface {
	// Don't depend on symlink, and don't use real cluster.
	// This is the local kube config matching localhost (testEnvLocalK8S.sh start)
	kubeconfig := filepath.Join(env.IstioSrc, "tests/util/kubeconfig")
	client, err := CreateInterface(kubeconfig)
	if err != nil {
		t.Skipf("Unable to create kube client from config %s, skipping test. Error: %v", kubeconfig, err)
	}

	// Verify that we can connect to the API server.
	_, err = client.CoreV1().Namespaces().List(metaV1.ListOptions{})
	if err != nil {
		t.Skipf("Unable to connect kube client from config %s, skipping test. Error: %v", kubeconfig, err)
	}

	return client
}

const (
	testService  = "test"
	resync       = 1 * time.Second
	domainSuffix = "company.com"
)

func (fx *FakeXdsUpdater) ConfigUpdate(*model.PushRequest) {
	select {
	case fx.Events <- XdsEvent{Type: "xds"}:
	default:
	}
}

func (fx *FakeXdsUpdater) ProxyUpdate(clusterID, ip string) {
	select {
	case fx.Events <- XdsEvent{Type: "proxy"}:
	default:
	}
}

// FakeXdsUpdater is used to test the registry.
type FakeXdsUpdater struct {
	// Events tracks notifications received by the updater
	Events chan XdsEvent
}

// XdsEvent is used to watch XdsEvents
type XdsEvent struct {
	// Type of the event
	Type string

	// The id of the event
	ID string
}

// NewFakeXDS creates a XdsUpdater reporting events via a channel.
func NewFakeXDS() *FakeXdsUpdater {
	return &FakeXdsUpdater{
		Events: make(chan XdsEvent, 100),
	}
}

func (fx *FakeXdsUpdater) EDSUpdate(shard, hostname string, namespace string, entry []*model.IstioEndpoint) error {
	select {
	case fx.Events <- XdsEvent{Type: "eds", ID: hostname}:
	default:
	}
	return nil

}

// SvcUpdate is called when a service port mapping definition is updated.
// This interface is WIP - labels, annotations and other changes to service may be
// updated to force a EDS and CDS recomputation and incremental push, as it doesn't affect
// LDS/RDS.
func (fx *FakeXdsUpdater) SvcUpdate(shard, hostname string, ports map[string]uint32, rports map[uint32]string) {
	select {
	case fx.Events <- XdsEvent{Type: "service", ID: hostname}:
	default:
	}
}

func (fx *FakeXdsUpdater) Wait(et string) *XdsEvent {
	for {
		select {
		case e := <-fx.Events:
			if e.Type == et {
				return &e
			}
			continue
		case <-time.After(5 * time.Second):
			return nil
		}
	}
}

// Clear any pending event
func (fx *FakeXdsUpdater) Clear() {
	wait := true
	for wait {
		select {
		case <-fx.Events:
		default:
			wait = false
		}
	}
}

func newLocalController(t *testing.T) (*Controller, *FakeXdsUpdater) {
	fx := NewFakeXDS()
	ki := makeClient(t)
	ctl := NewController(ki, Options{
		WatchedNamespace: "",
		ResyncPeriod:     resync,
		DomainSuffix:     domainSuffix,
		XDSUpdater:       fx,
	})
	ctl.Env = &model.Environment{
		Mesh: &meshconfig.MeshConfig{
			MixerCheckServer: "mixer",
		},
	}
	go ctl.Run(ctl.stop)
	return ctl, fx
}

func newFakeController(_ *testing.T) (*Controller, *FakeXdsUpdater) {
	fx := NewFakeXDS()
	clientSet := fake.NewSimpleClientset()
	c := NewController(clientSet, Options{
		WatchedNamespace: "", // tests create resources in multiple ns
		ResyncPeriod:     resync,
		DomainSuffix:     domainSuffix,
		XDSUpdater:       fx,
	})
	_ = c.AppendInstanceHandler(func(instance *model.ServiceInstance, event model.Event) {})
	_ = c.AppendServiceHandler(func(service *model.Service, event model.Event) {})
	c.Env = &model.Environment{
		Mesh: &meshconfig.MeshConfig{
			MixerCheckServer: "mixer",
		},
	}
	go c.Run(c.stop)
	return c, fx
}

func TestServices(t *testing.T) {
	ctl, fx := newFakeController(t)
	defer ctl.Stop()
	t.Parallel()
	ns := "ns-test"

	hostname := kube.ServiceHostname(testService, ns, domainSuffix)

	var sds model.ServiceDiscovery = ctl
	// "test", ports: http-example on 80
	makeService(testService, ns, ctl.client, t)
	<-fx.Events

	test.Eventually(t, "successfully added a service", func() bool {
		out, clientErr := sds.Services()
		if clientErr != nil {
			return false
		}
		log.Infof("Services: %#v", out)

		// Original test was checking for 'protocolTCP' - which is incorrect (the
		// port name is 'http'. It was working because the Service was created with
		// an invalid protocol, and the code was ignoring that ( not TCP/UDP).
		for _, item := range out {
			if item.Hostname == hostname &&
				len(item.Ports) == 1 &&
				item.Ports[0].Protocol == protocol.HTTP {
				return true
			}
		}
		return false
	})

	ctl.Env = &model.Environment{
		Mesh: &meshconfig.MeshConfig{
			MixerCheckServer: "mixer",
		},
		MeshNetworks: &meshconfig.MeshNetworks{
			Networks: map[string]*meshconfig.Network{
				"network1": {
					Endpoints: []*meshconfig.Network_NetworkEndpoints{
						{
							Ne: &meshconfig.Network_NetworkEndpoints_FromCidr{
								FromCidr: "10.10.1.1/24",
							},
						},
					},
				},
				"network2": {
					Endpoints: []*meshconfig.Network_NetworkEndpoints{
						{
							Ne: &meshconfig.Network_NetworkEndpoints_FromCidr{
								FromCidr: "10.11.1.1/24",
							},
						},
					},
				},
			},
		},
	}
	ctl.InitNetworkLookup(ctl.Env.MeshNetworks)

	// 2 ports 1001, 2 IPs
	createEndpoints(ctl, testService, ns, []string{"http-example", "foo"}, []string{"10.10.1.1", "10.11.1.2"}, t)

	svc, err := sds.GetService(hostname)
	if err != nil {
		t.Errorf("GetService(%q) encountered unexpected error: %v", hostname, err)
	}
	if svc == nil {
		t.Errorf("GetService(%q) => should exists", hostname)
	}
	if svc.Hostname != hostname {
		t.Errorf("GetService(%q) => %q", hostname, svc.Hostname)
	}

	test.Eventually(t, "successfully created endpoints", func() bool {
		ep, anotherErr := sds.InstancesByPort(svc, 80, nil)
		if anotherErr != nil {
			t.Errorf("error gettings instance by port: %v", anotherErr)
			return false
		}
		if len(ep) == 2 {
			return true
		}
		return false
	})

	ep, err := sds.InstancesByPort(svc, 80, nil)
	if err != nil {
		t.Errorf("GetInstancesByPort() encountered unexpected error: %v", err)
	}
	if len(ep) != 2 {
		t.Errorf("Invalid response for GetInstancesByPort %v", ep)
	}

	if ep[0].Endpoint.Address == "10.10.1.1" && ep[0].Endpoint.Network != "network1" {
		t.Errorf("Endpoint with IP 10.10.1.1 is expected to be in network1 but get: %s", ep[0].Endpoint.Network)
	}

	if ep[1].Endpoint.Address == "10.11.1.2" && ep[1].Endpoint.Network != "network2" {
		t.Errorf("Endpoint with IP 10.11.1.2 is expected to be in network2 but get: %s", ep[1].Endpoint.Network)
	}

	missing := kube.ServiceHostname("does-not-exist", ns, domainSuffix)
	svc, err = sds.GetService(missing)
	if err != nil {
		t.Errorf("GetService(%q) encountered unexpected error: %v", missing, err)
	}
	if svc != nil {
		t.Errorf("GetService(%q) => %s, should not exist", missing, svc.Hostname)
	}
}

func makeService(n, ns string, cl kubernetes.Interface, t *testing.T) {
	_, err := cl.CoreV1().Services(ns).Create(&coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{Name: n},
		Spec: coreV1.ServiceSpec{
			Ports: []coreV1.ServicePort{
				{
					Port:     80,
					Name:     "http-example",
					Protocol: coreV1.ProtocolTCP, // Not added automatically by fake
				},
			},
		},
	})
	if err != nil {
		t.Log("Service already created (rerunning test)")
	}
	log.Infof("Created service %s", n)
}

func TestController_GetPodLocality(t *testing.T) {
	t.Parallel()
	pod1 := generatePod("128.0.1.1", "pod1", "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
	pod2 := generatePod("128.0.1.2", "pod2", "nsB", "", "node2", map[string]string{"app": "prod-app"}, map[string]string{})
	podOverride := generatePod("128.0.1.2", "pod2", "nsB", "",
		"node1", map[string]string{"app": "prod-app", model.LocalityLabel: "regionOverride.zoneOverride.subzoneOverride"}, map[string]string{})
	testCases := []struct {
		name   string
		pods   []*coreV1.Pod
		nodes  []*coreV1.Node
		wantAZ map[*coreV1.Pod]string
	}{
		{
			name: "should return correct az for given address",
			pods: []*coreV1.Pod{pod1, pod2},
			nodes: []*coreV1.Node{
				generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1"}),
				generateNode("node2", map[string]string{NodeZoneLabel: "zone2", NodeRegionLabel: "region2"}),
			},
			wantAZ: map[*coreV1.Pod]string{
				pod1: "region1/zone1",
				pod2: "region2/zone2",
			},
		},
		{
			name: "should return false if pod isnt in the cache",
			wantAZ: map[*coreV1.Pod]string{
				pod1: "",
				pod2: "",
			},
		},
		{
			name: "should return false if node isnt in the cache",
			pods: []*coreV1.Pod{pod1, pod2},
			wantAZ: map[*coreV1.Pod]string{
				pod1: "",
				pod2: "",
			},
		},
		{
			name: "should return correct az if node has only region label",
			pods: []*coreV1.Pod{pod1, pod2},
			nodes: []*coreV1.Node{
				generateNode("node1", map[string]string{NodeRegionLabel: "region1"}),
				generateNode("node2", map[string]string{NodeRegionLabel: "region2"}),
			},
			wantAZ: map[*coreV1.Pod]string{
				pod1: "region1/",
				pod2: "region2/",
			},
		},
		{
			name: "should return correct az if node has only zone label",
			pods: []*coreV1.Pod{pod1, pod2},
			nodes: []*coreV1.Node{
				generateNode("node1", map[string]string{NodeZoneLabel: "zone1"}),
				generateNode("node2", map[string]string{NodeZoneLabel: "zone2"}),
			},
			wantAZ: map[*coreV1.Pod]string{
				pod1: "/zone1",
				pod2: "/zone2",
			},
		},
		{
			name: "should return correct az for given address",
			pods: []*coreV1.Pod{podOverride},
			nodes: []*coreV1.Node{
				generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1"}),
			},
			wantAZ: map[*coreV1.Pod]string{
				podOverride: "regionOverride/zoneOverride/subzoneOverride",
			},
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {

			// Setup kube caches
			controller, fx := newFakeController(t)
			defer controller.Stop()
			addNodes(t, controller, c.nodes...)
			addPods(t, controller, c.pods...)
			for _, pod := range c.pods {
				if err := waitForPod(controller, pod.Status.PodIP); err != nil {
					t.Errorf("wait for pod err: %v", err)
				}
				// pod first time occur will trigger xds push
				fx.Wait("xds")
			}

			// Verify expected existing pod AZs
			for pod, wantAZ := range c.wantAZ {
				az := controller.GetPodLocality(pod)
				if wantAZ != "" {
					if !reflect.DeepEqual(az, wantAZ) {
						t.Errorf("Wanted az: %s, got: %s", wantAZ, az)
					}
				} else {
					if az != "" {
						t.Errorf("Unexpectedly found az: %s for pod: %s", az, pod.ObjectMeta.Name)
					}
				}
			}
		})
	}

}

func TestGetProxyServiceInstances(t *testing.T) {
	controller, fx := newFakeController(t)
	defer controller.Stop()
	p := generatePod("128.0.0.1", "pod1", "nsa", "foo", "node1", map[string]string{"app": "test-app"}, map[string]string{})
	addPods(t, controller, p)
	if err := waitForPod(controller, p.Status.PodIP); err != nil {
		t.Errorf("wait for pod err: %v", err)
	}

	k8sSaOnVM := "acct4"
	canonicalSaOnVM := "acctvm2@gserviceaccount2.com"

	createService(controller, "svc1", "nsa",
		map[string]string{
			annotation.AlphaKubernetesServiceAccounts.Name: k8sSaOnVM,
			annotation.AlphaCanonicalServiceAccounts.Name:  canonicalSaOnVM},
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)
	ev := fx.Wait("service")
	if ev == nil {
		t.Fatal("Timeout creating service")
	}

	// Endpoints are generated by Kubernetes from pod labels and service selectors.
	// Here we manually create them for mocking purpose.
	svc1Ips := []string{"128.0.0.1"}
	portNames := []string{"tcp-port"}
	// Create 1 endpoint that refers to a pod in the same namespace.
	createEndpoints(controller, "svc1", "nsA", portNames, svc1Ips, t)

	// Creates 100 endpoints that refers to a pod in a different namespace.
	fakeSvcCounts := 100
	for i := 0; i < fakeSvcCounts; i++ {
		svcName := fmt.Sprintf("svc-fake-%d", i)
		createService(controller, svcName, "nsfake",
			map[string]string{
				annotation.AlphaKubernetesServiceAccounts.Name: k8sSaOnVM,
				annotation.AlphaCanonicalServiceAccounts.Name:  canonicalSaOnVM},
			[]int32{8080}, map[string]string{"app": "prod-app"}, t)
		fx.Wait("service")

		createEndpoints(controller, svcName, "nsfake", portNames, svc1Ips, t)
		fx.Wait("eds")
	}

	// Create 1 endpoint that refers to a pod in the same namespace.
	createEndpoints(controller, "svc1", "nsa", portNames, svc1Ips, t)
	fx.Wait("eds")

	var svcNode model.Proxy
	svcNode.Type = model.Router
	svcNode.IPAddresses = []string{"128.0.0.1"}
	svcNode.ID = "pod1.nsa"
	svcNode.DNSDomain = "nsa.svc.cluster.local"
	svcNode.Metadata = &model.NodeMetadata{}
	services, err := controller.GetProxyServiceInstances(&svcNode)
	if err != nil {
		t.Errorf("client encountered error during GetProxyServiceInstances(): %v", err)
	}

	if len(services) != fakeSvcCounts+1 {
		t.Errorf("GetProxyServiceInstances() returned wrong # of endpoints => %d, want %d", len(services), fakeSvcCounts+1)
	}

	hostname := kube.ServiceHostname("svc1", "nsa", domainSuffix)
	if services[0].Service.Hostname != hostname {
		t.Errorf("GetProxyServiceInstances() wrong service instance returned => hostname %q, want %q",
			services[0].Service.Hostname, hostname)
	}

	// Test that we can look up instances just by Proxy metadata
	metaServices, err := controller.GetProxyServiceInstances(&model.Proxy{
		Type:            "sidecar",
		IPAddresses:     []string{"1.1.1.1"},
		Locality:        &core.Locality{Region: "r", Zone: "z"},
		ConfigNamespace: "nsa",
		Metadata:        &model.NodeMetadata{ServiceAccount: "account"},
		WorkloadLabels:  labels.Collection{labels.Instance{"app": "prod-app"}},
	})
	if err != nil {
		t.Fatalf("got err getting service instances")
	}

	expected := &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{Family: 0,
			Address:     "1.1.1.1",
			ServicePort: &model.Port{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP},
			Locality:    "r/z",
		},
		Service: &model.Service{
			Hostname:        "svc1.nsa.svc.company.com",
			Address:         "10.0.0.1",
			Ports:           []*model.Port{{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP}},
			ServiceAccounts: []string{"acctvm2@gserviceaccount2.com", "spiffe://cluster.local/ns/nsa/sa/acct4"},
			Attributes: model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.KubernetesRegistry),
				Name:            "svc1",
				Namespace:       "nsa",
				UID:             "istio://nsa/services/svc1"},
		},
		Labels:         labels.Instance{"app": "prod-app"},
		ServiceAccount: "account",
	}
	if len(metaServices) != 1 {
		t.Fatalf("expected 1 instance, got %v", len(metaServices))
	}
	if !reflect.DeepEqual(expected, metaServices[0]) {
		t.Fatalf("expected instance %v, got %v", expected, metaServices[0])
	}

	// Test that we first look up instances by Proxy pod

	node := generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1"})
	addNodes(t, controller, node)

	// 1. pod without `istio-locality` label, get locality from node label.
	p = generatePod("129.0.0.1", "pod2", "nsa", "svcaccount", "node1",
		map[string]string{"app": "prod-app"}, nil)
	addPods(t, controller, p)
	if err := waitForPod(controller, p.Status.PodIP); err != nil {
		t.Errorf("wait for pod err: %v", err)
	}

	podServices, err := controller.GetProxyServiceInstances(&model.Proxy{
		Type:            "sidecar",
		IPAddresses:     []string{"129.0.0.1"},
		Locality:        &core.Locality{Region: "r", Zone: "z"},
		ConfigNamespace: "nsa",
		Metadata:        &model.NodeMetadata{ServiceAccount: "account"},
		WorkloadLabels:  labels.Collection{labels.Instance{"app": "prod-app"}},
	})
	if err != nil {
		t.Fatalf("got err getting service instances")
	}

	expected = &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Family:      0,
			Address:     "129.0.0.1",
			ServicePort: &model.Port{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP},
			Locality:    "region1/zone1",
		},
		Service: &model.Service{
			Hostname:        "svc1.nsa.svc.company.com",
			Address:         "10.0.0.1",
			Ports:           []*model.Port{{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP}},
			ServiceAccounts: []string{"acctvm2@gserviceaccount2.com", "spiffe://cluster.local/ns/nsa/sa/acct4"},
			Attributes: model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.KubernetesRegistry),
				Name:            "svc1",
				Namespace:       "nsa",
				UID:             "istio://nsa/services/svc1"},
		},
		Labels:         labels.Instance{"app": "prod-app"},
		ServiceAccount: "spiffe://cluster.local/ns/nsa/sa/svcaccount",
		TLSMode:        model.DisabledTLSModeLabel,
	}
	if len(podServices) != 1 {
		t.Fatalf("expected 1 instance, got %v", len(podServices))
	}
	if !reflect.DeepEqual(expected, podServices[0]) {
		t.Fatalf("expected instance %v, got %v", expected, podServices[0])
	}

	// 2. pod with `istio-locality` label, ignore node label.
	p = generatePod("129.0.0.2", "pod3", "nsa", "svcaccount", "node1",
		map[string]string{"app": "prod-app", "istio-locality": "region.zone"}, nil)
	addPods(t, controller, p)
	if err := waitForPod(controller, p.Status.PodIP); err != nil {
		t.Errorf("wait for pod err: %v", err)
	}

	podServices, err = controller.GetProxyServiceInstances(&model.Proxy{
		Type:            "sidecar",
		IPAddresses:     []string{"129.0.0.2"},
		Locality:        &core.Locality{Region: "r", Zone: "z"},
		ConfigNamespace: "nsa",
		Metadata:        &model.NodeMetadata{ServiceAccount: "account"},
		WorkloadLabels:  labels.Collection{labels.Instance{"app": "prod-app"}},
	})
	if err != nil {
		t.Fatalf("got err getting service instances")
	}

	expected = &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Family:      0,
			Address:     "129.0.0.2",
			ServicePort: &model.Port{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP},
			Locality:    "region/zone",
		},
		Service: &model.Service{
			Hostname:        "svc1.nsa.svc.company.com",
			Address:         "10.0.0.1",
			Ports:           []*model.Port{{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP}},
			ServiceAccounts: []string{"acctvm2@gserviceaccount2.com", "spiffe://cluster.local/ns/nsa/sa/acct4"},
			Attributes: model.ServiceAttributes{
				ServiceRegistry: string(serviceregistry.KubernetesRegistry),
				Name:            "svc1",
				Namespace:       "nsa",
				UID:             "istio://nsa/services/svc1"},
		},
		Labels:         labels.Instance{"app": "prod-app", "istio-locality": "region.zone"},
		ServiceAccount: "spiffe://cluster.local/ns/nsa/sa/svcaccount",
		TLSMode:        model.DisabledTLSModeLabel,
	}
	if len(podServices) != 1 {
		t.Fatalf("expected 1 instance, got %v", len(podServices))
	}
	if !reflect.DeepEqual(expected, podServices[0]) {
		t.Fatalf("expected instance %v, got %v", expected, podServices[0])
	}
}

func TestGetProxyServiceInstancesWithMultiIPs(t *testing.T) {
	pod1 := generatePod("128.0.0.1", "pod1", "nsa", "foo", "node1", map[string]string{"app": "test-app"}, map[string]string{})
	testCases := []struct {
		name    string
		pods    []*coreV1.Pod
		ips     []string
		ports   []int32
		wantNum int
	}{
		{
			name:    "multiple proxy ips single port",
			pods:    []*coreV1.Pod{pod1},
			ips:     []string{"128.0.0.1", "192.168.2.6"},
			ports:   []int32{8080},
			wantNum: 2,
		},
		{
			name:    "single proxy ip single port",
			pods:    []*coreV1.Pod{pod1},
			ips:     []string{"128.0.0.1"},
			ports:   []int32{8080},
			wantNum: 1,
		},
		{
			name:    "multiple proxy ips multiple ports",
			pods:    []*coreV1.Pod{pod1},
			ips:     []string{"128.0.0.1", "192.168.2.6"},
			ports:   []int32{8080, 9090},
			wantNum: 4,
		},
		{
			name:    "single proxy ip multiple ports",
			pods:    []*coreV1.Pod{pod1},
			ips:     []string{"128.0.0.1"},
			ports:   []int32{8080, 9090},
			wantNum: 2,
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			// Setup kube caches
			controller, fx := newFakeController(t)
			defer controller.Stop()
			addPods(t, controller, c.pods...)
			for _, pod := range c.pods {
				if err := waitForPod(controller, pod.Status.PodIP); err != nil {
					t.Errorf("wait for pod err: %v", err)
				}
			}

			createService(controller, "svc1", "nsa",
				map[string]string{
					annotation.AlphaKubernetesServiceAccounts.Name: "acct4",
					annotation.AlphaCanonicalServiceAccounts.Name:  "acctvm2@gserviceaccount2.com"},
				c.ports, map[string]string{"app": "test-app"}, t)
			ev := fx.Wait("service")
			if ev == nil {
				t.Error("Timeout creating service")
			}
			serviceInstances, err := controller.GetProxyServiceInstances(&model.Proxy{Metadata: &model.NodeMetadata{}, IPAddresses: c.ips})
			if err != nil {
				t.Errorf("client encountered error during GetProxyServiceInstances(): %v", err)
			}
			if len(serviceInstances) != c.wantNum {
				t.Errorf("GetProxyServiceInstances() returned wrong # of endpoints => %q, want %q", len(serviceInstances), c.wantNum)
			}
		})
	}
}

func TestController_GetIstioServiceAccounts(t *testing.T) {
	oldTrustDomain := spiffe.GetTrustDomain()
	spiffe.SetTrustDomain(domainSuffix)
	defer spiffe.SetTrustDomain(oldTrustDomain)

	controller, fx := newFakeController(t)
	defer controller.Stop()

	sa1 := "acct1"
	sa2 := "acct2"
	sa3 := "acct3"
	k8sSaOnVM := "acct4"
	canonicalSaOnVM := "acctvm@gserviceaccount.com"

	pods := []*coreV1.Pod{
		generatePod("128.0.0.1", "pod1", "nsA", sa1, "node1", map[string]string{"app": "test-app"}, map[string]string{}),
		generatePod("128.0.0.2", "pod2", "nsA", sa2, "node2", map[string]string{"app": "prod-app"}, map[string]string{}),
		generatePod("128.0.0.3", "pod3", "nsB", sa3, "node1", map[string]string{"app": "prod-app"}, map[string]string{}),
	}
	addPods(t, controller, pods...)
	for _, pod := range pods {
		if err := waitForPod(controller, pod.Status.PodIP); err != nil {
			t.Errorf("wait for pod err: %v", err)
		}
	}

	createService(controller, "svc1", "nsA",
		map[string]string{
			annotation.AlphaKubernetesServiceAccounts.Name: k8sSaOnVM,
			annotation.AlphaCanonicalServiceAccounts.Name:  canonicalSaOnVM},
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)
	fx.Wait("service")
	createService(controller, "svc2", "nsA", nil, []int32{8080}, map[string]string{"app": "staging-app"}, t)
	fx.Wait("service")

	// Endpoints are generated by Kubernetes from pod labels and service selectors.
	// Here we manually create them for mocking purpose.
	svc1Ips := []string{"128.0.0.2"}
	svc2Ips := make([]string, 0)
	portNames := []string{"tcp-port"}
	createEndpoints(controller, "svc1", "nsA", portNames, svc1Ips, t)
	createEndpoints(controller, "svc2", "nsA", portNames, svc2Ips, t)
	for i := 0; i < 2; i++ {
		<-fx.Events
	}

	hostname := kube.ServiceHostname("svc1", "nsA", domainSuffix)
	svc, err := controller.GetService(hostname)
	if err != nil {
		t.Fatalf("failed to get service: %v", err)
	}
	sa := controller.GetIstioServiceAccounts(svc, []int{8080})
	sort.Strings(sa)
	expected := []string{
		canonicalSaOnVM,
		"spiffe://company.com/ns/nsA/sa/" + sa2,
		"spiffe://company.com/ns/nsA/sa/" + k8sSaOnVM,
	}
	if !reflect.DeepEqual(sa, expected) {
		t.Errorf("Unexpected service accounts %v (expecting %v)", sa, expected)
	}

	hostname = kube.ServiceHostname("svc2", "nsA", domainSuffix)
	svc, err = controller.GetService(hostname)
	if err != nil {
		t.Fatalf("failed to get service: %v", err)
	}
	sa = controller.GetIstioServiceAccounts(svc, []int{})
	if len(sa) != 0 {
		t.Error("Failure: Expected to resolve 0 service accounts, but got: ", sa)
	}
}

func TestWorkloadHealthCheckInfo(t *testing.T) {
	controller, _ := newFakeController(t)
	defer controller.Stop()

	pod := generatePodWithProbes("128.0.0.1", "pod1", "nsa1", "", "node1", "/ready", intstr.Parse("8080"), "/live", intstr.Parse("9090"))
	addPods(t, controller, pod)

	if err := waitForPod(controller, pod.Status.PodIP); err != nil {
		t.Errorf("wait for pod err: %v", err)
	}
	probes := controller.WorkloadHealthCheckInfo("128.0.0.1")

	expected := []*model.Probe{
		{
			Path: "/ready",
			Port: &model.Port{
				Name:     "mgmt-8080",
				Port:     8080,
				Protocol: protocol.HTTP,
			},
		},
		{
			Path: "/live",
			Port: &model.Port{
				Name:     "mgmt-9090",
				Port:     9090,
				Protocol: protocol.HTTP,
			},
		},
	}

	if len(probes) != len(expected) {
		t.Errorf("Expecting %d probes but got %d\r\n", len(expected), len(probes))
		return
	}

	for i, exp := range expected {
		if !reflect.DeepEqual(exp, probes[i]) {
			t.Errorf("Probe %d, got:\n%#v\nwanted:\n%#v\n", i, probes[i], exp)
		}
	}
}

func TestWorkloadHealthCheckInfoPrometheusScrape(t *testing.T) {
	controller, _ := newFakeController(t)
	defer controller.Stop()

	pod := generatePod("128.0.1.6", "pod1", "nsA", "", "node1", map[string]string{"app": "test-app"},
		map[string]string{PrometheusScrape: "true"})
	addPods(t, controller, pod)
	if err := waitForPod(controller, pod.Status.PodIP); err != nil {
		t.Errorf("wait for pod err: %v", err)
	}

	controller.pods.podsByIP["128.0.1.6"] = "nsA/pod1"

	probes := controller.WorkloadHealthCheckInfo("128.0.1.6")

	expected := &model.Probe{
		Path: PrometheusPathDefault,
	}

	if len(probes) != 1 {
		t.Errorf("Expecting 1 probe but got %d\r\n", len(probes))
	} else if !reflect.DeepEqual(expected, probes[0]) {
		t.Errorf("Probe got:\n%#v\nwanted:\n%#v\n", probes[0], expected)
	}
}

func TestWorkloadHealthCheckInfoPrometheusPath(t *testing.T) {
	controller, _ := newFakeController(t)
	defer controller.Stop()

	pod := generatePod("128.0.1.7", "pod1", "nsA", "", "node1", map[string]string{"app": "test-app"},
		map[string]string{PrometheusScrape: "true", PrometheusPath: "/other"})
	addPods(t, controller, pod)
	if err := waitForPod(controller, pod.Status.PodIP); err != nil {
		t.Errorf("wait for pod err: %v", err)
	}
	controller.pods.podsByIP["128.0.1.7"] = "nsA/pod1"

	probes := controller.WorkloadHealthCheckInfo("128.0.1.7")

	expected := &model.Probe{
		Path: "/other",
	}

	if len(probes) != 1 {
		t.Errorf("Expecting 1 probe but got %d\r\n", len(probes))
	} else if !reflect.DeepEqual(expected, probes[0]) {
		t.Errorf("Probe got:\n%#v\nwanted:\n%#v\n", probes[0], expected)
	}
}

func TestWorkloadHealthCheckInfoPrometheusPort(t *testing.T) {
	controller, _ := newFakeController(t)
	defer controller.Stop()

	pod := generatePod("128.0.1.8", "pod1", "nsA", "", "node1", map[string]string{"app": "test-app"},
		map[string]string{PrometheusScrape: "true", PrometheusPort: "3210"})
	addPods(t, controller, pod)
	if err := waitForPod(controller, pod.Status.PodIP); err != nil {
		t.Errorf("wait for pod err: %v", err)
	}
	controller.pods.podsByIP["128.0.1.8"] = "nsA/pod1"

	probes := controller.WorkloadHealthCheckInfo("128.0.1.8")

	expected := &model.Probe{
		Port: &model.Port{
			Port: 3210,
		},
		Path: PrometheusPathDefault,
	}

	if len(probes) != 1 {
		t.Errorf("Expecting 1 probe but got %d\r\n", len(probes))
	} else if !reflect.DeepEqual(expected, probes[0]) {
		t.Errorf("Probe got:\n%#v\nwanted:\n%#v\n", probes[0], expected)
	}
}

func TestManagementPorts(t *testing.T) {
	controller, _ := newFakeController(t)

	pod := generatePodWithProbes("128.0.0.1", "pod1", "nsA", "", "node1", "/ready", intstr.Parse("8080"), "/live", intstr.Parse("9090"))
	addPods(t, controller, pod)
	if err := waitForPod(controller, pod.Status.PodIP); err != nil {
		t.Errorf("wait for pod err: %v", err)
	}
	controller.pods.podsByIP["128.0.0.1"] = "nsA/pod1"

	portList := controller.ManagementPorts("128.0.0.1")

	expected := model.PortList{
		{
			Name:     "mgmt-8080",
			Port:     8080,
			Protocol: protocol.HTTP,
		},
		{
			Name:     "mgmt-9090",
			Port:     9090,
			Protocol: protocol.HTTP,
		},
	}

	if len(portList) != len(expected) {
		t.Errorf("Expecting %d port but got %d\r\n", len(expected), len(portList))
	}

	if !reflect.DeepEqual(expected, portList) {
		t.Errorf("got port, got:\n%#v\nwanted:\n%#v\n", portList, expected)
	}
}

func TestController_Service(t *testing.T) {
	controller, fx := newFakeController(t)
	// Use a timeout to keep the test from hanging.

	createService(controller, "svc1", "nsA",
		map[string]string{},
		[]int32{8080}, map[string]string{"test-app": "test-app-1"}, t)
	<-fx.Events
	createService(controller, "svc2", "nsA",
		map[string]string{},
		[]int32{8081}, map[string]string{"test-app": "test-app-2"}, t)
	<-fx.Events
	createService(controller, "svc3", "nsA",
		map[string]string{},
		[]int32{8082}, map[string]string{"test-app": "test-app-3"}, t)
	<-fx.Events
	createService(controller, "svc4", "nsA",
		map[string]string{},
		[]int32{8083}, map[string]string{"test-app": "test-app-4"}, t)
	<-fx.Events

	expectedSvcList := []*model.Service{
		{
			Hostname: kube.ServiceHostname("svc1", "nsA", domainSuffix),
			Address:  "10.0.0.1",
			Ports: model.PortList{
				&model.Port{
					Name:     "tcp-port",
					Port:     8080,
					Protocol: protocol.TCP,
				},
			},
		},
		{
			Hostname: kube.ServiceHostname("svc2", "nsA", domainSuffix),
			Address:  "10.0.0.1",
			Ports: model.PortList{
				&model.Port{
					Name:     "tcp-port",
					Port:     8081,
					Protocol: protocol.TCP,
				},
			},
		},
		{
			Hostname: kube.ServiceHostname("svc3", "nsA", domainSuffix),
			Address:  "10.0.0.1",
			Ports: model.PortList{
				&model.Port{
					Name:     "tcp-port",
					Port:     8082,
					Protocol: protocol.TCP,
				},
			},
		},
		{
			Hostname: kube.ServiceHostname("svc4", "nsA", domainSuffix),
			Address:  "10.0.0.1",
			Ports: model.PortList{
				&model.Port{
					Name:     "tcp-port",
					Port:     8083,
					Protocol: protocol.TCP,
				},
			},
		},
	}

	svcList, _ := controller.Services()
	if len(svcList) != len(expectedSvcList) {
		t.Fatalf("Expecting %d service but got %d\r\n", len(expectedSvcList), len(svcList))
	}
	for i, exp := range expectedSvcList {
		if exp.Hostname != svcList[i].Hostname {
			t.Errorf("got hostname of %dst service, got:\n%#v\nwanted:\n%#v\n", i, svcList[i].Hostname, exp.Hostname)
		}
		if exp.Address != svcList[i].Address {
			t.Errorf("got address of %dst service, got:\n%#v\nwanted:\n%#v\n", i, svcList[i].Address, exp.Address)
		}
		if !reflect.DeepEqual(exp.Ports, svcList[i].Ports) {
			t.Errorf("got ports of %dst service, got:\n%#v\nwanted:\n%#v\n", i, svcList[i].Ports, exp.Ports)
		}
	}
}

func TestController_ExternalNameService(t *testing.T) {
	controller, fx := newFakeController(t)
	// Use a timeout to keep the test from hanging.

	k8sSvcs := []*coreV1.Service{
		createExternalNameService(controller, "svc1", "nsA",
			[]int32{8080}, "test-app-1.test.svc."+domainSuffix, t, fx.Events),
		createExternalNameService(controller, "svc2", "nsA",
			[]int32{8081}, "test-app-2.test.svc."+domainSuffix, t, fx.Events),
		createExternalNameService(controller, "svc3", "nsA",
			[]int32{8082}, "test-app-3.test.pod."+domainSuffix, t, fx.Events),
		createExternalNameService(controller, "svc4", "nsA",
			[]int32{8083}, "g.co", t, fx.Events),
	}

	expectedSvcList := []*model.Service{
		{
			Hostname: kube.ServiceHostname("svc1", "nsA", domainSuffix),
			Ports: model.PortList{
				&model.Port{
					Name:     "tcp-port",
					Port:     8080,
					Protocol: protocol.TCP,
				},
			},
			MeshExternal: true,
			Resolution:   model.DNSLB,
		},
		{
			Hostname: kube.ServiceHostname("svc2", "nsA", domainSuffix),
			Ports: model.PortList{
				&model.Port{
					Name:     "tcp-port",
					Port:     8081,
					Protocol: protocol.TCP,
				},
			},
			MeshExternal: true,
			Resolution:   model.DNSLB,
		},
		{
			Hostname: kube.ServiceHostname("svc3", "nsA", domainSuffix),
			Ports: model.PortList{
				&model.Port{
					Name:     "tcp-port",
					Port:     8082,
					Protocol: protocol.TCP,
				},
			},
			MeshExternal: true,
			Resolution:   model.DNSLB,
		},
		{
			Hostname: kube.ServiceHostname("svc4", "nsA", domainSuffix),
			Ports: model.PortList{
				&model.Port{
					Name:     "tcp-port",
					Port:     8083,
					Protocol: protocol.TCP,
				},
			},
			MeshExternal: true,
			Resolution:   model.DNSLB,
		},
	}

	svcList, _ := controller.Services()
	if len(svcList) != len(expectedSvcList) {
		t.Fatalf("Expecting %d service but got %d\r\n", len(expectedSvcList), len(svcList))
	}
	for i, exp := range expectedSvcList {
		if exp.Hostname != svcList[i].Hostname {
			t.Errorf("got hostname of %dst service, got:\n%#v\nwanted:\n%#v\n", i, svcList[i].Hostname, exp.Hostname)
		}
		if !reflect.DeepEqual(exp.Ports, svcList[i].Ports) {
			t.Errorf("got ports of %dst service, got:\n%#v\nwanted:\n%#v\n", i, svcList[i].Ports, exp.Ports)
		}
		if svcList[i].MeshExternal != exp.MeshExternal {
			t.Errorf("i=%v, MeshExternal==%v, should be %v: externalName='%s'", i, exp.MeshExternal, svcList[i].MeshExternal, k8sSvcs[i].Spec.ExternalName)
		}
		if svcList[i].Resolution != exp.Resolution {
			t.Errorf("i=%v, Resolution=='%v', should be '%v'", i, svcList[i].Resolution, exp.Resolution)
		}
		instances, err := controller.InstancesByPort(svcList[i], svcList[i].Ports[0].Port, labels.Collection{})
		if err != nil {
			t.Errorf("error getting instances by port: %s", err)
			continue
		}
		if len(instances) != 1 {
			t.Errorf("should be exactly 1 instance: len(instances) = %v", len(instances))
		}
		if instances[0].Endpoint.Address != k8sSvcs[i].Spec.ExternalName {
			t.Errorf("wrong instance endpoint address: '%s' != '%s'", instances[0].Endpoint.Address, k8sSvcs[i].Spec.ExternalName)
		}
	}

	wg := sync.WaitGroup{}
	wg.Add(len(k8sSvcs))
	deleteHandler := func(_ *model.Service, e model.Event) {
		if e == model.EventDelete {
			wg.Done()
		}
	}
	if err := controller.AppendServiceHandler(deleteHandler); err != nil {
		t.Fatalf("Failed to append service handler: %+v", err)
	}
	for _, s := range k8sSvcs {
		deleteExternalNameService(controller, s.Name, s.Namespace, t, fx.Events)
	}
	wg.Wait()
	svcList, _ = controller.Services()
	if len(svcList) != 0 {
		t.Fatalf("Should have 0 services at this point")
	}
	for _, exp := range expectedSvcList {
		instances, err := controller.InstancesByPort(exp, exp.Ports[0].Port, labels.Collection{})
		if err != nil {
			t.Errorf("error getting instances by port: %s", err)
			continue
		}
		if len(instances) != 0 {
			t.Errorf("should be exactly 0 instance: len(instances) = %v", len(instances))
		}
	}
}

func createEndpoints(controller *Controller, name, namespace string, portNames, ips []string, t *testing.T) {
	eas := make([]coreV1.EndpointAddress, 0)
	for _, ip := range ips {
		eas = append(eas, coreV1.EndpointAddress{IP: ip})
	}

	eps := make([]coreV1.EndpointPort, 0)
	for _, name := range portNames {
		eps = append(eps, coreV1.EndpointPort{Name: name, Port: 1001})
	}

	endpoint := &coreV1.Endpoints{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Subsets: []coreV1.EndpointSubset{{
			Addresses: eas,
			Ports:     eps,
		}},
	}
	if _, err := controller.client.CoreV1().Endpoints(namespace).Create(endpoint); err != nil {
		t.Errorf("failed to create endpoints %s in namespace %s (error %v)", name, namespace, err)
	}
}

func updateEndpoints(controller *Controller, name, namespace string, portNames, ips []string, t *testing.T) {
	eas := make([]coreV1.EndpointAddress, 0)
	for _, ip := range ips {
		eas = append(eas, coreV1.EndpointAddress{IP: ip})
	}

	eps := make([]coreV1.EndpointPort, 0)
	for _, name := range portNames {
		eps = append(eps, coreV1.EndpointPort{Name: name, Port: 1001})
	}

	endpoint := &coreV1.Endpoints{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Subsets: []coreV1.EndpointSubset{{
			Addresses: eas,
			Ports:     eps,
		}},
	}
	if _, err := controller.client.CoreV1().Endpoints(namespace).Update(endpoint); err != nil {
		t.Errorf("failed to update endpoints %s in namespace %s (error %v)", name, namespace, err)
	}
}

func createService(controller *Controller, name, namespace string, annotations map[string]string,
	ports []int32, selector map[string]string, t *testing.T) {

	svcPorts := make([]coreV1.ServicePort, 0)
	for _, p := range ports {
		svcPorts = append(svcPorts, coreV1.ServicePort{
			Name:     "tcp-port",
			Port:     p,
			Protocol: "http",
		})
	}
	service := &coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: coreV1.ServiceSpec{
			ClusterIP: "10.0.0.1", // FIXME: generate?
			Ports:     svcPorts,
			Selector:  selector,
			Type:      coreV1.ServiceTypeClusterIP,
		},
	}

	_, err := controller.client.CoreV1().Services(namespace).Create(service)
	if err != nil {
		t.Errorf("Cannot create service %s in namespace %s (error: %v)", name, namespace, err)
	}
}

func createServiceWithoutClusterIP(controller *Controller, name, namespace string, annotations map[string]string,
	ports []int32, selector map[string]string, t *testing.T) {

	svcPorts := make([]coreV1.ServicePort, 0)
	for _, p := range ports {
		svcPorts = append(svcPorts, coreV1.ServicePort{
			Name:     "tcp-port",
			Port:     p,
			Protocol: "http",
		})
	}
	service := &coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: coreV1.ServiceSpec{
			ClusterIP: coreV1.ClusterIPNone,
			Ports:     svcPorts,
			Selector:  selector,
			Type:      coreV1.ServiceTypeClusterIP,
		},
	}

	_, err := controller.client.CoreV1().Services(namespace).Create(service)
	if err != nil {
		t.Errorf("Cannot create service %s in namespace %s (error: %v)", name, namespace, err)
	}
}

// nolint: unparam
func createExternalNameService(controller *Controller, name, namespace string,
	ports []int32, externalName string, t *testing.T, xdsEvents <-chan XdsEvent) *coreV1.Service {

	defer func() {
		<-xdsEvents
	}()

	svcPorts := make([]coreV1.ServicePort, 0)
	for _, p := range ports {
		svcPorts = append(svcPorts, coreV1.ServicePort{
			Name:     "tcp-port",
			Port:     p,
			Protocol: "http",
		})
	}
	service := &coreV1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: coreV1.ServiceSpec{
			Ports:        svcPorts,
			Type:         coreV1.ServiceTypeExternalName,
			ExternalName: externalName,
		},
	}

	_, err := controller.client.CoreV1().Services(namespace).Create(service)
	if err != nil {
		t.Fatalf("Cannot create service %s in namespace %s (error: %v)", name, namespace, err)
	}
	return service
}

func deleteExternalNameService(controller *Controller, name, namespace string, t *testing.T, xdsEvents <-chan XdsEvent) {

	defer func() {
		<-xdsEvents
	}()

	err := controller.client.CoreV1().Services(namespace).Delete(name, &metaV1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Cannot delete service %s in namespace %s (error: %v)", name, namespace, err)
	}
}

func addPods(t *testing.T, controller *Controller, pods ...*coreV1.Pod) {
	for _, pod := range pods {
		newPod, err := controller.client.CoreV1().Pods(pod.Namespace).Create(pod)
		if err != nil {
			t.Errorf("Cannot create %s in namespace %s (error: %v)", pod.ObjectMeta.Name, pod.ObjectMeta.Namespace, err)
		}
		// Apiserver doesn't allow Create/Update to modify the pod status. Creating doesn't result in
		// events - since PodIP will be "".
		newPod.Status.PodIP = pod.Status.PodIP
		newPod.Status.Phase = coreV1.PodRunning
		_, _ = controller.client.CoreV1().Pods(pod.Namespace).UpdateStatus(newPod)
	}
}

func generatePod(ip, name, namespace, saName, node string, labels map[string]string, annotations map[string]string) *coreV1.Pod {
	automount := false
	return &coreV1.Pod{
		ObjectMeta: metaV1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
			Namespace:   namespace,
		},
		Spec: coreV1.PodSpec{
			ServiceAccountName:           saName,
			NodeName:                     node,
			AutomountServiceAccountToken: &automount,
			// Validation requires this
			Containers: []coreV1.Container{
				{
					Name:  "test",
					Image: "ununtu",
				},
			},
		},
		// The cache controller uses this as key, required by our impl.
		Status: coreV1.PodStatus{
			PodIP:  ip,
			HostIP: ip,
			Phase:  coreV1.PodRunning,
		},
	}
}

func generatePodWithProbes(ip, name, namespace, saName, node string, readinessPath string, readinessPort intstr.IntOrString,
	livenessPath string, livenessPort intstr.IntOrString) *coreV1.Pod {
	return &coreV1.Pod{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: coreV1.PodSpec{
			ServiceAccountName: saName,
			NodeName:           node,
			Containers: []coreV1.Container{{
				ReadinessProbe: &coreV1.Probe{
					Handler: coreV1.Handler{
						HTTPGet: &coreV1.HTTPGetAction{
							Path: readinessPath,
							Port: readinessPort,
						},
					},
				},
				LivenessProbe: &coreV1.Probe{
					Handler: coreV1.Handler{
						HTTPGet: &coreV1.HTTPGetAction{
							Path: livenessPath,
							Port: livenessPort,
						},
					},
				},
			}},
		},
		// The cache controller uses this as key, required by our impl.
		Status: coreV1.PodStatus{
			PodIP:  ip,
			HostIP: ip,
			Phase:  coreV1.PodRunning,
		},
	}
}

func generateNode(name string, labels map[string]string) *coreV1.Node {
	return &coreV1.Node{
		ObjectMeta: metaV1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func addNodes(t *testing.T, controller *Controller, nodes ...*coreV1.Node) {
	for _, node := range nodes {
		if _, err := controller.client.CoreV1().Nodes().Create(node); err != nil {
			// if err := controller.nodes.informer.GetStore().Add(node); err != nil {
			t.Errorf("Cannot create node %s (error: %v)", node.Name, err)
		}
	}
}

func TestEndpointUpdate(t *testing.T) {
	controller, fx := newFakeController(t)
	defer controller.Stop()

	// 1. incremental eds for normal service endpoint update

	createService(controller, "svc1", "nsa", nil,
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)
	if ev := fx.Wait("service"); ev == nil {
		t.Fatal("Timeout creating service")
	}

	// Endpoints are generated by Kubernetes from pod labels and service selectors.
	// Here we manually create them for mocking purpose.
	svc1Ips := []string{"128.0.0.1"}
	portNames := []string{"tcp-port"}
	// Create 1 endpoint that refers to a pod in the same namespace.
	createEndpoints(controller, "svc1", "nsa", portNames, svc1Ips, t)
	if ev := fx.Wait("eds"); ev == nil {
		t.Errorf("Timeout incremental eds")
	}

	// delete normal service
	err := controller.client.CoreV1().Services("nsa").Delete("svc1", &metaV1.DeleteOptions{})
	if err != nil {
		t.Errorf("Cannot delete service (error: %v)", err)
	}
	if ev := fx.Wait("service"); ev == nil {
		t.Errorf("Timeout deleting service")
	}

	// 2. full xds push request for headless service endpoint update

	// create a headless service
	createServiceWithoutClusterIP(controller, "svc1", "nsa", nil,
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)
	if ev := fx.Wait("service"); ev == nil {
		t.Errorf("Timeout creating service")
	}

	// Create 1 endpoint that refers to a pod in the same namespace.
	svc1Ips = append(svc1Ips, "128.0.0.2")
	updateEndpoints(controller, "svc1", "nsa", portNames, svc1Ips, t)
	if ev := fx.Wait("xds"); ev == nil {
		t.Errorf("Timeout xds push")
	}
}
