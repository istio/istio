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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/annotation"
	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
)

func TestNetworkUpdateTriggers(t *testing.T) {
	test.SetForTest(t, &features.MultiNetworkGatewayAPI, true)
	meshNetworks := meshwatcher.NewFixedNetworksWatcher(nil)
	c, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{
		ClusterID:       constants.DefaultClusterName,
		NetworksWatcher: meshNetworks,
		DomainSuffix:    "cluster.local",
		CRDs:            []schema.GroupVersionResource{gvr.KubernetesGateway},
	})

	if len(c.NetworkGateways()) != 0 {
		t.Fatal("did not expect any gateways yet")
	}

	notifyCh := make(chan struct{}, 10)
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
		// We may get up to 3 since we are creating 2 services, though sometimes it is collapsed into a single event depending no timing
		for range 3 {
			assert.ChannelHasItem(t, notifyCh)
			if n := len(getGws()); n == expectedGws {
				return
			}
		}
		t.Errorf("expected %d gateways but got %v", expectedGws, getGws())
	}

	t.Run("add meshnetworks", func(t *testing.T) {
		addMeshNetworksFromRegistryGateway(t, c, meshNetworks)
		expectGateways(t, 3)
	})
	t.Run("add labeled service", func(t *testing.T) {
		addLabeledServiceGateway(t, c, "nw0")
		expectGateways(t, 4)
	})
	t.Run("update labeled service network", func(t *testing.T) {
		addLabeledServiceGateway(t, c, "nw1")
		expectGateways(t, 4)
	})
	t.Run("add kubernetes gateway", func(t *testing.T) {
		addOrUpdateGatewayResource(t, c, 35443)
		expectGateways(t, 8)
	})
	t.Run("update kubernetes gateway", func(t *testing.T) {
		addOrUpdateGatewayResource(t, c, 45443)
		expectGateways(t, 8)
	})
	t.Run("remove kubernetes gateway", func(t *testing.T) {
		removeGatewayResource(t, c)
		expectGateways(t, 4)
	})
	t.Run("remove labeled service", func(t *testing.T) {
		removeLabeledServiceGateway(t, c)
		expectGateways(t, 3)
	})
	// gateways are created even with out service
	t.Run("add kubernetes gateway", func(t *testing.T) {
		addOrUpdateGatewayResource(t, c, 35443)
		expectGateways(t, 7)
	})
	t.Run("remove kubernetes gateway", func(t *testing.T) {
		removeGatewayResource(t, c)
		expectGateways(t, 3)
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
	ipType := k8sv1.IPAddressType
	hostnameType := k8sv1.HostnameAddressType
	clienttest.Wrap(t, kclient.New[*k8sv1.Gateway](c.client)).CreateOrUpdate(&k8sv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "eastwest-gwapi",
			Namespace: "istio-system",
			Labels:    map[string]string{label.TopologyNetwork.Name: "nw2"},
		},
		Spec: k8sv1.GatewaySpec{
			GatewayClassName: "istio",
			Addresses: []k8sv1.GatewaySpecAddress{
				{Type: &ipType, Value: "1.2.3.4"},
				{Type: &hostnameType, Value: "some hostname"},
			},
			Listeners: []k8sv1.Listener{
				{
					Name: "detected-by-options",
					TLS: &k8sv1.ListenerTLSConfig{
						Mode: &passthroughMode,
						Options: map[k8sv1.AnnotationKey]k8sv1.AnnotationValue{
							constants.ListenerModeOption: constants.ListenerModeAutoPassthrough,
						},
					},
					Port: k8sv1.PortNumber(customPort),
				},
				{
					Name: "detected-by-number",
					TLS:  &k8sv1.ListenerTLSConfig{Mode: &passthroughMode},
					Port: 15443,
				},
			},
		},
		Status: k8sv1.GatewayStatus{},
	})
}

func removeGatewayResource(t *testing.T, c *FakeController) {
	clienttest.Wrap(t, kclient.New[*k8sv1.Gateway](c.client)).Delete("eastwest-gwapi", "istio-system")
}

func addMeshNetworksFromRegistryGateway(t *testing.T, c *FakeController, watcher meshwatcher.TestNetworksWatcher) {
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
	clienttest.Wrap(t, c.services).Create(&corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "istio-meshnetworks-gw-2", Namespace: "istio-system"},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{Port: 15443, Protocol: corev1.ProtocolTCP}},
		},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{
			IP:    "1.2.3.5",
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
		"nw2": {
			Endpoints: []*meshconfig.Network_NetworkEndpoints{{
				Ne: &meshconfig.Network_NetworkEndpoints_FromRegistry{FromRegistry: "Kubernetes"},
			}},
			Gateways: []*meshconfig.Network_IstioNetworkGateway{{
				Port: 15443,
				Gw:   &meshconfig.Network_IstioNetworkGateway_RegistryServiceName{RegistryServiceName: "istio-meshnetworks-gw-2.istio-system.svc.cluster.local"},
			}},
		},
	}})
}

