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

package xds_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"

	"istio.io/api/annotation"
	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/test/xds"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

func TestNetworkGatewayUpdates(t *testing.T) {
	test.SetForTest(t, &features.MultiNetworkGatewayAPI, true)
	pod := &workload{
		kind: Pod,
		name: "app", namespace: "pod",
		ip: "10.10.10.10", port: 8080,
		metaNetwork: "network-1",
		labels:      map[string]string{label.TopologyNetwork.Name: "network-1"},
	}
	vm := &workload{
		kind: VirtualMachine,
		name: "vm", namespace: "default",
		ip: "10.10.10.30", port: 9090,
		metaNetwork: "vm",
	}
	// VM always sees itself directly
	vm.Expect(vm, "10.10.10.30:9090")

	workloads := []*workload{pod, vm}

	var kubeObjects []runtime.Object
	var configObjects []config.Config
	for _, w := range workloads {
		_, objs := w.kubeObjects()
		kubeObjects = append(kubeObjects, objs...)
		configObjects = append(configObjects, w.configs()...)
	}
	meshNetworks := meshwatcher.NewFixedNetworksWatcher(nil)
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		KubernetesObjects: kubeObjects,
		Configs:           configObjects,
		NetworksWatcher:   meshNetworks,
	})
	for _, w := range workloads {
		w.setupProxy(s)
	}

	t.Run("no gateway", func(t *testing.T) {
		vm.Expect(pod, "10.10.10.10:8080")
		vm.Test(t, s)
	})
	t.Run("gateway added via label", func(t *testing.T) {
		_, err := s.KubeClient().Kube().CoreV1().Services("istio-system").Create(context.TODO(), &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istio-ingressgateway",
				Namespace: "istio-system",
				Labels: map[string]string{
					label.TopologyNetwork.Name: "network-1",
				},
			},
			Spec: corev1.ServiceSpec{
				Type:  corev1.ServiceTypeLoadBalancer,
				Ports: []corev1.ServicePort{{Port: 15443, Protocol: corev1.ProtocolTCP}},
			},
			Status: corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "3.3.3.3"}}},
			},
		}, metav1.CreateOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if err := retry.Until(func() bool {
			return len(s.PushContext().NetworkManager().GatewaysForNetwork("network-1")) == 1
		}); err != nil {
			t.Fatal("push context did not reinitialize with gateways; xds event may not have been triggered")
		}
		vm.Expect(pod, "3.3.3.3:15443")
		vm.Test(t, s)
	})

	t.Run("gateway added via meshconfig", func(t *testing.T) {
		_, err := s.KubeClient().Kube().CoreV1().Services("istio-system").Create(context.TODO(), &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "istio-meshnetworks-gateway",
				Namespace: "istio-system",
			},
			Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
			Status: corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "4.4.4.4"}}},
			},
		}, metav1.CreateOptions{})
		meshNetworks.SetNetworks(&meshconfig.MeshNetworks{Networks: map[string]*meshconfig.Network{
			"network-1": {
				Endpoints: []*meshconfig.Network_NetworkEndpoints{
					{
						Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{FromRegistry: "Kubernetes"},
					},
				},
				Gateways: []*meshconfig.Network_IstioNetworkGateway{{
					Gw: &meshconfig.Network_IstioNetworkGateway_RegistryServiceName{
						RegistryServiceName: "istio-meshnetworks-gateway.istio-system.svc.cluster.local",
					},
					Port: 15443,
				}},
			},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if err := retry.Until(func() bool {
			return len(s.PushContext().NetworkManager().GatewaysForNetwork("network-1")) == 2
		}); err != nil {
			t.Fatal("push context did not reinitialize with gateways; xds event may not have been triggered")
		}
		vm.Expect(pod, "3.3.3.3:15443", "4.4.4.4:15443")
		vm.Test(t, s)
	})
}

