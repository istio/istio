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
	"context"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/api/annotation"
	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	clientnetworking "istio.io/client-go/pkg/apis/networking/v1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	labelutil "istio.io/istio/pilot/pkg/serviceregistry/util/label"
	"istio.io/istio/pilot/pkg/serviceregistry/util/xdsfake"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/config/visibility"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	pm "istio.io/istio/pkg/model"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
)

const (
	testService = "test"
)

// eventually polls cond until it completes (returns true) or times out (resulting in a test failure).
func eventually(t test.Failer, cond func() bool) {
	t.Helper()
	retry.UntilOrFail(t, cond, retry.Timeout(time.Second), retry.Delay(time.Millisecond*10))
}

func TestServices(t *testing.T) {
	networksWatcher := meshwatcher.NewFixedNetworksWatcher(&meshconfig.MeshNetworks{
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
	})

	ctl, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{NetworksWatcher: networksWatcher})
	t.Parallel()
	ns := "ns-test"

	hostname := kube.ServiceHostname(testService, ns, defaultFakeDomainSuffix)

	var sds model.ServiceDiscovery = ctl
	// "test", ports: http-example on 80
	makeService(testService, ns, ctl, t)

	eventually(t, func() bool {
		out := sds.Services()

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

	// 2 ports 1001, 2 IPs
	createEndpoints(t, ctl, testService, ns, []string{"http-example", "foo"}, []string{"10.10.1.1", "10.11.1.2"}, nil, nil)

	svc := sds.GetService(hostname)
	if svc == nil {
		t.Fatalf("GetService(%q) => should exists", hostname)
	}
	if svc.Hostname != hostname {
		t.Fatalf("GetService(%q) => %q", hostname, svc.Hostname)
	}
	assert.EventuallyEqual(t, func() int {
		ep := GetEndpointsForPort(svc, ctl.Endpoints, 80)
		return len(ep)
	}, 2)

	ep := GetEndpointsForPort(svc, ctl.Endpoints, 80)
	if len(ep) != 2 {
		t.Fatalf("Invalid response for GetEndpoints %v", ep)
	}

	if ep[0].FirstAddressOrNil() == "10.10.1.1" && ep[0].Network != "network1" {
		t.Fatalf("Endpoint with IP 10.10.1.1 is expected to be in network1 but get: %s", ep[0].Network)
	}

	if ep[1].FirstAddressOrNil() == "10.11.1.2" && ep[1].Network != "network2" {
		t.Fatalf("Endpoint with IP 10.11.1.2 is expected to be in network2 but get: %s", ep[1].Network)
	}

	missing := kube.ServiceHostname("does-not-exist", ns, defaultFakeDomainSuffix)
	svc = sds.GetService(missing)
	if svc != nil {
		t.Fatalf("GetService(%q) => %s, should not exist", missing, svc.Hostname)
	}
}

func makeService(n, ns string, cl *FakeController, t *testing.T) {
	clienttest.Wrap(t, cl.services).Create(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: n, Namespace: ns},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port:     80,
					Name:     "http-example",
					Protocol: corev1.ProtocolTCP, // Not added automatically by fake
				},
			},
		},
	})
	log.Infof("Created service %s", n)
	cl.opts.XDSUpdater.(*xdsfake.Updater).WaitOrFail(t, "service")
}

func TestController_GetPodLocality(t *testing.T) {
	pod1 := generatePod([]string{"128.0.1.1"}, "pod1", "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
	pod2 := generatePod([]string{"128.0.1.2"}, "pod2", "nsB", "", "node2", map[string]string{"app": "prod-app"}, map[string]string{})
	podOverride := generatePod([]string{"128.0.1.2"}, "pod2", "nsB", "",
		"node1", map[string]string{"app": "prod-app", pm.LocalityLabel: "regionOverride.zoneOverride.subzoneOverride"}, map[string]string{})
	podOverride2 := generatePod([]string{"128.0.1.2"}, "pod2", "nsB", "",
		"node1", map[string]string{"app": "prod-app", label.TopologyLocality.Name: "regionOverride.zoneOverride.subzoneOverride"}, map[string]string{})
	testCases := []struct {
		name   string
		pods   []*corev1.Pod
		nodes  []*corev1.Node
		wantAZ map[*corev1.Pod]string
	}{
		{
			name: "should return correct az for given address",
			pods: []*corev1.Pod{pod1, pod2},
			nodes: []*corev1.Node{
				generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", label.TopologySubzone.Name: "subzone1"}),
				generateNode("node2", map[string]string{NodeZoneLabel: "zone2", NodeRegionLabel: "region2", label.TopologySubzone.Name: "subzone2"}),
			},
			wantAZ: map[*corev1.Pod]string{
				pod1: "region1/zone1/subzone1",
				pod2: "region2/zone2/subzone2",
			},
		},
		{
			name: "should return correct az for given address",
			pods: []*corev1.Pod{pod1, pod2},
			nodes: []*corev1.Node{
				generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1"}),
				generateNode("node2", map[string]string{NodeZoneLabel: "zone2", NodeRegionLabel: "region2"}),
			},
			wantAZ: map[*corev1.Pod]string{
				pod1: "region1/zone1/",
				pod2: "region2/zone2/",
			},
		},
		{
			name: "should return false if pod isn't in the cache",
			wantAZ: map[*corev1.Pod]string{
				pod1: "",
				pod2: "",
			},
		},
		{
			name: "should return false if node isn't in the cache",
			pods: []*corev1.Pod{pod1, pod2},
			wantAZ: map[*corev1.Pod]string{
				pod1: "",
				pod2: "",
			},
		},
		{
			name: "should return correct az if node has only region label",
			pods: []*corev1.Pod{pod1, pod2},
			nodes: []*corev1.Node{
				generateNode("node1", map[string]string{NodeRegionLabel: "region1"}),
				generateNode("node2", map[string]string{NodeRegionLabel: "region2"}),
			},
			wantAZ: map[*corev1.Pod]string{
				pod1: "region1//",
				pod2: "region2//",
			},
		},
		{
			name: "should return correct az if node has only zone label",
			pods: []*corev1.Pod{pod1, pod2},
			nodes: []*corev1.Node{
				generateNode("node1", map[string]string{NodeZoneLabel: "zone1"}),
				generateNode("node2", map[string]string{NodeZoneLabel: "zone2"}),
			},
			wantAZ: map[*corev1.Pod]string{
				pod1: "/zone1/",
				pod2: "/zone2/",
			},
		},
		{
			name: "should return correct az if node has only subzone label",
			pods: []*corev1.Pod{pod1, pod2},
			nodes: []*corev1.Node{
				generateNode("node1", map[string]string{label.TopologySubzone.Name: "subzone1"}),
				generateNode("node2", map[string]string{label.TopologySubzone.Name: "subzone2"}),
			},
			wantAZ: map[*corev1.Pod]string{
				pod1: "//subzone1",
				pod2: "//subzone2",
			},
		},
		{
			name: "should return correct az for given address",
			pods: []*corev1.Pod{podOverride},
			nodes: []*corev1.Node{
				generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", label.TopologySubzone.Name: "subzone1"}),
			},
			wantAZ: map[*corev1.Pod]string{
				podOverride: "regionOverride/zoneOverride/subzoneOverride",
			},
		},
		{
			name: "should return correct az with new label",
			pods: []*corev1.Pod{podOverride2},
			nodes: []*corev1.Node{
				generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", label.TopologySubzone.Name: "subzone1"}),
			},
			wantAZ: map[*corev1.Pod]string{
				podOverride2: "regionOverride/zoneOverride/subzoneOverride",
			},
		},
	}

	for _, tc := range testCases {
		// If using t.Parallel() you must copy the iteration to a new local variable
		// https://github.com/golang/go/wiki/CommonMistakes#using-goroutines-on-loop-iterator-variables
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Setup kube caches
			controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})

			addNodes(t, controller, tc.nodes...)
			addPods(t, controller, fx, tc.pods...)

			// Verify expected existing pod AZs
			for pod, wantAZ := range tc.wantAZ {
				az := controller.getPodLocality(pod)
				if wantAZ != "" {
					if !reflect.DeepEqual(az, wantAZ) {
						t.Fatalf("Wanted az: %s, got: %s", wantAZ, az)
					}
				} else {
					if az != "" {
						t.Fatalf("Unexpectedly found az: %s for pod: %s", az, pod.ObjectMeta.Name)
					}
				}
			}
		})
	}
}

func TestProxyK8sHostnameLabel(t *testing.T) {
	clusterID := cluster.ID("fakeCluster")
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
		ClusterID: clusterID,
	})

	pod := generatePod([]string{"128.0.0.1"}, "pod1", "nsa", "foo", "node1", map[string]string{"app": "test-app"}, map[string]string{})
	addPods(t, controller, fx, pod)

	proxy := &model.Proxy{
		Type:        model.Router,
		IPAddresses: []string{"128.0.0.1"},
		ID:          "pod1.nsa",
		DNSDomain:   "nsa.svc.cluster.local",
		Metadata:    &model.NodeMetadata{Namespace: "nsa", ClusterID: clusterID, NodeName: pod.Spec.NodeName},
	}
	got := controller.GetProxyWorkloadLabels(proxy)
	if pod.Spec.NodeName != got[labelutil.LabelHostname] {
		t.Fatalf("expected node name %v, got %v", pod.Spec.NodeName, got[labelutil.LabelHostname])
	}
}

