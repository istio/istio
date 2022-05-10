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
	"sync"
	"testing"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	"github.com/google/go-cmp/cmp"
	coreV1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/annotation"
	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/filter"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/protocol"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	testService = "test"
)

// eventually polls cond until it completes (returns true) or times out (resulting in a test failure).
func eventually(t test.Failer, cond func() bool) {
	t.Helper()
	retry.UntilSuccessOrFail(t, func() error {
		if !cond() {
			return fmt.Errorf("failed to get positive condition")
		}
		return nil
	}, retry.Timeout(time.Second), retry.Delay(time.Millisecond*10))
}

func TestServices(t *testing.T) {
	networksWatcher := mesh.NewFixedNetworksWatcher(&meshconfig.MeshNetworks{
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
	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			ctl, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{NetworksWatcher: networksWatcher, Mode: mode})
			t.Parallel()
			ns := "ns-test"

			hostname := kube.ServiceHostname(testService, ns, defaultFakeDomainSuffix)

			var sds model.ServiceDiscovery = ctl
			// "test", ports: http-example on 80
			makeService(testService, ns, ctl.client.Kube(), t)
			<-fx.Events

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

			eventually(t, func() bool {
				ep := sds.InstancesByPort(svc, 80, nil)
				return len(ep) == 2
			})

			ep := sds.InstancesByPort(svc, 80, nil)
			if len(ep) != 2 {
				t.Fatalf("Invalid response for GetInstancesByPort %v", ep)
			}

			if ep[0].Endpoint.Address == "10.10.1.1" && ep[0].Endpoint.Network != "network1" {
				t.Fatalf("Endpoint with IP 10.10.1.1 is expected to be in network1 but get: %s", ep[0].Endpoint.Network)
			}

			if ep[1].Endpoint.Address == "10.11.1.2" && ep[1].Endpoint.Network != "network2" {
				t.Fatalf("Endpoint with IP 10.11.1.2 is expected to be in network2 but get: %s", ep[1].Endpoint.Network)
			}

			missing := kube.ServiceHostname("does-not-exist", ns, defaultFakeDomainSuffix)
			svc = sds.GetService(missing)
			if svc != nil {
				t.Fatalf("GetService(%q) => %s, should not exist", missing, svc.Hostname)
			}
		})
	}
}

func makeService(n, ns string, cl kubernetes.Interface, t *testing.T) {
	_, err := cl.CoreV1().Services(ns).Create(context.TODO(), &coreV1.Service{
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
	}, metaV1.CreateOptions{})
	if err != nil {
		t.Log("Service already created (rerunning test)")
	}
	log.Infof("Created service %s", n)
}

func TestController_GetPodLocality(t *testing.T) {
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
				generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", label.TopologySubzone.Name: "subzone1"}),
				generateNode("node2", map[string]string{NodeZoneLabel: "zone2", NodeRegionLabel: "region2", label.TopologySubzone.Name: "subzone2"}),
			},
			wantAZ: map[*coreV1.Pod]string{
				pod1: "region1/zone1/subzone1",
				pod2: "region2/zone2/subzone2",
			},
		},
		{
			name: "should return correct az for given address",
			pods: []*coreV1.Pod{pod1, pod2},
			nodes: []*coreV1.Node{
				generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1"}),
				generateNode("node2", map[string]string{NodeZoneLabel: "zone2", NodeRegionLabel: "region2"}),
			},
			wantAZ: map[*coreV1.Pod]string{
				pod1: "region1/zone1/",
				pod2: "region2/zone2/",
			},
		},
		{
			name: "should return false if pod isn't in the cache",
			wantAZ: map[*coreV1.Pod]string{
				pod1: "",
				pod2: "",
			},
		},
		{
			name: "should return false if node isn't in the cache",
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
				pod1: "region1//",
				pod2: "region2//",
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
				pod1: "/zone1/",
				pod2: "/zone2/",
			},
		},
		{
			name: "should return correct az if node has only subzone label",
			pods: []*coreV1.Pod{pod1, pod2},
			nodes: []*coreV1.Node{
				generateNode("node1", map[string]string{label.TopologySubzone.Name: "subzone1"}),
				generateNode("node2", map[string]string{label.TopologySubzone.Name: "subzone2"}),
			},
			wantAZ: map[*coreV1.Pod]string{
				pod1: "//subzone1",
				pod2: "//subzone2",
			},
		},
		{
			name: "should return correct az for given address",
			pods: []*coreV1.Pod{podOverride},
			nodes: []*coreV1.Node{
				generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", label.TopologySubzone.Name: "subzone1"}),
			},
			wantAZ: map[*coreV1.Pod]string{
				podOverride: "regionOverride/zoneOverride/subzoneOverride",
			},
		},
	}

	for _, tc := range testCases {
		// If using t.Parallel() you must copy the iteration to a new local variable
		// https://github.com/golang/go/wiki/CommonMistakes#using-goroutines-on-loop-iterator-variables
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Setup kube caches
			// Pod locality only matters for Endpoints
			controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{Mode: EndpointsOnly})

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