func TestMeshNetworking(t *testing.T) {
	test.SetForTest(t, &features.MultiNetworkGatewayAPI, true)
	ingressServiceScenarios := map[corev1.ServiceType]map[cluster.ID][]runtime.Object{
		corev1.ServiceTypeLoadBalancer: {
			// cluster/network 1's ingress can be found up by registry service name in meshNetworks (no label)
			"cluster-1": {gatewaySvc("istio-ingressgateway", "2.2.2.2", "")},
			// cluster/network 2's ingress can be found by it's network label
			"cluster-2": {gatewaySvc("istio-eastwestgateway", "3.3.3.3", "network-2")},
		},
		corev1.ServiceTypeClusterIP: {
			// cluster/network 1's ingress can be found up by registry service name in meshNetworks
			"cluster-1": {&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "istio-ingressgateway",
					Namespace: "istio-system",
				},
				Spec: corev1.ServiceSpec{
					Type:        corev1.ServiceTypeClusterIP,
					ExternalIPs: []string{"2.2.2.2"},
				},
			}},
			// cluster/network 2's ingress can be found by it's network label
			"cluster-2": {&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "istio-ingressgateway",
					Namespace: "istio-system",
					Labels: map[string]string{
						label.TopologyNetwork.Name: "network-2",
					},
				},
				Spec: corev1.ServiceSpec{
					Type:        corev1.ServiceTypeClusterIP,
					ExternalIPs: []string{"3.3.3.3"},
					Ports:       []corev1.ServicePort{{Port: 15443}},
				},
			}},
		},
		corev1.ServiceTypeNodePort: {
			"cluster-1": {
				&corev1.Node{Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{{Type: corev1.NodeExternalIP, Address: "2.2.2.2"}}}},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "istio-ingressgateway",
						Namespace:   "istio-system",
						Annotations: map[string]string{annotation.TrafficNodeSelector.Name: "{}"},
					},
					Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Ports: []corev1.ServicePort{{Port: 15443, NodePort: 25443}}},
				},
			},
			"cluster-2": {
				&corev1.Node{Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{{Type: corev1.NodeExternalIP, Address: "3.3.3.3"}}}},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "istio-ingressgateway",
						Namespace:   "istio-system",
						Annotations: map[string]string{annotation.TrafficNodeSelector.Name: "{}"},
						Labels: map[string]string{
							label.TopologyNetwork.Name: "network-2",
							// set the label here to test it = expectation doesn't change since we map back to that via NodePort
							label.NetworkingGatewayPort.Name: "443",
						},
					},
					Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Ports: []corev1.ServicePort{{Port: 443, NodePort: 25443}}},
				},
			},
		},
	}

	// network-2 does not need to be specified, gateways and endpoints are found by labels
	meshNetworkConfigs := map[string]*meshconfig.MeshNetworks{
		"gateway Address": {Networks: map[string]*meshconfig.Network{
			"network-1": {
				Endpoints: []*meshconfig.Network_NetworkEndpoints{{
					Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{FromRegistry: "cluster-1"},
				}},
				Gateways: []*meshconfig.Network_IstioNetworkGateway{{
					Gw: &meshconfig.Network_IstioNetworkGateway_Address{Address: "2.2.2.2"}, Port: 15443,
				}},
			},
		}},
		"gateway fromRegistry": {Networks: map[string]*meshconfig.Network{
			"network-1": {
				Endpoints: []*meshconfig.Network_NetworkEndpoints{{
					Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{FromRegistry: "cluster-1"},
				}},
				Gateways: []*meshconfig.Network_IstioNetworkGateway{{
					Gw: &meshconfig.Network_IstioNetworkGateway_RegistryServiceName{
						RegistryServiceName: "istio-ingressgateway.istio-system.svc.cluster.local",
					},
					Port: 15443,
				}},
			},
		}},
	}

	type trafficConfig struct {
		config.Config
		allowCrossNetwork bool
	}
	var trafficConfigs []trafficConfig
	for _, c := range []struct {
		name string
		mode v1beta1.PeerAuthentication_MutualTLS_Mode
	}{
		{"strict", v1beta1.PeerAuthentication_MutualTLS_STRICT},
		{"permissive", v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE},
		{"disable", v1beta1.PeerAuthentication_MutualTLS_DISABLE},
	} {
		name, mode := c.name, c.mode
		trafficConfigs = append(trafficConfigs, trafficConfig{
			Config: config.Config{
				Meta: config.Meta{
					GroupVersionKind: gvk.PeerAuthentication,
					Namespace:        "istio-system",
					Name:             "peer-authn-mtls-" + name,
				},
				Spec: &v1beta1.PeerAuthentication{
					Mtls: &v1beta1.PeerAuthentication_MutualTLS{Mode: mode},
				},
			},
			allowCrossNetwork: mode != v1beta1.PeerAuthentication_MutualTLS_DISABLE,
		})
	}

	for ingrType, ingressObjects := range ingressServiceScenarios {
		t.Run(string(ingrType), func(t *testing.T) {
			for name, networkConfig := range meshNetworkConfigs {
				t.Run(name, func(t *testing.T) {
					for _, cfg := range trafficConfigs {
						t.Run(cfg.Meta.Name, func(t *testing.T) {
							pod := &workload{
								kind: Pod,
								name: "unlabeled", namespace: "pod",
								ip: "10.10.10.10", port: 8080,
								metaNetwork: "network-1", clusterID: "cluster-1",
							}
							labeledPod := &workload{
								kind: Pod,
								name: "labeled", namespace: "pod",
								ip: "10.10.10.20", port: 9090,
								metaNetwork: "network-2", clusterID: "cluster-2",
								labels: map[string]string{label.TopologyNetwork.Name: "network-2"},
							}
							vm := &workload{
								kind: VirtualMachine,
								name: "vm", namespace: "default",
								ip: "10.10.10.30", port: 9090,
								metaNetwork: "vm",
							}

							// a workload entry with no endpoints in the local network should be ignored
							// in a remote network it should use gateway IP
							emptyAddress := &workload{
								kind: VirtualMachine,
								name: "empty-Address-net-2", namespace: "default",
								ip: "", port: 8080,
								metaNetwork: "network-2",
								labels:      map[string]string{label.TopologyNetwork.Name: "network-2"},
							}

							// gw does not have endpoints, it's just some proxy used to test REQUESTED_NETWORK_VIEW
							gw := &workload{
								kind: Other,
								name: "gw", ip: "2.2.2.2",
								networkView: []string{"vm"},
							}

							net1gw, net1GwPort := "2.2.2.2", "15443"
							net2gw, net2GwPort := "3.3.3.3", "15443"
							if ingrType == corev1.ServiceTypeNodePort {
								if name == "gateway fromRegistry" {
									net1GwPort = "25443"
								}
								// network 2 gateway uses the labels approach - always does nodeport mapping
								net2GwPort = "25443"
							}
							net1gw += ":" + net1GwPort
							net2gw += ":" + net2GwPort

							// local ip for self
							pod.Expect(pod, "10.10.10.10:8080")
							labeledPod.Expect(labeledPod, "10.10.10.20:9090")

							// vm has no gateway, use the original IP
							pod.Expect(vm, "10.10.10.30:9090")
							labeledPod.Expect(vm, "10.10.10.30:9090")

							vm.Expect(vm, "10.10.10.30:9090")

							if cfg.allowCrossNetwork {
								// pod in network-1 uses gateway to reach pod labeled with network-2
								pod.Expect(labeledPod, net2gw)
								pod.Expect(emptyAddress, net2gw)

								// pod labeled as network-2 should use gateway for network-1
								labeledPod.Expect(pod, net1gw)
								// vm uses gateway to get to pods
								vm.Expect(pod, net1gw)
								vm.Expect(labeledPod, net2gw)
								vm.Expect(emptyAddress, net2gw)
							}

							runMeshNetworkingTest(t, meshNetworkingTest{
								workloads:         []*workload{pod, labeledPod, vm, gw, emptyAddress},
								meshNetworkConfig: networkConfig,
								kubeObjects:       ingressObjects,
							}, cfg.Config)
						})
					}
				})
			}
		})
	}
}