func TestGetProxyServiceTargets(t *testing.T) {
	clusterID := cluster.ID("fakeCluster")
	networkID := network.ID("fakeNetwork")
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
		ClusterID: clusterID,
	})
	// add a network ID to test endpoints include topology.istio.io/network label
	controller.network = networkID

	p := generatePod([]string{"128.0.0.1"}, "pod1", "nsa", "foo", "node1", map[string]string{"app": "test-app"}, map[string]string{})
	addPods(t, controller, fx, p)

	k8sSaOnVM := "acct4"
	canonicalSaOnVM := "acctvm2@gserviceaccount2.com"

	createServiceWait(controller, "svc1", "nsa", []string{"10.0.0.1", "10.0.0.2"}, nil,
		map[string]string{
			annotation.AlphaKubernetesServiceAccounts.Name: k8sSaOnVM,
			annotation.AlphaCanonicalServiceAccounts.Name:  canonicalSaOnVM,
		},
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)

	// Endpoints are generated by Kubernetes from pod labels and service selectors.
	// Here we manually create them for mocking purpose.
	svc1Ips := []string{"128.0.0.1"}
	portNames := []string{"tcp-port"}
	// Create 1 endpoint that refers to a pod in the same namespace.
	createEndpoints(t, controller, "svc1", "nsA", portNames, svc1Ips, nil, nil)

	// Creates 100 endpoints that refers to a pod in a different namespace.
	fakeSvcCounts := 100
	for i := 0; i < fakeSvcCounts; i++ {
		svcName := fmt.Sprintf("svc-fake-%d", i)
		createServiceWait(controller, svcName, "nsfake", []string{"10.0.0.1", "10.0.0.2"}, nil,
			map[string]string{
				annotation.AlphaKubernetesServiceAccounts.Name: k8sSaOnVM,
				annotation.AlphaCanonicalServiceAccounts.Name:  canonicalSaOnVM,
			},
			[]int32{8080}, map[string]string{"app": "prod-app"}, t)

		createEndpoints(t, controller, svcName, "nsfake", portNames, svc1Ips, nil, nil)
		fx.WaitOrFail(t, "eds")
	}

	// Create 1 endpoint that refers to a pod in the same namespace.
	createEndpoints(t, controller, "svc1", "nsa", portNames, svc1Ips, nil, nil)
	fx.WaitOrFail(t, "eds")

	// this can test get pod by proxy ID
	svcNode := &model.Proxy{
		Type:        model.Router,
		IPAddresses: []string{"128.0.0.1"},
		ID:          "pod1.nsa",
		DNSDomain:   "nsa.svc.cluster.local",
		Metadata:    &model.NodeMetadata{Namespace: "nsa", ClusterID: clusterID},
	}
	serviceInstances := controller.GetProxyServiceTargets(svcNode)

	if len(serviceInstances) != 1 {
		t.Fatalf("GetProxyServiceTargets() expected 1 instance, got %d", len(serviceInstances))
	}

	hostname := kube.ServiceHostname("svc1", "nsa", defaultFakeDomainSuffix)
	if serviceInstances[0].Service.Hostname != hostname {
		t.Fatalf("GetProxyServiceTargets() wrong service instance returned => hostname %q, want %q",
			serviceInstances[0].Service.Hostname, hostname)
	}

	// Test that we can look up instances just by Proxy metadata
	metaServices := controller.GetProxyServiceTargets(&model.Proxy{
		Type:            "sidecar",
		IPAddresses:     []string{"1.1.1.1"},
		Locality:        &core.Locality{Region: "r", Zone: "z"},
		ConfigNamespace: "nsa",
		Labels: map[string]string{
			"app":                      "prod-app",
			label.SecurityTlsMode.Name: "mutual",
		},
		Metadata: &model.NodeMetadata{
			ServiceAccount: "account",
			ClusterID:      clusterID,
			Labels: map[string]string{
				"app":                      "prod-app",
				label.SecurityTlsMode.Name: "mutual",
			},
		},
	})

	expected := model.ServiceTarget{
		Service: &model.Service{
			Hostname: "svc1.nsa.svc.company.com",
			ClusterVIPs: model.AddressMap{
				Addresses: map[cluster.ID][]string{clusterID: {"10.0.0.1", "10.0.0.2"}},
			},
			DefaultAddress:  "10.0.0.1",
			Ports:           []*model.Port{{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP}},
			ServiceAccounts: []string{"acctvm2@gserviceaccount2.com", "spiffe://cluster.local/ns/nsa/sa/acct4"},
			Attributes: model.ServiceAttributes{
				ServiceRegistry: provider.Kubernetes,
				Name:            "svc1",
				Namespace:       "nsa",
				LabelSelectors:  map[string]string{"app": "prod-app"},
				K8sAttributes: model.K8sAttributes{
					Type: string(corev1.ServiceTypeClusterIP),
				},
			},
		},
		Port: model.ServiceInstancePort{
			ServicePort: &model.Port{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP},
			TargetPort:  0,
		},
	}

	if len(metaServices) != 1 {
		t.Fatalf("expected 1 instance, got %v", len(metaServices))
	}
	if !reflect.DeepEqual(expected, metaServices[0]) {
		t.Fatalf("expected instance %v, got %v", expected, metaServices[0])
	}

	// Test that we first look up instances by Proxy pod

	node := generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", label.TopologySubzone.Name: "subzone1"})
	addNodes(t, controller, node)

	// 1. pod without `istio-locality` label, get locality from node label.
	p = generatePod([]string{"129.0.0.1"}, "pod2", "nsa", "svcaccount", "node1",
		map[string]string{"app": "prod-app"}, nil)
	addPods(t, controller, fx, p)

	// this can test get pod by proxy ip address
	podServices := controller.GetProxyServiceTargets(&model.Proxy{
		Type:            "sidecar",
		IPAddresses:     []string{"129.0.0.1"},
		Locality:        &core.Locality{Region: "r", Zone: "z"},
		ConfigNamespace: "nsa",
		Labels: map[string]string{
			"app": "prod-app",
		},
		Metadata: &model.NodeMetadata{
			ServiceAccount: "account",
			ClusterID:      clusterID,
			Labels: map[string]string{
				"app": "prod-app",
			},
		},
	})

	expected = model.ServiceTarget{
		Service: &model.Service{
			Hostname: "svc1.nsa.svc.company.com",
			ClusterVIPs: model.AddressMap{
				Addresses: map[cluster.ID][]string{clusterID: {"10.0.0.1", "10.0.0.2"}},
			},
			DefaultAddress:  "10.0.0.1",
			Ports:           []*model.Port{{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP}},
			ServiceAccounts: []string{"acctvm2@gserviceaccount2.com", "spiffe://cluster.local/ns/nsa/sa/acct4"},
			Attributes: model.ServiceAttributes{
				ServiceRegistry: provider.Kubernetes,
				Name:            "svc1",
				Namespace:       "nsa",
				LabelSelectors:  map[string]string{"app": "prod-app"},
				K8sAttributes: model.K8sAttributes{
					Type: string(corev1.ServiceTypeClusterIP),
				},
			},
		},
		Port: model.ServiceInstancePort{
			ServicePort: &model.Port{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP},
			TargetPort:  0,
		},
	}
	if len(podServices) != 1 {
		t.Fatalf("expected 1 instance, got %v", len(podServices))
	}
	if !reflect.DeepEqual(expected, podServices[0]) {
		t.Fatalf("expected instance %v, got %v", expected, podServices[0])
	}

	// 2. pod with `istio-locality` label, ignore node label.
	p = generatePod([]string{"129.0.0.2"}, "pod3", "nsa", "svcaccount", "node1",
		map[string]string{"app": "prod-app", "istio-locality": "region.zone"}, nil)
	addPods(t, controller, fx, p)

	// this can test get pod by proxy ip address
	podServices = controller.GetProxyServiceTargets(&model.Proxy{
		Type:            "sidecar",
		IPAddresses:     []string{"129.0.0.2"},
		Locality:        &core.Locality{Region: "r", Zone: "z"},
		ConfigNamespace: "nsa",
		Labels: map[string]string{
			"app": "prod-app",
		},
		Metadata: &model.NodeMetadata{
			ServiceAccount: "account",
			ClusterID:      clusterID,
			Labels: map[string]string{
				"app": "prod-app",
			},
		},
	})

	expected = model.ServiceTarget{
		Service: &model.Service{
			Hostname: "svc1.nsa.svc.company.com",
			ClusterVIPs: model.AddressMap{
				Addresses: map[cluster.ID][]string{clusterID: {"10.0.0.1", "10.0.0.2"}},
			},
			DefaultAddress:  "10.0.0.1",
			Ports:           []*model.Port{{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP}},
			ServiceAccounts: []string{"acctvm2@gserviceaccount2.com", "spiffe://cluster.local/ns/nsa/sa/acct4"},
			Attributes: model.ServiceAttributes{
				ServiceRegistry: provider.Kubernetes,
				Name:            "svc1",
				Namespace:       "nsa",
				LabelSelectors:  map[string]string{"app": "prod-app"},
				K8sAttributes: model.K8sAttributes{
					Type: string(corev1.ServiceTypeClusterIP),
				},
			},
		},
		Port: model.ServiceInstancePort{
			ServicePort: &model.Port{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP},
		},
	}
	if len(podServices) != 1 {
		t.Fatalf("expected 1 instance, got %v", len(podServices))
	}
	if !reflect.DeepEqual(expected, podServices[0]) {
		t.Fatalf("expected instance %v, got %v", expected, podServices[0])
	}

	// pod with no services should return no service targets
	p = generatePod([]string{"130.0.0.1"}, "pod4", "nsa", "foo", "node1", map[string]string{"app": "no-service-app"}, map[string]string{})
	addPods(t, controller, fx, p)

	podServices = controller.GetProxyServiceTargets(&model.Proxy{
		Type:            "sidecar",
		IPAddresses:     []string{"130.0.0.1"},
		Locality:        &core.Locality{Region: "r", Zone: "z"},
		ConfigNamespace: "nsa",
		Labels: map[string]string{
			"app": "no-service-app",
		},
		Metadata: &model.NodeMetadata{
			ServiceAccount: "account",
			ClusterID:      clusterID,
			Labels: map[string]string{
				"app": "no-service-app",
			},
		},
	})
	if len(podServices) != 0 {
		t.Fatalf("expect 0 instance, got %v", len(podServices))
	}
}

