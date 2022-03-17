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

package xds

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/rand"

	"istio.io/api/annotation"
	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/api/security/v1beta1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/test/util/retry"
)

func TestNetworkGatewayUpdates(t *testing.T) {
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
	workloads := []*workload{pod, vm}

	kubeObjects := []runtime.Object{}
	var configObjects []config.Config
	for _, w := range workloads {
		_, objs := w.kubeObjects()
		kubeObjects = append(kubeObjects, objs...)
		configObjects = append(configObjects, w.configs()...)
	}
	meshNetworks := mesh.NewFixedNetworksWatcher(nil)
	s := NewFakeDiscoveryServer(t, FakeOptions{
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
		_, err := s.KubeClient().CoreV1().Services("istio-system").Create(context.TODO(), &corev1.Service{
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
		_, err := s.KubeClient().CoreV1().Services("istio-system").Create(context.TODO(), &corev1.Service{
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
	ingressServiceScenarios := map[corev1.ServiceType]map[cluster.ID][]runtime.Object{
		corev1.ServiceTypeLoadBalancer: {
			// cluster/network 1's ingress can be found up by registry service name in meshNetworks
			"cluster-1": {&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "istio-ingressgateway",
					Namespace: "istio-system",
				},
				Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "2.2.2.2"}}},
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
				Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
				Status: corev1.ServiceStatus{
					LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "3.3.3.3"}}},
				},
			}},
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
						Annotations: map[string]string{kube.NodeSelectorAnnotation: "{}"},
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
						Annotations: map[string]string{kube.NodeSelectorAnnotation: "{}"},
						Labels: map[string]string{
							label.TopologyNetwork.Name: "network-2",
							// set the label here to test it = expectation doesn't change since we map back to that via NodePort
							annotation.NetworkingGatewayPort.Name: "443",
						},
					},
					Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Ports: []corev1.ServicePort{{Port: 443, NodePort: 25443}}},
				},
			},
		},
	}

	// network-2 does not need to be specified, gateways and endpoints are found by labels
	meshNetworkConfigs := map[string]*meshconfig.MeshNetworks{
		"gateway address": {Networks: map[string]*meshconfig.Network{
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
	for name, mode := range map[string]v1beta1.PeerAuthentication_MutualTLS_Mode{
		"strict":     v1beta1.PeerAuthentication_MutualTLS_STRICT,
		"permissive": v1beta1.PeerAuthentication_MutualTLS_PERMISSIVE,
		"disable":    v1beta1.PeerAuthentication_MutualTLS_DISABLE,
	} {
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
		ingrType, ingressObjects := ingrType, ingressObjects
		t.Run(string(ingrType), func(t *testing.T) {
			for name, networkConfig := range meshNetworkConfigs {
				name, networkConfig := name, networkConfig
				t.Run(name, func(t *testing.T) {
					for _, cfg := range trafficConfigs {
						cfg := cfg
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
								// pod labeled as network-2 should use gateway for network-1
								labeledPod.Expect(pod, net1gw)
								// vm uses gateway to get to pods
								vm.Expect(pod, net1gw)
								vm.Expect(labeledPod, net2gw)
							}

							runMeshNetworkingTest(t, meshNetworkingTest{
								workloads:         []*workload{pod, labeledPod, vm, gw},
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

type meshNetworkingTest struct {
	workloads         []*workload
	meshNetworkConfig *meshconfig.MeshNetworks
	kubeObjects       map[cluster.ID][]runtime.Object
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
	s := NewFakeDiscoveryServer(t, FakeOptions{
		KubernetesObjectsByCluster: kubeObjects,
		Configs:                    configObjects,
		NetworksWatcher:            mesh.NewFixedNetworksWatcher(tt.meshNetworkConfig),
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

	expectations map[string][]string
}

func (w *workload) Expect(target *workload, ips ...string) {
	if w.expectations == nil {
		w.expectations = map[string][]string{}
	}
	w.expectations[target.clusterName()] = ips
}

func (w *workload) Test(t *testing.T, s *FakeDiscoveryServer) {
	if w.expectations == nil {
		return
	}

	t.Run(fmt.Sprintf("from %s", w.proxy.ID), func(t *testing.T) {
		// wait for eds cache update
		retry.UntilSuccessOrFail(t, func() error {
			eps := xdstest.ExtractLoadAssignments(s.Endpoints(w.proxy))
			for c, ips := range w.expectations {
				if !listEqualUnordered(eps[c], ips) {
					err := fmt.Errorf("cluster %s, expected ips %v ,but got %v", c, ips, eps[c])
					fmt.Println(err)
					return err
				}
			}
			return nil
		})
	})
}

func (w *workload) clusterName() string {
	name := w.name
	if w.kind == Pod {
		name = fmt.Sprintf("%s.%s.svc.cluster.local", w.name, w.namespace)
	}
	return fmt.Sprintf("outbound|%d||%s", w.port, name)
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
				GroupVersionKind:  collections.IstioNetworkingV1Alpha3Serviceentries.Resource().GroupVersionKind(),
				Name:              w.name,
				Namespace:         w.namespace,
				CreationTimestamp: time.Now(),
			},
			Spec: &networking.ServiceEntry{
				Hosts: []string{w.name},
				Ports: []*networking.Port{
					{Number: uint32(w.port), Name: "http", Protocol: "HTTP"},
				},
				Endpoints: []*networking.WorkloadEntry{{
					Address: w.ip,
					Labels:  w.labels,
				}},
				Resolution: networking.ServiceEntry_STATIC,
				Location:   networking.ServiceEntry_MESH_INTERNAL,
			},
		}}
	}
	return nil
}

func (w *workload) setupProxy(s *FakeDiscoveryServer) {
	p := &model.Proxy{
		ID: strings.Join([]string{w.name, w.namespace}, "."),
		Metadata: &model.NodeMetadata{
			Network:              w.metaNetwork,
			Labels:               w.labels,
			RequestedNetworkView: w.networkView,
		},
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
			"app":                      w.name,
			label.SecurityTlsMode.Name: model.IstioMutualTLSModeLabel,
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
		&corev1.Endpoints{
			ObjectMeta: baseMeta,
			Subsets: []corev1.EndpointSubset{{
				Addresses: []corev1.EndpointAddress{{
					IP: w.ip,
					TargetRef: &corev1.ObjectReference{
						APIVersion: "v1",
						Kind:       "Pod",
						Name:       podMeta.Name,
						Namespace:  podMeta.Namespace,
					},
				}},
				Ports: []corev1.EndpointPort{{
					Name:     "http",
					Port:     w.port,
					Protocol: corev1.ProtocolTCP,
				}},
			}},
		},
	}
}
