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
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
)

func TestNetworkUpdateTriggers(t *testing.T) {
	test.SetForTest(t, &features.MultiNetworkGatewayAPI, true)
	meshNetworks := mesh.NewFixedNetworksWatcher(nil)
	c, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{
		ClusterID:       "Kubernetes",
		NetworksWatcher: meshNetworks,
		DomainSuffix:    "cluster.local",
		CRDs:            []schema.GroupVersionResource{gvr.KubernetesGateway},
	})

	if len(c.NetworkGateways()) != 0 {
		t.Fatal("did not expect any gateways yet")
	}

	notifyCh := make(chan struct{}, 1)
	var (
		gwMu sync.Mutex
		gws  []model.NetworkGateway
	)
	setGws := func(v []model.NetworkGateway) {
		gwMu.Lock()
		defer gwMu.Unlock()
		gws = v
	}
	getGws := func() []model.NetworkGateway {
		gwMu.Lock()
		defer gwMu.Unlock()
		return gws
	}

	c.AppendNetworkGatewayHandler(func() {
		setGws(c.NetworkGateways())
		notifyCh <- struct{}{}
	})
	expectGateways := func(t *testing.T, expectedGws int) {
		// wait for a notification
		assert.ChannelHasItem(t, notifyCh)
		if n := len(getGws()); n != expectedGws {
			t.Errorf("expected %d gateways but got %d", expectedGws, n)
		}
	}

	t.Run("add meshnetworks", func(t *testing.T) {
		addMeshNetworksFromRegistryGateway(t, c, meshNetworks)
		expectGateways(t, 2)
	})
	t.Run("add labeled service", func(t *testing.T) {
		addLabeledServiceGateway(t, c, "nw0")
		expectGateways(t, 3)
	})
	t.Run("update labeled service network", func(t *testing.T) {
		addLabeledServiceGateway(t, c, "nw1")
		expectGateways(t, 3)
	})
	t.Run("add kubernetes gateway", func(t *testing.T) {
		addOrUpdateGatewayResource(t, c, 35443)
		expectGateways(t, 7)
	})
	t.Run("update kubernetes gateway", func(t *testing.T) {
		addOrUpdateGatewayResource(t, c, 45443)
		expectGateways(t, 7)
	})
	t.Run("remove kubernetes gateway", func(t *testing.T) {
		removeGatewayResource(t, c)
		expectGateways(t, 3)
	})
	t.Run("remove labeled service", func(t *testing.T) {
		removeLabeledServiceGateway(t, c)
		expectGateways(t, 2)
	})
	// gateways are created even with out service
	t.Run("add kubernetes gateway", func(t *testing.T) {
		addOrUpdateGatewayResource(t, c, 35443)
		expectGateways(t, 6)
	})
	t.Run("remove kubernetes gateway", func(t *testing.T) {
		removeGatewayResource(t, c)
		expectGateways(t, 2)
	})
	t.Run("remove meshnetworks", func(t *testing.T) {
		meshNetworks.SetNetworks(nil)
		expectGateways(t, 0)
	})
}

func addLabeledServiceGateway(t *testing.T, c *FakeController, nw string) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "istio-labeled-gw", Namespace: "arbitrary-ns", Labels: map[string]string{
			label.TopologyNetwork.Name: nw,
		}},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Port: 15443, Protocol: corev1.ProtocolTCP}},
		},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{
			IP:    "2.3.4.6",
			Ports: []corev1.PortStatus{{Port: 15443, Protocol: corev1.ProtocolTCP}},
		}}}},
	}
	clienttest.Wrap(t, c.services).CreateOrUpdate(svc)
}

func removeLabeledServiceGateway(t *testing.T, c *FakeController) {
	clienttest.Wrap(t, c.services).Delete("istio-labeled-gw", "arbitrary-ns")
}

// creates a gateway that exposes 2 ports that are valid auto-passthrough ports
// and it does so on an IP and a hostname
func addOrUpdateGatewayResource(t *testing.T, c *FakeController, customPort int) {
	passthroughMode := k8sv1.TLSModePassthrough
	ipType := v1beta1.IPAddressType
	hostnameType := v1beta1.HostnameAddressType
	clienttest.Wrap(t, kclient.New[*v1beta1.Gateway](c.client)).CreateOrUpdate(&v1beta1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "eastwest-gwapi",
			Namespace: "istio-system",
			Labels:    map[string]string{label.TopologyNetwork.Name: "nw2"},
		},
		Spec: v1beta1.GatewaySpec{
			GatewayClassName: "istio",
			Addresses: []v1beta1.GatewayAddress{
				{Type: &ipType, Value: "1.2.3.4"},
				{Type: &hostnameType, Value: "some hostname"},
			},
			Listeners: []v1beta1.Listener{
				{
					Name: "detected-by-options",
					TLS: &v1beta1.GatewayTLSConfig{
						Mode: &passthroughMode,
						Options: map[v1beta1.AnnotationKey]v1beta1.AnnotationValue{
							constants.ListenerModeOption: constants.ListenerModeAutoPassthrough,
						},
					},
					Port: v1beta1.PortNumber(customPort),
				},
				{
					Name: "detected-by-number",
					TLS:  &v1beta1.GatewayTLSConfig{Mode: &passthroughMode},
					Port: 15443,
				},
			},
		},
		Status: v1beta1.GatewayStatus{},
	})
}