func TestGetProxyServiceTargetsWithMultiIPsAndTargetPorts(t *testing.T) {
	pod1 := generatePod([]string{"128.0.0.1"}, "pod1", "nsa", "foo", "node1", map[string]string{"app": "test-app"}, map[string]string{})
	testCases := []struct {
		name      string
		pods      []*corev1.Pod
		ips       []string
		ports     []corev1.ServicePort
		wantPorts []model.ServiceInstancePort
	}{
		{
			name: "multiple proxy ips single port",
			pods: []*corev1.Pod{pod1},
			ips:  []string{"128.0.0.1", "192.168.2.6"},
			ports: []corev1.ServicePort{
				{
					Name:       "tcp-port",
					Port:       8080,
					Protocol:   "http",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
				},
			},
			wantPorts: []model.ServiceInstancePort{
				{
					ServicePort: &model.Port{
						Name:     "tcp-port",
						Port:     8080,
						Protocol: "TCP",
					},
					TargetPort: 8080,
				},
			},
		},
		{
			name: "single proxy ip single port",
			pods: []*corev1.Pod{pod1},
			ips:  []string{"128.0.0.1"},
			ports: []corev1.ServicePort{
				{
					Name:       "tcp-port",
					Port:       8080,
					Protocol:   "TCP",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
				},
			},
			wantPorts: []model.ServiceInstancePort{
				{
					ServicePort: &model.Port{
						Name:     "tcp-port",
						Port:     8080,
						Protocol: "TCP",
					},
					TargetPort: 8080,
				},
			},
		},
		{
			name: "multiple proxy ips multiple ports",
			pods: []*corev1.Pod{pod1},
			ips:  []string{"128.0.0.1", "192.168.2.6"},
			ports: []corev1.ServicePort{
				{
					Name:       "tcp-port-1",
					Port:       8080,
					Protocol:   "http",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
				},
				{
					Name:       "tcp-port-2",
					Port:       9090,
					Protocol:   "http",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 9090},
				},
			},
			wantPorts: []model.ServiceInstancePort{
				{
					ServicePort: &model.Port{
						Name:     "tcp-port-1",
						Port:     8080,
						Protocol: "TCP",
					},
					TargetPort: 8080,
				},
				{
					ServicePort: &model.Port{
						Name:     "tcp-port-2",
						Port:     9090,
						Protocol: "TCP",
					},
					TargetPort: 9090,
				},
				{
					ServicePort: &model.Port{
						Name:     "tcp-port-1",
						Port:     7442,
						Protocol: "TCP",
					},
					TargetPort: 7442,
				},
			},
		},
		{
			name: "single proxy ip multiple ports same target port with different protocols",
			pods: []*corev1.Pod{pod1},
			ips:  []string{"128.0.0.1"},
			ports: []corev1.ServicePort{
				{
					Name:       "tcp-port",
					Port:       8080,
					Protocol:   "TCP",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
				},
				{
					Name:       "http-port",
					Port:       9090,
					Protocol:   "TCP",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
				},
			},
			wantPorts: []model.ServiceInstancePort{
				{
					ServicePort: &model.Port{
						Name:     "tcp-port",
						Port:     8080,
						Protocol: "TCP",
					},
					TargetPort: 8080,
				},
				{
					ServicePort: &model.Port{
						Name:     "http-port",
						Port:     9090,
						Protocol: "HTTP",
					},
					TargetPort: 8080,
				},
			},
		},
		{
			name: "single proxy ip multiple ports same target port with overlapping protocols",
			pods: []*corev1.Pod{pod1},
			ips:  []string{"128.0.0.1"},
			ports: []corev1.ServicePort{
				{
					Name:       "http-7442",
					Port:       7442,
					Protocol:   "TCP",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 7442},
				},
				{
					Name:       "tcp-8443",
					Port:       8443,
					Protocol:   "TCP",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 7442},
				},
				{
					Name:       "http-7557",
					Port:       7557,
					Protocol:   "TCP",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 7442},
				},
			},
			wantPorts: []model.ServiceInstancePort{
				{
					ServicePort: &model.Port{
						Name:     "http-7442",
						Port:     7442,
						Protocol: "HTTP",
					},
					TargetPort: 7442,
				},
				{
					ServicePort: &model.Port{
						Name:     "tcp-8443",
						Port:     8443,
						Protocol: "TCP",
					},
					TargetPort: 7442,
				},
			},
		},
		{
			name: "single proxy ip multiple ports",
			pods: []*corev1.Pod{pod1},
			ips:  []string{"128.0.0.1"},
			ports: []corev1.ServicePort{
				{
					Name:       "tcp-port",
					Port:       8080,
					Protocol:   "TCP",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
				},
				{
					Name:       "http-port",
					Port:       9090,
					Protocol:   "TCP",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 9090},
				},
			},
			wantPorts: []model.ServiceInstancePort{
				{
					ServicePort: &model.Port{
						Name:     "tcp-port",
						Port:     8080,
						Protocol: "TCP",
					},
					TargetPort: 8080,
				},
				{
					ServicePort: &model.Port{
						Name:     "http-port",
						Port:     9090,
						Protocol: "HTTP",
					},
					TargetPort: 9090,
				},
			},
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			// Setup kube caches
			controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})

			addPods(t, controller, fx, c.pods...)

			createServiceWithTargetPorts(controller, "svc1", "nsa",
				map[string]string{
					annotation.AlphaKubernetesServiceAccounts.Name: "acct4",
					annotation.AlphaCanonicalServiceAccounts.Name:  "acctvm2@gserviceaccount2.com",
				},
				c.ports, map[string]string{"app": "test-app"}, t)

			serviceInstances := controller.GetProxyServiceTargets(&model.Proxy{Metadata: &model.NodeMetadata{}, IPAddresses: c.ips})

			for i, svc := range serviceInstances {
				assert.Equal(t, svc.Port, c.wantPorts[i])
			}
		})
	}
}

func TestGetProxyServiceTargets_WorkloadInstance(t *testing.T) {
	ctl, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{})

	createServiceWait(ctl, "ratings", "bookinfo-ratings", []string{"10.0.0.1"},
		map[string]string{},
		map[string]string{
			annotation.AlphaKubernetesServiceAccounts.Name: "ratings",
			annotation.AlphaCanonicalServiceAccounts.Name:  "ratings@gserviceaccount2.com",
		},
		[]int32{8080}, map[string]string{"app": "ratings"}, t)

	createServiceWait(ctl, "details", "bookinfo-details", []string{"10.0.0.2"},
		map[string]string{},
		map[string]string{
			annotation.AlphaKubernetesServiceAccounts.Name: "details",
			annotation.AlphaCanonicalServiceAccounts.Name:  "details@gserviceaccount2.com",
		},
		[]int32{9090}, map[string]string{"app": "details"}, t)

	createServiceWait(ctl, "reviews", "bookinfo-reviews", []string{"10.0.0.3"},
		map[string]string{},
		map[string]string{
			annotation.AlphaKubernetesServiceAccounts.Name: "reviews",
			annotation.AlphaCanonicalServiceAccounts.Name:  "reviews@gserviceaccount2.com",
		},
		[]int32{7070}, map[string]string{"app": "reviews"}, t)

	wiRatings1 := &model.WorkloadInstance{
		Name:      "ratings-1",
		Namespace: "bookinfo-ratings",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "ratings"},
			Addresses:    []string{"2.2.2.21", "2001:1::21"},
			EndpointPort: 8080,
		},
	}

	wiDetails1 := &model.WorkloadInstance{
		Name:      "details-1",
		Namespace: "bookinfo-details",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "details"},
			Addresses:    []string{"2.2.2.21"},
			EndpointPort: 9090,
		},
	}

	wiReviews1 := &model.WorkloadInstance{
		Name:      "reviews-1",
		Namespace: "bookinfo-reviews",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "reviews"},
			Addresses:    []string{"3.3.3.31"},
			EndpointPort: 7070,
		},
	}

	wiReviews2 := &model.WorkloadInstance{
		Name:      "reviews-2",
		Namespace: "bookinfo-reviews",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "reviews"},
			Addresses:    []string{"3.3.3.32"},
			EndpointPort: 7071,
		},
	}

	wiProduct1 := &model.WorkloadInstance{
		Name:      "productpage-1",
		Namespace: "bookinfo-productpage",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "productpage"},
			Addresses:    []string{"4.4.4.41", "2001:1::41"},
			EndpointPort: 6060,
		},
	}

	for _, wi := range []*model.WorkloadInstance{wiRatings1, wiDetails1, wiReviews1, wiReviews2, wiProduct1} {
		ctl.workloadInstanceHandler(wi, model.EventAdd) // simulate adding a workload entry
	}

	cases := []struct {
		name  string
		proxy *model.Proxy
		want  []model.ServiceTarget
	}{
		{
			name:  "proxy with unspecified IP",
			proxy: &model.Proxy{Metadata: &model.NodeMetadata{}, IPAddresses: nil},
			want:  nil,
		},
		{
			name:  "proxy with IP not in the registry",
			proxy: &model.Proxy{Metadata: &model.NodeMetadata{}, IPAddresses: []string{"1.1.1.1"}},
			want:  nil,
		},
		{
			name:  "proxy with IP from the registry, 1 matching WE, but no matching Service",
			proxy: &model.Proxy{Metadata: &model.NodeMetadata{}, IPAddresses: []string{"4.4.4.41", "2001:1::41"}},
			want:  nil,
		},
		{
			name:  "proxy with IP from the registry, 1 matching WE, and matching Service",
			proxy: &model.Proxy{Metadata: &model.NodeMetadata{}, IPAddresses: []string{"3.3.3.31"}},
			want: []model.ServiceTarget{{
				Service: &model.Service{
					Hostname: "reviews.bookinfo-reviews.svc.company.com",
				},
				Port: model.ServiceInstancePort{
					ServicePort: nil,
					TargetPort:  7070,
				},
			}},
		},
		{
			name:  "proxy with IP from the registry, 2 matching WE, and matching Service",
			proxy: &model.Proxy{Metadata: &model.NodeMetadata{}, IPAddresses: []string{"2.2.2.21"}},
			want: []model.ServiceTarget{{
				Service: &model.Service{
					Hostname: "details.bookinfo-details.svc.company.com",
				},
				Port: model.ServiceInstancePort{
					ServicePort: nil,
					TargetPort:  9090,
				},
			}},
		},
		{
			name: "proxy with IP from the registry, 2 matching WE, and matching Service, and proxy ID equal to WE with a different address",
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{}, IPAddresses: []string{"2.2.2.21"},
				ID: "reviews-1.bookinfo-reviews", ConfigNamespace: "bookinfo-reviews",
			},
			want: []model.ServiceTarget{{
				Service: &model.Service{
					Hostname: "details.bookinfo-details.svc.company.com",
				},
				Port: model.ServiceInstancePort{
					ServicePort: nil,
					TargetPort:  9090,
				},
			}},
		},
		{
			name: "proxy with IP from the registry, 2 matching WE, and matching Service, and proxy ID equal to WE name, but proxy.ID != proxy.ConfigNamespace",
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{}, IPAddresses: []string{"2.2.2.21"},
				ID: "ratings-1.bookinfo-ratings", ConfigNamespace: "wrong-namespace",
			},
			want: []model.ServiceTarget{{
				Service: &model.Service{
					Hostname: "details.bookinfo-details.svc.company.com",
				},
				Port: model.ServiceInstancePort{
					ServicePort: nil,
					TargetPort:  9090,
				},
			}},
		},
		{
			name: "proxy with IP from the registry, 2 matching WE, and matching Service, and proxy.ID == WE name",
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{}, IPAddresses: []string{"2.2.2.21", "2001:1::21"},
				ID: "ratings-1.bookinfo-ratings", ConfigNamespace: "bookinfo-ratings",
			},
			want: []model.ServiceTarget{{
				Service: &model.Service{
					Hostname: "ratings.bookinfo-ratings.svc.company.com",
				},
				Port: model.ServiceInstancePort{
					ServicePort: nil,
					TargetPort:  8080,
				},
			}},
		},
		{
			name: "proxy with IP from the registry, 2 matching WE, and matching Service, and proxy.ID != WE name, but proxy.ConfigNamespace == WE namespace",
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{}, IPAddresses: []string{"2.2.2.21", "2001:1::21"},
				ID: "wrong-name.bookinfo-ratings", ConfigNamespace: "bookinfo-ratings",
			},
			want: []model.ServiceTarget{{
				Service: &model.Service{
					Hostname: "ratings.bookinfo-ratings.svc.company.com",
				},
				Port: model.ServiceInstancePort{
					ServicePort: nil,
					TargetPort:  8080,
				},
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ctl.GetProxyServiceTargets(tc.proxy)

			if diff := cmp.Diff(len(tc.want), len(got)); diff != "" {
				t.Fatalf("GetProxyServiceTargets() returned unexpected number of service instances (--want/++got): %v", diff)
			}

			for i := range tc.want {
				assert.Equal(t, tc.want[i].Service.Hostname, got[i].Service.Hostname)
				assert.Equal(t, tc.want[i].Port.TargetPort, got[i].Port.TargetPort)
			}
		})
	}
}