func TestGetProxyServiceInstances(t *testing.T) {
	clusterID := cluster.ID("fakeCluster")
	networkID := network.ID("fakeNetwork")
	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
				Mode:      mode,
				ClusterID: clusterID,
			})
			// add a network ID to test endpoints include topology.istio.io/network label
			controller.network = networkID

			p := generatePod("128.0.0.1", "pod1", "nsa", "foo", "node1", map[string]string{"app": "test-app"}, map[string]string{})
			addPods(t, controller, fx, p)

			k8sSaOnVM := "acct4"
			canonicalSaOnVM := "acctvm2@gserviceaccount2.com"

			createService(controller, "svc1", "nsa",
				map[string]string{
					annotation.AlphaKubernetesServiceAccounts.Name: k8sSaOnVM,
					annotation.AlphaCanonicalServiceAccounts.Name:  canonicalSaOnVM,
				},
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
			createEndpoints(t, controller, "svc1", "nsA", portNames, svc1Ips, nil, nil)

			// Creates 100 endpoints that refers to a pod in a different namespace.
			fakeSvcCounts := 100
			for i := 0; i < fakeSvcCounts; i++ {
				svcName := fmt.Sprintf("svc-fake-%d", i)
				createService(controller, svcName, "nsfake",
					map[string]string{
						annotation.AlphaKubernetesServiceAccounts.Name: k8sSaOnVM,
						annotation.AlphaCanonicalServiceAccounts.Name:  canonicalSaOnVM,
					},
					[]int32{8080}, map[string]string{"app": "prod-app"}, t)
				fx.Wait("service")

				createEndpoints(t, controller, svcName, "nsfake", portNames, svc1Ips, nil, nil)
				fx.Wait("eds")
			}

			// Create 1 endpoint that refers to a pod in the same namespace.
			createEndpoints(t, controller, "svc1", "nsa", portNames, svc1Ips, nil, nil)
			fx.Wait("eds")

			// this can test get pod by proxy ID
			svcNode := &model.Proxy{
				Type:        model.Router,
				IPAddresses: []string{"128.0.0.1"},
				ID:          "pod1.nsa",
				DNSDomain:   "nsa.svc.cluster.local",
				Metadata:    &model.NodeMetadata{Namespace: "nsa", ClusterID: clusterID},
			}
			serviceInstances := controller.GetProxyServiceInstances(svcNode)

			if len(serviceInstances) != 1 {
				t.Fatalf("GetProxyServiceInstances() expected 1 instance, got %d", len(serviceInstances))
			}

			hostname := kube.ServiceHostname("svc1", "nsa", defaultFakeDomainSuffix)
			if serviceInstances[0].Service.Hostname != hostname {
				t.Fatalf("GetProxyServiceInstances() wrong service instance returned => hostname %q, want %q",
					serviceInstances[0].Service.Hostname, hostname)
			}

			// Test that we can look up instances just by Proxy metadata
			metaServices := controller.GetProxyServiceInstances(&model.Proxy{
				Type:            "sidecar",
				IPAddresses:     []string{"1.1.1.1"},
				Locality:        &core.Locality{Region: "r", Zone: "z"},
				ConfigNamespace: "nsa",
				Metadata: &model.NodeMetadata{
					ServiceAccount: "account",
					ClusterID:      clusterID,
					Labels: map[string]string{
						"app":                      "prod-app",
						label.SecurityTlsMode.Name: "mutual",
					},
				},
			})

			expected := &model.ServiceInstance{
				Service: &model.Service{
					Hostname: "svc1.nsa.svc.company.com",
					ClusterVIPs: model.AddressMap{
						Addresses: map[cluster.ID][]string{clusterID: {"10.0.0.1"}},
					},
					DefaultAddress:  "10.0.0.1",
					Ports:           []*model.Port{{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP}},
					ServiceAccounts: []string{"acctvm2@gserviceaccount2.com", "spiffe://cluster.local/ns/nsa/sa/acct4"},
					Attributes: model.ServiceAttributes{
						ServiceRegistry: provider.Kubernetes,
						Name:            "svc1",
						Namespace:       "nsa",
						LabelSelectors:  map[string]string{"app": "prod-app"},
					},
				},
				ServicePort: &model.Port{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP},
				Endpoint: &model.IstioEndpoint{
					Labels: labels.Instance{
						"app":                      "prod-app",
						label.SecurityTlsMode.Name: "mutual",
						NodeRegionLabelGA:          "r",
						NodeZoneLabelGA:            "z",
						label.TopologyCluster.Name: clusterID.String(),
						label.TopologyNetwork.Name: networkID.String(),
					},
					ServiceAccount:  "account",
					Address:         "1.1.1.1",
					Network:         networkID,
					EndpointPort:    0,
					ServicePortName: "tcp-port",
					Locality: model.Locality{
						Label:     "r/z",
						ClusterID: clusterID,
					},
					TLSMode: "mutual",
				},
			}

			if len(metaServices) != 1 {
				t.Fatalf("expected 1 instance, got %v", len(metaServices))
			}
			// Remove the discoverability function so that it's ignored by DeepEqual.
			clearDiscoverabilityPolicy(metaServices[0].Endpoint)
			if !reflect.DeepEqual(expected, metaServices[0]) {
				t.Fatalf("expected instance %v, got %v", expected, metaServices[0])
			}

			// Test that we first look up instances by Proxy pod

			node := generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", label.TopologySubzone.Name: "subzone1"})
			addNodes(t, controller, node)

			// 1. pod without `istio-locality` label, get locality from node label.
			p = generatePod("129.0.0.1", "pod2", "nsa", "svcaccount", "node1",
				map[string]string{"app": "prod-app"}, nil)
			addPods(t, controller, fx, p)

			// this can test get pod by proxy ip address
			podServices := controller.GetProxyServiceInstances(&model.Proxy{
				Type:            "sidecar",
				IPAddresses:     []string{"129.0.0.1"},
				Locality:        &core.Locality{Region: "r", Zone: "z"},
				ConfigNamespace: "nsa",
				Metadata: &model.NodeMetadata{
					ServiceAccount: "account",
					ClusterID:      clusterID,
					Labels: map[string]string{
						"app": "prod-app",
					},
				},
			})

			expected = &model.ServiceInstance{
				Service: &model.Service{
					Hostname: "svc1.nsa.svc.company.com",
					ClusterVIPs: model.AddressMap{
						Addresses: map[cluster.ID][]string{clusterID: {"10.0.0.1"}},
					},
					DefaultAddress:  "10.0.0.1",
					Ports:           []*model.Port{{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP}},
					ServiceAccounts: []string{"acctvm2@gserviceaccount2.com", "spiffe://cluster.local/ns/nsa/sa/acct4"},
					Attributes: model.ServiceAttributes{
						ServiceRegistry: provider.Kubernetes,
						Name:            "svc1",
						Namespace:       "nsa",
						LabelSelectors:  map[string]string{"app": "prod-app"},
					},
				},
				ServicePort: &model.Port{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP},
				Endpoint: &model.IstioEndpoint{
					Address:         "129.0.0.1",
					Network:         networkID,
					EndpointPort:    0,
					ServicePortName: "tcp-port",
					Locality: model.Locality{
						Label:     "region1/zone1/subzone1",
						ClusterID: clusterID,
					},
					Labels: labels.Instance{
						"app":                      "prod-app",
						NodeRegionLabelGA:          "region1",
						NodeZoneLabelGA:            "zone1",
						label.TopologySubzone.Name: "subzone1",
						label.TopologyCluster.Name: clusterID.String(),
						label.TopologyNetwork.Name: networkID.String(),
					},
					ServiceAccount: "spiffe://cluster.local/ns/nsa/sa/svcaccount",
					TLSMode:        model.DisabledTLSModeLabel,
					WorkloadName:   "pod2",
					Namespace:      "nsa",
				},
			}
			if len(podServices) != 1 {
				t.Fatalf("expected 1 instance, got %v", len(podServices))
			}
			clearDiscoverabilityPolicy(podServices[0].Endpoint)
			if !reflect.DeepEqual(expected, podServices[0]) {
				t.Fatalf("expected instance %v, got %v", expected, podServices[0])
			}

			// 2. pod with `istio-locality` label, ignore node label.
			p = generatePod("129.0.0.2", "pod3", "nsa", "svcaccount", "node1",
				map[string]string{"app": "prod-app", "istio-locality": "region.zone"}, nil)
			addPods(t, controller, fx, p)

			// this can test get pod by proxy ip address
			podServices = controller.GetProxyServiceInstances(&model.Proxy{
				Type:            "sidecar",
				IPAddresses:     []string{"129.0.0.2"},
				Locality:        &core.Locality{Region: "r", Zone: "z"},
				ConfigNamespace: "nsa",
				Metadata: &model.NodeMetadata{
					ServiceAccount: "account",
					ClusterID:      clusterID,
					Labels: map[string]string{
						"app": "prod-app",
					},
				},
			})

			expected = &model.ServiceInstance{
				Service: &model.Service{
					Hostname: "svc1.nsa.svc.company.com",
					ClusterVIPs: model.AddressMap{
						Addresses: map[cluster.ID][]string{clusterID: {"10.0.0.1"}},
					},
					DefaultAddress:  "10.0.0.1",
					Ports:           []*model.Port{{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP}},
					ServiceAccounts: []string{"acctvm2@gserviceaccount2.com", "spiffe://cluster.local/ns/nsa/sa/acct4"},
					Attributes: model.ServiceAttributes{
						ServiceRegistry: provider.Kubernetes,
						Name:            "svc1",
						Namespace:       "nsa",
						LabelSelectors:  map[string]string{"app": "prod-app"},
					},
				},
				ServicePort: &model.Port{Name: "tcp-port", Port: 8080, Protocol: protocol.TCP},
				Endpoint: &model.IstioEndpoint{
					Address:         "129.0.0.2",
					Network:         networkID,
					EndpointPort:    0,
					ServicePortName: "tcp-port",
					Locality: model.Locality{
						Label:     "region/zone",
						ClusterID: clusterID,
					},
					Labels: labels.Instance{
						"app":                      "prod-app",
						"istio-locality":           "region.zone",
						NodeRegionLabelGA:          "region",
						NodeZoneLabelGA:            "zone",
						label.TopologyCluster.Name: clusterID.String(),
						label.TopologyNetwork.Name: networkID.String(),
					},
					ServiceAccount: "spiffe://cluster.local/ns/nsa/sa/svcaccount",
					TLSMode:        model.DisabledTLSModeLabel,
					WorkloadName:   "pod3",
					Namespace:      "nsa",
				},
			}
			if len(podServices) != 1 {
				t.Fatalf("expected 1 instance, got %v", len(podServices))
			}
			clearDiscoverabilityPolicy(podServices[0].Endpoint)
			if !reflect.DeepEqual(expected, podServices[0]) {
				t.Fatalf("expected instance %v, got %v", expected, podServices[0])
			}
		})
	}
}