func removeGatewayResource(t *testing.T, c *FakeController) {
	clienttest.Wrap(t, kclient.New[*v1beta1.Gateway](c.client)).Delete("eastwest-gwapi", "istio-system")
}

func addMeshNetworksFromRegistryGateway(t *testing.T, c *FakeController, watcher mesh.NetworksWatcher) {
	clienttest.Wrap(t, c.services).Create(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "istio-meshnetworks-gw", Namespace: "istio-system"},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Port: 15443, Protocol: corev1.ProtocolTCP}},
		},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{
			IP:    "1.2.3.4",
			Ports: []corev1.PortStatus{{Port: 15443, Protocol: corev1.ProtocolTCP}},
		}}}},
	})
	watcher.SetNetworks(&meshconfig.MeshNetworks{Networks: map[string]*meshconfig.Network{
		"nw0": {
			Endpoints: []*meshconfig.Network_NetworkEndpoints{{
				Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{FromRegistry: "Kubernetes"},
			}},
			Gateways: []*meshconfig.Network_IstioNetworkGateway{{
				Port: 15443,
				Gw:   &meshconfig.Network_IstioNetworkGateway_RegistryServiceName{RegistryServiceName: "istio-meshnetworks-gw.istio-system.svc.cluster.local"},
			}},
		},
		"nw1": {
			Endpoints: []*meshconfig.Network_NetworkEndpoints{{
				Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{FromRegistry: "Kubernetes"},
			}},
			Gateways: []*meshconfig.Network_IstioNetworkGateway{{
				Port: 15443,
				Gw:   &meshconfig.Network_IstioNetworkGateway_RegistryServiceName{RegistryServiceName: "istio-meshnetworks-gw.istio-system.svc.cluster.local"},
			}},
		},
	}})
}

func TestAmbientSystemNamespaceNetworkChange(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbientControllers, true)

	s := newAmbientTestServer(t, testC, "")

	tracker := assert.NewTracker[string](t)

	s.controller.namespaces.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		tracker.Record(o.GetName())
	}))

	expectNetwork := func(t *testing.T, c *FakeController, network string) {
		retry.UntilSuccessOrFail(t, func() error {
			t.Helper()
			if c.networkFromSystemNamespace().String() != network {
				return fmt.Errorf("no network notify")
			}
			podNames := sets.New[string]("pod1", "pod2")
			svcNames := sets.New[string]("svc1")
			addresses := c.ambientIndex.All()
			for _, addr := range addresses {
				wl := addr.GetWorkload()
				if wl != nil {
					if !podNames.Contains(wl.Name) {
						continue
					}
					if addr.GetWorkload().Network != network {
						return fmt.Errorf("no network notify")
					}
				}
				svc := addr.GetService()
				if svc != nil {
					if !svcNames.Contains(svc.Name) {
						continue
					}
					for _, saddr := range svc.GetAddresses() {
						if saddr.GetNetwork() != network {
							return fmt.Errorf("no network notify")
						}
					}
				}
			}
			return nil
		})
	}

	s.addPods(t, "127.0.0.1", "pod1", "sa1", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertAddresses(t, s.addrXdsName("127.0.0.1"), "pod1")

	s.addPods(t, "127.0.0.2", "pod2", "sa2", map[string]string{"app": "a"}, nil, true, corev1.PodRunning)
	s.assertAddresses(t, s.addrXdsName("127.0.0.2"), "pod2")

	s.addService(t, "svc1", map[string]string{}, // labels
		map[string]string{}, // annotations
		[]int32{80},
		map[string]string{"app": "a"}, // selector
		"10.0.0.1",
	)
	s.assertAddresses(t, "", "pod1", "pod2", "svc1")

	createOrUpdateNamespace(t, s.controller, testNS, "")
	createOrUpdateNamespace(t, s.controller, systemNS, "")

	tracker.WaitOrdered(testNS, systemNS)

	t.Run("change namespace network to nw1", func(t *testing.T) {
		createOrUpdateNamespace(t, s.controller, systemNS, "nw1")
		tracker.WaitOrdered(systemNS)
		expectNetwork(t, s.controller, "nw1")
	})

	t.Run("change namespace network to nw2", func(t *testing.T) {
		createOrUpdateNamespace(t, s.controller, systemNS, "nw2")
		tracker.WaitOrdered(systemNS)
		expectNetwork(t, s.controller, "nw2")
	})

	t.Run("manually change namespace network to nw3, and update meshNetworks", func(t *testing.T) {
		s.controller.setNetworkFromNamespace(&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: systemNS,
				Labels: map[string]string{
					label.TopologyNetwork.Name: "nw3",
				},
			},
		})
		createOrUpdateNamespace(t, s.controller, systemNS, "nw3")
		tracker.WaitOrdered(systemNS)
		addMeshNetworksFromRegistryGateway(t, s.controller, s.controller.meshNetworksWatcher)
		expectNetwork(t, s.controller, "nw3")
	})
}

func createOrUpdateNamespace(t *testing.T, c *FakeController, name, network string) {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				label.TopologyNetwork.Name: network,
			},
		},
	}
	clienttest.Wrap(t, c.namespaces).CreateOrUpdate(namespace)
}