func TestEmptyAddressWorkloadEntry(t *testing.T) {
	test.SetForTest(t, &features.MultiNetworkGatewayAPI, true)
	type entry struct{ address, sa, network, version string }
	const name, port = "remote-we-svc", 80
	serviceCases := []struct {
		name, k8s, cfg string
		expectKind     workloadKind
	}{
		{
			expectKind: Pod,
			name:       "Service",
			k8s: `
---
apiVersion: v1
kind: Service
metadata:
  name: remote-we-svc
  namespace: test
spec:
  ports:
  - port: 80
    protocol: TCP
  selector:
    app: remote-we-svc
`,
		},
		{
			name: "ServiceEntry",
			cfg: `
---
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: remote-we-svc
  namespace: test
spec:
  hosts:
  - remote-we-svc
  ports:
    - number: 80
      name: http
      protocol: HTTP
  resolution: STATIC
  location: MESH_INTERNAL
  workloadSelector:
    labels:
      app: remote-we-svc
`,
		},
	}
	workloadCases := []struct {
		name         string
		entries      []entry
		expectations map[string][]xdstest.LocLbEpInfo
	}{
		{
			name: "single subset",
			entries: []entry{
				// the only local endpoint giving a weight of 1
				{sa: "foo", network: "network-1", address: "1.2.3.4", version: "v1"},
				// same network, no address is ignored and doesn't affect weight
				{sa: "foo", network: "network-1", address: "", version: "vj"},
				// these will me merged giving the remote gateway a weight of 2
				{sa: "foo", network: "network-2", address: "", version: "v1"},
				{sa: "foo", network: "network-2", address: "", version: "v1"},
				// this should not be included in the weight since it doesn't have an address OR a gateway
				{sa: "foo", network: "no-gateway-address", address: "", version: "v1"},
			},
			expectations: map[string][]xdstest.LocLbEpInfo{
				"": {xdstest.LocLbEpInfo{
					LbEps: []xdstest.LbEpInfo{
						{Address: "1.2.3.4", Weight: 1},
						{Address: "2.2.2.2", Weight: 2},
					},
					Weight: 3,
				}},
				"v1": {xdstest.LocLbEpInfo{
					LbEps: []xdstest.LbEpInfo{
						{Address: "1.2.3.4", Weight: 1},
						{Address: "2.2.2.2", Weight: 2},
					},
					Weight: 3,
				}},
			},
		},
		{
			name: "multiple subsets",
			entries: []entry{
				{sa: "foo", network: "network-1", address: "1.2.3.4", version: "v1"},
				{sa: "foo", network: "network-1", address: "", version: "v2"}, // ignored (does not contribute to weight)
				{sa: "foo", network: "network-2", address: "", version: "v1"},
				{sa: "foo", network: "network-2", address: "", version: "v2"},
			},
			expectations: map[string][]xdstest.LocLbEpInfo{
				"": {xdstest.LocLbEpInfo{
					LbEps: []xdstest.LbEpInfo{
						{Address: "1.2.3.4", Weight: 1},
						{Address: "2.2.2.2", Weight: 2},
					},
					Weight: 3,
				}},
				"v1": {xdstest.LocLbEpInfo{
					LbEps: []xdstest.LbEpInfo{
						{Address: "1.2.3.4", Weight: 1},
						{Address: "2.2.2.2", Weight: 1},
					},
					Weight: 2,
				}},
				"v2": {xdstest.LocLbEpInfo{
					LbEps: []xdstest.LbEpInfo{
						{Address: "2.2.2.2", Weight: 1},
					},
					Weight: 1,
				}},
			},
		},
	}

	for _, sc := range serviceCases {
		t.Run(sc.name, func(t *testing.T) {
			for _, tc := range workloadCases {
				t.Run(tc.name, func(t *testing.T) {
					client := &workload{
						kind:        Pod,
						name:        "client-pod",
						namespace:   "test",
						ip:          "10.0.0.1",
						port:        80,
						clusterID:   "cluster-1",
						metaNetwork: "network-1",
					}
					// expect self
					client.ExpectWithWeight(client, "", xdstest.LocLbEpInfo{
						Weight: 1,
						LbEps: []xdstest.LbEpInfo{
							{Address: "10.0.0.1", Weight: 1},
						},
					})
					for subset, eps := range tc.expectations {
						client.ExpectWithWeight(&workload{kind: sc.expectKind, name: name, namespace: "test", port: port}, subset, eps...)
					}
					configObjects := `
---
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: subset-se
  namespace: test
spec:
  host: "*"
  subsets:
  - name: v1
    labels:
      version: v1
  - name: v2
    labels:
      version: v2
  - name: v3
    labels:
      version: v3
`
					configObjects += sc.cfg
					for i, entry := range tc.entries {
						configObjects += fmt.Sprintf(`
---
apiVersion: networking.istio.io/v1
kind: WorkloadEntry
metadata:
  name: we-%d
  namespace: test
spec:
  address: %q
  serviceAccount: %q
  network: %q
  labels:
    app: remote-we-svc
    version: %q
`, i, entry.address, entry.sa, entry.network, entry.version)
					}

					runMeshNetworkingTest(t, meshNetworkingTest{
						workloads:       []*workload{client},
						configYAML:      configObjects,
						kubeObjectsYAML: map[cluster.ID]string{constants.DefaultClusterName: sc.k8s},
						kubeObjects: map[cluster.ID][]runtime.Object{constants.DefaultClusterName: {
							gatewaySvc("gateway-1", "1.1.1.1", "network-1"),
							gatewaySvc("gateway-2", "2.2.2.2", "network-2"),
						}},
					})
				})
			}
		})
	}
}

