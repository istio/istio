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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sv1 "sigs.k8s.io/gateway-api/apis/v1"

	"istio.io/api/label"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
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

	// Poll the controller's reported state rather than counting notify events.
	// Counting events races with the informer queue: a SetNetworks call can fire
	// before the Service Adds are drained, producing a partial notification first
	// and the full one once the queue catches up. State polling does not care
	// about the order or how many partial notifications fire.
	expectGateways := func(t *testing.T, expectedGws int) {
		assert.EventuallyEqual(t, func() int { return len(c.NetworkGateways()) }, expectedGws,
			retry.Timeout(30*time.Second), retry.BackoffDelay(5*time.Millisecond))
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

func TestGatewayResourceHBONEListener(t *testing.T) {
	test.SetForTest(t, &features.MultiNetworkGatewayAPI, true)
	c, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{
		ClusterID:    constants.DefaultClusterName,
		DomainSuffix: "cluster.local",
		CRDs:         []schema.GroupVersionResource{gvr.KubernetesGateway},
	})

	passthroughMode := k8sv1.TLSModePassthrough
	ipType := k8sv1.IPAddressType
	clienttest.Wrap(t, kclient.New[*k8sv1.Gateway](c.client)).CreateOrUpdate(&k8sv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "eastwest-hbone",
			Namespace: "istio-system",
			Labels:    map[string]string{label.TopologyNetwork.Name: "nw2"},
		},
		Spec: k8sv1.GatewaySpec{
			GatewayClassName: "istio",
			Addresses:        []k8sv1.GatewaySpecAddress{{Type: &ipType, Value: "1.2.3.4"}},
			Listeners: []k8sv1.Listener{
				{
					Name: "mtls",
					TLS: &k8sv1.ListenerTLSConfig{
						Mode: &passthroughMode,
						Options: map[k8sv1.AnnotationKey]k8sv1.AnnotationValue{
							constants.ListenerModeOption: constants.ListenerModeAutoPassthrough,
						},
					},
					Port: 15443,
				},
				{
					Name:     "hbone",
					Protocol: "HBONE",
					Port:     15008,
				},
			},
		},
	})

	assert.EventuallyEqual(t, func() int { return len(c.NetworkGateways()) }, 2,
		retry.Timeout(30*time.Second), retry.BackoffDelay(5*time.Millisecond))

	var mtls, hbone int
	for _, gw := range c.NetworkGateways() {
		switch {
		case gw.Port == 15443 && gw.HBONEPort == 0:
			mtls++
		case gw.HBONEPort == 15008 && gw.Port == 0:
			hbone++
		default:
			t.Fatalf("unexpected gateway %+v: an HBONE listener must set HBONEPort only", gw)
		}
	}
	assert.Equal(t, mtls, 1)
	assert.Equal(t, hbone, 1)
}

// TestGatewayResourceMTLSTerminatingListener covers the sidecar-to-ambient bridge listener on an
// ambient east-west gateway: TLS terminated with ISTIO_MUTUAL on 15443. IsAutoPassthrough treats
// any TLS listener on that port as passthrough when no mode says otherwise, which would hand the
// gateway ordinary sidecar mTLS it can only fail. It must be registered as terminating instead, so
// that only bridged traffic is sent to it, while a real passthrough listener stays as it was.
func TestGatewayResourceMTLSTerminatingListener(t *testing.T) {
	test.SetForTest(t, &features.MultiNetworkGatewayAPI, true)
	c, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{
		ClusterID:    constants.DefaultClusterName,
		DomainSuffix: "cluster.local",
		CRDs:         []schema.GroupVersionResource{gvr.KubernetesGateway},
	})

	terminate := k8sv1.TLSModeTerminate
	passthrough := k8sv1.TLSModePassthrough
	ipType := k8sv1.IPAddressType
	gateways := clienttest.Wrap(t, kclient.New[*k8sv1.Gateway](c.client))
	gateways.CreateOrUpdate(&k8sv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "eastwest-ambient",
			Namespace: "istio-system",
			// Gateways are tracked by UID, which the fake client does not assign.
			UID:    "eastwest-ambient",
			Labels: map[string]string{label.TopologyNetwork.Name: "nw2"},
		},
		Spec: k8sv1.GatewaySpec{
			GatewayClassName: constants.EastWestGatewayClassName,
			Addresses:        []k8sv1.GatewaySpecAddress{{Type: &ipType, Value: "1.2.3.4"}},
			Listeners: []k8sv1.Listener{
				{Name: "mesh", Protocol: "HBONE", Port: 15008},
				{
					Name:     "mtls",
					Protocol: k8sv1.TLSProtocolType,
					Port:     15443,
					TLS: &k8sv1.ListenerTLSConfig{
						Mode:    &terminate,
						Options: map[k8sv1.AnnotationKey]k8sv1.AnnotationValue{"gateway.istio.io/tls-terminate-mode": "ISTIO_MUTUAL"},
					},
				},
			},
		},
	})
	gateways.CreateOrUpdate(&k8sv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "eastwest-sidecar",
			Namespace: "istio-system",
			UID:       "eastwest-sidecar",
			Labels:    map[string]string{label.TopologyNetwork.Name: "nw2"},
		},
		Spec: k8sv1.GatewaySpec{
			GatewayClassName: "istio",
			Addresses:        []k8sv1.GatewaySpecAddress{{Type: &ipType, Value: "5.6.7.8"}},
			Listeners: []k8sv1.Listener{{
				Name:     "tls-passthrough",
				Protocol: k8sv1.TLSProtocolType,
				Port:     15443,
				TLS: &k8sv1.ListenerTLSConfig{
					Mode:    &passthrough,
					Options: map[k8sv1.AnnotationKey]k8sv1.AnnotationValue{constants.ListenerModeOption: constants.ListenerModeAutoPassthrough},
				},
			}},
		},
	})

	assert.EventuallyEqual(t, func() int { return len(c.NetworkGateways()) }, 3,
		retry.Timeout(30*time.Second), retry.BackoffDelay(5*time.Millisecond))

	got := map[string]model.NetworkGateway{}
	for _, gw := range c.NetworkGateways() {
		got[fmt.Sprintf("%s:%d/%d", gw.Addr, gw.Port, gw.HBONEPort)] = gw
	}
	if gw, f := got["1.2.3.4:0/15008"]; !f || gw.TerminatesMTLS {
		t.Errorf("ambient gateway HBONE entry = %+v (found=%v), want HBONEPort only", gw, f)
	}
	if gw, f := got["1.2.3.4:15443/0"]; !f || !gw.TerminatesMTLS {
		t.Errorf("ambient gateway mtls entry = %+v (found=%v), want Port 15443 marked as terminating", gw, f)
	}
	if gw, f := got["5.6.7.8:15443/0"]; !f || gw.TerminatesMTLS {
		t.Errorf("sidecar gateway entry = %+v (found=%v), want an ordinary passthrough Port 15443", gw, f)
	}
}