func TestController_Service(t *testing.T) {
	controller, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{})

	// Use a timeout to keep the test from hanging.

	createServiceWait(controller, "svc1", "nsA", []string{"10.0.0.1"},
		map[string]string{}, map[string]string{},
		[]int32{8080}, map[string]string{"test-app": "test-app-1"}, t)
	createServiceWait(controller, "svc2", "nsA", []string{"10.0.0.1"},
		map[string]string{}, map[string]string{},
		[]int32{8081}, map[string]string{"test-app": "test-app-2"}, t)
	createServiceWait(controller, "svc3", "nsA", []string{"10.0.0.1"},
		map[string]string{}, map[string]string{},
		[]int32{8082}, map[string]string{"test-app": "test-app-3"}, t)
	createServiceWait(controller, "svc4", "nsA", []string{"10.0.0.1"},
		map[string]string{}, map[string]string{},
		[]int32{8083}, map[string]string{"test-app": "test-app-4"}, t)

	expectedSvcList := []*model.Service{
		{
			Hostname:       kube.ServiceHostname("svc1", "nsA", defaultFakeDomainSuffix),
			DefaultAddress: "10.0.0.1",
			Ports: model.PortList{
				&model.Port{
					Name:     "tcp-port",
					Port:     8080,
					Protocol: protocol.TCP,
				},
			},
		},
		{
			Hostname:       kube.ServiceHostname("svc2", "nsA", defaultFakeDomainSuffix),
			DefaultAddress: "10.0.0.1",
			Ports: model.PortList{
				&model.Port{
					Name:     "tcp-port",
					Port:     8081,
					Protocol: protocol.TCP,
				},
			},
		},
		{
			Hostname:       kube.ServiceHostname("svc3", "nsA", defaultFakeDomainSuffix),
			DefaultAddress: "10.0.0.1",
			Ports: model.PortList{
				&model.Port{
					Name:     "tcp-port",
					Port:     8082,
					Protocol: protocol.TCP,
				},
			},
		},
		{
			Hostname:       kube.ServiceHostname("svc4", "nsA", defaultFakeDomainSuffix),
			DefaultAddress: "10.0.0.1",
			Ports: model.PortList{
				&model.Port{
					Name:     "tcp-port",
					Port:     8083,
					Protocol: protocol.TCP,
				},
			},
		},
	}

	svcList := controller.Services()
	servicesEqual(svcList, expectedSvcList)
}

func TestController_ServiceWithFixedDiscoveryNamespaces(t *testing.T) {
	meshWatcher := meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{
		DiscoverySelectors: []*meshconfig.LabelSelector{
			{
				MatchLabels: map[string]string{
					"pilot-discovery": "enabled",
				},
			},
			{
				MatchExpressions: []*meshconfig.LabelSelectorRequirement{
					{
						Key:      "env",
						Operator: string(metav1.LabelSelectorOpIn),
						Values:   []string{"test", "dev"},
					},
				},
			},
		},
	})

	svc1 := &model.Service{
		Hostname:       kube.ServiceHostname("svc1", "nsA", defaultFakeDomainSuffix),
		DefaultAddress: "10.0.0.1",
		Ports: model.PortList{
			&model.Port{
				Name:     "tcp-port",
				Port:     8080,
				Protocol: protocol.TCP,
			},
		},
	}
	svc2 := &model.Service{
		Hostname:       kube.ServiceHostname("svc2", "nsA", defaultFakeDomainSuffix),
		DefaultAddress: "10.0.0.1",
		Ports: model.PortList{
			&model.Port{
				Name:     "tcp-port",
				Port:     8081,
				Protocol: protocol.TCP,
			},
		},
	}
	svc3 := &model.Service{
		Hostname:       kube.ServiceHostname("svc3", "nsB", defaultFakeDomainSuffix),
		DefaultAddress: "10.0.0.1",
		Ports: model.PortList{
			&model.Port{
				Name:     "tcp-port",
				Port:     8082,
				Protocol: protocol.TCP,
			},
		},
	}
	svc4 := &model.Service{
		Hostname:       kube.ServiceHostname("svc4", "nsB", defaultFakeDomainSuffix),
		DefaultAddress: "10.0.0.1",
		Ports: model.PortList{
			&model.Port{
				Name:     "tcp-port",
				Port:     8083,
				Protocol: protocol.TCP,
			},
		},
	}

	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
		MeshWatcher: meshWatcher,
	})

	nsA := "nsA"
	nsB := "nsB"

	// event handlers should only be triggered for services in namespaces selected for discovery
	createNamespace(t, controller.client.Kube(), nsA, map[string]string{"pilot-discovery": "enabled"})
	createNamespace(t, controller.client.Kube(), nsB, map[string]string{})

	// service event handlers should trigger for svc1 and svc2
	createServiceWait(controller, "svc1", nsA, []string{"10.0.0.1"},
		map[string]string{}, map[string]string{},
		[]int32{8080}, map[string]string{"test-app": "test-app-1"}, t)
	createServiceWait(controller, "svc2", nsA, []string{"10.0.0.1"},
		map[string]string{}, map[string]string{},
		[]int32{8081}, map[string]string{"test-app": "test-app-2"}, t)
	// service event handlers should not trigger for svc3 and svc4
	createService(controller, "svc3", nsB, []string{"10.0.0.1"},
		map[string]string{}, map[string]string{},
		[]int32{8082}, map[string]string{"test-app": "test-app-3"}, t)
	createService(controller, "svc4", nsB, []string{"10.0.0.1"},
		map[string]string{}, map[string]string{},
		[]int32{8083}, map[string]string{"test-app": "test-app-4"}, t)

	expectedSvcList := []*model.Service{svc1, svc2}
	eventually(t, func() bool {
		svcList := controller.Services()
		return servicesEqual(svcList, expectedSvcList)
	})

	// test updating namespace with adding discovery label
	updateNamespace(t, controller.client.Kube(), nsB, map[string]string{"env": "test"})
	// service event handlers should trigger for svc3 and svc4
	fx.WaitOrFail(t, "service")
	fx.WaitOrFail(t, "service")
	expectedSvcList = []*model.Service{svc1, svc2, svc3, svc4}
	eventually(t, func() bool {
		svcList := controller.Services()
		return servicesEqual(svcList, expectedSvcList)
	})

	// test updating namespace by removing discovery label
	updateNamespace(t, controller.client.Kube(), nsA, map[string]string{"pilot-discovery": "disabled"})
	// service event handlers should trigger for svc1 and svc2
	fx.WaitOrFail(t, "service")
	fx.WaitOrFail(t, "service")
	expectedSvcList = []*model.Service{svc3, svc4}
	eventually(t, func() bool {
		svcList := controller.Services()
		return servicesEqual(svcList, expectedSvcList)
	})
}

func TestController_ServiceWithChangingDiscoveryNamespaces(t *testing.T) {
	svc1 := &model.Service{
		Hostname:       kube.ServiceHostname("svc1", "nsA", defaultFakeDomainSuffix),
		DefaultAddress: "10.0.0.1",
		Ports: model.PortList{
			&model.Port{
				Name:     "tcp-port",
				Port:     8080,
				Protocol: protocol.TCP,
			},
		},
	}
	svc2 := &model.Service{
		Hostname:       kube.ServiceHostname("svc2", "nsA", defaultFakeDomainSuffix),
		DefaultAddress: "10.0.0.1",
		Ports: model.PortList{
			&model.Port{
				Name:     "tcp-port",
				Port:     8081,
				Protocol: protocol.TCP,
			},
		},
	}
	svc3 := &model.Service{
		Hostname:       kube.ServiceHostname("svc3", "nsB", defaultFakeDomainSuffix),
		DefaultAddress: "10.0.0.1",
		Ports: model.PortList{
			&model.Port{
				Name:     "tcp-port",
				Port:     8082,
				Protocol: protocol.TCP,
			},
		},
	}
	svc4 := &model.Service{
		Hostname:       kube.ServiceHostname("svc4", "nsC", defaultFakeDomainSuffix),
		DefaultAddress: "10.0.0.1",
		Ports: model.PortList{
			&model.Port{
				Name:     "tcp-port",
				Port:     8083,
				Protocol: protocol.TCP,
			},
		},
	}

	updateMeshConfig := func(
		meshConfig *meshconfig.MeshConfig,
		expectedSvcList []*model.Service,
		expectedNumSvcEvents int,
		testMeshWatcher meshwatcher.TestWatcher,
		fx *xdsfake.Updater,
		controller *FakeController,
	) {
		// update meshConfig
		testMeshWatcher.Set(meshConfig)

		// assert firing of service events
		for i := 0; i < expectedNumSvcEvents; i++ {
			fx.WaitOrFail(t, "service")
		}

		eventually(t, func() bool {
			svcList := controller.Services()
			return servicesEqual(svcList, expectedSvcList)
		})
	}

	meshWatcher := meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{})

	nsA := "nsA"
	nsB := "nsB"
	nsC := "nsC"

	client := kubelib.NewFakeClient(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsA, Labels: map[string]string{"app": "foo"}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsB, Labels: map[string]string{"app": "bar"}}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsC, Labels: map[string]string{"app": "baz"}}},
	)
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
		Client:      client,
		MeshWatcher: meshWatcher,
	})

	// service event handlers should trigger for all svcs
	createServiceWait(controller, "svc1", nsA, []string{"10.0.0.1"},
		map[string]string{}, map[string]string{},
		[]int32{8080}, map[string]string{"test-app": "test-app-1"}, t)
	createServiceWait(controller, "svc2", nsA, []string{"10.0.0.1"},
		map[string]string{}, map[string]string{},
		[]int32{8081}, map[string]string{"test-app": "test-app-2"}, t)
	createServiceWait(controller, "svc3", nsB, []string{"10.0.0.1"},
		map[string]string{}, map[string]string{},
		[]int32{8082}, map[string]string{"test-app": "test-app-3"}, t)
	createServiceWait(controller, "svc4", nsC, []string{"10.0.0.1"},
		map[string]string{}, map[string]string{},
		[]int32{8083}, map[string]string{"test-app": "test-app-4"}, t)

	expectedSvcList := []*model.Service{svc1, svc2, svc3, svc4}
	eventually(t, func() bool {
		svcList := controller.Services()
		return servicesEqual(svcList, expectedSvcList)
	})

	// restrict namespaces to nsA (expect 2 delete events for svc3 and svc4)
	updateMeshConfig(
		&meshconfig.MeshConfig{
			DiscoverySelectors: []*meshconfig.LabelSelector{
				{
					MatchLabels: map[string]string{
						"app": "foo",
					},
				},
			},
		},
		[]*model.Service{svc1, svc2},
		2,
		meshWatcher,
		fx,
		controller,
	)

	// restrict namespaces to nsB (1 create event should trigger for nsB service and 2 delete events for nsA services)
	updateMeshConfig(
		&meshconfig.MeshConfig{
			DiscoverySelectors: []*meshconfig.LabelSelector{
				{
					MatchLabels: map[string]string{
						"app": "bar",
					},
				},
			},
		},
		[]*model.Service{svc3},
		3,
		meshWatcher,
		fx,
		controller,
	)

	// expand namespaces to nsA and nsB with selectors (2 create events should trigger for nsA services)
	updateMeshConfig(
		&meshconfig.MeshConfig{
			DiscoverySelectors: []*meshconfig.LabelSelector{
				{
					MatchExpressions: []*meshconfig.LabelSelectorRequirement{
						{
							Key:      "app",
							Operator: string(metav1.LabelSelectorOpIn),
							Values:   []string{"foo", "bar"},
						},
					},
				},
			},
		},
		[]*model.Service{svc1, svc2, svc3},
		2,
		meshWatcher,
		fx,
		controller,
	)

	// permit all discovery namespaces by omitting discovery selectors (1 create event should trigger for the nsC service)
	updateMeshConfig(
		&meshconfig.MeshConfig{
			DiscoverySelectors: []*meshconfig.LabelSelector{},
		},
		[]*model.Service{svc1, svc2, svc3, svc4},
		1,
		meshWatcher,
		fx,
		controller,
	)
}