func TestGetProxyServiceInstancesWithMultiIPsAndTargetPorts(t *testing.T) {
	pod1 := generatePod("128.0.0.1", "pod1", "nsa", "foo", "node1", map[string]string{"app": "test-app"}, map[string]string{})
	testCases := []struct {
		name          string
		pods          []*coreV1.Pod
		ips           []string
		ports         []coreV1.ServicePort
		wantEndpoints []model.IstioEndpoint
	}{
		{
			name: "multiple proxy ips single port",
			pods: []*coreV1.Pod{pod1},
			ips:  []string{"128.0.0.1", "192.168.2.6"},
			ports: []coreV1.ServicePort{
				{
					Name:       "tcp-port",
					Port:       8080,
					Protocol:   "http",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
				},
			},
			wantEndpoints: []model.IstioEndpoint{
				{
					Address:         "128.0.0.1",
					ServicePortName: "tcp-port",
					EndpointPort:    8080,
				},
				{
					Address:         "192.168.2.6",
					ServicePortName: "tcp-port",
					EndpointPort:    8080,
				},
			},
		},
		{
			name: "single proxy ip single port",
			pods: []*coreV1.Pod{pod1},
			ips:  []string{"128.0.0.1"},
			ports: []coreV1.ServicePort{
				{
					Name:       "tcp-port",
					Port:       8080,
					Protocol:   "TCP",
					TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 8080},
				},
			},
			wantEndpoints: []model.IstioEndpoint{
				{
					Address:         "128.0.0.1",
					ServicePortName: "tcp-port",
					EndpointPort:    8080,
				},
			},
		},
		{
			name: "multiple proxy ips multiple ports",
			pods: []*coreV1.Pod{pod1},
			ips:  []string{"128.0.0.1", "192.168.2.6"},
			ports: []coreV1.ServicePort{
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
			wantEndpoints: []model.IstioEndpoint{
				{
					Address:         "128.0.0.1",
					ServicePortName: "tcp-port-1",
					EndpointPort:    8080,
				},
				{
					Address:         "192.168.2.6",
					ServicePortName: "tcp-port-1",
					EndpointPort:    8080,
				},
				{
					Address:         "128.0.0.1",
					ServicePortName: "tcp-port-2",
					EndpointPort:    9090,
				},
				{
					Address:         "192.168.2.6",
					ServicePortName: "tcp-port-2",
					EndpointPort:    9090,
				},
			},
		},
		{
			name: "single proxy ip multiple ports same target port with different protocols",
			pods: []*coreV1.Pod{pod1},
			ips:  []string{"128.0.0.1"},
			ports: []coreV1.ServicePort{
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
			wantEndpoints: []model.IstioEndpoint{
				{
					Address:         "128.0.0.1",
					ServicePortName: "tcp-port",
					EndpointPort:    8080,
				},
				{
					Address:         "128.0.0.1",
					ServicePortName: "http-port",
					EndpointPort:    8080,
				},
			},
		},
		{
			name: "single proxy ip multiple ports same target port with overlapping protocols",
			pods: []*coreV1.Pod{pod1},
			ips:  []string{"128.0.0.1"},
			ports: []coreV1.ServicePort{
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
			wantEndpoints: []model.IstioEndpoint{
				{
					Address:         "128.0.0.1",
					ServicePortName: "http-7442",
					EndpointPort:    7442,
				},
				{
					Address:         "128.0.0.1",
					ServicePortName: "tcp-8443",
					EndpointPort:    7442,
				},
			},
		},
		{
			name: "single proxy ip multiple ports",
			pods: []*coreV1.Pod{pod1},
			ips:  []string{"128.0.0.1"},
			ports: []coreV1.ServicePort{
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
			wantEndpoints: []model.IstioEndpoint{
				{
					Address:         "128.0.0.1",
					ServicePortName: "tcp-port",
					EndpointPort:    8080,
				},
				{
					Address:         "128.0.0.1",
					ServicePortName: "http-port",
					EndpointPort:    9090,
				},
			},
		},
	}

	for _, c := range testCases {
		for mode, name := range EndpointModeNames {
			mode := mode
			t.Run(fmt.Sprintf("%s_%s", c.name, name), func(t *testing.T) {
				// Setup kube caches
				controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{Mode: mode})

				addPods(t, controller, fx, c.pods...)

				createServiceWithTargetPorts(controller, "svc1", "nsa",
					map[string]string{
						annotation.AlphaKubernetesServiceAccounts.Name: "acct4",
						annotation.AlphaCanonicalServiceAccounts.Name:  "acctvm2@gserviceaccount2.com",
					},
					c.ports, map[string]string{"app": "test-app"}, t)

				ev := fx.Wait("service")
				if ev == nil {
					t.Fatal("Timeout creating service")
				}
				serviceInstances := controller.GetProxyServiceInstances(&model.Proxy{Metadata: &model.NodeMetadata{}, IPAddresses: c.ips})

				for i, svc := range serviceInstances {
					if svc.Endpoint.Address != c.wantEndpoints[i].Address {
						t.Errorf("wrong endpoint address at #i endpoint, got %v want %v", svc.Endpoint.Address, c.wantEndpoints[i].Address)
					}
					if svc.Endpoint.EndpointPort != c.wantEndpoints[i].EndpointPort {
						t.Errorf("wrong endpoint port at #i endpoint, got %v want %v", svc.Endpoint.EndpointPort, c.wantEndpoints[i].EndpointPort)
					}
					if svc.Endpoint.ServicePortName != c.wantEndpoints[i].ServicePortName {
						t.Errorf("wrong svc port at #i endpoint, got %v want %v", svc.Endpoint.ServicePortName, c.wantEndpoints[i].ServicePortName)
					}
				}
			})
		}
	}
}

func TestGetProxyServiceInstances_WorkloadInstance(t *testing.T) {
	ctl, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})

	createService(ctl, "ratings", "bookinfo-ratings",
		map[string]string{
			annotation.AlphaKubernetesServiceAccounts.Name: "ratings",
			annotation.AlphaCanonicalServiceAccounts.Name:  "ratings@gserviceaccount2.com",
		},
		[]int32{8080}, map[string]string{"app": "ratings"}, t)
	fx.WaitOrFail(t, "service")

	createService(ctl, "details", "bookinfo-details",
		map[string]string{
			annotation.AlphaKubernetesServiceAccounts.Name: "details",
			annotation.AlphaCanonicalServiceAccounts.Name:  "details@gserviceaccount2.com",
		},
		[]int32{9090}, map[string]string{"app": "details"}, t)
	fx.WaitOrFail(t, "service")

	createService(ctl, "reviews", "bookinfo-reviews",
		map[string]string{
			annotation.AlphaKubernetesServiceAccounts.Name: "reviews",
			annotation.AlphaCanonicalServiceAccounts.Name:  "reviews@gserviceaccount2.com",
		},
		[]int32{7070}, map[string]string{"app": "reviews"}, t)
	fx.WaitOrFail(t, "service")

	wiRatings1 := &model.WorkloadInstance{
		Name:      "ratings-1",
		Namespace: "bookinfo-ratings",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "ratings"},
			Address:      "2.2.2.21",
			EndpointPort: 8080,
		},
	}

	wiDetails1 := &model.WorkloadInstance{
		Name:      "details-1",
		Namespace: "bookinfo-details",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "details"},
			Address:      "2.2.2.21",
			EndpointPort: 9090,
		},
	}

	wiReviews1 := &model.WorkloadInstance{
		Name:      "reviews-1",
		Namespace: "bookinfo-reviews",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "reviews"},
			Address:      "3.3.3.31",
			EndpointPort: 7070,
		},
	}

	wiReviews2 := &model.WorkloadInstance{
		Name:      "reviews-2",
		Namespace: "bookinfo-reviews",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "reviews"},
			Address:      "3.3.3.32",
			EndpointPort: 7071,
		},
	}

	wiProduct1 := &model.WorkloadInstance{
		Name:      "productpage-1",
		Namespace: "bookinfo-productpage",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "productpage"},
			Address:      "4.4.4.41",
			EndpointPort: 6060,
		},
	}

	for _, wi := range []*model.WorkloadInstance{wiRatings1, wiDetails1, wiReviews1, wiReviews2, wiProduct1} {
		ctl.WorkloadInstanceHandler(wi, model.EventAdd) // simulate adding a workload entry
	}

	cases := []struct {
		name  string
		proxy *model.Proxy
		want  []*model.ServiceInstance
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
			proxy: &model.Proxy{Metadata: &model.NodeMetadata{}, IPAddresses: []string{"4.4.4.41"}},
			want:  nil,
		},
		{
			name:  "proxy with IP from the registry, 1 matching WE, and matching Service",
			proxy: &model.Proxy{Metadata: &model.NodeMetadata{}, IPAddresses: []string{"3.3.3.31"}},
			want: []*model.ServiceInstance{{
				Service: &model.Service{
					Hostname: "reviews.bookinfo-reviews.svc.company.com",
				},
				Endpoint: &model.IstioEndpoint{
					Labels:          map[string]string{"app": "reviews"},
					Address:         "3.3.3.31",
					ServicePortName: "tcp-port",
					EndpointPort:    7070,
				},
			}},
		},
		{
			name:  "proxy with IP from the registry, 2 matching WE, and matching Service",
			proxy: &model.Proxy{Metadata: &model.NodeMetadata{}, IPAddresses: []string{"2.2.2.21"}},
			want: []*model.ServiceInstance{{
				Service: &model.Service{
					Hostname: "details.bookinfo-details.svc.company.com",
				},
				Endpoint: &model.IstioEndpoint{
					Labels:          map[string]string{"app": "details"}, // should pick "details" because of ordering
					Address:         "2.2.2.21",
					ServicePortName: "tcp-port",
					EndpointPort:    9090,
				},
			}},
		},
		{
			name: "proxy with IP from the registry, 2 matching WE, and matching Service, and proxy ID equal to WE with a different address",
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{}, IPAddresses: []string{"2.2.2.21"},
				ID: "reviews-1.bookinfo-reviews", ConfigNamespace: "bookinfo-reviews",
			},
			want: []*model.ServiceInstance{{
				Service: &model.Service{
					Hostname: "details.bookinfo-details.svc.company.com",
				},
				Endpoint: &model.IstioEndpoint{
					Labels:          map[string]string{"app": "details"}, // should pick "details" because of ordering
					Address:         "2.2.2.21",
					ServicePortName: "tcp-port",
					EndpointPort:    9090,
				},
			}},
		},
		{
			name: "proxy with IP from the registry, 2 matching WE, and matching Service, and proxy ID equal to WE name, but proxy.ID != proxy.ConfigNamespace",
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{}, IPAddresses: []string{"2.2.2.21"},
				ID: "ratings-1.bookinfo-ratings", ConfigNamespace: "wrong-namespace",
			},
			want: []*model.ServiceInstance{{
				Service: &model.Service{
					Hostname: "details.bookinfo-details.svc.company.com",
				},
				Endpoint: &model.IstioEndpoint{
					Labels:          map[string]string{"app": "details"}, // should pick "details" because of ordering
					Address:         "2.2.2.21",
					ServicePortName: "tcp-port",
					EndpointPort:    9090,
				},
			}},
		},
		{
			name: "proxy with IP from the registry, 2 matching WE, and matching Service, and proxy.ID == WE name",
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{}, IPAddresses: []string{"2.2.2.21"},
				ID: "ratings-1.bookinfo-ratings", ConfigNamespace: "bookinfo-ratings",
			},
			want: []*model.ServiceInstance{{
				Service: &model.Service{
					Hostname: "ratings.bookinfo-ratings.svc.company.com",
				},
				Endpoint: &model.IstioEndpoint{
					Labels:          map[string]string{"app": "ratings"}, // should pick "ratings"
					Address:         "2.2.2.21",
					ServicePortName: "tcp-port",
					EndpointPort:    8080,
				},
			}},
		},
		{
			name: "proxy with IP from the registry, 2 matching WE, and matching Service, and proxy.ID != WE name, but proxy.ConfigNamespace == WE namespace",
			proxy: &model.Proxy{
				Metadata: &model.NodeMetadata{}, IPAddresses: []string{"2.2.2.21"},
				ID: "wrong-name.bookinfo-ratings", ConfigNamespace: "bookinfo-ratings",
			},
			want: []*model.ServiceInstance{{
				Service: &model.Service{
					Hostname: "ratings.bookinfo-ratings.svc.company.com",
				},
				Endpoint: &model.IstioEndpoint{
					Labels:          map[string]string{"app": "ratings"}, // should pick "ratings"
					Address:         "2.2.2.21",
					ServicePortName: "tcp-port",
					EndpointPort:    8080,
				},
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ctl.GetProxyServiceInstances(tc.proxy)

			if diff := cmp.Diff(len(tc.want), len(got)); diff != "" {
				t.Fatalf("GetProxyServiceInstances() returned unexpected number of service instances (--want/++got): %v", diff)
			}

			for i := range tc.want {
				if diff := cmp.Diff(tc.want[i].Service.Hostname, got[i].Service.Hostname); diff != "" {
					t.Fatalf("GetProxyServiceInstances() returned unexpected value [%d].Service.Hostname (--want/++got): %v", i, diff)
				}
				if diff := cmp.Diff(tc.want[i].Endpoint, got[i].Endpoint); diff != "" {
					t.Fatalf("GetProxyServiceInstances() returned unexpected value [%d].Endpoint (--want/++got): %v", i, diff)
				}
			}
		})
	}
}