// An ambient east-west gateway without spec.addresses is registered from its Service. The Service
// only carries ports, so whether its mTLS port terminates comes from the Gateway it was deployed for.
func TestLabeledServiceGatewayMTLSTerminatingListener(t *testing.T) {
	for _, gatewayFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("gateway first=%v", gatewayFirst), func(t *testing.T) {
			test.SetForTest(t, &features.MultiNetworkGatewayAPI, true)
			c, _ := NewFakeControllerWithOptions(t, FakeControllerOptions{
				ClusterID:    constants.DefaultClusterName,
				DomainSuffix: "cluster.local",
				CRDs:         []schema.GroupVersionResource{gvr.KubernetesGateway},
			})

			terminate := k8sv1.TLSModeTerminate
			addGateway := func() {
				clienttest.Wrap(t, kclient.New[*k8sv1.Gateway](c.client)).CreateOrUpdate(&k8sv1.Gateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "eastwest-ambient",
						Namespace: "istio-system",
						UID:       "eastwest-ambient",
						Labels:    map[string]string{label.TopologyNetwork.Name: "nw2"},
					},
					Spec: k8sv1.GatewaySpec{
						GatewayClassName: constants.EastWestGatewayClassName,
						Listeners: []k8sv1.Listener{
							{Name: "mesh", Protocol: "HBONE", Port: 15008},
							{
								Name:     "mtls",
								Protocol: k8sv1.TLSProtocolType,
								Port:     15443,
								TLS: &k8sv1.ListenerTLSConfig{
									Mode:    &terminate,
									Options: map[k8sv1.AnnotationKey]k8sv1.AnnotationValue{"gateway.istio.io/tls-terminate-mode": "ISTIO_MUTUAL"},
								},
							},
						},
					},
				})
			}
			addService := func() {
				clienttest.Wrap(t, c.services).CreateOrUpdate(&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{Name: "eastwest-ambient", Namespace: "istio-system", Labels: map[string]string{
						label.TopologyNetwork.Name:                   "nw2",
						label.IoK8sNetworkingGatewayGatewayName.Name: "eastwest-ambient",
					}},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeLoadBalancer,
						Ports: []corev1.ServicePort{
							{Name: "mesh", Port: 15008, Protocol: corev1.ProtocolTCP},
							{Name: "mtls", Port: 15443, Protocol: corev1.ProtocolTCP},
						},
					},
					Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{Ingress: []corev1.LoadBalancerIngress{{IP: "2.3.4.5"}}}},
				})
			}
			gateways := func() map[string]bool {
				got := map[string]bool{}
				for _, gw := range c.NetworkGateways() {
					got[fmt.Sprintf("%s:%d/%d", gw.Addr, gw.Port, gw.HBONEPort)] = gw.TerminatesMTLS
				}
				return got
			}
			if gatewayFirst {
				addGateway()
				addService()
			} else {
				// Let the Service register while its Gateway is unknown, as on an istiod restart
				// where the Service informer delivers first; the Gateway must then correct it.
				addService()
				assert.EventuallyEqual(t, gateways, map[string]bool{"2.3.4.5:15443/15008": false},
					retry.Timeout(30*time.Second), retry.BackoffDelay(5*time.Millisecond))
				addGateway()
			}
			// A classic passthrough gateway on the same network must stay unmarked.
			addLabeledServiceGateway(t, c, "nw2")

			want := map[string]bool{"2.3.4.5:15443/0": true, "2.3.4.5:0/15008": false, "2.3.4.6:15443/0": false}
			assert.EventuallyEqual(t, gateways, want, retry.Timeout(30*time.Second), retry.BackoffDelay(5*time.Millisecond))
		})
	}
}