func TestControllerResourceScoping(t *testing.T) {
	svc1 := &model.Service{
		Hostname:       kube.ServiceHostname("svc1", "nsA", defaultFakeDomainSuffix),
		DefaultAddress: "10.0.0.1",
		Ports: model.PortList{
			&model.Port{
				Name:     "tcp-port",
				Port:     8080,
				Protocol: protocol.TCP,
			},
		},
	}
	svc2 := &model.Service{
		Hostname:       kube.ServiceHostname("svc2", "nsA", defaultFakeDomainSuffix),
		DefaultAddress: "10.0.0.2",
		Ports: model.PortList{
			&model.Port{
				Name:     "tcp-port",
				Port:     8081,
				Protocol: protocol.TCP,
			},
		},
	}
	svc3 := &model.Service{
		Hostname:       kube.ServiceHostname("svc3", "nsB", defaultFakeDomainSuffix),
		DefaultAddress: "10.0.0.3",
		Ports: model.PortList{
			&model.Port{
				Name:     "tcp-port",
				Port:     8082,
				Protocol: protocol.TCP,
			},
		},
	}
	svc4 := &model.Service{
		Hostname:       kube.ServiceHostname("svc4", "nsC", defaultFakeDomainSuffix),
		DefaultAddress: "10.0.0.4",
		Ports: model.PortList{
			&model.Port{
				Name:     "tcp-port",
				Port:     8083,
				Protocol: protocol.TCP,
			},
		},
	}

	updateMeshConfig := func(
		meshConfig *meshconfig.MeshConfig,
		expectedSvcList []*model.Service,
		expectedNumSvcEvents int,
		testMeshWatcher meshwatcher.TestWatcher,
		fx *xdsfake.Updater,
		controller *FakeController,
	) {
		t.Helper()
		// update meshConfig
		testMeshWatcher.Set(meshConfig)

		// assert firing of service events
		for i := 0; i < expectedNumSvcEvents; i++ {
			fx.WaitOrFail(t, "service")
		}

		eventually(t, func() bool {
			svcList := controller.Services()
			return servicesEqual(svcList, expectedSvcList)
		})
	}

	client := kubelib.NewFakeClient()
	t.Cleanup(client.Shutdown)
	meshWatcher := meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{})

	nsA := "nsA"
	nsB := "nsB"
	nsC := "nsC"

	createNamespace(t, client.Kube(), nsA, map[string]string{"app": "foo"})
	createNamespace(t, client.Kube(), nsB, map[string]string{"app": "bar"})
	createNamespace(t, client.Kube(), nsC, map[string]string{"app": "baz"})

	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
		Client:      client,
		MeshWatcher: meshWatcher,
	})

	// service event handlers should trigger for all svcs
	createServiceWait(controller, "svc1", nsA, []string{"10.0.0.1"},
		map[string]string{},
		map[string]string{},
		[]int32{8080}, map[string]string{"test-app": "test-app-1"}, t)

	createServiceWait(controller, "svc2", nsA, []string{"10.0.0.2"},
		map[string]string{},
		map[string]string{},
		[]int32{8081}, map[string]string{"test-app": "test-app-2"}, t)

	createServiceWait(controller, "svc3", nsB, []string{"10.0.0.3"},
		map[string]string{},
		map[string]string{},
		[]int32{8082}, map[string]string{"test-app": "test-app-3"}, t)

	createServiceWait(controller, "svc4", nsC, []string{"10.0.0.4"},
		map[string]string{},
		map[string]string{},
		[]int32{8083}, map[string]string{"test-app": "test-app-4"}, t)

	expectedSvcList := []*model.Service{svc1, svc2, svc3, svc4}
	eventually(t, func() bool {
		svcList := controller.Services()
		return servicesEqual(svcList, expectedSvcList)
	})

	fx.Clear()

	// restrict namespaces to nsA (expect 2 delete events for svc3 and svc4)
	updateMeshConfig(
		&meshconfig.MeshConfig{
			DiscoverySelectors: []*meshconfig.LabelSelector{
				{
					MatchLabels: map[string]string{
						"app": "foo",
					},
				},
			},
		},
		[]*model.Service{svc1, svc2},
		2,
		meshWatcher,
		fx,
		controller,
	)

	// namespace nsB, nsC deselected
	fx.AssertEmpty(t, 0)

	// create vs1 in nsA
	createVirtualService(controller, "vs1", nsA, map[string]string{}, t)

	// create vs1 in nsB
	createVirtualService(controller, "vs2", nsB, map[string]string{}, t)

	// expand namespaces to nsA and nsB with selectors (expect events svc3 and a full push event for nsB selected)
	updateMeshConfig(
		&meshconfig.MeshConfig{
			DiscoverySelectors: []*meshconfig.LabelSelector{
				{
					MatchExpressions: []*meshconfig.LabelSelectorRequirement{
						{
							Key:      "app",
							Operator: string(metav1.LabelSelectorOpIn),
							Values:   []string{"foo", "bar"},
						},
					},
				},
			},
		},
		[]*model.Service{svc1, svc2, svc3},
		1,
		meshWatcher,
		fx,
		controller,
	)

	// namespace nsB selected
	fx.AssertEmpty(t, 0)
}

func TestEndpoints_WorkloadInstances(t *testing.T) {
	ctl, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{})

	createServiceWithTargetPorts(ctl, "ratings", "bookinfo-ratings",
		map[string]string{
			annotation.AlphaKubernetesServiceAccounts.Name: "ratings",
			annotation.AlphaCanonicalServiceAccounts.Name:  "ratings@gserviceaccount2.com",
		},
		[]corev1.ServicePort{
			{
				Name:       "http-port",
				Port:       8080,
				Protocol:   "TCP",
				TargetPort: intstr.IntOrString{Type: intstr.String, StrVal: "http"},
			},
		},
		map[string]string{"app": "ratings"}, t)

	wiRatings1 := &model.WorkloadInstance{
		Name:      "ratings-1",
		Namespace: "bookinfo-ratings",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "ratings"},
			Addresses:    []string{"2.2.2.2"},
			EndpointPort: 8081, // should be ignored since it doesn't define PortMap
		},
	}

	wiRatings2 := &model.WorkloadInstance{
		Name:      "ratings-2",
		Namespace: "bookinfo-ratings",
		Endpoint: &model.IstioEndpoint{
			Labels:    labels.Instance{"app": "ratings"},
			Addresses: []string{"2.2.2.2"},
		},
		PortMap: map[string]uint32{
			"http": 8082, // should be used
		},
	}

	wiRatings3 := &model.WorkloadInstance{
		Name:      "ratings-3",
		Namespace: "bookinfo-ratings",
		Endpoint: &model.IstioEndpoint{
			Labels:    labels.Instance{"app": "ratings"},
			Addresses: []string{"2.2.2.2", "2001:1::2"},
		},
		PortMap: map[string]uint32{
			"http": 8083, // should be used
		},
	}

	for _, wi := range []*model.WorkloadInstance{wiRatings1, wiRatings2, wiRatings3} {
		ctl.workloadInstanceHandler(wi, model.EventAdd) // simulate adding a workload entry
	}

	// get service object
	svcs := ctl.Services()
	if len(svcs) != 1 {
		t.Fatalf("failed to get services (%v)", svcs)
	}

	endpoints := GetEndpoints(svcs[0], ctl.Endpoints)

	want := []string{"2.2.2.2:8082", "2.2.2.2:8083", "[2001:1::2]:8083"} // expect both WorkloadEntries even though they have the same IP

	var got []string
	for _, instance := range endpoints {
		for _, addr := range instance.Addresses {
			got = append(got, net.JoinHostPort(addr, strconv.Itoa(int(instance.EndpointPort))))
		}
	}
	sort.Strings(got)

	assert.Equal(t, want, got)
}

func TestExternalNameServiceInstances(t *testing.T) {
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})
	createExternalNameService(controller, "svc5", "nsA",
		[]int32{1, 2, 3}, "foo.co", t, fx)

	converted := controller.Services()
	assert.Equal(t, len(converted), 1)

	eps := GetEndpointsForPort(converted[0], controller.Endpoints, 1)
	assert.Equal(t, len(eps), 0)
	assert.Equal(t, converted[0].Attributes, model.ServiceAttributes{
		ServiceRegistry:          "Kubernetes",
		Name:                     "svc5",
		Namespace:                "nsA",
		Labels:                   nil,
		ExportTo:                 nil,
		LabelSelectors:           nil,
		Aliases:                  nil,
		ClusterExternalAddresses: nil,
		ClusterExternalPorts:     nil,
		K8sAttributes: model.K8sAttributes{
			Type:         string(corev1.ServiceTypeExternalName),
			ExternalName: "foo.co",
		},
	})
}

func createEndpoints(t *testing.T, controller *FakeController, name, namespace string,
	portNames, ips []string, refs []*corev1.ObjectReference, labels map[string]string,
) {
	if labels == nil {
		labels = make(map[string]string)
	}
	// Add the reference to the service. Used by EndpointSlice logic only.
	labels[discovery.LabelServiceName] = name

	if refs == nil {
		refs = make([]*corev1.ObjectReference, len(ips))
	}
	var portNum int32 = 1001
	eas := make([]corev1.EndpointAddress, 0)
	for i, ip := range ips {
		eas = append(eas, corev1.EndpointAddress{IP: ip, TargetRef: refs[i]})
	}

	eps := make([]corev1.EndpointPort, 0)
	for _, name := range portNames {
		eps = append(eps, corev1.EndpointPort{Name: name, Port: portNum})
	}

	// Endpoints is deprecated in k8s >=1.33, but we should still support it.
	// nolint: staticcheck
	endpoint := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Subsets: []corev1.EndpointSubset{{
			Addresses: eas,
			Ports:     eps,
		}},
	}
	clienttest.NewWriter[*corev1.Endpoints](t, controller.client).CreateOrUpdate(endpoint) // nolint: staticcheck

	// Create endpoint slice as well
	esps := make([]discovery.EndpointPort, 0)
	for _, name := range portNames {
		n := name // Create a stable reference to take the pointer from
		esps = append(esps, discovery.EndpointPort{Name: &n, Port: &portNum})
	}

	sliceEndpoint := make([]discovery.Endpoint, 0, len(ips))
	for i, ip := range ips {
		sliceEndpoint = append(sliceEndpoint, discovery.Endpoint{
			Addresses: []string{ip},
			TargetRef: refs[i],
		})
	}
	endpointSlice := &discovery.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Endpoints: sliceEndpoint,
		Ports:     esps,
	}
	clienttest.NewWriter[*discovery.EndpointSlice](t, controller.client).CreateOrUpdate(endpointSlice)
}