func TestController_GetIstioServiceAccounts(t *testing.T) {
	oldTrustDomain := spiffe.GetTrustDomain()
	spiffe.SetTrustDomain(defaultFakeDomainSuffix)
	defer spiffe.SetTrustDomain(oldTrustDomain)

	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{Mode: mode})

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
			addPods(t, controller, fx, pods...)

			createService(controller, "svc1", "nsA",
				map[string]string{
					annotation.AlphaKubernetesServiceAccounts.Name: k8sSaOnVM,
					annotation.AlphaCanonicalServiceAccounts.Name:  canonicalSaOnVM,
				},
				[]int32{8080}, map[string]string{"app": "prod-app"}, t)
			fx.Wait("service")
			createService(controller, "svc2", "nsA", nil, []int32{8080}, map[string]string{"app": "staging-app"}, t)
			fx.Wait("service")

			// Endpoints are generated by Kubernetes from pod labels and service selectors.
			// Here we manually create them for mocking purpose.
			svc1Ips := []string{"128.0.0.2"}
			svc2Ips := make([]string, 0)
			portNames := []string{"tcp-port"}
			createEndpoints(t, controller, "svc1", "nsA", portNames, svc1Ips, nil, nil)
			createEndpoints(t, controller, "svc2", "nsA", portNames, svc2Ips, nil, nil)

			// We expect only one EDS update with Endpoints.
			<-fx.Events

			hostname := kube.ServiceHostname("svc1", "nsA", defaultFakeDomainSuffix)
			svc := controller.GetService(hostname)
			sa := controller.GetIstioServiceAccounts(svc, []int{8080})
			sort.Strings(sa)
			expected := []string{
				canonicalSaOnVM,
				"spiffe://company.com/ns/nsA/sa/" + sa2,
				"spiffe://company.com/ns/nsA/sa/" + k8sSaOnVM,
			}
			if !reflect.DeepEqual(sa, expected) {
				t.Fatalf("Unexpected service accounts %v (expecting %v)", sa, expected)
			}

			hostname = kube.ServiceHostname("svc2", "nsA", defaultFakeDomainSuffix)
			svc = controller.GetService(hostname)
			sa = controller.GetIstioServiceAccounts(svc, []int{})
			if len(sa) != 0 {
				t.Fatal("Failure: Expected to resolve 0 service accounts, but got: ", sa)
			}
		})
	}
}

func TestController_Service(t *testing.T) {
	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{Mode: mode})

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
		})
	}
}