func gatewaySvc(name, ip, network string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "istio-system",
			Labels:    map[string]string{label.TopologyNetwork.Name: network},
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Port: 15443}},
		},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: ip}}},
		},
	}
}

type meshNetworkingTest struct {
	workloads         []*workload
	meshNetworkConfig *meshconfig.MeshNetworks
	kubeObjects       map[cluster.ID][]runtime.Object
	kubeObjectsYAML   map[cluster.ID]string
	configYAML        string
}

func runMeshNetworkingTest(t *testing.T, tt meshNetworkingTest, configs ...config.Config) {
	kubeObjects := map[cluster.ID][]runtime.Object{}
	for k, v := range tt.kubeObjects {
		kubeObjects[k] = v
	}
	configObjects := configs
	for _, w := range tt.workloads {
		k8sCluster, objs := w.kubeObjects()
		if k8sCluster != "" {
			kubeObjects[k8sCluster] = append(kubeObjects[k8sCluster], objs...)
		}
		configObjects = append(configObjects, w.configs()...)
	}
	s := xds.NewFakeDiscoveryServer(t, xds.FakeOptions{
		KubernetesObjectsByCluster:      kubeObjects,
		KubernetesObjectStringByCluster: tt.kubeObjectsYAML,
		ConfigString:                    tt.configYAML,
		Configs:                         configObjects,
		NetworksWatcher:                 meshwatcher.NewFixedNetworksWatcher(tt.meshNetworkConfig),
	})
	for _, w := range tt.workloads {
		w.setupProxy(s)
	}
	for _, w := range tt.workloads {
		w.Test(t, s)
	}
}