func updateEndpoints(controller *FakeController, name, namespace string, portNames, ips []string, t *testing.T) {
	var portNum int32 = 1001
	eas := make([]corev1.EndpointAddress, 0)
	for _, ip := range ips {
		eas = append(eas, corev1.EndpointAddress{IP: ip})
	}

	eps := make([]corev1.EndpointPort, 0)
	for _, name := range portNames {
		eps = append(eps, corev1.EndpointPort{Name: name, Port: portNum})
	}

	// Endpoints is deprecated in k8s >=1.33, but we should still support it.
	// nolint: staticcheck
	endpoint := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Subsets: []corev1.EndpointSubset{{
			Addresses: eas,
			Ports:     eps,
		}},
	}
	if _, err := controller.client.Kube().CoreV1().Endpoints(namespace).Update(context.TODO(), endpoint, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update endpoints %s in namespace %s (error %v)", name, namespace, err)
	}

	// Update endpoint slice as well
	esps := make([]discovery.EndpointPort, 0)
	for i := range portNames {
		esps = append(esps, discovery.EndpointPort{Name: &portNames[i], Port: &portNum})
	}
	endpointSlice := &discovery.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				discovery.LabelServiceName: name,
			},
		},
		Endpoints: []discovery.Endpoint{
			{
				Addresses: ips,
			},
		},
		Ports: esps,
	}
	if _, err := controller.client.Kube().DiscoveryV1().EndpointSlices(namespace).Update(context.TODO(), endpointSlice, metav1.UpdateOptions{}); err != nil {
		t.Errorf("failed to create endpoint slice %s in namespace %s (error %v)", name, namespace, err)
	}
}

func createServiceWithTargetPorts(controller *FakeController, name, namespace string, annotations map[string]string,
	svcPorts []corev1.ServicePort, selector map[string]string, t *testing.T,
) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:  "10.0.0.1", // FIXME: generate?
			ClusterIPs: []string{"10.0.0.1", "10.0.0.2"},
			Ports:      svcPorts,
			Selector:   selector,
			Type:       corev1.ServiceTypeClusterIP,
		},
	}

	clienttest.Wrap(t, controller.services).Create(service)
	controller.opts.XDSUpdater.(*xdsfake.Updater).WaitOrFail(t, "service")
}

func createServiceWait(controller *FakeController, name, namespace string, ips []string, labels, annotations map[string]string,
	ports []int32, selector map[string]string, t *testing.T,
) {
	t.Helper()
	createService(controller, name, namespace, ips, labels, annotations, ports, selector, t)
	controller.opts.XDSUpdater.(*xdsfake.Updater).WaitOrFail(t, "service")
}

func createService(controller *FakeController, name, namespace string, ips []string, labels, annotations map[string]string,
	ports []int32, selector map[string]string, t *testing.T,
) {
	service := generateService(name, namespace, labels, annotations, ports, selector, ips)
	clienttest.Wrap(t, controller.services).CreateOrUpdate(service)
}

func generateService(name, namespace string, labels, annotations map[string]string,
	ports []int32, selector map[string]string, ips []string,
) *corev1.Service {
	svcPorts := make([]corev1.ServicePort, 0)
	for _, p := range ports {
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:     "tcp-port",
			Port:     p,
			Protocol: "http",
		})
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
			Labels:      labels,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP:  ips[0],
			ClusterIPs: ips,
			Ports:      svcPorts,
			Selector:   selector,
			Type:       corev1.ServiceTypeClusterIP,
		},
	}
}

func createVirtualService(controller *FakeController, name, namespace string,
	annotations map[string]string,
	t *testing.T,
) {
	vs := &clientnetworking.VirtualService{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
	}

	clienttest.NewWriter[*clientnetworking.VirtualService](t, controller.client).Create(vs)
}

func getService(controller *FakeController, name, namespace string, t *testing.T) *corev1.Service {
	svc, err := controller.client.Kube().CoreV1().Services(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Cannot get service %s in namespace %s (error: %v)", name, namespace, err)
	}
	return svc
}

func updateService(controller *FakeController, svc *corev1.Service, t *testing.T) *corev1.Service {
	svcUpdated, err := controller.client.Kube().CoreV1().Services(svc.Namespace).Update(context.TODO(), svc, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Cannot update service %s in namespace %s (error: %v)", svc.Name, svc.Namespace, err)
	}
	return svcUpdated
}

func createServiceWithoutClusterIP(controller *FakeController, name, namespace string, annotations map[string]string,
	ports []int32, selector map[string]string, t *testing.T,
) {
	svcPorts := make([]corev1.ServicePort, 0)
	for _, p := range ports {
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:     "tcp-port",
			Port:     p,
			Protocol: "http",
		})
	}
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: corev1.ClusterIPNone,
			Ports:     svcPorts,
			Selector:  selector,
			Type:      corev1.ServiceTypeClusterIP,
		},
	}

	clienttest.Wrap(t, controller.services).Create(service)
}

// nolint: unparam
func createExternalNameService(controller *FakeController, name, namespace string,
	ports []int32, externalName string, t *testing.T, xdsEvents *xdsfake.Updater,
) *corev1.Service {
	svcPorts := make([]corev1.ServicePort, 0)
	for _, p := range ports {
		svcPorts = append(svcPorts, corev1.ServicePort{
			Name:     fmt.Sprintf("tcp-port-%d", p),
			Port:     p,
			Protocol: "http",
		})
	}
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports:        svcPorts,
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: externalName,
		},
	}

	clienttest.Wrap(t, controller.services).Create(service)
	xdsEvents.MatchOrFail(t, xdsfake.Event{Type: "service"})
	return service
}

func servicesEqual(svcList, expectedSvcList []*model.Service) bool {
	if len(svcList) != len(expectedSvcList) {
		return false
	}
	for i, exp := range expectedSvcList {
		if exp.Hostname != svcList[i].Hostname {
			return false
		}
		if exp.DefaultAddress != svcList[i].DefaultAddress {
			return false
		}
		if !reflect.DeepEqual(exp.Ports, svcList[i].Ports) {
			return false
		}
	}
	return true
}

func addPods(t *testing.T, controller *FakeController, fx *xdsfake.Updater, pods ...*corev1.Pod) {
	pc := clienttest.Wrap(t, controller.podsClient)
	for _, pod := range pods {
		newPod := pc.CreateOrUpdate(pod)
		setPodReady(newPod)
		// Apiserver doesn't allow Create/Update to modify the pod status. Creating doesn't result in
		// events - since PodIP will be "".
		newPod.Status.PodIP = pod.Status.PodIP
		newPod.Status.Phase = corev1.PodRunning
		pc.UpdateStatus(newPod)
		waitForPod(t, controller, pod.Status.PodIP)
		// pod first time occur will trigger proxy push
		fx.WaitOrFail(t, "proxy")
	}
}

func setPodReady(pod *corev1.Pod) {
	pod.Status.Conditions = []corev1.PodCondition{
		{
			Type:               corev1.PodReady,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
		},
	}
}

func generatePod(ips []string, name, namespace, saName, node string, labels map[string]string, annotations map[string]string) *corev1.Pod {
	automount := false
	coreIPs := slices.Map(ips, func(ip string) corev1.PodIP {
		return corev1.PodIP{IP: ip}
	})
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
			Namespace:   namespace,
		},
		Spec: corev1.PodSpec{
			ServiceAccountName:           saName,
			NodeName:                     node,
			AutomountServiceAccountToken: &automount,
			// Validation requires this
			Containers: []corev1.Container{
				{
					Name:  "test",
					Image: "ununtu",
				},
			},
		},
		// The cache controller uses this as key, required by our impl.
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:               corev1.PodReady,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
				},
			},
			PodIP:  ips[0],
			HostIP: ips[0],
			PodIPs: coreIPs,
			Phase:  corev1.PodRunning,
		},
	}
}