func TestController_ServiceWithFixedDiscoveryNamespaces(t *testing.T) {
	meshWatcher := mesh.NewFixedWatcher(&meshconfig.MeshConfig{
		DiscoverySelectors: []*metaV1.LabelSelector{
			{
				MatchLabels: map[string]string{
					"pilot-discovery": "enabled",
				},
			},
			{
				MatchExpressions: []metaV1.LabelSelectorRequirement{
					{
						Key:      "env",
						Operator: metaV1.LabelSelectorOpIn,
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

	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
				Mode:        mode,
				MeshWatcher: meshWatcher,
			})

			nsA := "nsA"
			nsB := "nsB"

			// event handlers should only be triggered for services in namespaces selected for discovery
			createNamespace(t, controller.client.Kube(), nsA, map[string]string{"pilot-discovery": "enabled"})
			createNamespace(t, controller.client.Kube(), nsB, map[string]string{})

			// wait for namespaces to be created
			eventually(t, func() bool {
				list, err := controller.client.Kube().CoreV1().Namespaces().List(context.TODO(), metaV1.ListOptions{})
				if err != nil {
					t.Fatalf("error listing namespaces: %v", err)
				}
				return len(list.Items) == 2
			})

			// service event handlers should trigger for svc1 and svc2
			createService(controller, "svc1", nsA,
				map[string]string{},
				[]int32{8080}, map[string]string{"test-app": "test-app-1"}, t)
			if ev := fx.Wait("service"); ev == nil {
				t.Fatal("Timeout creating service")
			}
			createService(controller, "svc2", nsA,
				map[string]string{},
				[]int32{8081}, map[string]string{"test-app": "test-app-2"}, t)
			if ev := fx.Wait("service"); ev == nil {
				t.Fatal("Timeout creating service")
			}
			// service event handlers should not trigger for svc3 and svc4
			createService(controller, "svc3", nsB,
				map[string]string{},
				[]int32{8082}, map[string]string{"test-app": "test-app-3"}, t)
			createService(controller, "svc4", nsB,
				map[string]string{},
				[]int32{8083}, map[string]string{"test-app": "test-app-4"}, t)

			expectedSvcList := []*model.Service{svc1, svc2}
			eventually(t, func() bool {
				svcList := controller.Services()
				return servicesEqual(svcList, expectedSvcList)
			})

			// test updating namespace with adding discovery label
			updateNamespace(t, controller.client.Kube(), nsB, map[string]string{"env": "test"})
			// service event handlers should trigger for svc3 and svc4
			if ev := fx.Wait("service"); ev == nil {
				t.Fatal("Timeout creating service")
			}
			if ev := fx.Wait("service"); ev == nil {
				t.Fatal("Timeout creating service")
			}
			expectedSvcList = []*model.Service{svc1, svc2, svc3, svc4}
			eventually(t, func() bool {
				svcList := controller.Services()
				return servicesEqual(svcList, expectedSvcList)
			})

			// test updating namespace by removing discovery label
			updateNamespace(t, controller.client.Kube(), nsA, map[string]string{"pilot-discovery": "disabled"})
			// service event handlers should trigger for svc1 and svc2
			if ev := fx.Wait("service"); ev == nil {
				t.Fatal("Timeout creating service")
			}
			if ev := fx.Wait("service"); ev == nil {
				t.Fatal("Timeout creating service")
			}
			expectedSvcList = []*model.Service{svc3, svc4}
			eventually(t, func() bool {
				svcList := controller.Services()
				return servicesEqual(svcList, expectedSvcList)
			})
		})
	}
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
		testMeshWatcher *mesh.TestWatcher,
		fx *FakeXdsUpdater,
		controller *FakeController,
	) {
		// update meshConfig
		if err := testMeshWatcher.Update(meshConfig, 5); err != nil {
			t.Fatalf("%v", err)
		}

		// assert firing of service events
		for i := 0; i < expectedNumSvcEvents; i++ {
			if ev := fx.Wait("service"); ev == nil {
				t.Fatal("timed out waiting for service event")
			}
		}

		eventually(t, func() bool {
			svcList := controller.Services()
			return servicesEqual(svcList, expectedSvcList)
		})
	}

	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			client := kubelib.NewFakeClient()
			meshWatcher := mesh.NewTestWatcher(&meshconfig.MeshConfig{})
			discoveryNamespacesFilter := filter.NewDiscoveryNamespacesFilter(
				client.KubeInformer().Core().V1().Namespaces().Lister(),
				meshWatcher.Mesh().DiscoverySelectors,
			)

			controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
				Client:                    client,
				Mode:                      mode,
				MeshWatcher:               meshWatcher,
				DiscoveryNamespacesFilter: discoveryNamespacesFilter,
			})

			nsA := "nsA"
			nsB := "nsB"
			nsC := "nsC"

			createNamespace(t, controller.client.Kube(), nsA, map[string]string{"app": "foo"})
			createNamespace(t, controller.client.Kube(), nsB, map[string]string{"app": "bar"})
			createNamespace(t, controller.client.Kube(), nsC, map[string]string{"app": "baz"})

			// wait for namespaces to be created
			eventually(t, func() bool {
				list, err := controller.client.Kube().CoreV1().Namespaces().List(context.TODO(), metaV1.ListOptions{})
				if err != nil {
					t.Fatalf("error listing namespaces: %v", err)
				}
				return len(list.Items) == 3
			})

			// assert that namespace membership has been updated
			eventually(t, func() bool {
				members := discoveryNamespacesFilter.GetMembers()
				return members.Has(nsA) && members.Has(nsB) && members.Has(nsC)
			})

			// service event handlers should trigger for all svcs
			createService(controller, "svc1", nsA,
				map[string]string{},
				[]int32{8080}, map[string]string{"test-app": "test-app-1"}, t)
			if ev := fx.Wait("service"); ev == nil {
				t.Fatal("Timeout creating service")
			}
			createService(controller, "svc2", nsA,
				map[string]string{},
				[]int32{8081}, map[string]string{"test-app": "test-app-2"}, t)
			if ev := fx.Wait("service"); ev == nil {
				t.Fatal("Timeout creating service")
			}
			createService(controller, "svc3", nsB,
				map[string]string{},
				[]int32{8082}, map[string]string{"test-app": "test-app-3"}, t)
			if ev := fx.Wait("service"); ev == nil {
				t.Fatal("Timeout creating service")
			}
			createService(controller, "svc4", nsC,
				map[string]string{},
				[]int32{8083}, map[string]string{"test-app": "test-app-4"}, t)
			if ev := fx.Wait("service"); ev == nil {
				t.Fatal("Timeout creating service")
			}

			expectedSvcList := []*model.Service{svc1, svc2, svc3, svc4}
			eventually(t, func() bool {
				svcList := controller.Services()
				return servicesEqual(svcList, expectedSvcList)
			})

			// restrict namespaces to nsA (expect 2 delete events for svc3 and svc4)
			updateMeshConfig(
				&meshconfig.MeshConfig{
					DiscoverySelectors: []*metaV1.LabelSelector{
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
					DiscoverySelectors: []*metaV1.LabelSelector{
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
					DiscoverySelectors: []*metaV1.LabelSelector{
						{
							MatchExpressions: []metaV1.LabelSelectorRequirement{
								{
									Key:      "app",
									Operator: metaV1.LabelSelectorOpIn,
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
					DiscoverySelectors: []*metaV1.LabelSelector{},
				},
				[]*model.Service{svc1, svc2, svc3, svc4},
				1,
				meshWatcher,
				fx,
				controller,
			)
		})
	}
}

func TestInstancesByPort_WorkloadInstances(t *testing.T) {
	ctl, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})

	createServiceWithTargetPorts(ctl, "ratings", "bookinfo-ratings",
		map[string]string{
			annotation.AlphaKubernetesServiceAccounts.Name: "ratings",
			annotation.AlphaCanonicalServiceAccounts.Name:  "ratings@gserviceaccount2.com",
		},
		[]coreV1.ServicePort{
			{
				Name:       "http-port",
				Port:       8080,
				Protocol:   "TCP",
				TargetPort: intstr.IntOrString{Type: intstr.String, StrVal: "http"},
			},
		},
		map[string]string{"app": "ratings"}, t)
	fx.WaitOrFail(t, "service")

	wiRatings1 := &model.WorkloadInstance{
		Name:      "ratings-1",
		Namespace: "bookinfo-ratings",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "ratings"},
			Address:      "2.2.2.2",
			EndpointPort: 8081, // should be ignored since it doesn't define PortMap
		},
	}

	wiRatings2 := &model.WorkloadInstance{
		Name:      "ratings-2",
		Namespace: "bookinfo-ratings",
		Endpoint: &model.IstioEndpoint{
			Labels:  labels.Instance{"app": "ratings"},
			Address: "2.2.2.2",
		},
		PortMap: map[string]uint32{
			"http": 8082, // should be used
		},
	}

	wiRatings3 := &model.WorkloadInstance{
		Name:      "ratings-3",
		Namespace: "bookinfo-ratings",
		Endpoint: &model.IstioEndpoint{
			Labels:  labels.Instance{"app": "ratings"},
			Address: "2.2.2.2",
		},
		PortMap: map[string]uint32{
			"http": 8083, // should be used
		},
	}

	for _, wi := range []*model.WorkloadInstance{wiRatings1, wiRatings2, wiRatings3} {
		ctl.WorkloadInstanceHandler(wi, model.EventAdd) // simulate adding a workload entry
	}

	// get service object

	svcs := ctl.Services()
	if len(svcs) != 1 {
		t.Fatalf("failed to get services (%v)", svcs)
	}

	// get service instances

	instances := ctl.InstancesByPort(svcs[0], 8080, nil)

	want := []string{"2.2.2.2:8082", "2.2.2.2:8083"} // expect both WorkloadEntries even though they have the same IP

	var got []string
	for _, instance := range instances {
		got = append(got, net.JoinHostPort(instance.Endpoint.Address, strconv.Itoa(int(instance.Endpoint.EndpointPort))))
	}
	sort.Strings(got)

	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("InstancesByPort() returned unexpected list of endpoints (--want/++got): %v", diff)
	}
}

//
func TestExternalNameServiceInstances(t *testing.T) {
	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{Mode: mode})
			createExternalNameService(controller, "svc5", "nsA",
				[]int32{1, 2, 3}, "foo.co", t, fx.Events)

			converted := controller.Services()
			if len(converted) != 1 {
				t.Fatalf("failed to get services (%v)s", converted)
			}
			instances := controller.InstancesByPort(converted[0], 1, nil)
			if len(instances) != 1 {
				t.Fatalf("expected 1 instance, got %v", instances)
			}
			if instances[0].ServicePort.Port != 1 {
				t.Fatalf("expected port 1, got %v", instances[0].ServicePort.Port)
			}
		})
	}
}