type workloadKind int

const (
	Other workloadKind = iota
	Pod
	VirtualMachine
)

type workload struct {
	kind workloadKind

	name      string
	namespace string

	ip   string
	port int32

	clusterID   cluster.ID
	metaNetwork network.ID
	networkView []string

	labels map[string]string

	proxy *model.Proxy

	expectations         map[string][]string
	weightedExpectations map[string][]xdstest.LocLbEpInfo
}

func (w *workload) Expect(target *workload, ips ...string) {
	if w.expectations == nil {
		w.expectations = map[string][]string{}
	}
	w.expectations[target.clusterName("")] = ips
}

func (w *workload) ExpectWithWeight(target *workload, subset string, eps ...xdstest.LocLbEpInfo) {
	if w.weightedExpectations == nil {
		w.weightedExpectations = make(map[string][]xdstest.LocLbEpInfo)
	}
	w.weightedExpectations[target.clusterName(subset)] = eps
}

func (w *workload) Test(t *testing.T, s *xds.FakeDiscoveryServer) {
	if w.expectations == nil && w.weightedExpectations == nil {
		return
	}
	t.Run(fmt.Sprintf("from %s", w.proxy.ID), func(t *testing.T) {
		w.testUnweighted(t, s)
		w.testWeighted(t, s)
	})
}

func (w *workload) testUnweighted(t *testing.T, s *xds.FakeDiscoveryServer) {
	if w.expectations == nil {
		return
	}
	t.Run("unweighted", func(t *testing.T) {
		// wait for eds cache update
		retry.UntilSuccessOrFail(t, func() error {
			eps := xdstest.ExtractLoadAssignments(s.Endpoints(w.proxy))

			for c, want := range w.expectations {
				got := eps[c]
				if !slices.EqualUnordered(got, want) {
					err := fmt.Errorf("cluster %s, expected %v, but got %v", c, want, got)
					fmt.Println(err)
					return err
				}
			}
			for c, got := range eps {
				want := w.expectations[c]
				if !slices.EqualUnordered(got, want) {
					err := fmt.Errorf("cluster %s, expected %v, but got %v", c, want, got)
					fmt.Println(err)
					return err
				}
			}
			return nil
		}, retry.Timeout(3*time.Second))
	})
}