func generateNode(name string, labels map[string]string) *corev1.Node {
	return &corev1.Node{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func addNodes(t *testing.T, controller *FakeController, nodes ...*corev1.Node) {
	for _, node := range nodes {
		clienttest.Wrap(t, controller.nodes).CreateOrUpdate(node)
		waitForNode(t, controller, node.Name)
	}
}

func TestEndpointUpdate(t *testing.T) {
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})

	pod1 := generatePod([]string{"128.0.0.1"}, "pod1", "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
	pods := []*corev1.Pod{pod1}
	addPods(t, controller, fx, pods...)

	// 1. incremental eds for normal service endpoint update
	createServiceWait(controller, "svc1", "nsa", []string{"10.0.0.1"}, nil, nil,
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)

	// Endpoints are generated by Kubernetes from pod labels and service selectors.
	// Here we manually create them for mocking purpose.
	svc1Ips := []string{"128.0.0.1"}
	portNames := []string{"tcp-port"}
	// Create 1 endpoint that refers to a pod in the same namespace.
	createEndpoints(t, controller, "svc1", "nsa", portNames, svc1Ips, nil, nil)
	fx.WaitOrFail(t, "eds")

	// delete normal service
	clienttest.Wrap(t, controller.services).Delete("svc1", "nsa")
	fx.WaitOrFail(t, "service")

	// 2. full xds push request for headless service endpoint update

	// create a headless service
	createServiceWithoutClusterIP(controller, "svc1", "nsa", nil,
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)
	fx.WaitOrFail(t, "service")

	// Create 1 endpoint that refers to a pod in the same namespace.
	svc1Ips = append(svc1Ips, "128.0.0.2")
	updateEndpoints(controller, "svc1", "nsa", portNames, svc1Ips, t)
	host := string(kube.ServiceHostname("svc1", "nsa", controller.opts.DomainSuffix))
	fx.MatchOrFail(t, xdsfake.Event{Type: "xds full", ID: host})
}

// Validates that when Pilot sees Endpoint before the corresponding Pod, it triggers endpoint event on pod event.
func TestEndpointUpdateBeforePodUpdate(t *testing.T) {
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})

	addNodes(t, controller, generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", label.TopologySubzone.Name: "subzone1"}))
	// Setup help functions to make the test more explicit
	addPod := func(name, ip string) {
		pod := generatePod([]string{ip}, name, "nsA", name, "node1", map[string]string{"app": "prod-app"}, map[string]string{})
		addPods(t, controller, fx, pod)
	}
	deletePod := func(name, ip string) {
		if err := controller.client.Kube().CoreV1().Pods("nsA").Delete(context.TODO(), name, metav1.DeleteOptions{}); err != nil {
			t.Fatal(err)
		}
		retry.UntilSuccessOrFail(t, func() error {
			controller.pods.RLock()
			defer controller.pods.RUnlock()
			if _, ok := controller.pods.podsByIP[ip]; ok {
				return fmt.Errorf("pod still present")
			}
			return nil
		}, retry.Timeout(time.Second))
	}
	addService := func(name string) {
		// create service
		createServiceWait(controller, name, "nsA", []string{"10.0.0.1"}, nil, nil,
			[]int32{8080}, map[string]string{"app": "prod-app"}, t)
	}
	addEndpoint := func(svcName string, ips []string, pods []string) {
		var refs []*corev1.ObjectReference
		for _, pod := range pods {
			if pod == "" {
				refs = append(refs, nil)
			} else {
				refs = append(refs, &corev1.ObjectReference{
					Kind:      "Pod",
					Namespace: "nsA",
					Name:      pod,
				})
			}
		}
		createEndpoints(t, controller, svcName, "nsA", []string{"tcp-port"}, ips, refs, nil)
	}
	assertEndpointsEvent := func(ips []string, pods []string) {
		t.Helper()
		ev := fx.WaitOrFail(t, "eds")
		var gotIps []string
		for _, e := range ev.Endpoints {
			gotIps = append(gotIps, e.Addresses...)
		}
		var gotSA []string
		var expectedSa []string
		for _, e := range pods {
			if e == "" {
				expectedSa = append(expectedSa, "")
			} else {
				expectedSa = append(expectedSa, "spiffe://cluster.local/ns/nsA/sa/"+e)
			}
		}

		for _, e := range ev.Endpoints {
			gotSA = append(gotSA, e.ServiceAccount)
		}
		if !reflect.DeepEqual(gotIps, ips) {
			t.Fatalf("expected ips %v, got %v", ips, gotIps)
		}
		if !reflect.DeepEqual(gotSA, expectedSa) {
			t.Fatalf("expected SAs %v, got %v", expectedSa, gotSA)
		}
	}
	assertPendingResync := func(expected int) {
		t.Helper()
		retry.UntilSuccessOrFail(t, func() error {
			controller.pods.RLock()
			defer controller.pods.RUnlock()
			if len(controller.pods.needResync) != expected {
				return fmt.Errorf("expected %d pods needing resync, got %d", expected, len(controller.pods.needResync))
			}
			return nil
		}, retry.Timeout(time.Second))
	}

	// standard ordering
	addService("svc")
	addPod("pod1", "172.0.1.1")
	addEndpoint("svc", []string{"172.0.1.1"}, []string{"pod1"})
	assertEndpointsEvent([]string{"172.0.1.1"}, []string{"pod1"})
	fx.Clear()

	// Create the endpoint, then later add the pod. Should eventually get an update for the endpoint
	addEndpoint("svc", []string{"172.0.1.1", "172.0.1.2"}, []string{"pod1", "pod2"})
	assertEndpointsEvent([]string{"172.0.1.1"}, []string{"pod1"})
	fx.Clear()
	addPod("pod2", "172.0.1.2")
	assertEndpointsEvent([]string{"172.0.1.1", "172.0.1.2"}, []string{"pod1", "pod2"})
	fx.Clear()

	// Create the endpoint without a pod reference. We should see it immediately
	addEndpoint("svc", []string{"172.0.1.1", "172.0.1.2", "172.0.1.3"}, []string{"pod1", "pod2", ""})
	assertEndpointsEvent([]string{"172.0.1.1", "172.0.1.2", "172.0.1.3"}, []string{"pod1", "pod2", ""})
	fx.Clear()

	// Delete a pod before the endpoint
	addEndpoint("svc", []string{"172.0.1.1"}, []string{"pod1"})
	deletePod("pod2", "172.0.1.2")
	assertEndpointsEvent([]string{"172.0.1.1"}, []string{"pod1"})
	fx.Clear()

	// add another service
	addService("other")
	// Add endpoints for the new service, and the old one. Both should be missing the last IP
	addEndpoint("other", []string{"172.0.1.1", "172.0.1.2"}, []string{"pod1", "pod2"})
	addEndpoint("svc", []string{"172.0.1.1", "172.0.1.2"}, []string{"pod1", "pod2"})
	assertEndpointsEvent([]string{"172.0.1.1"}, []string{"pod1"})
	assertEndpointsEvent([]string{"172.0.1.1"}, []string{"pod1"})
	fx.Clear()
	// Add the pod, expect the endpoints update for both
	addPod("pod2", "172.0.1.2")
	assertEndpointsEvent([]string{"172.0.1.1", "172.0.1.2"}, []string{"pod1", "pod2"})
	assertEndpointsEvent([]string{"172.0.1.1", "172.0.1.2"}, []string{"pod1", "pod2"})

	// Check for memory leaks
	assertPendingResync(0)
	addEndpoint("svc", []string{"172.0.1.1", "172.0.1.2", "172.0.1.3"}, []string{"pod1", "pod2", "pod3"})
	// This is really an implementation detail here - but checking to sanity check our test
	assertPendingResync(1)
	// Remove the endpoint again, with no pod events in between. Should have no memory leaks
	addEndpoint("svc", []string{"172.0.1.1", "172.0.1.2"}, []string{"pod1", "pod2"})
	// TODO this case would leak
	// assertPendingResync(0)

	// completely remove the endpoint
	addEndpoint("svc", []string{"172.0.1.1", "172.0.1.2", "172.0.1.3"}, []string{"pod1", "pod2", "pod3"})
	assertPendingResync(1)
	if err := controller.client.Kube().CoreV1().Endpoints("nsA").Delete(context.TODO(), "svc", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := controller.client.Kube().DiscoveryV1().EndpointSlices("nsA").Delete(context.TODO(), "svc", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	assertPendingResync(0)
}

func TestWorkloadInstanceHandlerMultipleEndpoints(t *testing.T) {
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})

	// Create an initial pod with a service, and endpoint.
	pod1 := generatePod([]string{"172.0.1.1"}, "pod1", "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
	pod2 := generatePod([]string{"172.0.1.2"}, "pod2", "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
	pods := []*corev1.Pod{pod1, pod2}
	nodes := []*corev1.Node{
		generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", label.TopologySubzone.Name: "subzone1"}),
	}
	addNodes(t, controller, nodes...)
	addPods(t, controller, fx, pods...)
	createServiceWait(controller, "svc1", "nsA", []string{"10.0.0.1"}, nil, nil,
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)
	pod1Ips := []string{"172.0.1.1"}
	portNames := []string{"tcp-port"}
	createEndpoints(t, controller, "svc1", "nsA", portNames, pod1Ips, nil, nil)
	fx.WaitOrFail(t, "eds")

	// Simulate adding a workload entry (fired through invocation of WorkloadInstanceHandler)
	controller.workloadInstanceHandler(&model.WorkloadInstance{
		Namespace: "nsA",
		Endpoint: &model.IstioEndpoint{
			Labels:         labels.Instance{"app": "prod-app"},
			ServiceAccount: "account",
			Addresses:      []string{"2.2.2.2", "2001:1::2"},
			EndpointPort:   8080,
		},
	}, model.EventAdd)

	expectedEndpointIPs := []string{"172.0.1.1", "2.2.2.2", "2001:1::2"}
	// Check if an EDS event is fired
	ev := fx.WaitOrFail(t, "eds")
	// check if the hostname matches that of k8s service svc1.nsA
	if ev.ID != "svc1.nsA.svc.company.com" {
		t.Fatalf("eds event for workload entry addition did not match the expected service. got %s, want %s",
			ev.ID, "svc1.nsA.svc.company.com")
	}
	// we should have the pod IP and the workload Entry's IP in the endpoints..
	// the first endpoint should be that of the k8s pod and the second one should be the workload entry

	gotEndpointIPs := make([]string, 0, len(ev.Endpoints))
	for _, ep := range ev.Endpoints {
		gotEndpointIPs = append(gotEndpointIPs, ep.Addresses...)
	}
	if !reflect.DeepEqual(gotEndpointIPs, expectedEndpointIPs) {
		t.Fatalf("eds update after adding workload entry did not match expected list. got %v, want %v",
			gotEndpointIPs, expectedEndpointIPs)
	}

	// Check if InstancesByPort returns the same list
	converted := controller.Services()
	if len(converted) != 1 {
		t.Fatalf("failed to get services (%v), converted", converted)
	}
	endpoints := GetEndpoints(converted[0], controller.Endpoints)
	gotEndpointIPs = []string{}
	for _, instance := range endpoints {
		gotEndpointIPs = append(gotEndpointIPs, instance.Addresses...)
	}
	if !reflect.DeepEqual(gotEndpointIPs, expectedEndpointIPs) {
		t.Fatalf("InstancesByPort after adding workload entry did not match expected list. got %v, want %v",
			gotEndpointIPs, expectedEndpointIPs)
	}

	// Now add a k8s pod to the service and ensure that eds updates contain both pod IPs and workload entry IPs.
	updateEndpoints(controller, "svc1", "nsA", portNames, []string{"172.0.1.1", "172.0.1.2"}, t)
	ev = fx.WaitOrFail(t, "eds")
	gotEndpointIPs = []string{}
	for _, ep := range ev.Endpoints {
		gotEndpointIPs = append(gotEndpointIPs, ep.Addresses...)
	}
	expectedEndpointIPs = []string{"172.0.1.1", "172.0.1.2", "2.2.2.2", "2001:1::2"}
	if !reflect.DeepEqual(gotEndpointIPs, expectedEndpointIPs) {
		t.Fatalf("eds update after adding pod did not match expected list. got %v, want %v",
			gotEndpointIPs, expectedEndpointIPs)
	}
}

func TestWorkloadInstanceHandler_WorkloadInstanceIndex(t *testing.T) {
	ctl, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{})

	verifyGetByIP := func(address string, want []*model.WorkloadInstance) {
		t.Helper()
		got := ctl.workloadInstancesIndex.GetByIP(address)

		assert.Equal(t, want, got)
	}

	wi1 := &model.WorkloadInstance{
		Name:      "ratings-1",
		Namespace: "bookinfo",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "ratings"},
			Addresses:    []string{"2.2.2.2"},
			EndpointPort: 8080,
		},
	}

	// simulate adding a workload entry
	ctl.workloadInstanceHandler(wi1, model.EventAdd)

	verifyGetByIP("2.2.2.2", []*model.WorkloadInstance{wi1})

	wi2 := &model.WorkloadInstance{
		Name:      "details-1",
		Namespace: "bookinfo",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "details"},
			Addresses:    []string{"3.3.3.3"},
			EndpointPort: 9090,
		},
	}

	// simulate adding a workload entry
	ctl.workloadInstanceHandler(wi2, model.EventAdd)

	verifyGetByIP("2.2.2.2", []*model.WorkloadInstance{wi1})
	verifyGetByIP("3.3.3.3", []*model.WorkloadInstance{wi2})

	wiWithMulAddrs := &model.WorkloadInstance{
		Name:      "details-2",
		Namespace: "bookinfo",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "details"},
			Addresses:    []string{"4.4.4.4", "2001:1::4"},
			EndpointPort: 9090,
		},
	}

	// simulate adding a workload entry
	ctl.workloadInstanceHandler(wiWithMulAddrs, model.EventAdd)
	verifyGetByIP("2.2.2.2", []*model.WorkloadInstance{wi1})
	verifyGetByIP("3.3.3.3", []*model.WorkloadInstance{wi2})
	// TODO: change from "4.4.4.4" to "4.4.4.4,2001:1::4"
	verifyGetByIP("4.4.4.4", []*model.WorkloadInstance{wiWithMulAddrs})

	wi3 := &model.WorkloadInstance{
		Name:      "details-1",
		Namespace: "bookinfo",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "details"},
			Addresses:    []string{"2.2.2.2"}, // update IP
			EndpointPort: 9090,
		},
	}

	// simulate updating a workload entry
	ctl.workloadInstanceHandler(wi3, model.EventUpdate)

	verifyGetByIP("3.3.3.3", nil)
	verifyGetByIP("2.2.2.2", []*model.WorkloadInstance{wi3, wi1})

	// simulate deleting a workload entry
	ctl.workloadInstanceHandler(wi3, model.EventDelete)

	verifyGetByIP("2.2.2.2", []*model.WorkloadInstance{wi1})

	// simulate deleting a workload entry
	ctl.workloadInstanceHandler(wi1, model.EventDelete)

	verifyGetByIP("2.2.2.2", nil)
}