func TestController_ExternalNameService(t *testing.T) {
	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			deleteWg := sync.WaitGroup{}
			controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
				Mode: mode,
				ServiceHandler: func(_ *model.Service, e model.Event) {
					if e == model.EventDelete {
						deleteWg.Done()
					}
				},
			})

			k8sSvcs := []*coreV1.Service{
				createExternalNameService(controller, "svc1", "nsA",
					[]int32{8080}, "test-app-1.test.svc."+defaultFakeDomainSuffix, t, fx.Events),
				createExternalNameService(controller, "svc2", "nsA",
					[]int32{8081}, "test-app-2.test.svc."+defaultFakeDomainSuffix, t, fx.Events),
				createExternalNameService(controller, "svc3", "nsA",
					[]int32{8082}, "test-app-3.test.pod."+defaultFakeDomainSuffix, t, fx.Events),
				createExternalNameService(controller, "svc4", "nsA",
					[]int32{8083}, "g.co", t, fx.Events),
			}

			expectedSvcList := []*model.Service{
				{
					Hostname: kube.ServiceHostname("svc1", "nsA", defaultFakeDomainSuffix),
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
					Hostname: kube.ServiceHostname("svc2", "nsA", defaultFakeDomainSuffix),
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
					Hostname: kube.ServiceHostname("svc3", "nsA", defaultFakeDomainSuffix),
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
					Hostname: kube.ServiceHostname("svc4", "nsA", defaultFakeDomainSuffix),
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

			svcList := controller.Services()
			if len(svcList) != len(expectedSvcList) {
				t.Fatalf("Expecting %d service but got %d\r\n", len(expectedSvcList), len(svcList))
			}
			for i, exp := range expectedSvcList {
				if exp.Hostname != svcList[i].Hostname {
					t.Fatalf("got hostname of %dst service, got:\n%#v\nwanted:\n%#v\n", i+1, svcList[i].Hostname, exp.Hostname)
				}
				if !reflect.DeepEqual(exp.Ports, svcList[i].Ports) {
					t.Fatalf("got ports of %dst service, got:\n%#v\nwanted:\n%#v\n", i+1, svcList[i].Ports, exp.Ports)
				}
				if svcList[i].MeshExternal != exp.MeshExternal {
					t.Fatalf("i=%v, MeshExternal==%v, should be %v: externalName='%s'", i+1, exp.MeshExternal, svcList[i].MeshExternal, k8sSvcs[i].Spec.ExternalName)
				}
				if svcList[i].Resolution != exp.Resolution {
					t.Fatalf("i=%v, Resolution=='%v', should be '%v'", i+1, svcList[i].Resolution, exp.Resolution)
				}
				instances := controller.InstancesByPort(svcList[i], svcList[i].Ports[0].Port, nil)
				if len(instances) != 1 {
					t.Fatalf("should be exactly 1 instance: len(instances) = %v", len(instances))
				}
				if instances[0].Endpoint.Address != k8sSvcs[i].Spec.ExternalName {
					t.Fatalf("wrong instance endpoint address: '%s' != '%s'", instances[0].Endpoint.Address, k8sSvcs[i].Spec.ExternalName)
				}
			}

			deleteWg.Add(len(k8sSvcs))
			for _, s := range k8sSvcs {
				deleteExternalNameService(controller, s.Name, s.Namespace, t, fx.Events)
			}
			deleteWg.Wait()

			svcList = controller.Services()
			if len(svcList) != 0 {
				t.Fatalf("Should have 0 services at this point")
			}
			for _, exp := range expectedSvcList {
				instances := controller.InstancesByPort(exp, exp.Ports[0].Port, nil)
				if len(instances) != 0 {
					t.Fatalf("should be exactly 0 instance: len(instances) = %v", len(instances))
				}
			}
		})
	}
}

func createEndpoints(t *testing.T, controller *FakeController, name, namespace string,
	portNames, ips []string, refs []*coreV1.ObjectReference, labels map[string]string) {
	if labels == nil {
		labels = make(map[string]string)
	}
	// Add the reference to the service. Used by EndpointSlice logic only.
	labels[discovery.LabelServiceName] = name

	if refs == nil {
		refs = make([]*coreV1.ObjectReference, len(ips))
	}
	var portNum int32 = 1001
	eas := make([]coreV1.EndpointAddress, 0)
	for i, ip := range ips {
		eas = append(eas, coreV1.EndpointAddress{IP: ip, TargetRef: refs[i]})
	}

	eps := make([]coreV1.EndpointPort, 0)
	for _, name := range portNames {
		eps = append(eps, coreV1.EndpointPort{Name: name, Port: portNum})
	}

	endpoint := &coreV1.Endpoints{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Subsets: []coreV1.EndpointSubset{{
			Addresses: eas,
			Ports:     eps,
		}},
	}
	if _, err := controller.client.Kube().CoreV1().Endpoints(namespace).Create(context.TODO(), endpoint, metaV1.CreateOptions{}); err != nil {
		if errors.IsAlreadyExists(err) {
			_, err = controller.client.Kube().CoreV1().Endpoints(namespace).Update(context.TODO(), endpoint, metaV1.UpdateOptions{})
		}
		if err != nil {
			t.Fatalf("failed to create endpoints %s in namespace %s (error %v)", name, namespace, err)
		}
	}

	// Create endpoint slice as well
	esps := make([]discovery.EndpointPort, 0)
	for _, name := range portNames {
		n := name // Create a stable reference to take the pointer from
		esps = append(esps, discovery.EndpointPort{Name: &n, Port: &portNum})
	}

	var sliceEndpoint []discovery.Endpoint
	for i, ip := range ips {
		sliceEndpoint = append(sliceEndpoint, discovery.Endpoint{
			Addresses: []string{ip},
			TargetRef: refs[i],
		})
	}
	endpointSlice := &discovery.EndpointSlice{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Endpoints: sliceEndpoint,
		Ports:     esps,
	}
	if _, err := controller.client.Kube().DiscoveryV1().EndpointSlices(namespace).Create(context.TODO(), endpointSlice, metaV1.CreateOptions{}); err != nil {
		if errors.IsAlreadyExists(err) {
			_, err = controller.client.Kube().DiscoveryV1().EndpointSlices(namespace).Update(context.TODO(), endpointSlice, metaV1.UpdateOptions{})
		}
		if err != nil {
			t.Fatalf("failed to create endpoint slice %s in namespace %s (error %v)", name, namespace, err)
		}
	}
}