func TestAmbientSystemNamespaceNetworkChange(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbient, true)
	testNS := "test"
	systemNS := "istio-system"

	networksWatcher := meshwatcher.NewFixedNetworksWatcher(nil)
	s, fx := NewFakeControllerWithOptions(t, FakeControllerOptions{
		SystemNamespace: systemNS,
		NetworksWatcher: networksWatcher,
		ConfigCluster:   true,
	})

	tracker := assert.NewTracker[string](t)

	s.namespaces.AddEventHandler(controllers.ObjectHandler(func(o controllers.Object) {
		tracker.Record(o.GetName())
	}))

	expectNetwork := func(t *testing.T, c *FakeController, network string) {
		t.Helper()
		retry.UntilSuccessOrFail(t, func() error {
			t.Helper()
			if c.networkFromSystemNamespace().String() != network {
				return fmt.Errorf("no network system notify")
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
						return fmt.Errorf("no network workload notify")
					}
				}
				svc := addr.GetService()
				if svc != nil {
					if !svcNames.Contains(svc.Name) {
						continue
					}
					for _, saddr := range svc.GetAddresses() {
						if saddr.GetNetwork() != network {
							return fmt.Errorf("no network service notify")
						}
					}
				}
			}
			return nil
		}, retry.Timeout(time.Second*5))
	}

	pc := clienttest.NewWriter[*corev1.Pod](t, s.client)
	sc := clienttest.NewWriter[*corev1.Service](t, s.client)
	pod1 := generatePod([]string{"127.0.0.1"}, "pod1", testNS, "sa1", "node1", map[string]string{"app": "a"}, nil)
	pc.CreateOrUpdateStatus(pod1)
	fx.WaitOrFail(t, "xds")

	pod2 := generatePod([]string{"127.0.0.2"}, "pod2", testNS, "sa2", "node1", map[string]string{"app": "a"}, nil)
	pc.CreateOrUpdateStatus(pod2)
	fx.WaitOrFail(t, "xds")

	sc.CreateOrUpdate(generateService("svc1", testNS, map[string]string{}, // labels
		map[string]string{}, // annotations
		[]int32{80},
		map[string]string{"app": "a"}, // selector
		[]string{"10.0.0.1"},
	))
	fx.WaitOrFail(t, "xds")

	createOrUpdateNamespace(t, s, testNS, "")
	createOrUpdateNamespace(t, s, systemNS, "")

	tracker.WaitOrdered(testNS, systemNS)

	t.Run("change namespace network to nw1", func(t *testing.T) {
		createOrUpdateNamespace(t, s, systemNS, "nw1")
		tracker.WaitOrdered(systemNS)
		expectNetwork(t, s, "nw1")
	})

	t.Run("change namespace network to nw2", func(t *testing.T) {
		createOrUpdateNamespace(t, s, systemNS, "nw2")
		tracker.WaitOrdered(systemNS)
		expectNetwork(t, s, "nw2")
	})

	t.Run("manually change namespace network to nw3, and update meshNetworks", func(t *testing.T) {
		s.setNetworkFromNamespace(&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: systemNS,
				Labels: map[string]string{
					label.TopologyNetwork.Name: "nw3",
				},
			},
		})
		createOrUpdateNamespace(t, s, systemNS, "nw3")
		tracker.WaitOrdered(systemNS)
		addMeshNetworksFromRegistryGateway(t, s, networksWatcher)
		expectNetwork(t, s, "nw3")
	})
}

func TestAmbientSync(t *testing.T) {
	test.SetForTest(t, &features.EnableAmbient, true)
	systemNS := "istio-system"
	stop := test.NewStop(t)
	s, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{
		SystemNamespace: systemNS,
		NetworksWatcher: meshwatcher.NewFixedNetworksWatcher(nil),
		SkipRun:         true,
		CRDs:            []schema.GroupVersionResource{gvr.KubernetesGateway},
		ConfigCluster:   true,
	})
	go s.Run(stop)
	assert.EventuallyEqual(t, s.ambientIndex.HasSynced, true)

	gtw := clienttest.NewWriter[*k8sv1.Gateway](t, s.client)

	gateway := &k8sv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "remote-beta",
			Namespace: "default",
			Annotations: map[string]string{
				annotation.GatewayServiceAccount.Name: "eastwest-istio-eastwest",
			},
			Labels: map[string]string{
				label.TopologyNetwork.Name: "beta",
			},
		},
		Spec: k8sv1.GatewaySpec{
			GatewayClassName: "istio-remote",
			Addresses: []k8sv1.GatewaySpecAddress{
				{
					Type:  ptr.Of(k8sv1.IPAddressType),
					Value: "172.18.1.45",
				},
			},
			Listeners: []k8sv1.Listener{
				{
					Name:     "cross-network",
					Port:     15008,
					Protocol: k8sv1.ProtocolType("HBONE"),
					TLS: &k8sv1.ListenerTLSConfig{
						Mode: ptr.Of(k8sv1.TLSModeType("Passthrough")),
						Options: map[k8sv1.AnnotationKey]k8sv1.AnnotationValue{
							"gateway.istio.io/listener-protocol": "auto-passthrough",
						},
					},
				},
			},
		},
		Status: k8sv1.GatewayStatus{
			Addresses: []k8sv1.GatewayStatusAddress{
				{
					Type:  ptr.Of(k8sv1.IPAddressType),
					Value: "172.18.1.45",
				},
			},
		},
	}
	gtw.Create(gateway)
	assert.EventuallyEqual(t, func() int {
		return len(s.ambientIndex.All())
	}, 1)
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