func TestUpdateEdsCacheOnServiceUpdate(t *testing.T) {
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})

	// Create an initial pod with a service, and endpoint.
	pod1 := generatePod([]string{"172.0.1.1"}, "pod1", "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
	pod2 := generatePod([]string{"172.0.1.2"}, "pod2", "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
	pods := []*corev1.Pod{pod1, pod2}
	nodes := []*corev1.Node{
		generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", label.TopologySubzone.Name: "subzone1"}),
	}
	addNodes(t, controller, nodes...)
	addPods(t, controller, fx, pods...)
	createServiceWait(controller, "svc1", "nsA", []string{"10.0.0.1"}, nil, nil,
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)

	pod1Ips := []string{"172.0.1.1"}
	portNames := []string{"tcp-port"}
	createEndpoints(t, controller, "svc1", "nsA", portNames, pod1Ips, nil, nil)
	fx.WaitOrFail(t, "eds")

	// update service selector
	svc := getService(controller, "svc1", "nsA", t)
	svc.Spec.Selector = map[string]string{
		"app": "prod-app",
		"foo": "bar",
	}
	// set `K8SServiceSelectWorkloadEntries` to false temporarily
	tmp := features.EnableK8SServiceSelectWorkloadEntries
	features.EnableK8SServiceSelectWorkloadEntries = false
	defer func() {
		features.EnableK8SServiceSelectWorkloadEntries = tmp
	}()
	svc = updateService(controller, svc, t)
	// don't update eds cache if `K8S_SELECT_WORKLOAD_ENTRIES` is disabled
	fx.WaitOrFail(t, "service")
	fx.AssertEmpty(t, 0)

	features.EnableK8SServiceSelectWorkloadEntries = true
	svc.Spec.Selector = map[string]string{
		"app": "prod-app",
	}
	updateService(controller, svc, t)
	// update eds cache if `K8S_SELECT_WORKLOAD_ENTRIES` is enabled
	fx.WaitOrFail(t, "eds cache")
}

func TestVisibilityNoneService(t *testing.T) {
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})
	serviceHandler := func(_, curr *model.Service, _ model.Event) {
		pushReq := &model.PushRequest{
			Full:           true,
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: string(curr.Hostname), Namespace: curr.Attributes.Namespace}),
			Reason:         model.NewReasonStats(model.ServiceUpdate),
		}
		fx.ConfigUpdate(pushReq)
	}
	controller.Controller.AppendServiceHandler(serviceHandler)

	// Create an initial pod with a service with None visibility, and endpoint.
	pod1 := generatePod([]string{"172.0.1.1"}, "pod1", "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
	pod2 := generatePod([]string{"172.0.1.2"}, "pod2", "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
	pods := []*corev1.Pod{pod1, pod2}
	nodes := []*corev1.Node{
		generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", label.TopologySubzone.Name: "subzone1"}),
	}
	addNodes(t, controller, nodes...)
	addPods(t, controller, fx, pods...)
	createServiceWait(controller, "svc1", "nsA", []string{"10.0.0.1"}, nil, map[string]string{annotation.NetworkingExportTo.Name: "~"},
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)

	pod1Ips := []string{"172.0.1.1"}
	portNames := []string{"tcp-port"}
	createEndpoints(t, controller, "svc1", "nsA", portNames, pod1Ips, nil, nil)
	// We should not get any events - service should be ignored.
	fx.AssertEmpty(t, 0)

	// update service and remove exportTo annotation.
	svc := getService(controller, "svc1", "nsA", t)
	svc.Annotations = map[string]string{}
	updateService(controller, svc, t)
	fx.WaitOrFail(t, "service")
	host := string(kube.ServiceHostname("svc1", "nsA", controller.opts.DomainSuffix))
	// We should see a full push.
	fx.MatchOrFail(t, xdsfake.Event{Type: "xds full", ID: host})
}

func TestDiscoverySelector(t *testing.T) {
	networksWatcher := meshwatcher.NewFixedNetworksWatcher(&meshconfig.MeshNetworks{
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
	})
	ctl, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{NetworksWatcher: networksWatcher})
	t.Parallel()
	ns := "ns-test"

	hostname := kube.ServiceHostname(testService, ns, defaultFakeDomainSuffix)

	var sds model.ServiceDiscovery = ctl
	// "test", ports: http-example on 80
	makeService(testService, ns, ctl, t)

	eventually(t, func() bool {
		out := sds.Services()

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

	svc := sds.GetService(hostname)
	if svc == nil {
		t.Fatalf("GetService(%q) => should exists", hostname)
	}
	if svc.Hostname != hostname {
		t.Fatalf("GetService(%q) => %q", hostname, svc.Hostname)
	}

	missing := kube.ServiceHostname("does-not-exist", ns, defaultFakeDomainSuffix)
	svc = sds.GetService(missing)
	if svc != nil {
		t.Fatalf("GetService(%q) => %s, should not exist", missing, svc.Hostname)
	}
}

func TestStripNodeUnusedFields(t *testing.T) {
	inputNode := &corev1.Node{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
			Labels: map[string]string{
				NodeZoneLabel:              "zone1",
				NodeRegionLabel:            "region1",
				label.TopologySubzone.Name: "subzone1",
			},
			Annotations: map[string]string{
				"annotation1": "foo",
				"annotation2": "bar",
			},
			ManagedFields: []metav1.ManagedFieldsEntry{
				{
					Manager: "test",
				},
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					Name: "test",
				},
			},
		},
		Status: corev1.NodeStatus{
			Allocatable: map[corev1.ResourceName]resource.Quantity{
				"cpu": {
					Format: "500m",
				},
			},
			Capacity: map[corev1.ResourceName]resource.Quantity{
				"cpu": {
					Format: "500m",
				},
			},
			Images: []corev1.ContainerImage{
				{
					Names: []string{"test"},
				},
			},
			Conditions: []corev1.NodeCondition{
				{
					Type: corev1.NodeMemoryPressure,
				},
			},
		},
	}

	expectNode := &corev1.Node{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
			Labels: map[string]string{
				NodeZoneLabel:              "zone1",
				NodeRegionLabel:            "region1",
				label.TopologySubzone.Name: "subzone1",
			},
		},
	}

	controller, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{})
	addNodes(t, controller, inputNode)

	assert.Equal(t, expectNode, controller.nodes.Get(inputNode.Name, ""))
}

func TestStripPodUnusedFields(t *testing.T) {
	inputPod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app": "test",
			},
			Annotations: map[string]string{
				"annotation1": "foo",
				"annotation2": "bar",
			},
			ManagedFields: []metav1.ManagedFieldsEntry{
				{
					Manager: "test",
				},
			},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Name: "init-container",
				},
			},
			Containers: []corev1.Container{
				{
					Name: "container-1",
					Ports: []corev1.ContainerPort{
						{
							Name: "http",
						},
					},
				},
				{
					Name: "container-2",
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "test",
				},
			},
		},
		Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "init-container",
				},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "container-1",
				},
				{
					Name: "container-2",
				},
			},
			PodIP:  "1.1.1.1",
			HostIP: "1.1.1.1",
			Phase:  corev1.PodRunning,
		},
	}

	expectPod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
			Labels: map[string]string{
				"app": "test",
			},
			Annotations: map[string]string{
				"annotation1": "foo",
				"annotation2": "bar",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Ports: []corev1.ContainerPort{
						{
							Name: "http",
						},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			PodIP:  "1.1.1.1",
			HostIP: "1.1.1.1",
			Phase:  corev1.PodRunning,
		},
	}

	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})
	addPods(t, controller, fx, inputPod)

	output := controller.pods.getPodByKey(config.NamespacedName(expectPod))
	// The final pod status conditions will be determined by the function addPods.
	// So we assign these status conditions to expect pod.
	expectPod.Status.Conditions = output.Status.Conditions
	if !reflect.DeepEqual(expectPod, output) {
		t.Fatalf("Wanted: %v\n. Got: %v", expectPod, output)
	}
}

func TestServiceUpdateNeedsPush(t *testing.T) {
	newService := func(exportTo visibility.Instance, ports []int) *model.Service {
		s := &model.Service{
			Attributes: model.ServiceAttributes{
				ExportTo: sets.New(exportTo),
			},
		}
		for _, port := range ports {
			s.Ports = append(s.Ports, &model.Port{
				Port: port,
			})
		}
		return s
	}

	type testcase struct {
		name     string
		prev     *corev1.Service
		curr     *corev1.Service
		prevConv *model.Service
		currConv *model.Service
		expect   bool
	}

	tests := []testcase{
		{
			name:     "no change",
			prevConv: newService(visibility.Public, []int{80}),
			currConv: newService(visibility.Public, []int{80}),
			expect:   false,
		},
		{
			name:     "new service",
			prevConv: nil,
			currConv: newService(visibility.Public, []int{80}),
			expect:   true,
		},
		{
			name:     "new service with none visibility",
			prevConv: nil,
			currConv: newService(visibility.None, []int{80}),
			expect:   false,
		},
		{
			name:     "public visibility, spec change",
			prevConv: newService(visibility.Public, []int{80}),
			currConv: newService(visibility.Public, []int{80, 443}),
			expect:   true,
		},
		{
			name:     "none visibility, spec change",
			prevConv: newService(visibility.None, []int{80}),
			currConv: newService(visibility.None, []int{80, 443}),
			expect:   false,
		},
	}

	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "bar",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: 80, TargetPort: intstr.FromInt32(8080)}},
		},
	}
	updatedSvc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "bar",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{Port: 80, TargetPort: intstr.FromInt32(8081)}},
		},
	}
	tests = append(tests,
		testcase{
			name:     "target ports changed",
			prev:     &svc,
			curr:     &updatedSvc,
			prevConv: kube.ConvertService(svc, constants.DefaultClusterLocalDomain, "", nil),
			currConv: kube.ConvertService(updatedSvc, constants.DefaultClusterLocalDomain, "", nil),
			expect:   true,
		},
		testcase{
			name:     "target ports unchanged",
			prev:     &svc,
			curr:     &svc,
			prevConv: kube.ConvertService(svc, constants.DefaultClusterLocalDomain, "", nil),
			currConv: kube.ConvertService(svc, constants.DefaultClusterLocalDomain, "", nil),
			expect:   false,
		})

	for _, test := range tests {
		actual := serviceUpdateNeedsPush(test.prev, test.curr, test.prevConv, test.currConv)
		if actual != test.expect {
			t.Fatalf("%s: expected %v, got %v", test.name, test.expect, actual)
		}
	}
}