func updateEndpoints(controller *FakeController, name, namespace string, portNames, ips []string, t *testing.T) {
	var portNum int32 = 1001
	eas := make([]coreV1.EndpointAddress, 0)
	for _, ip := range ips {
		eas = append(eas, coreV1.EndpointAddress{IP: ip})
	}

	eps := make([]coreV1.EndpointPort, 0)
	for _, name := range portNames {
		eps = append(eps, coreV1.EndpointPort{Name: name, Port: portNum})
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
	if _, err := controller.client.Kube().CoreV1().Endpoints(namespace).Update(context.TODO(), endpoint, metaV1.UpdateOptions{}); err != nil {
		t.Fatalf("failed to update endpoints %s in namespace %s (error %v)", name, namespace, err)
	}

	// Update endpoint slice as well
	esps := make([]discovery.EndpointPort, 0)
	for i := range portNames {
		esps = append(esps, discovery.EndpointPort{Name: &portNames[i], Port: &portNum})
	}
	endpointSlice := &discovery.EndpointSlice{
		ObjectMeta: metaV1.ObjectMeta{
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
	if _, err := controller.client.Kube().DiscoveryV1().EndpointSlices(namespace).Update(context.TODO(), endpointSlice, metaV1.UpdateOptions{}); err != nil {
		t.Errorf("failed to create endpoint slice %s in namespace %s (error %v)", name, namespace, err)
	}
}

func createServiceWithTargetPorts(controller *FakeController, name, namespace string, annotations map[string]string,
	svcPorts []coreV1.ServicePort, selector map[string]string, t *testing.T) {
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

	_, err := controller.client.Kube().CoreV1().Services(namespace).Create(context.TODO(), service, metaV1.CreateOptions{})
	if err != nil {
		t.Fatalf("Cannot create service %s in namespace %s (error: %v)", name, namespace, err)
	}
}

func createService(controller *FakeController, name, namespace string, annotations map[string]string,
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

	_, err := controller.client.Kube().CoreV1().Services(namespace).Create(context.TODO(), service, metaV1.CreateOptions{})
	if err != nil {
		t.Fatalf("Cannot create service %s in namespace %s (error: %v)", name, namespace, err)
	}
}

func getService(controller *FakeController, name, namespace string, t *testing.T) *coreV1.Service {
	svc, err := controller.client.Kube().CoreV1().Services(namespace).Get(context.TODO(), name, metaV1.GetOptions{})
	if err != nil {
		t.Fatalf("Cannot get service %s in namespace %s (error: %v)", name, namespace, err)
	}
	return svc
}

func updateService(controller *FakeController, svc *coreV1.Service, t *testing.T) *coreV1.Service {
	svc, err := controller.client.Kube().CoreV1().Services(svc.Namespace).Update(context.TODO(), svc, metaV1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Cannot update service %s in namespace %s (error: %v)", svc.Name, svc.Namespace, err)
	}
	return svc
}

func createServiceWithoutClusterIP(controller *FakeController, name, namespace string, annotations map[string]string,
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

	_, err := controller.client.Kube().CoreV1().Services(namespace).Create(context.TODO(), service, metaV1.CreateOptions{})
	if err != nil {
		t.Fatalf("Cannot create service %s in namespace %s (error: %v)", name, namespace, err)
	}
}

// nolint: unparam
func createExternalNameService(controller *FakeController, name, namespace string,
	ports []int32, externalName string, t *testing.T, xdsEvents <-chan FakeXdsEvent) *coreV1.Service {
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

	_, err := controller.client.Kube().CoreV1().Services(namespace).Create(context.TODO(), service, metaV1.CreateOptions{})
	if err != nil {
		t.Fatalf("Cannot create service %s in namespace %s (error: %v)", name, namespace, err)
	}
	return service
}

func deleteExternalNameService(controller *FakeController, name, namespace string, t *testing.T, xdsEvents <-chan FakeXdsEvent) {
	defer func() {
		<-xdsEvents
	}()

	err := controller.client.Kube().CoreV1().Services(namespace).Delete(context.TODO(), name, metaV1.DeleteOptions{})
	if err != nil {
		t.Fatalf("Cannot delete service %s in namespace %s (error: %v)", name, namespace, err)
	}
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

func addPods(t *testing.T, controller *FakeController, fx *FakeXdsUpdater, pods ...*coreV1.Pod) {
	for _, pod := range pods {
		p, _ := controller.client.Kube().CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metaV1.GetOptions{})
		var newPod *coreV1.Pod
		var err error
		if p == nil {
			newPod, err = controller.client.Kube().CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metaV1.CreateOptions{})
			if err != nil {
				t.Fatalf("Cannot create %s in namespace %s (error: %v)", pod.ObjectMeta.Name, pod.ObjectMeta.Namespace, err)
			}
		} else {
			newPod, err = controller.client.Kube().CoreV1().Pods(pod.Namespace).Update(context.TODO(), pod, metaV1.UpdateOptions{})
			if err != nil {
				t.Fatalf("Cannot update %s in namespace %s (error: %v)", pod.ObjectMeta.Name, pod.ObjectMeta.Namespace, err)
			}
		}

		setPodReady(newPod)
		// Apiserver doesn't allow Create/Update to modify the pod status. Creating doesn't result in
		// events - since PodIP will be "".
		newPod.Status.PodIP = pod.Status.PodIP
		newPod.Status.Phase = coreV1.PodRunning
		_, _ = controller.client.Kube().CoreV1().Pods(pod.Namespace).UpdateStatus(context.TODO(), newPod, metaV1.UpdateOptions{})
		if err := waitForPod(controller, pod.Status.PodIP); err != nil {
			t.Fatal(err)
		}
		// pod first time occur will trigger proxy push
		fx.Wait("proxy")
	}
}

func setPodReady(pod *coreV1.Pod) {
	pod.Status.Conditions = []coreV1.PodCondition{
		{
			Type:               coreV1.PodReady,
			Status:             coreV1.ConditionTrue,
			LastTransitionTime: metaV1.Now(),
		},
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
			Conditions: []coreV1.PodCondition{
				{
					Type:               coreV1.PodReady,
					Status:             coreV1.ConditionTrue,
					LastTransitionTime: metaV1.Now(),
				},
			},
			PodIP:  ip,
			HostIP: ip,
			Phase:  coreV1.PodRunning,
		},
	}
}

func generateNode(name string, labels map[string]string) *coreV1.Node {
	return &coreV1.Node{
		TypeMeta: metaV1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},
		ObjectMeta: metaV1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func addNodes(t *testing.T, controller *FakeController, nodes ...*coreV1.Node) {
	fakeClient := controller.client
	for _, node := range nodes {
		_, err := fakeClient.Kube().CoreV1().Nodes().Create(context.TODO(), node, metaV1.CreateOptions{})
		if errors.IsAlreadyExists(err) {
			if _, err := fakeClient.Kube().CoreV1().Nodes().Update(context.TODO(), node, metaV1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
		} else if err != nil {
			t.Fatal(err)
		}
		if err := waitForNode(controller, node.Name); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEndpointUpdate(t *testing.T) {
	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{Mode: mode})

			pod1 := generatePod("128.0.0.1", "pod1", "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
			pods := []*coreV1.Pod{pod1}
			addPods(t, controller, fx, pods...)

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
			createEndpoints(t, controller, "svc1", "nsa", portNames, svc1Ips, nil, nil)
			if ev := fx.Wait("eds"); ev == nil {
				t.Fatalf("Timeout incremental eds")
			}

			// delete normal service
			err := controller.client.Kube().CoreV1().Services("nsa").Delete(context.TODO(), "svc1", metaV1.DeleteOptions{})
			if err != nil {
				t.Fatalf("Cannot delete service (error: %v)", err)
			}
			if ev := fx.Wait("service"); ev == nil {
				t.Fatalf("Timeout deleting service")
			}

			// 2. full xds push request for headless service endpoint update

			// create a headless service
			createServiceWithoutClusterIP(controller, "svc1", "nsa", nil,
				[]int32{8080}, map[string]string{"app": "prod-app"}, t)
			if ev := fx.Wait("service"); ev == nil {
				t.Fatalf("Timeout creating service")
			}

			// Create 1 endpoint that refers to a pod in the same namespace.
			svc1Ips = append(svc1Ips, "128.0.0.2")
			updateEndpoints(controller, "svc1", "nsa", portNames, svc1Ips, t)
			ev := fx.Wait("xds")
			if ev == nil {
				t.Fatalf("Timeout xds push")
			}
			if ev.ID != string(kube.ServiceHostname("svc1", "nsa", controller.opts.DomainSuffix)) {
				t.Errorf("Expect service %s updated, but got %s",
					kube.ServiceHostname("svc1", "nsa", controller.opts.DomainSuffix), ev.ID)
			}
		})
	}
}

// Validates that when Pilot sees Endpoint before the corresponding Pod, it triggers endpoint event on pod event.
func TestEndpointUpdateBeforePodUpdate(t *testing.T) {
	for mode, name := range EndpointModeNames {
		mode := mode
		t.Run(name, func(t *testing.T) {
			controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{Mode: mode})

			addNodes(t, controller, generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", label.TopologySubzone.Name: "subzone1"}))
			// Setup help functions to make the test more explicit
			addPod := func(name, ip string) {
				pod := generatePod(ip, name, "nsA", name, "node1", map[string]string{"app": "prod-app"}, map[string]string{})
				addPods(t, controller, fx, pod)
			}
			deletePod := func(name, ip string) {
				if err := controller.client.Kube().CoreV1().Pods("nsA").Delete(context.TODO(), name, metaV1.DeleteOptions{}); err != nil {
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
				createService(controller, name, "nsA", nil,
					[]int32{8080}, map[string]string{"app": "prod-app"}, t)
				if ev := fx.Wait("service"); ev == nil {
					t.Fatal("Timeout creating service")
				}
			}
			addEndpoint := func(svcName string, ips []string, pods []string) {
				var refs []*coreV1.ObjectReference
				for _, pod := range pods {
					if pod == "" {
						refs = append(refs, nil)
					} else {
						refs = append(refs, &coreV1.ObjectReference{
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
				ev := fx.Wait("eds")
				if ev == nil {
					t.Fatalf("Timeout incremental eds")
				}
				var gotIps []string
				for _, e := range ev.Endpoints {
					gotIps = append(gotIps, e.Address)
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
			if err := controller.client.Kube().CoreV1().Endpoints("nsA").Delete(context.TODO(), "svc", metaV1.DeleteOptions{}); err != nil {
				t.Fatal(err)
			}
			if err := controller.client.Kube().DiscoveryV1().EndpointSlices("nsA").Delete(context.TODO(), "svc", metaV1.DeleteOptions{}); err != nil {
				t.Fatal(err)
			}
			assertPendingResync(0)
		})
	}
}

func TestWorkloadInstanceHandlerMultipleEndpoints(t *testing.T) {
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})

	// Create an initial pod with a service, and endpoint.
	pod1 := generatePod("172.0.1.1", "pod1", "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
	pod2 := generatePod("172.0.1.2", "pod2", "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
	pods := []*coreV1.Pod{pod1, pod2}
	nodes := []*coreV1.Node{
		generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", label.TopologySubzone.Name: "subzone1"}),
	}
	addNodes(t, controller, nodes...)
	addPods(t, controller, fx, pods...)
	createService(controller, "svc1", "nsA", nil,
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)
	if ev := fx.Wait("service"); ev == nil {
		t.Fatal("Timeout creating service")
	}
	pod1Ips := []string{"172.0.1.1"}
	portNames := []string{"tcp-port"}
	createEndpoints(t, controller, "svc1", "nsA", portNames, pod1Ips, nil, nil)
	if ev := fx.Wait("eds"); ev == nil {
		t.Fatal("Timeout incremental eds")
	}

	// Simulate adding a workload entry (fired through invocation of WorkloadInstanceHandler)
	controller.WorkloadInstanceHandler(&model.WorkloadInstance{
		Namespace: "nsA",
		Endpoint: &model.IstioEndpoint{
			Labels:         labels.Instance{"app": "prod-app"},
			ServiceAccount: "account",
			Address:        "2.2.2.2",
			EndpointPort:   8080,
		},
	}, model.EventAdd)

	expectedEndpointIPs := []string{"172.0.1.1", "2.2.2.2"}
	// Check if an EDS event is fired
	if ev := fx.Wait("eds"); ev == nil {
		t.Fatal("Did not get eds event when workload entry was added")
	} else {
		// check if the hostname matches that of k8s service svc1.nsA
		if ev.ID != "svc1.nsA.svc.company.com" {
			t.Fatalf("eds event for workload entry addition did not match the expected service. got %s, want %s",
				ev.ID, "svc1.nsA.svc.company.com")
		}
		// we should have the pod IP and the workload Entry's IP in the endpoints..
		// the first endpoint should be that of the k8s pod and the second one should be the workload entry

		var gotEndpointIPs []string
		for _, ep := range ev.Endpoints {
			gotEndpointIPs = append(gotEndpointIPs, ep.Address)
		}
		if !reflect.DeepEqual(gotEndpointIPs, expectedEndpointIPs) {
			t.Fatalf("eds update after adding workload entry did not match expected list. got %v, want %v",
				gotEndpointIPs, expectedEndpointIPs)
		}
	}

	// Check if InstancesByPort returns the same list
	converted := controller.Services()
	if len(converted) != 1 {
		t.Fatalf("failed to get services (%v), converted", converted)
	}
	instances := controller.InstancesByPort(converted[0], 8080, labels.Instance{
		"app": "prod-app",
	})
	var gotEndpointIPs []string
	for _, instance := range instances {
		gotEndpointIPs = append(gotEndpointIPs, instance.Endpoint.Address)
	}
	if !reflect.DeepEqual(gotEndpointIPs, expectedEndpointIPs) {
		t.Fatalf("InstancesByPort after adding workload entry did not match expected list. got %v, want %v",
			gotEndpointIPs, expectedEndpointIPs)
	}

	// Now add a k8s pod to the service and ensure that eds updates contain both pod IPs and workload entry IPs.
	updateEndpoints(controller, "svc1", "nsA", portNames, []string{"172.0.1.1", "172.0.1.2"}, t)
	if ev := fx.Wait("eds"); ev == nil {
		t.Fatal("Timeout incremental eds")
	} else {
		var gotEndpointIPs []string
		for _, ep := range ev.Endpoints {
			gotEndpointIPs = append(gotEndpointIPs, ep.Address)
		}
		expectedEndpointIPs = []string{"172.0.1.1", "172.0.1.2", "2.2.2.2"}
		if !reflect.DeepEqual(gotEndpointIPs, expectedEndpointIPs) {
			t.Fatalf("eds update after adding pod did not match expected list. got %v, want %v",
				gotEndpointIPs, expectedEndpointIPs)
		}
	}
}

func TestWorkloadInstanceHandler_WorkloadInstanceIndex(t *testing.T) {
	ctl, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{})

	verifyGetByIP := func(address string, want []*model.WorkloadInstance) {
		got := ctl.workloadInstancesIndex.GetByIP(address)

		if diff := cmp.Diff(want, got); diff != "" {
			t.Fatalf("workload index is not valid (--want/++got): %v", diff)
		}
	}

	wi1 := &model.WorkloadInstance{
		Name:      "ratings-1",
		Namespace: "bookinfo",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "ratings"},
			Address:      "2.2.2.2",
			EndpointPort: 8080,
		},
	}

	// simulate adding a workload entry
	ctl.WorkloadInstanceHandler(wi1, model.EventAdd)

	verifyGetByIP("2.2.2.2", []*model.WorkloadInstance{wi1})

	wi2 := &model.WorkloadInstance{
		Name:      "details-1",
		Namespace: "bookinfo",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "details"},
			Address:      "3.3.3.3",
			EndpointPort: 9090,
		},
	}

	// simulate adding a workload entry
	ctl.WorkloadInstanceHandler(wi2, model.EventAdd)

	verifyGetByIP("2.2.2.2", []*model.WorkloadInstance{wi1})
	verifyGetByIP("3.3.3.3", []*model.WorkloadInstance{wi2})

	wi3 := &model.WorkloadInstance{
		Name:      "details-1",
		Namespace: "bookinfo",
		Endpoint: &model.IstioEndpoint{
			Labels:       labels.Instance{"app": "details"},
			Address:      "2.2.2.2", // update IP
			EndpointPort: 9090,
		},
	}

	// simulate updating a workload entry
	ctl.WorkloadInstanceHandler(wi3, model.EventUpdate)

	verifyGetByIP("3.3.3.3", nil)
	verifyGetByIP("2.2.2.2", []*model.WorkloadInstance{wi3, wi1})

	// simulate deleting a workload entry
	ctl.WorkloadInstanceHandler(wi3, model.EventDelete)

	verifyGetByIP("2.2.2.2", []*model.WorkloadInstance{wi1})

	// simulate deleting a workload entry
	ctl.WorkloadInstanceHandler(wi1, model.EventDelete)

	verifyGetByIP("2.2.2.2", nil)
}

func TestKubeEndpointsControllerOnEvent(t *testing.T) {
	testCases := []struct {
		mode      EndpointMode
		tombstone cache.DeletedFinalStateUnknown
	}{
		{
			mode: EndpointsOnly,
			tombstone: cache.DeletedFinalStateUnknown{
				Key: "namespace/name",
				Obj: &coreV1.Endpoints{},
			},
		},
		{
			mode: EndpointSliceOnly,
			tombstone: cache.DeletedFinalStateUnknown{
				Key: "namespace/name",
				Obj: &discovery.EndpointSlice{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(EndpointModeNames[tc.mode], func(t *testing.T) {
			controller, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{Mode: tc.mode})

			if err := controller.endpoints.onEvent(tc.tombstone, model.EventDelete); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestUpdateEdsCacheOnServiceUpdate(t *testing.T) {
	controller, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{})

	// Create an initial pod with a service, and endpoint.
	pod1 := generatePod("172.0.1.1", "pod1", "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
	pod2 := generatePod("172.0.1.2", "pod2", "nsA", "", "node1", map[string]string{"app": "prod-app"}, map[string]string{})
	pods := []*coreV1.Pod{pod1, pod2}
	nodes := []*coreV1.Node{
		generateNode("node1", map[string]string{NodeZoneLabel: "zone1", NodeRegionLabel: "region1", label.TopologySubzone.Name: "subzone1"}),
	}
	addNodes(t, controller, nodes...)
	addPods(t, controller, fx, pods...)
	createService(controller, "svc1", "nsA", nil,
		[]int32{8080}, map[string]string{"app": "prod-app"}, t)
	if ev := fx.Wait("service"); ev == nil {
		t.Fatal("Timeout creating service")
	}

	pod1Ips := []string{"172.0.1.1"}
	portNames := []string{"tcp-port"}
	createEndpoints(t, controller, "svc1", "nsA", portNames, pod1Ips, nil, nil)
	if ev := fx.Wait("eds"); ev == nil {
		t.Fatal("Timeout incremental eds")
	}

	// update service selector
	svc := getService(controller, "svc1", "nsA", t)
	svc.Spec.Selector = map[string]string{
		"app": "prod-app",
		"foo": "bar",
	}
	svc.Spec.Selector = map[string]string{
		"app": "prod-app",
	}
	updateService(controller, svc, t)
	if ev := fx.Wait("eds cache"); ev == nil {
		t.Fatal("Timeout updating eds cache")
	}
}

func clearDiscoverabilityPolicy(ep *model.IstioEndpoint) {
	if ep != nil {
		ep.DiscoverabilityPolicy = nil
	}
}