func (w *workload) testWeighted(t *testing.T, s *xds.FakeDiscoveryServer) {
	if w.weightedExpectations == nil {
		return
	}
	t.Run("weighted", func(t *testing.T) {
		// wait for eds cache update
		retry.UntilSuccessOrFail(t, func() error {
			eps := xdstest.ExtractLocalityLbEndpoints(s.Endpoints(w.proxy))
			for c, want := range w.weightedExpectations {
				got := eps[c]
				if err := xdstest.CompareEndpoints(c, got, want); err != nil {
					return err
				}
			}
			for c, got := range eps {
				want := w.weightedExpectations[c]
				if err := xdstest.CompareEndpoints(c, got, want); err != nil {
					return err
				}
			}
			return nil
		}, retry.Timeout(3*time.Second))
	})
}

func (w *workload) clusterName(subset string) string {
	name := w.name
	if w.kind == Pod {
		name = fmt.Sprintf("%s.%s.svc.cluster.local", w.name, w.namespace)
	}
	return fmt.Sprintf("outbound|%d|%s|%s", w.port, subset, name)
}

func (w *workload) kubeObjects() (cluster.ID, []runtime.Object) {
	if w.kind == Pod {
		return w.clusterID, w.buildPodService()
	}
	return "", nil
}

func (w *workload) configs() []config.Config {
	if w.kind == VirtualMachine {
		return []config.Config{{
			Meta: config.Meta{
				GroupVersionKind:  gvk.ServiceEntry,
				Name:              w.name,
				Namespace:         w.namespace,
				CreationTimestamp: time.Now(),
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{w.name},
				Ports: []*networking.ServicePort{
					{Number: uint32(w.port), Name: "http", Protocol: "HTTP"},
				},
				Endpoints: []*networking.WorkloadEntry{{
					Address:        w.ip,
					Labels:         w.labels,
					Network:        string(w.metaNetwork),
					ServiceAccount: w.name,
				}},
				Resolution: networking.ServiceEntry_STATIC,
				Location:   networking.ServiceEntry_MESH_INTERNAL,
			},
		}}
	}
	return nil
}

func (w *workload) setupProxy(s *xds.FakeDiscoveryServer) {
	p := &model.Proxy{
		ID:     strings.Join([]string{w.name, w.namespace}, "."),
		Labels: w.labels,
		Metadata: &model.NodeMetadata{
			Namespace:            w.namespace,
			Network:              w.metaNetwork,
			Labels:               w.labels,
			RequestedNetworkView: w.networkView,
		},
		ConfigNamespace: w.namespace,
	}
	if w.kind == Pod {
		p.Metadata.ClusterID = w.clusterID
	} else {
		p.Metadata.InterceptionMode = "NONE"
	}
	w.proxy = s.SetupProxy(p)
}

func (w *workload) buildPodService() []runtime.Object {
	baseMeta := metav1.ObjectMeta{
		Name: w.name,
		Labels: labels.Instance{
			"app":                        w.name,
			label.SecurityTlsMode.Name:   model.IstioMutualTLSModeLabel,
			discoveryv1.LabelServiceName: w.name,
		},
		Namespace: w.namespace,
	}
	podMeta := baseMeta
	podMeta.Name = w.name + "-" + rand.String(4)
	for k, v := range w.labels {
		podMeta.Labels[k] = v
	}

	return []runtime.Object{
		&corev1.Pod{
			ObjectMeta: podMeta,
		},
		&corev1.Service{
			ObjectMeta: baseMeta,
			Spec: corev1.ServiceSpec{
				ClusterIP: "1.2.3.4", // just can't be 0.0.0.0/ClusterIPNone, not used in eds
				Selector:  baseMeta.Labels,
				Ports: []corev1.ServicePort{{
					Port:     w.port,
					Name:     "http",
					Protocol: corev1.ProtocolTCP,
				}},
			},
		},
		&discoveryv1.EndpointSlice{
			ObjectMeta: baseMeta,
			Endpoints: []discoveryv1.Endpoint{{
				Addresses:  []string{w.ip},
				Conditions: discoveryv1.EndpointConditions{},
				Hostname:   nil,
				TargetRef: &corev1.ObjectReference{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       podMeta.Name,
					Namespace:  podMeta.Namespace,
				},
				DeprecatedTopology: nil,
				NodeName:           nil,
				Zone:               nil,
				Hints:              nil,
			}},
			Ports: []discoveryv1.EndpointPort{{
				Name:     ptr.Of("http"),
				Port:     ptr.Of(w.port),
				Protocol: ptr.Of(corev1.ProtocolTCP),
			}},
		},
	}
}
